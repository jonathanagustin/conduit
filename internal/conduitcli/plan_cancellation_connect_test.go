package conduitcli

import (
	"bytes"
	"context"
	"encoding/json"
	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"google.golang.org/protobuf/proto"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/operator"
)

func executeCLICancellation(t *testing.T, server, actor, mode, format string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := NewRoot(Runtime{Stdout: &out, Stderr: io.Discard, Getenv: func(string) string { return "" }})
	args := []string{"--server", server, "--development-subject", actor, "--output", format, "plan", "cancel", "plan", "--reason", "reviewed"}
	if mode != "" {
		args = append(args, "--write-transport", mode)
	}
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestCLIPlanCancellationConnectUsesNativeAuthority(t *testing.T) {
	for _, governed := range []bool{false, true} {
		for _, actor := range []string{"alice", "author", "outsider", "root"} {
			t.Run(actor+strconv.FormatBool(governed), func(t *testing.T) {
				f := newCLIApprovalFixture(t, governed, 1, false, false)
				out, err := executeCLICancellation(t, f.server.URL, actor, "connect", "json")
				allowed := (!governed && (actor == "alice" || actor == "root")) || (governed && actor == "author")
				if (err == nil) != allowed {
					t.Fatalf("allowed=%v error=%v output=%s", allowed, err, out)
				}
				stored := storedCLIApproval(t, f)
				if allowed {
					var receipt operator.PlanCancellationReceipt
					if err := json.Unmarshal([]byte(out), &receipt); err != nil || receipt.APIVersion != "conduit.plan-cancellation/v1" || receipt.Kind != "PlanCancellationReceipt" || receipt.Plan.ID != stored.ID || receipt.Plan.Revision != stored.Revision || receipt.Cancellation.Subject != actor || receipt.Cancellation.Reason != "reviewed" || stored.Status != controlplane.PlanCancelled {
						t.Fatalf("receipt=%#v native=%#v error=%v", receipt, stored, err)
					}
				} else if stored.Status != controlplane.PlanAwaitingApproval || stored.Cancellation != nil {
					t.Fatal("refused command changed native Plan")
				}
				if strings.Contains(out, "PRIVATE") || f.other.Load() != 1 || f.rest.Load() != 0 || f.rpc.Load() != 0 || f.driver.calls.Load() != 0 {
					t.Fatalf("wrong command, credential disclosure, retry or approval handoff: %s calls=%d", out, f.other.Load())
				}
			})
		}
	}
}

func TestCLIPlanCancellationKeepsRESTDefaultAndVersionedExplicitReceipt(t *testing.T) {
	for _, mode := range []string{"", "rest"} {
		f := newCLIApprovalFixture(t, false, 1, false, false)
		out, err := executeCLICancellation(t, f.server.URL, "alice", mode, "json")
		if err != nil {
			t.Fatal(err)
		}
		var record controlplane.PlanRecord
		var shape map[string]any
		if json.Unmarshal([]byte(out), &record) != nil || json.Unmarshal([]byte(out), &shape) != nil || record.ID != "plan" || record.Plan.Environment != "dev" || record.Status != controlplane.PlanCancelled || record.Cancellation == nil || shape["apiVersion"] != nil || f.other.Load() != 1 {
			t.Fatalf("REST contract changed: %s", out)
		}
	}
	for _, format := range []string{"json", "yaml", "csv", "table"} {
		t.Run(format, func(t *testing.T) {
			f := newCLIApprovalFixture(t, false, 1, false, false)
			out, err := executeCLICancellation(t, f.server.URL, "alice", "connect", format)
			if err != nil {
				t.Fatal(err)
			}
			record := storedCLIApproval(t, f)
			if !strings.Contains(out, "conduit.plan-cancellation/v1") || !strings.Contains(out, "PlanCancellationReceipt") || !strings.Contains(out, strconv.FormatUint(record.Revision, 10)) || !strings.Contains(out, "reviewed") || strings.Contains(out, "PRIVATE") {
				t.Fatalf("versioned exact receipt changed: %s", out)
			}
		})
	}
}

func TestCLIPlanCancellationRejectsSelectionBeforeHTTPAndDoesNotReplayRefusals(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/conduit.plans.v1.PlanCancellationService/CancelPlan" {
			t.Error("wrong protocol or replay")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"code":"unavailable","message":"PRIVATE"}`))
	}))
	defer server.Close()
	for _, choice := range []struct{ mode, format string }{{"invalid", "json"}, {"connect", "invalid"}} {
		_, err := executeCLICancellation(t, server.URL, "alice", choice.mode, choice.format)
		if err == nil || calls.Load() != 0 {
			t.Fatalf("invalid selection sent HTTP: %v %d", err, calls.Load())
		}
	}
	out, err := executeCLICancellation(t, server.URL, "alice", "connect", "json")
	if err == nil || !strings.Contains(err.Error(), "outcome is unknown") || strings.Contains(err.Error(), "PRIVATE") || out != "" || calls.Load() != 1 {
		t.Fatalf("refusal replay/evidence: %s %v calls=%d", out, err, calls.Load())
	}
}

func TestCLIPlanCancellationUsesConfiguredTLSIdentity(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/conduit.plans.v1.PlanCancellationService/CancelPlan" || r.Header.Get("Authorization") != "Bearer fixture-cancellation-access" || r.Header.Get("X-Platform-Subject") != "" {
			t.Error("configured production identity lost or replaced by development identity")
		}
		plan := planReadPage().Plans[0]
		plan.Id = "plan"
		plan.Status = "CANCELLED"
		reply := &plansv1.CancelPlanResponse{Plan: plan, Cancellation: &plansv1.RecordedPlanCancellation{Subject: "fixture-user", Reason: "reviewed", CreatedAt: plan.UpdatedAt}}
		wire, err := proto.Marshal(reply)
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/proto")
		_, _ = w.Write(wire)
	}))
	defer server.Close()
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("fixture-cancellation-access\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	root := NewRoot(Runtime{Stdout: &out, Stderr: io.Discard, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"--server", server.URL, "--ca-file", certificatePEM(t, "hub", server), "--bearer-token-file", token, "plan", "cancel", "plan", "--reason", "reviewed", "--write-transport", "connect"})
	if err := root.Execute(); err != nil || calls.Load() != 1 || strings.Contains(out.String(), "fixture-cancellation-access") {
		t.Fatal("TLS identity request failed or leaked", err, calls.Load())
	}
}
func TestCLIPlanCancellationUsesConfiguredTimeout(t *testing.T) {
	canceled := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
			canceled <- true
		case <-time.After(2 * time.Second):
			canceled <- false
		}
	}))
	defer server.Close()
	var out bytes.Buffer
	root := NewRoot(Runtime{Stdout: &out, Stderr: io.Discard, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"--server", server.URL, "--development-subject", "alice", "--timeout", "100ms", "plan", "cancel", "plan", "--reason", "reviewed", "--write-transport", "connect"})
	if err := root.Execute(); err == nil {
		t.Fatal("timeout became success")
	}
	select {
	case ok := <-canceled:
		if !ok {
			t.Fatal("timeout did not cancel actual command")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("command timeout not observed")
	}
}

type restOnlyCancellationService struct {
	*fakeService
	cancellations int
}

func (service *restOnlyCancellationService) CancelPlan(ctx context.Context, id, reason string) (controlplane.PlanRecord, error) {
	service.cancellations++
	return service.fakeService.CancelPlan(ctx, id, reason)
}

func TestCLIPlanCancellationDoesNotSubstituteRESTOnlyService(t *testing.T) {
	service := &restOnlyCancellationService{fakeService: &fakeService{}}
	var out bytes.Buffer
	root := NewRoot(Runtime{Stdout: &out, Stderr: io.Discard, Getenv: func(string) string { return "" }, ServiceFactory: func(context.Context, ConnectionConfig) (Service, func(), error) { return service, func() {}, nil }})
	root.SetArgs([]string{"--server", "http://127.0.0.1:1", "--development-subject", "alice", "plan", "cancel", "plan", "--reason", "reviewed", "--write-transport", "connect"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "unavailable") || service.cancellations != 0 || out.Len() != 0 {
		t.Fatalf("REST-only substitution: %v cancellations=%d output=%s", err, service.cancellations, out.String())
	}
}

func TestCLIPlanCancellationResponseLossKeepsUnknownOutcomeWithoutReplay(t *testing.T) {
	f := newCLIApprovalFixture(t, false, 1, false, false)
	handler := f.server.Config.Handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/conduit.plans.v1.PlanCancellationService/CancelPlan" {
			recorded := httptest.NewRecorder()
			handler.ServeHTTP(recorded, r)
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	out, err := executeCLICancellation(t, server.URL, "alice", "connect", "json")
	stored := storedCLIApproval(t, f)
	if err == nil || !strings.Contains(err.Error(), "outcome is unknown") || out != "" || stored.Status != controlplane.PlanCancelled || f.other.Load() != 1 || f.driver.calls.Load() != 0 {
		t.Fatalf("lost response outcome: %v output=%s status=%s calls=%d", err, out, stored.Status, f.other.Load())
	}
}
