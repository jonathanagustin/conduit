package operator

import (
	"context"
	"errors"
	"io"
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

func approvalTransportReply() *plansv1.ApprovePlanResponse {
	at := timestamppb.New(time.Date(2026, time.November, 16, 1, 2, 3, 123456789, time.UTC))
	return &plansv1.ApprovePlanResponse{Plan: &plansv1.PlanSummary{Id: "plan", Environment: "dev", Status: "FUTURE_STATUS", StepCount: 1, ApprovalCount: 1, RequiredApprovals: 2, CreatedAt: at, UpdatedAt: at, Revision: 9007199254740993}, Approvals: []*plansv1.PlanApproval{{Subject: "alice", Rationale: " reviewed ", CreatedAt: at}}, ChangeRequestId: "cr"}
}
func TestPlanApprovalConnectRejectsCorruptEvidenceWithoutReplay(t *testing.T) {
	cases := map[string]func(*plansv1.ApprovePlanResponse){
		"empty-reviews":           func(p *plansv1.ApprovePlanResponse) { p.Plan.ApprovalCount = 0; p.Approvals = nil },
		"missing-plan":            func(p *plansv1.ApprovePlanResponse) { p.Plan = nil },
		"wrong-plan":              func(p *plansv1.ApprovePlanResponse) { p.Plan.Id = "different" },
		"missing-status":          func(p *plansv1.ApprovePlanResponse) { p.Plan.Status = "" },
		"missing-time":            func(p *plansv1.ApprovePlanResponse) { p.Plan.CreatedAt = nil },
		"invalid-time":            func(p *plansv1.ApprovePlanResponse) { p.Plan.UpdatedAt = &timestamppb.Timestamp{Nanos: 1000000000} },
		"negative-count":          func(p *plansv1.ApprovePlanResponse) { p.Plan.StepCount = -1 },
		"approval-count-mismatch": func(p *plansv1.ApprovePlanResponse) { p.Plan.ApprovalCount = 2 },
		"missing-review":          func(p *plansv1.ApprovePlanResponse) { p.Approvals[0] = nil },
		"missing-reviewer":        func(p *plansv1.ApprovePlanResponse) { p.Approvals[0].Subject = "" },
		"missing-review-time":     func(p *plansv1.ApprovePlanResponse) { p.Approvals[0].CreatedAt = nil },
		"oversized": func(p *plansv1.ApprovePlanResponse) {
			p.Approvals[0].Rationale = strings.Repeat("PRIVATE", maxOperatorResponseBytes)
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			reply := approvalTransportReply()
			change(reply)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != plansv1connect.PlanApprovalServiceApprovePlanProcedure {
					t.Error("replayed outside generated command")
				}
				wire, err := proto.Marshal(reply)
				if err != nil {
					t.Error(err)
					return
				}
				w.Header().Set("Content-Type", "application/proto")
				_, _ = w.Write(wire)
			}))
			defer server.Close()
			client, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ApprovePlanConnect(t.Context(), "plan", " reviewed ")
			var failure *PlanApprovalFailure
			if !errors.As(err, &failure) || failure.OutcomeKnown || strings.Contains(err.Error(), "PRIVATE") || calls.Load() != 1 {
				t.Fatal("bad evidence accepted or replayed", err, calls.Load())
			}
		})
	}
}
func TestPlanApprovalConnectPreservesExactReceiptAndRequest(t *testing.T) {
	reply := approvalTransportReply()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		request := &plansv1.ApprovePlanRequest{}
		if proto.Unmarshal(body, request) != nil || request.PlanId != "plan" || request.Rationale != " reviewed " {
			t.Error("command input changed")
		}
		wire, err := proto.Marshal(reply)
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/proto")
		_, _ = w.Write(wire)
	}))
	defer server.Close()
	client, err := NewHTTPControlPlane(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := client.ApprovePlanConnect(t.Context(), "plan", " reviewed ")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || receipt.Plan.Revision != reply.Plan.Revision || string(receipt.Plan.Status) != reply.Plan.Status || !receipt.Plan.CreatedAt.Equal(reply.Plan.CreatedAt.AsTime()) || len(receipt.Approvals) != 1 || receipt.Approvals[0].Rationale != " reviewed " || receipt.ChangeRequestID != "cr" {
		t.Fatal("native evidence changed", receipt)
	}
}
func TestPlanApprovalConnectNeverFollowsRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 307, 308} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls, targetCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1); w.WriteHeader(http.StatusNoContent) }))
			defer target.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				http.Redirect(w, r, target.URL+"/v1/plans/plan/approvals", status)
			}))
			defer source.Close()
			client, err := NewHTTPControlPlane(source.URL, source.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ApprovePlanConnect(t.Context(), "plan", "reviewed")
			var failure *PlanApprovalFailure
			if !errors.As(err, &failure) || failure.OutcomeKnown || calls.Load() != 1 || targetCalls.Load() != 0 {
				t.Fatal("command redirect followed", err, calls.Load(), targetCalls.Load())
			}
		})
	}
}
func TestPlanApprovalConnectOutcomeDetailsAreScopedAndDiagnosticsAreBounded(t *testing.T) {
	cases := []struct {
		name           string
		details        []*plansv1.PlanApprovalOutcome
		known, handoff bool
	}{
		{name: "absent"},
		{name: "wrong-plan", details: []*plansv1.PlanApprovalOutcome{{PlanId: "other", ApprovalRecorded: true, HandoffFailed: true}}},
		{name: "no-proof", details: []*plansv1.PlanApprovalOutcome{{PlanId: "plan"}}},
		{name: "ambiguous", details: []*plansv1.PlanApprovalOutcome{{PlanId: "plan", ApprovalRecorded: true}, {PlanId: "other", ApprovalRecorded: true}}},
		{name: "recorded-response-unavailable", details: []*plansv1.PlanApprovalOutcome{{PlanId: "plan", ApprovalRecorded: true}}, known: true},
		{name: "recorded-handoff-failed", details: []*plansv1.PlanApprovalOutcome{{PlanId: "plan", ApprovalRecorded: true, HandoffFailed: true}}, known: true, handoff: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			failure := connect.NewError(connect.CodeUnavailable, "PRIVATE-SERVER-DIAGNOSTIC")
			for _, outcome := range tc.details {
				detail, err := connectproto.NewErrorDetail(outcome)
				if err != nil {
					t.Fatal(err)
				}
				failure = failure.WithDetail(detail)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_ = connecthttp.NewErrorWriter().Write(w, r, failure)
			}))
			defer server.Close()
			client, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ApprovePlanConnect(t.Context(), "plan", "reviewed")
			var actual *PlanApprovalFailure
			if !errors.As(err, &actual) || actual.OutcomeKnown != tc.known || actual.ApprovalRecorded != tc.known || actual.HandoffFailed != tc.handoff || actual.Code != "unavailable" || strings.Contains(err.Error(), "PRIVATE") || calls.Load() != 1 {
				t.Fatal("unscoped or diagnostic result", err, calls.Load())
			}
		})
	}
}
func TestPlanApprovalConnectPreflightAndCancellation(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer server.Close()
	client, err := NewHTTPControlPlane(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range [][2]string{{"", "reviewed"}, {"plan", " "}, {"plan", strings.Repeat("x", maxOperatorResponseBytes)}} {
		if _, err := client.ApprovePlanConnect(t.Context(), input[0], input[1]); err == nil {
			t.Fatal("invalid command accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid input sent")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := client.ApprovePlanConnect(ctx, "plan", "reviewed"); result <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("command did not start")
	}
	cancel()
	select {
	case err := <-result:
		var failure *PlanApprovalFailure
		if !errors.As(err, &failure) || failure.OutcomeKnown || calls.Load() != 1 {
			t.Fatal("cancellation replayed or became known", err, calls.Load())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("command cancellation was not observed")
	}
}

func TestPlanApprovalConnectNeverFollowsCommandRedirects(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls, targets atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targets.Add(1); http.Error(w, "replayed mutation", 500) }))
			defer target.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				http.Redirect(w, r, target.URL+"/v1/plans/plan/approvals", status)
			}))
			defer source.Close()
			client, err := NewHTTPControlPlane(source.URL, source.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ApprovePlanConnect(t.Context(), "plan", "reviewed")
			var failure *PlanApprovalFailure
			if !errors.As(err, &failure) || failure.OutcomeKnown || calls.Load() != 1 || targets.Load() != 0 {
				t.Fatal("command redirect was followed or relabeled", err, calls.Load(), targets.Load())
			}
		})
	}
}
