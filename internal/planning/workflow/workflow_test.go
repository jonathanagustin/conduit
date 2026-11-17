package workflow

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	observability "github.com/jonathanagustin/conduit/internal/observability/domain"
	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

type fakeTurnPlanner struct {
	turns []*application.TurnResult
	idx   int
}

func (f *fakeTurnPlanner) Turn(_ context.Context, _ *domain.Session, _ string) (*application.TurnResult, error) {
	if f.idx >= len(f.turns) {
		return f.turns[len(f.turns)-1], nil
	}
	out := f.turns[f.idx]
	f.idx++
	return out, nil
}

type fakeAcceptAuditor struct {
	records []application.Audit
	actors  []string
	fail    error
}

func (f *fakeAcceptAuditor) RecordAccept(_ context.Context, actor string, record application.Audit) error {
	if f.fail != nil {
		return f.fail
	}
	f.actors = append(f.actors, actor)
	f.records = append(f.records, record)
	return nil
}

type callbackUpdateCallbacks struct {
	onAccept   func()
	onReject   func(error)
	onComplete func(any, error)
}

func (c callbackUpdateCallbacks) Accept() {
	if c.onAccept != nil {
		c.onAccept()
	}
}

func (c callbackUpdateCallbacks) Reject(err error) {
	if c.onReject != nil {
		c.onReject(err)
	}
}

func (c callbackUpdateCallbacks) Complete(success any, err error) {
	if c.onComplete != nil {
		c.onComplete(success, err)
	}
}

func TestSessionWorkflowLifecycleUpdatesValidatorsAndMetrics(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	recorder := NewMetricsRecorder()
	suite.SetLogger(StructuredLogger(slog.Default()))
	suite.SetMetricsHandler(recorder)
	env := suite.NewTestWorkflowEnvironment()

	planner := &fakeTurnPlanner{
		turns: []*application.TurnResult{
			{
				Messages:       []domain.Message{{Role: domain.RoleModel, Text: "Inspected repository: Node app."}},
				Inspection:     &domain.Inspection{Language: "JavaScript", Kind: "service", ToolCalls: []string{"inspect_repo", "read_file"}},
				NeedsInput:     []domain.Question{{ID: "node_ver", Name: "node_ver", Label: "Node version?", Type: domain.QuestionSelect, Options: []string{"18", "20"}, Required: true}},
				ResolvedCommit: "0123456789abcdef0123456789abcdef01234567",
			},
			{
				Messages: []domain.Message{{Role: domain.RoleModel, Text: "Drafted plan for Node 20."}},
				Plan:     &domain.Plan{Text: `{"summary":"Node 20 service","baseImage":"node:20-slim","buildSteps":["npm ci"],"ports":[8080],"networkAllow":[],"sso":{"required":false,"notes":""},"risks":[]}`},
			},
			{
				Messages:        []domain.Message{{Role: domain.RoleModel, Text: "Revised plan with SSO enabled."}},
				Plan:            &domain.Plan{Text: `{"summary":"Node 20 service with SSO","baseImage":"node:20-slim","buildSteps":["npm ci"],"ports":[8080],"networkAllow":[],"sso":{"required":true,"notes":"oidc"},"risks":[]}`},
				Model:           "claude-sonnet-4-20250514",
				ConfiguredModel: "configured-alias", ProviderModel: "provider-version",
			},
		},
	}
	auditor := &fakeAcceptAuditor{}
	acts := Activities{Planner: planner, Auditor: auditor}

	env.RegisterWorkflowWithOptions(SessionWorkflow, workflow.RegisterOptions{Name: WorkflowName})
	env.RegisterActivityWithOptions(acts.TurnActivity, activity.RegisterOptions{Name: ActTurn})
	env.RegisterActivityWithOptions(acts.AuditAcceptActivity, activity.RegisterOptions{Name: ActAuditAccept})

	// Chain each update step after the previous step completes or rejects so
	// activity execution under -race cannot advance the mock clock past later
	// dependent updates before session.NeedsInput or session.Plan is updated.
	var prePlanErr, wrongActorErr error
	var turn1Session, turn2Session, turn3Session *domain.Session

	scheduleStep7Accept := func() {
		env.RegisterDelayedCallback(func() {
			env.UpdateWorkflow(UpdateAccept, "accept-final", callbackUpdateCallbacks{
				onComplete: func(_ any, err error) {
					if err != nil {
						t.Errorf("accept failed: %v", err)
					}
				},
			}, AcceptRequest{Actor: "alice"})
		}, time.Millisecond)
	}

	scheduleStep6Turn3 := func() {
		env.RegisterDelayedCallback(func() {
			env.UpdateWorkflow(UpdateTurn, "turn-3", callbackUpdateCallbacks{
				onComplete: func(val any, err error) {
					if err != nil {
						t.Errorf("turn 3 failed: %v", err)
						return
					}
					turn3Session = val.(*domain.Session)
					scheduleStep7Accept()
				},
			}, TurnRequest{Actor: "alice", Message: "Enable SSO in the plan"})
		}, time.Millisecond)
	}

	scheduleStep5Turn2 := func() {
		env.RegisterDelayedCallback(func() {
			env.UpdateWorkflow(UpdateTurn, "turn-2", callbackUpdateCallbacks{
				onComplete: func(val any, err error) {
					if err != nil {
						t.Errorf("turn 2 failed: %v", err)
						return
					}
					turn2Session = val.(*domain.Session)
					scheduleStep6Turn3()
				},
			}, TurnRequest{Actor: "alice", Message: "Generate the initial plan"})
		}, time.Millisecond)
	}

	scheduleStep4Answer := func() {
		env.RegisterDelayedCallback(func() {
			env.UpdateWorkflow(UpdateAnswer, "answer-1", callbackUpdateCallbacks{
				onComplete: func(_ any, err error) {
					if err != nil {
						t.Errorf("answer failed: %v", err)
						return
					}
					scheduleStep5Turn2()
				},
			}, AnswerRequest{Actor: "alice", Answers: []domain.Answer{{ID: "node_ver", Value: "20"}}})
		}, time.Millisecond)
	}

	scheduleStep3WrongActor := func() {
		env.RegisterDelayedCallback(func() {
			env.UpdateWorkflow(UpdateTurn, "turn-bob", callbackUpdateCallbacks{
				onReject: func(err error) {
					wrongActorErr = err
					scheduleStep4Answer()
				},
			}, TurnRequest{Actor: "bob", Message: "intrude"})
		}, time.Millisecond)
	}

	scheduleStep2Turn1 := func() {
		env.RegisterDelayedCallback(func() {
			env.UpdateWorkflow(UpdateTurn, "turn-1", callbackUpdateCallbacks{
				onComplete: func(val any, err error) {
					if err != nil {
						t.Errorf("turn 1 failed: %v", err)
						return
					}
					turn1Session = val.(*domain.Session)
					scheduleStep3WrongActor()
				},
			}, TurnRequest{Actor: "alice", Message: "Please inspect and package"})
		}, time.Millisecond)
	}

	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateAccept, "accept-early", callbackUpdateCallbacks{
			onReject: func(err error) {
				prePlanErr = err
				scheduleStep2Turn1()
			},
		}, AcceptRequest{Actor: "alice"})
	}, time.Millisecond)

	env.ExecuteWorkflow(WorkflowName, Input{
		SessionID: "s-1",
		Actor:     "alice",
		RepoURL:   "https://github.com/org/app",
		Ref:       "main",
	})
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete after Accept")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	if !errors.Is(MapWorkflowError(prePlanErr), domain.ErrPlanRequired) {
		t.Fatalf("prePlanErr = %v, want ErrPlanRequired", prePlanErr)
	}
	if !errors.Is(MapWorkflowError(wrongActorErr), domain.ErrNotFound) {
		t.Fatalf("wrongActorErr = %v, want ErrNotFound", wrongActorErr)
	}
	if turn1Session == nil || turn1Session.Model != domain.DefaultModelLabel {
		t.Fatalf("turn1Session.Model = %+v, want %q", turn1Session, domain.DefaultModelLabel)
	}
	if turn2Session == nil || turn2Session.Status != domain.StatusReady {
		t.Fatalf("turn2Session = %+v", turn2Session)
	}
	if turn3Session == nil || len(turn3Session.Messages) != 6 {
		t.Fatalf("turn3Session messages = %+v, want all 6 messages preserved", turn3Session)
	}

	var final domain.Session
	if err := env.GetWorkflowResult(&final); err != nil {
		t.Fatal(err)
	}
	if final.Status != domain.StatusAccepted || final.Binding == nil {
		t.Fatalf("final = %+v", final)
	}
	if final.Binding.ConfiguredModel != "configured-alias" || final.Binding.ProviderModel != "provider-version" ||
		len(auditor.records) != 1 || auditor.records[0].ConfiguredModel != "configured-alias" || auditor.records[0].ProviderModel != "provider-version" {
		t.Fatalf("binding or Accept audit lost distinct model ids: %+v / %+v", final.Binding, auditor.records)
	}
	if final.Binding.Model != "claude-sonnet-4-20250514" {
		t.Fatalf("binding.Model = %q, want claude-sonnet-4-20250514", final.Binding.Model)
	}
	if len(auditor.records) != 1 || auditor.actors[0] != "alice" || auditor.records[0].PlanHash != final.Binding.Plan.SHA256 {
		t.Fatalf("audit records = %+v actors = %+v", auditor.records, auditor.actors)
	}

	// Verify observability metrics and Conduit Prometheus exposition validation.
	if got := recorder.CounterValue(MetricPlanningTurnsTotal, "pass"); got != 3 {
		t.Fatalf("turns pass counter = %d, want 3", got)
	}
	if got := recorder.CounterValue(MetricPlanningAcceptsTotal, "pass"); got != 1 {
		t.Fatalf("accepts pass counter = %d, want 1", got)
	}
	reg := observability.NewRegistry()
	recorder.Snapshot(reg)
	if err := reg.Validate(); err != nil {
		t.Fatalf("observability registry validation failed: %v", err)
	}
	rendered := reg.Render()
	if !strings.Contains(rendered, MetricPlanningTurnsTotal) || !strings.Contains(rendered, MetricPlanningAcceptsTotal) {
		t.Fatalf("rendered metrics missing expected series:\n%s", rendered)
	}
}
