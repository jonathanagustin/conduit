package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1/plansv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

type carrierKey struct{}

func planClient(t *testing.T, cfg PlanSummaryConfig) plansv1connect.PlanReadServiceClient {
	t.Helper()
	mux := http.NewServeMux()
	RegisterPlanSummaryConnect(mux, cfg)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return plansv1connect.NewPlanReadServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL)))
}

func authenticated(r *http.Request) (context.Context, error) {
	return context.WithValue(r.Context(), carrierKey{}, "robot-on-behalf-of-alice"), nil
}

func TestPlanSummaryGeneratedClientPreservesNativeValues(t *testing.T) {
	created := time.Date(2026, time.November, 16, 12, 5, 6, 123456789, time.FixedZone("offset", 3600))
	updated := created.Add(2*time.Second + 17*time.Nanosecond)
	revision := uint64(1)<<63 + 123
	client := planClient(t, PlanSummaryConfig{Authenticate: authenticated,
		Read: func(ctx context.Context, query controlplane.PlanQuery, cursor string, limit int) (controlplane.PlanSummaryPage, error) {
			if ctx.Value(carrierKey{}) != "robot-on-behalf-of-alice" {
				t.Fatal("original authenticated carrier was discarded")
			}
			if query.View != "summary" || query.Environment != "east" || query.Status != "RUNNING" || query.InstallationID != "app" || query.Search != " source " || cursor != "opaque/+=%" || limit != 17 {
				t.Fatalf("changed native query: %+v cursor=%q limit=%d", query, cursor, limit)
			}
			return controlplane.PlanSummaryPage{
				Plans: []controlplane.PlanSummary{{ID: "plan-a", Environment: "east", Status: "FUTURE_STATE", StepCount: 101, ApprovalCount: 2, RequiredApprovals: 3, CreatedAt: created, UpdatedAt: updated, Revision: revision}}, Total: 987,
				PageMetadata: controlplane.PageMetadata{PageSize: 1, Limit: 17, NextCursor: "next/+=", PreviousCursor: "previous/+=", HasPrevious: true},
			}, nil
		}})
	limit := int32(17)
	page, err := client.ListPlanSummaries(t.Context(), &plansv1.ListPlanSummariesRequest{Limit: &limit, Cursor: "opaque/+=%", Environment: " east ", Status: " RUNNING ", InstallationId: " app ", Search: " source "})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 987 || page.PageSize != 1 || page.Limit != 17 || !page.HasPrevious || page.NextCursor != "next/+=" || page.PreviousCursor != "previous/+=" || len(page.Plans) != 1 {
		t.Fatalf("page metadata changed: %v", page)
	}
	plan := page.Plans[0]
	if plan.Id != "plan-a" || plan.Environment != "east" || plan.Status != "FUTURE_STATE" || plan.StepCount != 101 || plan.ApprovalCount != 2 || plan.RequiredApprovals != 3 || plan.Revision != revision || !plan.CreatedAt.AsTime().Equal(created) || !plan.UpdatedAt.AsTime().Equal(updated) || plan.CreatedAt.Nanos != 123456789 {
		t.Fatalf("native summary changed: %v", plan)
	}
}

type observedBody struct {
	*strings.Reader
	reads int
}

func (body *observedBody) Read(p []byte) (int, error) { body.reads++; return body.Reader.Read(p) }
func (*observedBody) Close() error                    { return nil }

func TestPlanSummaryAuthenticatesBeforeBodyAndReader(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{{"anonymous", controlplane.ErrUnauthorized, http.StatusUnauthorized}, {"forbidden", controlplane.ErrForbidden, http.StatusForbidden}, {"identity backend", errors.New("PRIVATE identity diagnostic"), http.StatusInternalServerError}} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			mux := http.NewServeMux()
			RegisterPlanSummaryConnect(mux, PlanSummaryConfig{
				Authenticate: func(*http.Request) (context.Context, error) { return nil, tc.err },
				Read: func(context.Context, controlplane.PlanQuery, string, int) (controlplane.PlanSummaryPage, error) {
					reads++
					return controlplane.PlanSummaryPage{}, nil
				},
			})
			body := &observedBody{Reader: strings.NewReader(`{broken "actor":"administrator"`)}
			request := httptest.NewRequest(http.MethodPost, strings.TrimPrefix(PlanSummaryConnectRoute, "POST "), body)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != tc.status || body.reads != 0 || reads != 0 || strings.Contains(response.Body.String(), "PRIVATE") {
				t.Fatalf("status=%d bodyReads=%d readerCalls=%d body=%s", response.Code, body.reads, reads, response.Body.String())
			}
		})
	}
}

func TestPlanSummaryOptionalLimitsAndEmptyRead(t *testing.T) {
	reads := 0
	client := planClient(t, PlanSummaryConfig{Authenticate: authenticated,
		Read: func(_ context.Context, _ controlplane.PlanQuery, _ string, limit int) (controlplane.PlanSummaryPage, error) {
			reads++
			return controlplane.PlanSummaryPage{PageMetadata: controlplane.PageMetadata{Limit: limit}}, nil
		}})
	for _, invalid := range []int32{-1, 0, 201} {
		_, err := client.ListPlanSummaries(t.Context(), &plansv1.ListPlanSummariesRequest{Limit: &invalid})
		if connect.CodeOf(err) != connect.CodeInvalidArgument || reads != 0 {
			t.Fatalf("limit=%d err=%v reads=%d", invalid, err, reads)
		}
	}
	page, err := client.ListPlanSummaries(t.Context(), &plansv1.ListPlanSummariesRequest{})
	if err != nil || page.Limit != 100 || page.Total != 0 || len(page.Plans) != 0 || reads != 1 {
		t.Fatalf("native default/empty result changed: page=%v err=%v reads=%d", page, err, reads)
	}
}

func TestPlanSummaryUnavailableAndInvalidStoredEvidence(t *testing.T) {
	for _, tc := range []struct {
		name string
		page controlplane.PlanSummaryPage
		err  error
		code connect.Code
	}{
		{name: "backend", err: errors.New("PRIVATE postgres password=example"), code: connect.CodeInternal},
		{name: "cursor", err: controlplane.ErrInvalidPlanSummaryQuery, code: connect.CodeInvalidArgument},
		{name: "invalid timestamp", page: controlplane.PlanSummaryPage{Plans: []controlplane.PlanSummary{{ID: "plan", CreatedAt: time.Date(0, time.January, 1, 0, 0, 0, 0, time.UTC)}}}, code: connect.CodeInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := planClient(t, PlanSummaryConfig{Authenticate: authenticated, Read: func(context.Context, controlplane.PlanQuery, string, int) (controlplane.PlanSummaryPage, error) {
				return tc.page, tc.err
			}})
			page, err := client.ListPlanSummaries(t.Context(), &plansv1.ListPlanSummariesRequest{})
			if page != nil || connect.CodeOf(err) != tc.code || strings.Contains(err.Error(), "PRIVATE") || strings.Contains(err.Error(), "password") {
				t.Fatalf("result=%v err=%v", page, err)
			}
			if tc.code == connect.CodeInternal && !strings.Contains(err.Error(), "Plan summary index unavailable") {
				t.Fatalf("internal diagnostic was not sanitized: %v", err)
			}
		})
	}
	client := planClient(t, PlanSummaryConfig{Authenticate: authenticated})
	if _, err := client.ListPlanSummaries(t.Context(), &plansv1.ListPlanSummariesRequest{}); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("missing reader became an empty index: %v", err)
	}
}

func TestPlanSummaryPreservesDecodedNativeUnicode(t *testing.T) {
	client := planClient(t, PlanSummaryConfig{Authenticate: authenticated,
		Read: func(context.Context, controlplane.PlanQuery, string, int) (controlplane.PlanSummaryPage, error) {
			return controlplane.PlanSummaryPage{Plans: []controlplane.PlanSummary{{ID: "plan", Environment: "east", Status: "\xff\xfeFUTURE"}}}, nil
		}})
	page, err := client.ListPlanSummaries(t.Context(), &plansv1.ListPlanSummariesRequest{})
	if err != nil || len(page.Plans) != 1 || page.Plans[0].Status != "\ufffd\ufffdFUTURE" {
		t.Fatalf("decoded native Unicode changed: %v %v", page, err)
	}
}

func TestPlanSummaryOnlyMountsTheRegisteredPOSTRead(t *testing.T) {
	if PlanSummaryConnectRoute != "POST "+plansv1connect.PlanReadServiceListPlanSummariesProcedure {
		t.Fatal("route differs from the generated procedure")
	}
	authCalls := 0
	mux := http.NewServeMux()
	RegisterPlanSummaryConnect(mux, PlanSummaryConfig{Authenticate: func(r *http.Request) (context.Context, error) { authCalls++; return r.Context(), nil }})
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, plansv1connect.PlanReadServiceListPlanSummariesProcedure, http.StatusMethodNotAllowed},
		{http.MethodPost, "/conduit.plans.v1.PlanReadService/ApprovePlan", http.StatusNotFound},
		{http.MethodPost, "/conduit.plans.v1.PlanReadService/CancelPlan", http.StatusNotFound},
		{http.MethodPost, "/conduit.plans.v1.PlanReadService/GetPlan", http.StatusNotFound},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`)))
		if w.Code != tc.status || authCalls != 0 {
			t.Fatalf("%s %s status=%d auth=%d", tc.method, tc.path, w.Code, authCalls)
		}
	}
}

func TestPlanSummaryCancelsTheActualReader(t *testing.T) {
	ready, canceled := make(chan struct{}), make(chan struct{})
	client := planClient(t, PlanSummaryConfig{Authenticate: authenticated,
		Read: func(ctx context.Context, _ controlplane.PlanQuery, _ string, _ int) (controlplane.PlanSummaryPage, error) {
			close(ready)
			select {
			case <-ctx.Done():
				close(canceled)
				return controlplane.PlanSummaryPage{}, ctx.Err()
			case <-time.After(2 * time.Second):
				return controlplane.PlanSummaryPage{}, errors.New("reader context did not cancel")
			}
		}})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := client.ListPlanSummaries(ctx, &plansv1.ListPlanSummariesRequest{}); result <- err }()
	<-ready
	cancel()
	if err := <-result; connect.CodeOf(err) != connect.CodeCanceled {
		t.Fatalf("client cancellation changed: %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP reader retained a canceled request")
	}
}
