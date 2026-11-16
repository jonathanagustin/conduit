package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	packagingv1 "github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1/packagingv1connect"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"google.golang.org/protobuf/encoding/protojson"
)

const eventsProcedure = "/conduit.packaging.v1.PackagingRunService/ListPackagingRunEvents"

func callEvents(mux http.Handler, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, eventsProcedure, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func TestConnectEventsRetainNativePageValuesAndRedaction(t *testing.T) {
	at := time.Date(2026, 11, 16, 9, 12, 3, 123456789, time.FixedZone("offset", 3600))
	rows := []domain.StepEvent{
		{Sequence: 3, At: at, Step: domain.StepHandoff, Outcome: string(domain.OutcomeHandoffRecorded), SourceDigest: "source", ArtifactDigest: "artifact", ProposalRevision: "rev", ApprovalID: "approval", PlanID: "plan", TargetPlanRef: "visible/app"},
		{Sequence: 1, At: at, Step: domain.StepAssess, Outcome: string(domain.OutcomePass), Reason: "assessed"},
		{Sequence: 2, At: at, Step: domain.StepValidate, Outcome: string(domain.OutcomeFailRequired), Reason: "-----BEGIN PRIVATE KEY-----\nnot-for-readers"},
	}
	store := &catalog{runs: []domain.RunStatus{{RunID: "history", TargetPlanRef: "visible/app"}}, events: map[string][]domain.StepEvent{"history": rows}}
	before := append([]domain.StepEvent(nil), rows...)
	mux := mountBoth(Config{Authorize: authed(), Visible: allowEnvironment("visible"), Reader: store})
	values := url.Values{"view": {"summary"}, "limit": {"1"}}
	var previous string
	for i := 0; i < 3; i++ {
		native := httptest.NewRecorder()
		mux.ServeHTTP(native, httptest.NewRequest(http.MethodGet, "/v1/packaging-runs/history/events?"+values.Encode(), nil))
		if native.Code != 200 {
			t.Fatalf("REST=%d %s", native.Code, native.Body.String())
		}
		var expected eventPage
		if err := json.Unmarshal(native.Body.Bytes(), &expected); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]any{"runId": "history", "view": "summary", "limit": 1, "cursor": values.Get("cursor")})
		wire := callEvents(mux, string(body))
		if wire.Code != 200 {
			t.Fatalf("RPC=%d %s", wire.Code, wire.Body.String())
		}
		actual := &packagingv1.ListPackagingRunEventsResponse{}
		if err := protojson.Unmarshal(wire.Body.Bytes(), actual); err != nil {
			t.Fatal(err)
		}
		if actual.Tier != expected.Tier || actual.Configured != expected.Configured || actual.Authority != expected.Authority || actual.PlanAuthority != expected.PlanAuthority || actual.ApprovalAuthority != expected.ApprovalAuthority || actual.RunId != expected.RunID || actual.Total != int64(expected.Total) || actual.Total != 3 || actual.PageSize != int32(expected.PageSize) || actual.Limit != int32(expected.Limit) || actual.HasPrevious != expected.HasPrevious || actual.NextCursor != expected.NextCursor || actual.PreviousCursor != expected.PreviousCursor || len(actual.Items) != 1 {
			t.Fatalf("REST=%+v RPC=%+v", expected, actual)
		}
		event := actual.Items[0]
		want := expected.Items[0]
		if !event.At.AsTime().Equal(want.At) {
			t.Fatalf("timestamp=%s expected=%s", event.At.AsTime(), want.At)
		}
		got := domain.StepEvent{Sequence: int(event.Sequence), At: want.At, Step: event.Step, Outcome: event.Outcome, Reason: event.Reason, SourceDigest: event.SourceDigest, ArtifactDigest: event.ArtifactDigest, ProposalRevision: event.ProposalRevision, ApprovalID: event.ApprovalId, PlanID: event.PlanId, TargetPlanRef: event.TargetPlanRef}
		if !reflect.DeepEqual(got, want) || event.Sequence != int64(i+1) {
			t.Fatalf("event=%+v expected=%+v", got, want)
		}
		if strings.Contains(wire.Body.String(), "PRIVATE KEY") || strings.Contains(wire.Body.String(), "not-for-readers") {
			t.Fatal("credential leaked")
		}
		if i == 1 && event.Reason != "redacted" {
			t.Fatalf("redaction=%q", event.Reason)
		}
		previous = actual.PreviousCursor
		values.Set("cursor", actual.NextCursor)
	}
	back, _ := json.Marshal(map[string]any{"runId": "history", "view": "summary", "limit": 1, "cursor": previous})
	response := callEvents(mux, string(back))
	actual := &packagingv1.ListPackagingRunEventsResponse{}
	if err := protojson.Unmarshal(response.Body.Bytes(), actual); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || len(actual.Items) != 1 || actual.Items[0].Sequence != 2 {
		t.Fatalf("backward page=%+v status=%d", actual, response.Code)
	}
	if !reflect.DeepEqual(store.events["history"], before) {
		t.Fatal("history read changed stored journal")
	}
}

type observedHistoryReader struct {
	catalog
	reads, histories  int
	readErr, eventErr error
}

func (r *observedHistoryReader) Read(ctx context.Context, id string) (domain.RunStatus, error) {
	r.reads++
	if r.readErr != nil {
		return domain.RunStatus{}, r.readErr
	}
	return r.catalog.Read(ctx, id)
}
func (r *observedHistoryReader) Events(ctx context.Context, id string) ([]domain.StepEvent, error) {
	r.histories++
	if r.eventErr != nil {
		return nil, r.eventErr
	}
	return r.catalog.Events(ctx, id)
}

func TestConnectEventsRefuseBeforeBodyAndParentHistory(t *testing.T) {
	if ConnectEventsRoute != "POST "+packagingv1connect.PackagingRunServiceListPackagingRunEventsProcedure {
		t.Fatal("route differs from generated procedure")
	}
	for _, status := range []int{401, 403} {
		store := &observedHistoryReader{}
		body := &connectObservedBody{Reader: strings.NewReader(`{"runId":"hidden"}`)}
		request := httptest.NewRequest(http.MethodPost, eventsProcedure, body)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Connect-Protocol-Version", "1")
		response := httptest.NewRecorder()
		mountBoth(Config{Reader: store, Authorize: func(*http.Request) error { return StatusError{Status: status, Message: "refused"} }}).ServeHTTP(response, request)
		if response.Code != status || body.reads != 0 || store.reads != 0 || store.histories != 0 {
			t.Fatalf("status=%d body=%d reads=%d histories=%d", response.Code, body.reads, store.reads, store.histories)
		}
	}
	store := &observedHistoryReader{catalog: catalog{runs: []domain.RunStatus{{RunID: "hidden", TargetPlanRef: "secret/app"}}}}
	response := callEvents(mountBoth(Config{Reader: store, Authorize: authed(), Visible: allowEnvironment("visible")}), `{"runId":"hidden","actor":"administrator"}`)
	if response.Code != 404 || store.reads != 1 || store.histories != 0 {
		t.Fatalf("hidden parent=%d reads=%d histories=%d body=%s", response.Code, store.reads, store.histories, response.Body.String())
	}
}

func TestConnectEventsValidateAndRetainUnconfiguredState(t *testing.T) {
	store := &observedHistoryReader{}
	mux := mountBoth(Config{Reader: store, Authorize: authed()})
	for _, body := range []string{`{"limit":0}`, `{"limit":-1}`, `{"limit":21}`, `{"view":"wrong"}`, `{"cursor":"malformed"}`} {
		response := callEvents(mux, body)
		if response.Code != 400 || !strings.Contains(response.Body.String(), "invalid_argument") {
			t.Fatalf("input=%s status=%d body=%s", body, response.Code, response.Body.String())
		}
	}
	if store.reads != 0 || store.histories != 0 {
		t.Fatal("invalid request reached parent store")
	}
	response := callEvents(mountBoth(Config{Authorize: authed()}), `{"runId":"not-invented"}`)
	actual := &packagingv1.ListPackagingRunEventsResponse{}
	if err := protojson.Unmarshal(response.Body.Bytes(), actual); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || actual.Configured || actual.RunId != "" || actual.Total != 0 || actual.Limit != 20 || len(actual.Items) != 0 || actual.Authority != "packaging-run" || actual.PlanAuthority != "conduit-plan" || actual.ApprovalAuthority != "conduit-governance" {
		t.Fatalf("unconfigured=%+v status=%d", actual, response.Code)
	}
}

func TestConnectEventsSanitizeStoreErrorsAndMissingHistory(t *testing.T) {
	for _, tc := range []struct {
		readErr, eventErr error
		status            int
	}{
		{readErr: errors.New("PRIVATE store failure"), status: 503},
		{eventErr: errors.New("PRIVATE journal failure"), status: 503},
		{readErr: ErrNotFound, status: 404},
		{eventErr: ErrNotFound, status: 404},
	} {
		store := &observedHistoryReader{catalog: catalog{runs: []domain.RunStatus{{RunID: "history", TargetPlanRef: "visible/app"}}}, readErr: tc.readErr, eventErr: tc.eventErr}
		mux := mountBoth(Config{Reader: store, Authorize: authed(), Visible: allowEnvironment("visible")})
		native := httptest.NewRecorder()
		mux.ServeHTTP(native, httptest.NewRequest(http.MethodGet, "/v1/packaging-runs/history/events", nil))
		expectedNative := tc.status
		if expectedNative == http.StatusServiceUnavailable {
			expectedNative = http.StatusBadGateway
		}
		if native.Code != expectedNative || strings.Contains(native.Body.String(), "PRIVATE") {
			t.Fatalf("native status=%d body=%s", native.Code, native.Body.String())
		}
		response := callEvents(mux, `{"runId":"history"}`)
		if response.Code != tc.status || strings.Contains(response.Body.String(), "PRIVATE") {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
}

func TestGeneratedClientReadsRecordedEvents(t *testing.T) {
	store := &catalog{runs: []domain.RunStatus{{RunID: "history", TargetPlanRef: "visible/app"}}, events: map[string][]domain.StepEvent{"history": {{Sequence: 7, At: time.Now().UTC(), Step: domain.StepAssess}}}}
	server := httptest.NewServer(mountBoth(Config{Reader: store, Authorize: authed(), Visible: allowEnvironment("visible")}))
	defer server.Close()
	response, err := connectClient(server).ListPackagingRunEvents(t.Context(), &packagingv1.ListPackagingRunEventsRequest{RunId: "history"})
	if err != nil || response.RunId != "history" || len(response.Items) != 1 || response.Items[0].Sequence != 7 {
		t.Fatalf("generated client response=%+v err=%v", response, err)
	}
}

func TestConnectEventsRefusesUnrepresentableStoredTime(t *testing.T) {
	store := &catalog{runs: []domain.RunStatus{{RunID: "history", TargetPlanRef: "visible/app"}}, events: map[string][]domain.StepEvent{"history": {{Sequence: 1, At: time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), Step: domain.StepAssess}}}}
	response := callEvents(mountBoth(Config{Reader: store, Authorize: authed(), Visible: allowEnvironment("visible")}), `{"runId":"history"}`)
	if response.Code != 503 || !strings.Contains(response.Body.String(), "unavailable") || strings.Contains(response.Body.String(), "0000") || strings.Contains(response.Body.String(), "timestamp") {
		t.Fatalf("unrepresentable time=%d %s", response.Code, response.Body.String())
	}
}
