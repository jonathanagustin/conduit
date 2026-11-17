package composition

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	assistant "github.com/jonathanagustin/conduit/internal/assistant/application"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	packaging "github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

type assistantWorker struct {
	mu          sync.Mutex
	runs        map[string]domain.RunStatus
	cancels     int
	starts      []domain.RunIdentity
	cancelError error
	onDispatch  func()
}

func (w *assistantWorker) Configured() bool { return true }
func (w *assistantWorker) Read(_ context.Context, id string) (domain.RunStatus, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	s, ok := w.runs[id]
	if !ok {
		return s, packaging.ErrRunNotFound
	}
	return s, nil
}
func (w *assistantWorker) List(context.Context) ([]domain.RunStatus, error) { return nil, nil }
func (w *assistantWorker) Events(context.Context, string) ([]domain.StepEvent, error) {
	return nil, nil
}
func (w *assistantWorker) Cancel(_ context.Context, id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.runs[id]; !ok {
		return packaging.ErrRunNotFound
	}
	w.cancels++
	if w.onDispatch != nil {
		w.onDispatch()
	}
	return w.cancelError
}
func (w *assistantWorker) Start(_ context.Context, id domain.RunIdentity) (domain.RunStatus, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.runs[id.RunID]; ok {
		return domain.RunStatus{}, errors.New("workflow already exists")
	}
	w.starts = append(w.starts, id)
	s := domain.RunStatus{RunID: id.RunID, SourceDigest: id.SourceDigest, TargetPlanRef: id.TargetPlanRef, TemplateVersion: id.TemplateVersion, ToolVersions: id.ToolVersions, Phase: domain.PhaseAssess}
	w.runs[id.RunID] = s
	if w.onDispatch != nil {
		w.onDispatch()
	}
	return s, nil
}

type actionFixture struct {
	repository controlplane.Repository
	worker     *assistantWorker
	handler    http.Handler
	options    controlplane.HandlerOptions
}

func newActionFixture(t *testing.T, repository controlplane.Repository) *actionFixture {
	t.Helper()
	ctx := t.Context()
	g := controlplane.NewGovernanceService(repository)
	for _, team := range []controlplane.Team{
		{Name: "Operator", DisplayName: "Operator", AdminUsers: []string{"alice"}, MemberUsers: []string{"alice"}},
		{Name: "release-reviewers", DisplayName: "Reviewers", AdminUsers: []string{"bob"}, MemberUsers: []string{"bob"}},
	} {
		if _, err := g.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := g.PutGrant(ctx, controlplane.ResourceGrant{ResourceRID: "environment/env-a", Team: "Operator", Roles: []string{"environment:editor"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CreatePlan(ctx, controlplane.PlanRecord{ID: "fleet-a", Status: controlplane.PlanAwaitingApproval, CreatedAt: time.Now(), Plan: controlplane.Plan{Environment: "env-a", Steps: []controlplane.PlanStep{{Entity: "api", InstallationID: "api", Product: "acme:api", Version: "1"}}}}); err != nil {
		t.Fatal(err)
	}
	f := &actionFixture{repository: repository, worker: &assistantWorker{runs: map[string]domain.RunStatus{"run-a": {RunID: "run-a", Phase: domain.PhaseReview, SourceDigest: "sha256:source", ProposalRevision: "revision-1", TargetPlanRef: "env-a/api", TemplateVersion: "template-1", ToolVersions: "tools-1"}}}, options: controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}}
	f.restart(t)
	return f
}
func (f *actionFixture) restart(t *testing.T) {
	t.Helper()
	h, err := NewHub(f.repository, f.options, Options{PackagingStatus: f.worker, PackagingControls: f.worker})
	if err != nil {
		t.Fatal(err)
	}
	f.handler = h
}
func (f *actionFixture) propose(t *testing.T, action string) string {
	t.Helper()
	body := `{"mode":"ontology","tool":"propose_governed_action","arguments":{"actionId":"` + action + `","targetEntityId":"run-a","targetObjectType":"PackagingRun","environment":"env-a","installationId":"api","productId":"acme:api"}}`
	r := postAssistant(f.handler, "alice", "", "/v1/assistant/tools", body)
	var result assistant.Result
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil || !result.Allowed || result.ChangeRequestID == "" {
		t.Fatalf("proposal %d %s %v", r.Code, r.Body.String(), err)
	}
	if f.worker.cancels != 0 || len(f.worker.starts) != 0 {
		t.Fatal("proposal dispatched before review")
	}
	stored, err := f.repository.GetChangeRequest(t.Context(), result.ChangeRequestID)
	if err != nil || stored.Status != controlplane.ChangePendingApprovals || stored.RequirementsSatisfied {
		t.Fatalf("proposal gained approval: %+v %v", stored, err)
	}
	return result.ChangeRequestID
}
func (f *actionFixture) approve(t *testing.T, id string, viaAssistant bool) *controlplane.ChangeRequest {
	t.Helper()
	var status int
	var raw string
	if viaAssistant {
		r := postAssistant(f.handler, "bob", "Release Approver", "/v1/assistant/tools", `{"mode":"ontology","tool":"governed_write","arguments":{"action":"approve_change","targetId":"`+id+`","justification":"reviewed exact source"}}`)
		status, raw = r.Code, r.Body.String()
		var result assistant.Result
		if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil || !result.Applied || result.Execution == nil || result.Execution.Outcome != "CANCEL_REQUEST_ACCEPTED" {
			t.Fatalf("assistant review %d %s %v", status, raw, err)
		}
	} else {
		r := postAssistant(f.handler, "bob", "", "/v1/change-requests/"+id+"/approve", `{"rationale":"reviewed exact source"}`)
		status, raw = r.Code, r.Body.String()
	}
	if status != http.StatusOK {
		t.Fatalf("approve %d %s", status, raw)
	}
	stored, err := f.repository.GetChangeRequest(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return &stored
}

func TestAssistantApprovalDispatchesExactPackagingAction(t *testing.T) {
	for _, backend := range []string{"memory", "file"} {
		for _, viaAssistant := range []bool{false, true} {
			t.Run(backend+string(rune('0'+boolInt(viaAssistant))), func(t *testing.T) {
				var repository controlplane.Repository = controlplane.NewMemoryRepository()
				if backend == "file" {
					file, err := controlplane.NewFileRepository(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					repository = file
				}
				f := newActionFixture(t, repository)
				id := f.propose(t, "cancel_packaging_run")
				f.restart(t)
				f.worker.onDispatch = func() {
					stored, err := repository.GetChangeRequest(context.Background(), id)
					if err != nil || stored.Status != controlplane.ChangeApproved || stored.Execution == nil || stored.Execution.Status != controlplane.ExecutionRunning {
						t.Errorf("worker dispatched before durable approval/claim: %+v %v", stored, err)
					}
				}
				stored := f.approve(t, id, viaAssistant)
				if f.worker.cancels != 1 || stored.Execution == nil || stored.Execution.Status != controlplane.ExecutionSucceeded || stored.Execution.Items[0].CommandOutcome != "CANCEL_REQUEST_ACCEPTED" || stored.Execution.Items[0].ObservedState != domain.PhaseReview {
					t.Fatalf("approval did not execute exact command with truthful receipt: cancels=%d request=%+v", f.worker.cancels, stored.Execution)
				}
				again := f.approve(t, id, viaAssistant)
				if f.worker.cancels != 1 || again.Execution.Status != controlplane.ExecutionSucceeded {
					t.Fatal("successful approval replay dispatched again")
				}
				events, err := repository.ListAuthorizationAudit(t.Context(), "packaging-run/run-a")
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, event := range events {
					if event.Action == packaging.ControlCancel && event.Context["decisionId"] == stored.TargetID && event.Context["sourceDigest"] == "sha256:source" {
						found = true
					}
				}
				if !found {
					t.Fatal("command has no exact-bound decision audit")
				}
			})
		}
	}
}

func TestAssistantAnnouncementsAndApprovedDispatchShareGovernance(t *testing.T) {
	f := newActionFixture(t, controlplane.NewMemoryRepository())
	probe := &committedOpeningProbe{t: t, repository: f.repository}
	f.options.Announcements.ChangeRequestOpenings = probe
	f.restart(t)
	id := f.propose(t, "cancel_packaging_run")
	if len(probe.openings) != 1 || probe.openings[0].ID != id {
		t.Fatalf("assistant proposal lost its announcement: %#v", probe.openings)
	}
	reviewed := f.approve(t, id, true)
	if reviewed.Execution == nil || reviewed.Execution.Status != controlplane.ExecutionSucceeded || f.worker.cancels != 1 || len(probe.openings) != 1 {
		t.Fatalf("approval lost its exact action or repeated its opening: execution=%#v cancels=%d openings=%#v", reviewed.Execution, f.worker.cancels, probe.openings)
	}
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func TestAssistantExecutionRefusesStaleTargetsAndAuthority(t *testing.T) {
	for _, change := range []string{"source", "revision", "target", "template", "tools", "grant", "membership", "handoff", "worker-error"} {
		t.Run(change, func(t *testing.T) {
			f := newActionFixture(t, controlplane.NewMemoryRepository())
			id := f.propose(t, "cancel_packaging_run")
			run, _ := f.worker.Read(t.Context(), "run-a")
			switch change {
			case "source":
				run.SourceDigest = "sha256:new"
			case "revision":
				run.ProposalRevision = "revision-2"
			case "target":
				run.TargetPlanRef = "env-b/other"
			case "template":
				run.TemplateVersion = "template-2"
			case "tools":
				run.ToolVersions = "tools-2"
			case "handoff":
				run.PlanID = "plan-handed-off"
			case "worker-error":
				f.worker.cancelError = errors.New("worker offline")
			case "grant":
				g := controlplane.NewGovernanceService(f.repository)
				grants, _ := f.repository.ListResourceGrants(t.Context(), "environment/env-a")
				grant := grants[0]
				grant.Roles = []string{"environment:viewer"}
				if _, err := g.PutGrant(t.Context(), grant); err != nil {
					t.Fatal(err)
				}
			case "membership":
				g := controlplane.NewGovernanceService(f.repository)
				team, _ := f.repository.GetTeam(t.Context(), "Operator")
				team.AdminUsers = []string{"other"}
				team.MemberUsers = []string{"other"}
				if _, err := g.PutTeam(t.Context(), team); err != nil {
					t.Fatal(err)
				}
			}
			f.worker.runs["run-a"] = run
			response := postAssistant(f.handler, "bob", "", "/v1/change-requests/"+id+"/approve", `{"rationale":"reviewed"}`)
			stored, _ := f.repository.GetChangeRequest(t.Context(), id)
			if response.Code == http.StatusOK || stored.Execution == nil || stored.Execution.Status != controlplane.ExecutionFailed {
				t.Fatalf("stale execution not visibly refused: %d %s %+v", response.Code, response.Body.String(), stored.Execution)
			}
			want := 0
			if change == "worker-error" {
				want = 1
			}
			if f.worker.cancels != want {
				t.Fatalf("refused %s reached worker: %d", change, f.worker.cancels)
			}
		})
	}
}

func TestAssistantRefusesForeignTargetSelfApprovalAndHistoricalIntent(t *testing.T) {
	f := newActionFixture(t, controlplane.NewMemoryRepository())
	for _, bad := range []struct{ field, value string }{{"environment", "env-b"}, {"installationId", "wrong"}, {"productId", "other"}} {
		args := map[string]string{"actionId": "cancel_packaging_run", "targetEntityId": "run-a", "targetObjectType": "PackagingRun", "environment": "env-a", "installationId": "api", "productId": "acme:api"}
		args[bad.field] = bad.value
		body, _ := json.Marshal(map[string]any{"mode": "ontology", "tool": "propose_governed_action", "arguments": args})
		r := postAssistant(f.handler, "alice", "", "/v1/assistant/tools", string(body))
		var result assistant.Result
		err := json.Unmarshal(r.Body.Bytes(), &result)
		if err != nil || result.Allowed || result.ChangeRequestID != "" || result.Error == "" {
			t.Fatalf("foreign target %s accepted or failed to exercise the resolver: %s %v", bad.field, r.Body.String(), err)
		}
	}

	id := f.propose(t, "cancel_packaging_run")
	own := postAssistant(f.handler, "alice", "Release Approver", "/v1/change-requests/"+id+"/approve", `{"rationale":"own"}`)
	if own.Code == http.StatusOK || f.worker.cancels != 0 {
		t.Fatal("self approval dispatched")
	}
	// Historical records lack a binding and remain approval-only.
	g := controlplane.NewGovernanceService(f.repository)
	legacy, err := g.ProposeHumanChange(t.Context(), "PLAN", "legacy:cancel", "env-a", []controlplane.EntityChange{{ID: "legacy", Type: controlplane.ChangeTypeUpdateEntity, InstallationID: "api", Request: map[string]any{"actionId": "cancel_packaging_run", "targetEntityId": "run-a"}, ReferencedResources: map[string]controlplane.ReferencedResource{"environment": {RID: "environment/env-a", Name: "env-a"}, "installation": {RID: "installation/env-a/api", Name: "api"}, "product": {RID: "product/acme:api", Name: "acme:api"}}}}, controlplane.Principal{Username: "alice"}, "Historical intent", "old")
	if err != nil {
		t.Fatal(err)
	}
	r := postAssistant(f.handler, "bob", "", "/v1/change-requests/"+legacy.ID+"/approve", `{"rationale":"historical"}`)
	if r.Code != http.StatusOK || f.worker.cancels != 0 {
		t.Fatal("historical record gained execution", r.Code, r.Body.String())
	}
}

type failingReceiptRepository struct {
	controlplane.Repository
	failReceipt bool
}

func (r *failingReceiptRepository) UpdateChangeRequest(ctx context.Context, id string, update func(*controlplane.ChangeRequest) error) (controlplane.ChangeRequest, error) {
	if r.failReceipt {
		r.failReceipt = false
		return controlplane.ChangeRequest{}, errors.New("receipt connection lost")
	}
	return r.Repository.UpdateChangeRequest(ctx, id, update)
}
func TestAssistantLostRerunAcknowledgementRecoversOneSuccessor(t *testing.T) {
	repository := &failingReceiptRepository{Repository: controlplane.NewMemoryRepository()}
	f := newActionFixture(t, repository)
	run := f.worker.runs["run-a"]
	run.Phase = domain.PhaseComplete
	run.PlanID = "old-plan"
	f.worker.runs["run-a"] = run
	id := f.propose(t, "rerun_packaging_run")
	f.worker.onDispatch = func() { repository.failReceipt = true }
	first := postAssistant(f.handler, "bob", "", "/v1/change-requests/"+id+"/approve", `{"rationale":"reviewed"}`)
	stored, _ := repository.GetChangeRequest(t.Context(), id)
	if first.Code == http.StatusOK || stored.Execution == nil || stored.Execution.Status != controlplane.ExecutionRunning || len(f.worker.starts) != 1 {
		t.Fatalf("lost receipt state %d %+v starts=%d", first.Code, stored.Execution, len(f.worker.starts))
	}
	f.worker.onDispatch = nil
	f.restart(t)
	recovered := f.approve(t, id, false)
	if len(f.worker.starts) != 1 || recovered.Execution.Status != controlplane.ExecutionSucceeded || recovered.Execution.Items[0].CommandOutcome != "RUN_OBSERVED" {
		t.Fatalf("resume duplicated or lost successor: starts=%d execution=%+v", len(f.worker.starts), recovered.Execution)
	}
}

func TestAssistantRejectedProposalAndFeedbackUseRealServices(t *testing.T) {
	f := newActionFixture(t, controlplane.NewMemoryRepository())
	id := f.propose(t, "cancel_packaging_run")
	rejected := postAssistant(f.handler, "bob", "Security Admin", "/v1/assistant/tools", `{"mode":"ontology","tool":"governed_write","arguments":{"action":"reject_change","targetId":"`+id+`","justification":"do not stop it"}}`)
	var result assistant.Result
	if err := json.Unmarshal(rejected.Body.Bytes(), &result); err != nil || !result.Applied || result.State != controlplane.ChangeRejected || f.worker.cancels != 0 {
		t.Fatalf("rejection did not use real review service: %d %s %v", rejected.Code, rejected.Body.String(), err)
	}
	approved := postAssistant(f.handler, "bob", "", "/v1/change-requests/"+id+"/approve", `{"rationale":"too late"}`)
	if approved.Code == http.StatusOK || f.worker.cancels != 0 {
		t.Fatal("rejected request executed")
	}
	add := postAssistant(f.handler, "alice", "", "/v1/assistant/tools", `{"mode":"ontology","tool":"governed_write","arguments":{"action":"add_eval_case","targetId":"run-a","justification":"explicit operator feedback","value":"2Gi","expectedOutcome":"FAIL_REQUIRED","confirm":true}}`)
	if err := json.Unmarshal(add.Body.Bytes(), &result); err != nil || !result.Applied || result.ObjectID == "" {
		t.Fatalf("feedback did not call case service: %d %s %v", add.Code, add.Body.String(), err)
	}
	read := driveAs(t, f.handler, http.MethodGet, "/v1/intelligence/evals", "alice", "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), result.ObjectID) || !strings.Contains(read.Body.String(), "explicit operator feedback") {
		t.Fatalf("feedback effect absent from real suite: %d %s", read.Code, read.Body.String())
	}
	unsupported := postAssistant(f.handler, "alice", "", "/v1/assistant/tools", `{"mode":"ontology","tool":"governed_write","arguments":{"action":"promote"}}`)
	if unsupported.Code != http.StatusBadRequest || !strings.Contains(unsupported.Body.String(), "TOOL_INVALID") {
		t.Fatalf("unsupported action did not get transport refusal: %d %s", unsupported.Code, unsupported.Body.String())
	}
}

func TestAssistantRunningClaimBeforeDispatchCanBeResumed(t *testing.T) {
	f := newActionFixture(t, controlplane.NewMemoryRepository())
	id := f.propose(t, "cancel_packaging_run")
	// Simulate process death after durable approval/claim but before the worker
	// command or its decision audit. No fabricated success receipt is written.
	_, err := f.repository.UpdateChangeRequest(t.Context(), id, func(record *controlplane.ChangeRequest) error {
		record.Status = controlplane.ChangeApproved
		record.RequirementsSatisfied = true
		record.Reviews = []controlplane.ChangeReview{{Subject: "bob", EffectiveSubject: "bob", Decision: "APPROVE", Rationale: "reviewed", Teams: []string{"release-reviewers"}, CreatedAt: time.Now()}}
		for i := range record.Requirements {
			record.Requirements[i].SatisfiedBy = []string{"bob"}
		}
		record.Execution = &controlplane.ChangeExecution{Status: controlplane.ExecutionRunning, StartedAt: time.Now()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f.restart(t)
	stored := f.approve(t, id, false)
	if f.worker.cancels != 1 || stored.Execution.Status != controlplane.ExecutionSucceeded {
		t.Fatal("running claim could not recover", stored.Execution)
	}
}

func TestAssistantRecoveryKeepsTheReviewedDelegatedHuman(t *testing.T) {
	f := newActionFixture(t, controlplane.NewMemoryRepository())
	id := f.propose(t, "cancel_packaging_run")
	g := controlplane.NewGovernanceService(f.repository)
	dispatches := 0
	g.SetAfterChangeApproved(func(_ context.Context, request controlplane.ChangeRequest) (controlplane.ChangeRequest, error) {
		dispatches++
		return request, nil
	})
	bob := controlplane.Principal{Username: "service-carrier", ServiceAccount: true, Teams: []string{"release-reviewers"}, OnBehalfOf: &controlplane.Principal{Username: "bob", Teams: []string{"release-reviewers"}}}
	stored, _, err := g.Approve(t.Context(), id, bob, "reviewed exact target")
	if err != nil || dispatches != 1 || stored.Reviews[0].EffectiveSubject != "bob" {
		t.Fatalf("original delegated review: %+v %v calls=%d", stored.Reviews, err, dispatches)
	}
	charlie := controlplane.Principal{Username: "service-carrier", ServiceAccount: true, Teams: []string{"release-reviewers"}, OnBehalfOf: &controlplane.Principal{Username: "charlie", Teams: []string{"release-reviewers"}}}
	if _, _, err := g.Approve(t.Context(), id, charlie, "different human"); !errors.Is(err, controlplane.ErrForbidden) || dispatches != 1 {
		t.Fatalf("different represented human reused approval: %v calls=%d", err, dispatches)
	}
	if _, _, err := g.Approve(t.Context(), id, bob, "recover original"); err != nil || dispatches != 2 {
		t.Fatalf("original carrier/human could not recover: %v calls=%d", err, dispatches)
	}
}

type overlappingCancelWorker struct {
	*assistantWorker
	firstEntered chan struct{}
	releaseFirst chan struct{}
	firstError   error
	secondError  error
	calls        atomic.Int32
}

func (w *overlappingCancelWorker) Cancel(context.Context, string) error {
	if w.calls.Add(1) == 1 {
		close(w.firstEntered)
		<-w.releaseFirst
		return w.firstError
	}
	return w.secondError
}
func TestAssistantCanonicalSuccessSurvivesALateCompetingFailure(t *testing.T) {
	testAssistantCompetingReceipts(t, true)
}

func TestAssistantLateAcknowledgementReplacesCompetingFailure(t *testing.T) {
	testAssistantCompetingReceipts(t, false)
}

func testAssistantCompetingReceipts(t *testing.T, successFirst bool) {
	t.Helper()
	f := newActionFixture(t, controlplane.NewMemoryRepository())
	worker := &overlappingCancelWorker{assistantWorker: f.worker, firstEntered: make(chan struct{}), releaseFirst: make(chan struct{})}
	if successFirst {
		worker.firstError = errors.New("late competing command failure")
	} else {
		worker.secondError = errors.New("early competing command failure")
	}
	handler, err := NewHub(f.repository, f.options, Options{PackagingStatus: worker, PackagingControls: worker})
	if err != nil {
		t.Fatal(err)
	}
	f.handler = handler
	id := f.propose(t, "cancel_packaging_run")
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		first <- postAssistant(handler, "bob", "", "/v1/change-requests/"+id+"/approve", `{"rationale":"initial review"}`)
	}()
	select {
	case <-worker.firstEntered:
	case <-time.After(20 * time.Second):
		t.Fatal("first command did not enter")
	}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(worker.releaseFirst) }) })
	// The second attempt sees the durable RUNNING claim and resumes the same
	// bound idempotent command. Finish it before releasing the first attempt,
	// exercising acknowledgement-before-failure and failure-before-acknowledgement.
	competing := postAssistant(handler, "bob", "", "/v1/change-requests/"+id+"/approve", `{"rationale":"recover overlapping acknowledgement"}`)
	canonical, err := f.repository.GetChangeRequest(t.Context(), id)
	wantCode, wantStatus := http.StatusOK, controlplane.ExecutionSucceeded
	if !successFirst {
		wantCode, wantStatus = http.StatusBadGateway, controlplane.ExecutionFailed
	}
	if err != nil || competing.Code != wantCode || canonical.Execution == nil || canonical.Execution.Status != wantStatus {
		t.Fatalf("second attempt did not save outcome: %d %s %+v %v", competing.Code, competing.Body.String(), canonical.Execution, err)
	}
	saved, _ := json.Marshal(canonical.Execution)
	release.Do(func() { close(worker.releaseFirst) })
	var late *httptest.ResponseRecorder
	select {
	case late = <-first:
	case <-time.After(20 * time.Second):
		t.Fatal("competing attempt did not complete")
	}
	final, err := f.repository.GetChangeRequest(t.Context(), id)
	actual, _ := json.Marshal(final.Execution)
	if err != nil || (successFirst && string(actual) != string(saved)) || final.Execution == nil || final.Execution.Status != controlplane.ExecutionSucceeded || late.Code != http.StatusOK || !strings.Contains(late.Body.String(), `"commandOutcome":"CANCEL_REQUEST_ACCEPTED"`) {
		t.Fatalf("late attempt contradicted canonical success: status=%d response=%s saved=%s final=%s error=%v", late.Code, late.Body.String(), saved, actual, err)
	}
	if successFirst && final.Revision != canonical.Revision {
		t.Fatalf("late attempt rewrote canonical success: revision %d -> %d", canonical.Revision, final.Revision)
	}
	if worker.calls.Load() != 2 {
		t.Fatalf("barrier did not exercise two overlapping commands: %d", worker.calls.Load())
	}
}
