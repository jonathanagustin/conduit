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

func approvalClient(t *testing.T, cfg PlanApprovalConfig) plansv1connect.PlanApprovalServiceClient {
	t.Helper()
	mux := http.NewServeMux()
	RegisterPlanApprovalConnect(mux, cfg)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return plansv1connect.NewPlanApprovalServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL)))
}

func TestPlanApprovalGeneratedClientPreservesNativeResultAndCarrier(t *testing.T) {
	created := time.Date(2026, 11, 16, 12, 1, 2, 123456789, time.FixedZone("native", 3600))
	updated := created.Add(time.Second + 17)
	calls := 0
	client := approvalClient(t, PlanApprovalConfig{Authenticate: authenticated, Approve: func(ctx context.Context, id, rationale string) (controlplane.PlanRecord, error) {
		calls++
		if ctx.Value(carrierKey{}) != "robot-on-behalf-of-alice" || id != "plan/+=%" || rationale != "  reviewed  " {
			t.Fatal("command input or original carrier changed")
		}
		return controlplane.PlanRecord{ID: id, Plan: controlplane.Plan{Environment: "east", Steps: []controlplane.PlanStep{{}, {}}}, Status: controlplane.PlanApproved, RequiredApprovals: 1, ChangeRequestID: "cr", Approvals: []controlplane.Approval{{Subject: "robot", Rationale: "a\xff\xffb", CreatedAt: updated}}, Revision: 1<<63 + 9, CreatedAt: created, UpdatedAt: updated, Lease: &controlplane.PlanLease{Token: "PRIVATE-EXECUTION-CREDENTIAL"}}, nil
	}})
	response, err := client.ApprovePlan(t.Context(), &plansv1.ApprovePlanRequest{PlanId: "plan/+=%", Rationale: "  reviewed  "})
	if err != nil {
		t.Fatal(err)
	}
	plan := response.Plan
	if calls != 1 || plan.Id != "plan/+=%" || plan.Environment != "east" || plan.Status != string(controlplane.PlanApproved) || plan.StepCount != 2 || plan.ApprovalCount != 1 || plan.RequiredApprovals != 1 || plan.Revision != 1<<63+9 || !plan.CreatedAt.AsTime().Equal(created) || !plan.UpdatedAt.AsTime().Equal(updated) || response.ChangeRequestId != "cr" || len(response.Approvals) != 1 || response.Approvals[0].Subject != "robot" || response.Approvals[0].Rationale != "a\ufffd\ufffdb" || !response.Approvals[0].CreatedAt.AsTime().Equal(updated) || strings.Contains(response.String(), "PRIVATE") {
		t.Fatalf("native result changed: %v", response)
	}
}

func TestPlanApprovalGeneratedClientSanitizesTypedRefusalsWithoutRetry(t *testing.T) {
	for _, tc := range []struct {
		failure error
		code    connect.Code
	}{{controlplane.ErrUnauthorized, connect.CodeUnauthenticated}, {controlplane.ErrForbidden, connect.CodePermissionDenied}, {controlplane.ErrNotFound, connect.CodeNotFound}, {controlplane.ErrConflict, connect.CodeAlreadyExists}, {controlplane.ErrInvalidTransition, connect.CodeFailedPrecondition}, {controlplane.ErrDigestMismatch, connect.CodeInvalidArgument}, {controlplane.ValidationErrors{{Path: "field", Message: "invalid"}}, connect.CodeInvalidArgument}, {controlplane.ErrDigestMismatch, connect.CodeInvalidArgument}, {errors.New("store"), connect.CodeInternal}} {
		calls := 0
		client := approvalClient(t, PlanApprovalConfig{Authenticate: authenticated, Approve: func(context.Context, string, string) (controlplane.PlanRecord, error) {
			calls++
			return controlplane.PlanRecord{}, fmt.Errorf("%w: PRIVATE-DIAGNOSTIC", tc.failure)
		}})
		_, err := client.ApprovePlan(t.Context(), &plansv1.ApprovePlanRequest{PlanId: "plan", Rationale: "reviewed"})
		if calls != 1 || connect.CodeOf(err) != tc.code || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatalf("typed refusal: calls=%d error=%v expected=%v", calls, err, tc.code)
		}
	}
}

func TestPlanApprovalResultSerializationFailureRetainsRecordedOutcome(t *testing.T) {
	calls := 0
	client := approvalClient(t, PlanApprovalConfig{Authenticate: authenticated, Approve: func(context.Context, string, string) (controlplane.PlanRecord, error) {
		calls++
		return controlplane.PlanRecord{ID: "plan", CreatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}, nil
	}})
	_, err := client.ApprovePlan(t.Context(), &plansv1.ApprovePlanRequest{PlanId: "plan", Rationale: "reviewed"})
	var failure *connect.Error
	if calls != 1 || connect.CodeOf(err) != connect.CodeInternal || !errors.As(err, &failure) || len(failure.Details()) != 1 {
		t.Fatalf("missing recorded serialization outcome: %v", err)
	}
	message, decodeErr := connectproto.UnmarshalErrorDetail(failure.Details()[0])
	outcome, ok := message.(*plansv1.PlanApprovalOutcome)
	if decodeErr != nil || !ok || !outcome.ApprovalRecorded || outcome.HandoffFailed || outcome.PlanId != "plan" {
		t.Fatalf("wrong recorded outcome: %v %v", message, decodeErr)
	}
}

func TestPlanApprovalAuthenticatesBeforeDecodeAndLimitsCommandPayload(t *testing.T) {
	for _, tc := range []struct {
		auth   func(*http.Request) (context.Context, error)
		status int
	}{{nil, 401}, {func(*http.Request) (context.Context, error) { return nil, nil }, 401}, {func(*http.Request) (context.Context, error) { return nil, controlplane.ErrForbidden }, 403}, {func(*http.Request) (context.Context, error) { return nil, errors.New("PRIVATE-AUTH") }, 500}} {
		mux := http.NewServeMux()
		calls := 0
		RegisterPlanApprovalConnect(mux, PlanApprovalConfig{Authenticate: tc.auth, Approve: func(context.Context, string, string) (controlplane.PlanRecord, error) {
			calls++
			return controlplane.PlanRecord{}, nil
		}})
		request := httptest.NewRequest(http.MethodPost, plansv1connect.PlanApprovalServiceApprovePlanProcedure, strings.NewReader("invalid JSON"))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if calls != 0 || response.Code != tc.status || strings.Contains(response.Body.String(), "PRIVATE") {
			t.Fatalf("authentication occurred after decode/command: %d %s calls=%d", response.Code, response.Body.String(), calls)
		}
	}
	calls := 0
	client := approvalClient(t, PlanApprovalConfig{Authenticate: authenticated, Approve: func(context.Context, string, string) (controlplane.PlanRecord, error) {
		calls++
		return controlplane.PlanRecord{}, nil
	}})
	_, err := client.ApprovePlan(t.Context(), &plansv1.ApprovePlanRequest{PlanId: "plan", Rationale: strings.Repeat("x", 1<<20)})
	if connect.CodeOf(err) != connect.CodeResourceExhausted || calls != 0 {
		t.Fatalf("oversized command reached native mutation: %v calls=%d", err, calls)
	}
}
