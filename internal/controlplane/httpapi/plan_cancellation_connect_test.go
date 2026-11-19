package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1/plansv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func cancellationClient(t *testing.T, cfg PlanCancellationConfig) plansv1connect.PlanCancellationServiceClient {
	t.Helper()
	mux := http.NewServeMux()
	RegisterPlanCancellationConnect(mux, cfg)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return plansv1connect.NewPlanCancellationServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL)))
}

func TestPlanCancellationPreservesNativeIntentResultAndCarrier(t *testing.T) {
	created := time.Date(2026, 11, 19, 1, 2, 3, 123456789, time.FixedZone("native", 3600))
	calls := 0
	client := cancellationClient(t, PlanCancellationConfig{Authenticate: authenticated, Cancel: func(ctx context.Context, id string, cmd controlplane.PlanCancellationCommand) (controlplane.PlanRecord, error) {
		calls++
		if ctx.Value(carrierKey{}) != "robot-on-behalf-of-alice" || id != "plan/+=%" || cmd.Reason != "  reviewed  " || !cmd.CreateSuppression || cmd.SuppressionDurationSeconds != 90 || cmd.InstallationID != "install/1" {
			t.Fatalf("changed command: %s %#v", id, cmd)
		}
		return controlplane.PlanRecord{ID: id, Plan: controlplane.Plan{Environment: "east", Steps: []controlplane.PlanStep{{}, {}}}, Status: controlplane.PlanCancelled, RequiredApprovals: 2, Approvals: []controlplane.Approval{{}}, Revision: 1<<63 + 9, CreatedAt: created, UpdatedAt: created.Add(time.Second), ChangeRequestID: "cr", Cancellation: &controlplane.PlanCancellation{Subject: "robot", Reason: "a\xffb", CreatedAt: created}, Lease: &controlplane.PlanLease{Token: "PRIVATE-LEASE"}}, nil
	}})
	got, err := client.CancelPlan(t.Context(), &plansv1.CancelPlanRequest{PlanId: "plan/+=%", Reason: "  reviewed  ", CreateSuppression: true, SuppressionDurationSeconds: 90, InstallationId: "install/1"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || got.Plan.Id != "plan/+=%" || got.Plan.Status != "CANCELLED" || got.Plan.Environment != "east" || got.Plan.StepCount != 2 || got.Plan.RequiredApprovals != 2 || got.Plan.ApprovalCount != 1 || got.Plan.Revision != 1<<63+9 || !got.Plan.CreatedAt.AsTime().Equal(created) || !got.Plan.UpdatedAt.AsTime().Equal(created.Add(time.Second)) || got.ChangeRequestId != "cr" || got.Cancellation.Subject != "robot" || got.Cancellation.Reason != "a\ufffdb" || !got.Cancellation.CreatedAt.AsTime().Equal(created) || strings.Contains(got.String(), "PRIVATE") {
		t.Fatalf("changed result: %v", got)
	}
}

func TestPlanCancellationSanitizesFailuresWithoutInferringNoEffects(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code connect.Code
	}{
		{controlplane.ErrUnauthorized, connect.CodeUnauthenticated}, {controlplane.ErrForbidden, connect.CodePermissionDenied}, {controlplane.ErrNotFound, connect.CodeNotFound}, {controlplane.ErrConflict, connect.CodeAborted}, {controlplane.ErrInvalidTransition, connect.CodeFailedPrecondition}, {controlplane.PlanCancellationValidationError("bad duration"), connect.CodeInvalidArgument}, {controlplane.ValidationErrors{{Path: "field", Message: "bad"}}, connect.CodeInvalidArgument}, {context.Canceled, connect.CodeCanceled}, {context.DeadlineExceeded, connect.CodeDeadlineExceeded}, {errors.New("store"), connect.CodeInternal},
	} {
		t.Run(tc.code.String()+tc.err.Error(), func(t *testing.T) {
			calls := 0
			client := cancellationClient(t, PlanCancellationConfig{Authenticate: authenticated, Cancel: func(context.Context, string, controlplane.PlanCancellationCommand) (controlplane.PlanRecord, error) {
				calls++
				return controlplane.PlanRecord{}, fmt.Errorf("%w: PRIVATE", tc.err)
			}})
			_, err := client.CancelPlan(t.Context(), &plansv1.CancelPlanRequest{PlanId: "plan", Reason: "stop"})
			var failure *connect.Error
			if calls != 1 || connect.CodeOf(err) != tc.code || strings.Contains(err.Error(), "PRIVATE") || !errors.As(err, &failure) || len(failure.Details()) != 0 {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestPlanCancellationRetainsCompletedCommandWhenResultCannotMap(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []string{"Plan time", "cancellation time"} {
		t.Run(tc, func(t *testing.T) {
			calls := 0
			record := controlplane.PlanRecord{ID: "plan", Status: controlplane.PlanCancelled, CreatedAt: now, UpdatedAt: now, Cancellation: &controlplane.PlanCancellation{Subject: "operator", Reason: "stop", CreatedAt: now}}
			switch tc {
			case "Plan time":
				record.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
			case "cancellation time":
				record.Cancellation.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
			}
			client := cancellationClient(t, PlanCancellationConfig{Authenticate: authenticated, Cancel: func(context.Context, string, controlplane.PlanCancellationCommand) (controlplane.PlanRecord, error) {
				calls++
				return record, nil
			}})
			_, err := client.CancelPlan(t.Context(), &plansv1.CancelPlanRequest{PlanId: "plan", Reason: "stop"})
			var failure *connect.Error
			if calls != 1 || connect.CodeOf(err) != connect.CodeInternal || !errors.As(err, &failure) || len(failure.Details()) != 1 {
				t.Fatalf("completed result: %v calls=%d", err, calls)
			}
			msg, err := connectproto.UnmarshalErrorDetail(failure.Details()[0])
			outcome, ok := msg.(*plansv1.PlanCancellationOutcome)
			if err != nil || !ok || !outcome.CommandCompleted || outcome.PlanId != "plan" {
				t.Fatalf("outcome=%v %v", msg, err)
			}
		})
	}
}

func TestPlanCancellationAuthenticatesBeforeDecodeAndBoundsPayload(t *testing.T) {
	for _, tc := range []struct {
		auth   func(*http.Request) (context.Context, error)
		status int
	}{
		{nil, 401}, {func(*http.Request) (context.Context, error) { return nil, nil }, 401}, {func(*http.Request) (context.Context, error) { return nil, controlplane.ErrForbidden }, 403}, {func(*http.Request) (context.Context, error) { return nil, errors.New("PRIVATE") }, 500},
	} {
		mux := http.NewServeMux()
		calls := 0
		RegisterPlanCancellationConnect(mux, PlanCancellationConfig{Authenticate: tc.auth, Cancel: func(context.Context, string, controlplane.PlanCancellationCommand) (controlplane.PlanRecord, error) {
			calls++
			return controlplane.PlanRecord{}, nil
		}})
		r := httptest.NewRequest(http.MethodPost, strings.TrimPrefix(PlanCancellationConnectRoute, "POST "), strings.NewReader("{"))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.status || calls != 0 || strings.Contains(w.Body.String(), "PRIVATE") {
			t.Fatalf("auth=%d calls=%d body=%s", w.Code, calls, w.Body.String())
		}
	}
	calls := 0
	client := cancellationClient(t, PlanCancellationConfig{Authenticate: authenticated, Cancel: func(context.Context, string, controlplane.PlanCancellationCommand) (controlplane.PlanRecord, error) {
		calls++
		return controlplane.PlanRecord{}, nil
	}})
	for _, req := range []*plansv1.CancelPlanRequest{{Reason: "stop"}, {PlanId: "plan", Reason: strings.Repeat("x", 1<<20)}} {
		_, err := client.CancelPlan(t.Context(), req)
		if err == nil {
			t.Fatal("invalid command admitted")
		}
	}
	if calls != 0 {
		t.Fatalf("invalid command calls=%d", calls)
	}
}

func TestPlanCancellationRejectsUnboundNativeResultWithoutOutcomeClaim(t *testing.T) {
	for _, tc := range []string{"wrong Plan", "missing cancellation", "wrong status"} {
		t.Run(tc, func(t *testing.T) {
			now := time.Now().UTC()
			record := controlplane.PlanRecord{ID: "plan", Status: controlplane.PlanCancelled, CreatedAt: now, UpdatedAt: now, Cancellation: &controlplane.PlanCancellation{CreatedAt: now}}
			switch tc {
			case "wrong Plan":
				record.ID = "PRIVATE-OTHER-PLAN"
			case "missing cancellation":
				record.Cancellation = nil
			case "wrong status":
				record.Status = controlplane.PlanApproved
			}
			client := cancellationClient(t, PlanCancellationConfig{Authenticate: authenticated, Cancel: func(context.Context, string, controlplane.PlanCancellationCommand) (controlplane.PlanRecord, error) {
				return record, nil
			}})
			got, err := client.CancelPlan(t.Context(), &plansv1.CancelPlanRequest{PlanId: "plan", Reason: "stop"})
			var failure *connect.Error
			if got != nil || connect.CodeOf(err) != connect.CodeInternal || !errors.As(err, &failure) || len(failure.Details()) != 0 || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("invalid result=%v %v", got, err)
			}
		})
	}
}

func TestPlanCancellationForwardsClientCancellationToNativeCommand(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	client := cancellationClient(t, PlanCancellationConfig{Authenticate: authenticated, Cancel: func(ctx context.Context, _ string, _ controlplane.PlanCancellationCommand) (controlplane.PlanRecord, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return controlplane.PlanRecord{}, ctx.Err()
	}})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := client.CancelPlan(ctx, &plansv1.CancelPlanRequest{PlanId: "plan", Reason: "stop"})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("native command never started")
	}
	cancel()
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("native command did not receive cancellation")
	}
	select {
	case err := <-done:
		if connect.CodeOf(err) != connect.CodeCanceled {
			t.Fatalf("client cancellation=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client cancellation stalled")
	}
}
