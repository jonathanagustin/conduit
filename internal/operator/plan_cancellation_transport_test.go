package operator

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1/plansv1connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func cancellationTransportReply() *plansv1.CancelPlanResponse {
	at := timestamppb.New(time.Date(2026, 11, 19, 1, 2, 3, 123456789, time.UTC))
	return &plansv1.CancelPlanResponse{Plan: &plansv1.PlanSummary{Id: "plan", Environment: "dev", Status: "CANCELLED", StepCount: 1, ApprovalCount: 1, RequiredApprovals: 2, CreatedAt: proto.Clone(at).(*timestamppb.Timestamp), UpdatedAt: proto.Clone(at).(*timestamppb.Timestamp), Revision: 9007199254740993}, Cancellation: &plansv1.RecordedPlanCancellation{Subject: "alice", Reason: "reviewed", CreatedAt: proto.Clone(at).(*timestamppb.Timestamp)}, ChangeRequestId: "cr"}
}

func TestPlanCancellationClientBindsReceiptAndRefusesCorruptEvidence(t *testing.T) {
	cases := map[string]func(*plansv1.CancelPlanResponse){
		"valid":                     func(*plansv1.CancelPlanResponse) {},
		"missing Plan":              func(r *plansv1.CancelPlanResponse) { r.Plan = nil },
		"wrong Plan":                func(r *plansv1.CancelPlanResponse) { r.Plan.Id = "other" },
		"wrong status":              func(r *plansv1.CancelPlanResponse) { r.Plan.Status = "APPROVED" },
		"missing Plan time":         func(r *plansv1.CancelPlanResponse) { r.Plan.CreatedAt = nil },
		"invalid Plan time":         func(r *plansv1.CancelPlanResponse) { r.Plan.UpdatedAt.Nanos = 1000000000 },
		"negative count":            func(r *plansv1.CancelPlanResponse) { r.Plan.ApprovalCount = -1 },
		"missing cancellation":      func(r *plansv1.CancelPlanResponse) { r.Cancellation = nil },
		"wrong reason":              func(r *plansv1.CancelPlanResponse) { r.Cancellation.Reason = "another operation" },
		"missing subject":           func(r *plansv1.CancelPlanResponse) { r.Cancellation.Subject = "" },
		"invalid cancellation time": func(r *plansv1.CancelPlanResponse) { r.Cancellation.CreatedAt.Nanos = 1000000000 },
		"missing cancellation time": func(r *plansv1.CancelPlanResponse) { r.Cancellation.CreatedAt = nil },
		"oversized": func(r *plansv1.CancelPlanResponse) {
			r.ChangeRequestId = strings.Repeat("PRIVATE", maxOperatorResponseBytes)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			reply := cancellationTransportReply()
			mutate(reply)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "POST" || r.URL.Path != plansv1connect.PlanCancellationServiceCancelPlanProcedure {
					t.Error("wrong procedure or replay")
				}
				data, err := proto.Marshal(reply)
				if err != nil {
					t.Error(err)
					return
				}
				w.Header().Set("Content-Type", "application/proto")
				_, _ = w.Write(data)
			}))
			defer server.Close()
			client, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			got, err := client.CancelPlanConnect(t.Context(), "plan", " reviewed ")
			if calls.Load() != 1 {
				t.Fatalf("calls=%d", calls.Load())
			}
			if name == "valid" {
				if err != nil || got.APIVersion != "conduit.plan-cancellation/v1" || got.Kind != "PlanCancellationReceipt" || got.Plan.Revision != 9007199254740993 || got.Plan.ApprovalCount != 1 || got.Cancellation.Subject != "alice" || got.Cancellation.Reason != "reviewed" || !got.Cancellation.CreatedAt.Equal(reply.Cancellation.CreatedAt.AsTime()) || got.ChangeRequestID != "cr" {
					t.Fatalf("receipt=%#v error=%v", got, err)
				}
			} else {
				var failure *PlanCancellationFailure
				if !errors.As(err, &failure) || failure.OutcomeKnown || strings.Contains(err.Error(), "PRIVATE") {
					t.Fatalf("corrupt evidence accepted: %#v %v", got, err)
				}
			}
		})
	}
}

func TestPlanCancellationClientBindsCompletedOutcome(t *testing.T) {
	for _, tc := range []struct {
		name, id                    string
		completed, duplicate, known bool
	}{{"valid", "plan", true, false, true}, {"wrong Plan", "other", true, false, false}, {"not completed", "plan", false, false, false}, {"duplicates", "plan", true, true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				detail, err := connectproto.NewErrorDetail(&plansv1.PlanCancellationOutcome{PlanId: tc.id, CommandCompleted: tc.completed})
				if err != nil {
					t.Error(err)
					return
				}
				failure := connect.NewError(connect.CodeInternal, "PRIVATE").WithDetail(detail)
				if tc.duplicate {
					failure = failure.WithDetail(detail)
				}
				_ = connecthttp.NewErrorWriter().Write(w, r, failure)
			}))
			defer server.Close()
			client, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.CancelPlanConnect(t.Context(), "plan", "reviewed")
			var failure *PlanCancellationFailure
			if !errors.As(err, &failure) || failure.OutcomeKnown != tc.known || failure.CommandCompleted != tc.known || strings.Contains(err.Error(), "PRIVATE") || calls.Load() != 1 {
				t.Fatalf("outcome=%#v error=%v calls=%d", failure, err, calls.Load())
			}
		})
	}
}

func TestPlanCancellationClientRefusesRedirectsAndUnknownFailures(t *testing.T) {
	for _, status := range []int{301, 302, 307, 308, 401, 403, 404, 409, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls, targetCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls.Add(1) }))
			defer target.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", target.URL)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"code":"internal","message":"PRIVATE"}`))
			}))
			defer server.Close()
			client, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.CancelPlanConnect(t.Context(), "plan", "reviewed")
			var failure *PlanCancellationFailure
			if !errors.As(err, &failure) || failure.OutcomeKnown || calls.Load() != 1 || targetCalls.Load() != 0 || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("failure=%#v error=%v calls=%d target=%d", failure, err, calls.Load(), targetCalls.Load())
			}
		})
	}
}
