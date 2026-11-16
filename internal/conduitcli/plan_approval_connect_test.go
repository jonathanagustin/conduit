package conduitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"github.com/jonathanagustin/conduit/internal/operator"
)

const cliApprovalProcedure = "/conduit.plans.v1.PlanApprovalService/ApprovePlan"

type cliApprovalDriver struct {
	calls atomic.Int32
	fail  bool
}

func (*cliApprovalDriver) Driver() string { return controlplane.PlanOrchestratorTemporal }
func (driver *cliApprovalDriver) Begin(_ context.Context, id string) (controlplane.PlanExecutionHandoff, error) {
	driver.calls.Add(1)
	if driver.fail {
		return controlplane.PlanExecutionHandoff{}, errors.New("PRIVATE-SEQUENCER-DIAGNOSTIC")
	}
	return controlplane.PlanExecutionHandoff{Driver: controlplane.PlanOrchestratorTemporal, PlanID: id, WorkflowID: "plan-execute-" + id}, nil
}

type cliApprovalFixture struct {
	repository       *controlplane.MemoryRepository
	server           *httptest.Server
	driver           *cliApprovalDriver
	rpc, rest, other atomic.Int32
}

func newCLIApprovalFixture(t *testing.T, governed bool, required int, fail, drop bool) *cliApprovalFixture {
	t.Helper()
	f := &cliApprovalFixture{repository: controlplane.NewMemoryRepository(), driver: &cliApprovalDriver{fail: fail}}
	governance := controlplane.NewGovernanceService(f.repository)
	for _, team := range []controlplane.Team{
		{Name: "operators", DisplayName: "Operators", AdminUsers: []string{"owner"}, MemberUsers: []string{"alice"}},
		{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"owner"}, MemberUsers: []string{"bob", "author"}},
	} {
		if _, err := governance.PutTeam(t.Context(), team); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: "environment/dev", Team: "operators", Operations: []string{controlplane.OperationOperate}}); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, time.November, 16, 1, 2, 3, 123456789, time.UTC)
	record := controlplane.PlanRecord{ID: "plan", DesiredStateDigest: "sha256:desired", Plan: controlplane.Plan{Environment: "dev", Generation: "one", Steps: []controlplane.PlanStep{{InstallationID: "app", Product: "team:app"}}}, Status: controlplane.PlanAwaitingApproval, RequiredApprovals: required, Revision: 9007199254740993, CreatedAt: at, UpdatedAt: at, Lease: &controlplane.PlanLease{Token: "PRIVATE-EXECUTION-TOKEN"}}
	if governed {
		record.ChangeRequestID = "cr"
		if _, err := f.repository.CreateChangeRequest(t.Context(), controlplane.ChangeRequest{ID: "cr", PlanID: "plan", TargetKind: "PLAN", TargetID: "plan", Environment: "dev", Title: "Review", Status: controlplane.ChangePendingApprovals, Author: controlplane.Principal{Username: "author"}, Requirements: []controlplane.ReviewerRequirement{{ID: "independent-review", Kind: controlplane.RequirementTeam, Team: "reviewers", Reason: "independent review"}}, Revision: 1, CreatedAt: at, UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.repository.CreatePlan(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	handler, err := composition.NewHub(f.repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}}, composition.Options{PlanOrchestrator: f.driver})
	if err != nil {
		t.Fatal(err)
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == cliApprovalProcedure {
			f.rpc.Add(1)
		} else if r.URL.Path == "/v1/plans/plan/approvals" {
			f.rest.Add(1)
		} else {
			f.other.Add(1)
		}
		if drop && r.URL.Path == cliApprovalProcedure {
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
	t.Cleanup(f.server.Close)
	return f
}
func executeCLIApproval(t *testing.T, server, actor, mode, format string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := NewRoot(Runtime{Stdout: &out, Stderr: io.Discard, Getenv: func(string) string { return "" }})
	args := []string{"--server", server, "--development-subject", actor, "--output", format, "plan", "approve", "plan", "--rationale", "reviewed target and digest"}
	if mode != "" {
		args = append(args, "--write-transport", mode)
	}
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}
func storedCLIApproval(t *testing.T, f *cliApprovalFixture) controlplane.PlanRecord {
	t.Helper()
	p, err := f.repository.GetPlan(t.Context(), "plan")
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestCLIPlanApprovalConnectUsesNativeAuthorityStateAndAudit(t *testing.T) {
	for _, governed := range []bool{false, true} {
		for _, actor := range []string{"alice", "bob", "author", "outsider", "root"} {
			t.Run(actor+map[bool]string{true: "-governed", false: "-crless"}[governed], func(t *testing.T) {
				f := newCLIApprovalFixture(t, governed, 1, false, false)
				before := storedCLIApproval(t, f)
				output, err := executeCLIApproval(t, f.server.URL, actor, "connect", "json")
				allowed := (!governed && (actor == "root" || actor == "alice")) || (governed && actor == "bob")
				after := storedCLIApproval(t, f)
				if (err == nil) != allowed {
					t.Fatalf("allowed=%v err=%v output=%s", allowed, err, output)
				}
				if strings.Contains(output, "PRIVATE") {
					t.Fatal("execution credential leaked")
				}
				if f.rpc.Load() != 1 || f.rest.Load() != 0 || f.other.Load() != 0 {
					t.Fatal("wrong command or replay", f.rpc.Load(), f.rest.Load())
				}
				if allowed {
					var receipt operator.PlanApprovalReceipt
					if err := json.Unmarshal([]byte(output), &receipt); err != nil {
						t.Fatal(err)
					}
					if receipt.APIVersion != "conduit.plan-approval/v1" || receipt.Kind != "PlanApprovalReceipt" || receipt.Plan.ID != after.ID || receipt.Plan.Revision != after.Revision || receipt.Plan.Status != after.Status || receipt.Plan.ApprovalCount != len(after.Approvals) || len(receipt.Approvals) != 1 || receipt.Approvals[0].Subject != actor || receipt.Approvals[0].Rationale != "reviewed target and digest" || !receipt.Approvals[0].CreatedAt.Equal(after.Approvals[0].CreatedAt) || !receipt.Plan.UpdatedAt.Equal(after.UpdatedAt) || receipt.ChangeRequestID != after.ChangeRequestID {
						t.Fatalf("receipt does not match persisted native evidence: %+v", receipt)
					}
					if f.driver.calls.Load() != 1 {
						t.Fatal("missing native sequencing")
					}
				} else {
					if len(after.Approvals) != 0 || after.Revision != before.Revision || after.Status != before.Status || f.driver.calls.Load() != 0 {
						t.Fatal("refused command changed native state")
					}
					var failure *operator.PlanApprovalFailure
					if !errors.As(err, &failure) || failure.OutcomeKnown {
						t.Fatal("missing detail became a known outcome", err)
					}
				}
				audits, err := f.repository.ListAuthorizationAudit(t.Context(), "")
				if err != nil {
					t.Fatal(err)
				}
				decision := "DENY"
				if allowed {
					decision = "ALLOW"
				}
				found := false
				for _, event := range audits {
					if event.Subject == actor && event.Decision == decision && (event.Action == controlplane.OperationOperate || event.Action == "CHANGE_REQUEST_APPROVE") {
						found = true
					}
				}
				if !found {
					t.Fatalf("native authorization audit missing for %s %s: %+v", actor, decision, audits)
				}
			})
		}
	}
}
func TestCLIPlanApprovalConnectPartialDuplicateAndFailedHandoff(t *testing.T) {
	t.Run("partial-and-duplicate", func(t *testing.T) {
		f := newCLIApprovalFixture(t, false, 2, false, false)
		if out, err := executeCLIApproval(t, f.server.URL, "alice", "connect", "json"); err != nil {
			t.Fatal(err, out)
		}
		first := storedCLIApproval(t, f)
		if len(first.Approvals) != 1 || first.Status != controlplane.PlanAwaitingApproval || f.driver.calls.Load() != 0 {
			t.Fatal("partial approval sequenced a Plan")
		}
		if _, err := executeCLIApproval(t, f.server.URL, "alice", "connect", "json"); err == nil {
			t.Fatal("duplicate approval accepted")
		}
		after := storedCLIApproval(t, f)
		if after.Revision != first.Revision || len(after.Approvals) != 1 || f.rpc.Load() != 2 || f.rest.Load() != 0 || f.driver.calls.Load() != 0 {
			t.Fatal("duplicate was replayed or mutated")
		}
	})
	t.Run("recorded-before-handoff-failure", func(t *testing.T) {
		f := newCLIApprovalFixture(t, false, 1, true, false)
		out, err := executeCLIApproval(t, f.server.URL, "alice", "connect", "json")
		var failure *operator.PlanApprovalFailure
		if !errors.As(err, &failure) || !failure.OutcomeKnown || !failure.ApprovalRecorded || !failure.HandoffFailed || !strings.Contains(err.Error(), "approval was recorded") || strings.Contains(err.Error(), "PRIVATE") || out != "" {
			t.Fatal("recorded approval outcome lost", err, out)
		}
		if len(storedCLIApproval(t, f).Approvals) != 1 || f.rpc.Load() != 1 || f.rest.Load() != 0 || f.driver.calls.Load() != 1 {
			t.Fatal("handoff failure caused replay")
		}
	})
	t.Run("lost-reply-is-unknown", func(t *testing.T) {
		f := newCLIApprovalFixture(t, false, 1, false, true)
		out, err := executeCLIApproval(t, f.server.URL, "alice", "connect", "json")
		var failure *operator.PlanApprovalFailure
		if !errors.As(err, &failure) || failure.OutcomeKnown || !strings.Contains(err.Error(), "outcome is unknown") || out != "" {
			t.Fatal("lost response claimed an outcome", err, out)
		}
		if len(storedCLIApproval(t, f).Approvals) != 1 || f.rpc.Load() != 1 || f.rest.Load() != 0 || f.driver.calls.Load() != 1 {
			t.Fatal("lost response caused replay")
		}
	})
}

func TestCLIPlanApprovalKeepsRESTDefaultAndExplicitRollback(t *testing.T) {
	for _, mode := range []string{"", "rest"} {
		t.Run("mode-"+mode, func(t *testing.T) {
			f := newCLIApprovalFixture(t, false, 1, false, false)
			out, err := executeCLIApproval(t, f.server.URL, "alice", mode, "json")
			if err != nil {
				t.Fatal(err)
			}
			var record controlplane.PlanRecord
			if err := json.Unmarshal([]byte(out), &record); err != nil {
				t.Fatal(err)
			}
			var shape map[string]any
			if err := json.Unmarshal([]byte(out), &shape); err != nil {
				t.Fatal(err)
			}
			if record.ID != "plan" || record.Plan.Environment != "dev" || len(record.Approvals) != 1 || shape["apiVersion"] != nil || f.rest.Load() != 1 || f.rpc.Load() != 0 {
				t.Fatal("REST full-document contract changed")
			}
		})
	}
}
func TestCLIPlanApprovalReceiptFormatsPreserveNativeRevision(t *testing.T) {
	for _, format := range []string{"json", "yaml", "csv", "table"} {
		t.Run(format, func(t *testing.T) {
			f := newCLIApprovalFixture(t, false, 1, false, false)
			out, err := executeCLIApproval(t, f.server.URL, "alice", "connect", format)
			if err != nil {
				t.Fatal(err)
			}
			record := storedCLIApproval(t, f)
			if !strings.Contains(out, strconv.FormatUint(record.Revision, 10)) || !strings.Contains(out, "conduit.plan-approval/v1") || !strings.Contains(out, "PlanApprovalReceipt") || strings.Contains(out, "PRIVATE") {
				t.Fatal("receipt format lost exact versioned evidence", out)
			}
		})
	}
}
func TestCLIPlanApprovalRejectsSelectionAndOutputBeforeSending(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); http.Error(w, "unexpected mutation", 500) }))
	defer server.Close()
	for _, input := range [][2]string{{"unknown", "json"}, {"connect", "pretty"}, {"rest", "pretty"}, {"", "pretty"}} {
		if _, err := executeCLIApproval(t, server.URL, "alice", input[0], input[1]); err == nil {
			t.Fatal("invalid selection accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid selection sent a command")
	}
	var out bytes.Buffer
	root := NewRoot(Runtime{Stdout: &out, Stderr: io.Discard, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"--server", server.URL, "--development-subject", "alice", "plan", "approve", "plan", "--write-transport", "connect"})
	if err := root.Execute(); err == nil || calls.Load() != 0 {
		t.Fatal("missing rationale sent a command", err)
	}
}
func TestCLIPlanApprovalUsesConfiguredTLSIdentity(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != cliApprovalProcedure || r.Header.Get("Authorization") != "Bearer fixture-approval-access" || r.Header.Get("X-Platform-Subject") != "" {
			t.Error("configured production identity lost or replaced by development identity")
		}
		plan := planReadPage().Plans[0]
		plan.Id = "plan"
		plan.ApprovalCount = 1
		reply := &plansv1.ApprovePlanResponse{Plan: plan, Approvals: []*plansv1.PlanApproval{{Subject: "fixture-user", Rationale: "reviewed", CreatedAt: plan.UpdatedAt}}}
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
	if err := os.WriteFile(token, []byte("fixture-approval-access\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	root := NewRoot(Runtime{Stdout: &out, Stderr: io.Discard, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"--server", server.URL, "--ca-file", certificatePEM(t, "hub", server), "--bearer-token-file", token, "plan", "approve", "plan", "--rationale", "reviewed", "--write-transport", "connect"})
	if err := root.Execute(); err != nil || calls.Load() != 1 || strings.Contains(out.String(), "fixture-approval-access") {
		t.Fatal("TLS identity request failed or leaked", err, calls.Load())
	}
}
func TestCLIPlanApprovalUsesConfiguredTimeout(t *testing.T) {
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
	root.SetArgs([]string{"--server", server.URL, "--development-subject", "alice", "--timeout", "100ms", "plan", "approve", "plan", "--rationale", "reviewed", "--write-transport", "connect"})
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

func TestCLIPlanApprovalDoesNotUseRESTOnlyInjectedService(t *testing.T) {
	service := &fakeService{}
	var out bytes.Buffer
	root := NewRoot(Runtime{Stdout: &out, Stderr: io.Discard, Getenv: func(string) string { return "" }, ServiceFactory: func(context.Context, ConnectionConfig) (Service, func(), error) { return service, func() {}, nil }})
	root.SetArgs([]string{"--server", "http://127.0.0.1:1", "--development-subject", "alice", "plan", "approve", "plan", "--rationale", "reviewed", "--write-transport", "connect"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "unavailable") || service.approvedPlan != "" || out.Len() != 0 {
		t.Fatal("REST-only service silently substituted for Connect", err, service.approvedPlan, out.String())
	}
}
