package workflow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	tw "go.temporal.io/sdk/workflow"

	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

func requireTurnError(t *testing.T, err error, kind string) {
	t.Helper()
	var app *temporal.ApplicationError
	if !errors.As(err, &app) || app.Type() != kind {
		t.Errorf("want %s, got %v", kind, err)
	}
}

func TestCancelBeforeTurnAndRetainedRunIDs(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(SessionWorkflow, tw.RegisterOptions{Name: WorkflowName})
	calls := 0
	env.RegisterActivityWithOptions(func(context.Context, TurnActivityInput) (*application.TurnResult, error) {
		calls++
		return &application.TurnResult{Messages: []domain.Message{{Role: domain.RoleModel, Text: "new result"}}}, nil
	}, activity.RegisterOptions{Name: ActTurn})
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateCancelTurn, "cancel-before-start", callbackUpdateCallbacks{onComplete: func(value any, err error) {
			if err != nil {
				t.Error(err)
				return
			}
			receipt := value.(*CancelTurnReceipt)
			if receipt.State != "cancelled" || receipt.RunID != "old" || receipt.ValueTier != "CONDUIT_NATIVE" {
				t.Errorf("receipt: %#v", receipt)
			}
			env.UpdateWorkflow(UpdateTurn, "cancelled-turn", callbackUpdateCallbacks{onComplete: func(_ any, err error) {
				requireTurnError(t, err, ErrTypeRunCancelled)
				if calls != 0 {
					t.Error("cancelled-before-start turn invoked activity")
				}
				env.UpdateWorkflow(UpdateTurn, "new-turn", callbackUpdateCallbacks{onComplete: func(value any, err error) {
					if err != nil {
						t.Error(err)
						return
					}
					got := value.(*domain.Session)
					if calls != 1 || len(got.Messages) != 2 || got.Messages[0].Text != "new request" {
						t.Errorf("new turn: %#v calls=%d", got, calls)
					}
					env.UpdateWorkflow(UpdateTurn, "reused-run-id", callbackUpdateCallbacks{onComplete: func(_ any, err error) {
						requireTurnError(t, err, ErrTypeRunConflict)
						if calls != 1 {
							t.Error("reused run id executed again")
						}
						env.UpdateWorkflow(UpdateCancelTurn, "cancel-completed", callbackUpdateCallbacks{onComplete: func(value any, err error) {
							if err != nil || value.(*CancelTurnReceipt).State != "completed" {
								t.Errorf("completed receipt: %#v %v", value, err)
							}
							env.CancelWorkflow()
						}}, CancelTurnRequest{Actor: "alice", RunID: "new"})
					}}, TurnRequest{Actor: "alice", RunID: "new", Message: "must not run"})
				}}, TurnRequest{Actor: "alice", RunID: "new", Message: "new request"})
			}}, TurnRequest{Actor: "alice", RunID: "old", Message: "must not run"})
		}}, CancelTurnRequest{Actor: "alice", RunID: "old"})
	}, time.Millisecond)
	env.ExecuteWorkflow(WorkflowName, Input{SessionID: "ps_cancel", Actor: "alice", RepoURL: "https://github.com/org/app", Ref: "main"})
	if calls != 1 {
		t.Fatalf("expected exactly one admitted activity, got %d", calls)
	}
}

func TestCancelActiveTurnDoesNotPublishResult(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(SessionWorkflow, tw.RegisterOptions{Name: WorkflowName})
	env.RegisterActivityWithOptions(func(context.Context, TurnActivityInput) (*application.TurnResult, error) { return nil, nil }, activity.RegisterOptions{Name: ActTurn})
	env.OnActivity(ActTurn, mock.Anything, mock.Anything).Return(&application.TurnResult{Plan: &domain.Plan{Text: "late synthetic plan"}}, nil).After(time.Second)
	var receipt *CancelTurnReceipt
	finished := false
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateTurn, "slow-turn", callbackUpdateCallbacks{onComplete: func(_ any, err error) {
			requireTurnError(t, err, ErrTypeRunCancelled)
			finished = true
			value, queryErr := env.QueryWorkflow(QuerySession)
			if queryErr != nil {
				t.Error(queryErr)
				return
			}
			var session domain.Session
			if err := value.Get(&session); err != nil {
				t.Error(err)
			}
			if session.Plan != nil || len(session.Messages) != 0 || len(session.Events) != 0 {
				t.Errorf("cancelled turn changed state: %#v", session)
			}
			env.UpdateWorkflow(UpdateCancelTurn, "terminal-receipt", callbackUpdateCallbacks{onComplete: func(value any, err error) {
				if err != nil || value.(*CancelTurnReceipt).State != "cancelled" {
					t.Errorf("terminal receipt: %#v %v", value, err)
				}
				env.CancelWorkflow()
			}}, CancelTurnRequest{Actor: "alice", RunID: "slow"})
		}}, TurnRequest{Actor: "alice", RunID: "slow", Message: "inspect"})
	}, time.Millisecond)
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateCancelTurn, "stale-cancel", callbackUpdateCallbacks{onComplete: func(value any, err error) {
			if err != nil || value.(*CancelTurnReceipt).State != "cancelled" {
				t.Errorf("stale receipt: %#v %v", value, err)
			}
		}}, CancelTurnRequest{Actor: "alice", RunID: "old"})
	}, 3*time.Millisecond)
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateCancelTurn, "cancel-active", callbackUpdateCallbacks{onComplete: func(value any, err error) {
			if err != nil {
				t.Error(err)
				return
			}
			receipt = value.(*CancelTurnReceipt)
		}}, CancelTurnRequest{Actor: "alice", RunID: "slow"})
	}, 5*time.Millisecond)
	env.ExecuteWorkflow(WorkflowName, Input{SessionID: "ps_cancel", Actor: "alice", RepoURL: "https://github.com/org/app", Ref: "main", TurnRuns: map[string]string{"old": "cancelled"}})
	if !finished || receipt == nil || receipt.State != "cancellation_requested" {
		t.Fatalf("active cancellation: finished=%v receipt=%#v", finished, receipt)
	}
}

func TestLateTurnResultFenceAndCarry(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(func(ctx tw.Context) error {
		carry := map[string]string{"old": "cancelled", "done": "completed"}
		runs, err := newTurnRuns(ctx, carry, "alice", func() bool { return false })
		if err != nil {
			return err
		}
		carry["old"] = "completed"
		if runs.states["old"] != "cancelled" {
			t.Error("continued run state aliases input")
		}
		copied := runs.snapshot()
		copied["old"] = "completed"
		if runs.states["old"] != "cancelled" {
			t.Error("continued run state aliases output")
		}
		_, err = runs.turn(ctx, "old", func(tw.Context, func() bool) (*domain.Session, error) {
			t.Error("continued tombstone executed")
			return nil, nil
		})
		requireTurnError(t, err, ErrTypeRunCancelled)
		session, err := initSession(ctx, Input{SessionID: "ps_late", Actor: "alice", RepoURL: "https://github.com/org/app", Ref: "main"})
		if err != nil {
			return err
		}
		before := domain.CloneSession(session)
		_, err = applyTurnResult(ctx, session, TurnRequest{Message: "late request"}, application.TurnResult{Plan: &domain.Plan{Text: "late plan"}, Messages: []domain.Message{{Role: domain.RoleModel, Text: "late output"}}, ResolvedCommit: "late commit"}, func() bool { return false })
		requireTurnError(t, err, ErrTypeRunCancelled)
		if !reflect.DeepEqual(session, before) {
			t.Errorf("late result mutated session: %#v", session)
		}
		return nil
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
}

func TestCancelTurnRefusesUnverifiedIdentity(t *testing.T) {
	for _, actor := range []string{"", "bob"} {
		t.Run(actor, func(t *testing.T) {
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			rejected := false
			env.RegisterDelayedCallback(func() {
				env.UpdateWorkflow(UpdateCancelTurn, "wrong-owner", callbackUpdateCallbacks{onReject: func(err error) { requireTurnError(t, err, ErrTypeForbidden); rejected = true; env.CancelWorkflow() }}, CancelTurnRequest{Actor: actor, RunID: "run"})
			}, time.Millisecond)
			env.ExecuteWorkflow(SessionWorkflow, Input{SessionID: "ps_owner", Actor: "alice", RepoURL: "https://github.com/org/app", Ref: "main"})
			if !rejected {
				t.Error("unverified cancellation was not rejected")
			}
		})
	}
}

func TestSessionContinueAsNewRetainsCancellationFences(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetCurrentHistoryLength(100)
	env.RegisterActivityWithOptions(func(context.Context, TurnActivityInput) (*application.TurnResult, error) {
		return &application.TurnResult{Messages: []domain.Message{{Role: domain.RoleModel, Text: "recorded result"}}}, nil
	}, activity.RegisterOptions{Name: ActTurn})
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateTurn, "complete-for-continue", callbackUpdateCallbacks{onComplete: func(_ any, err error) {
			if err != nil {
				t.Error(err)
			}
		}}, TurnRequest{Actor: "alice", RunID: "new", Message: "inspect"})
	}, time.Millisecond)
	env.ExecuteWorkflow(SessionWorkflow, Input{SessionID: "ps_continue", Actor: "alice", RepoURL: "https://github.com/org/app", Ref: "main", ContinueAsNewAfterEvents: 1, TurnRuns: map[string]string{"old": "cancelled"}})
	var next *tw.ContinueAsNewError
	if !errors.As(env.GetWorkflowError(), &next) {
		t.Fatalf("expected continue-as-new, got %v", env.GetWorkflowError())
	}
	var in Input
	if err := converter.GetDefaultDataConverter().FromPayloads(next.Input, &in); err != nil {
		t.Fatal(err)
	}
	if in.CANCount != 1 || in.TurnRuns["old"] != "cancelled" || in.TurnRuns["new"] != "completed" || in.Carry == nil || len(in.Carry.Messages) != 2 {
		t.Fatalf("continued state: %#v", in)
	}
	resumed := suite.NewTestWorkflowEnvironment()
	calls := 0
	resumed.RegisterActivityWithOptions(func(context.Context, TurnActivityInput) (*application.TurnResult, error) {
		calls++
		return &application.TurnResult{}, nil
	}, activity.RegisterOptions{Name: ActTurn})
	resumed.RegisterDelayedCallback(func() {
		resumed.UpdateWorkflow(UpdateTurn, "old-after-continue", callbackUpdateCallbacks{onComplete: func(_ any, err error) { requireTurnError(t, err, ErrTypeRunCancelled); resumed.CancelWorkflow() }}, TurnRequest{Actor: "alice", RunID: "old", Message: "must not run"})
	}, time.Millisecond)
	resumed.ExecuteWorkflow(SessionWorkflow, in)
	if calls != 0 {
		t.Fatal("continued workflow forgot cancelled run")
	}
}

func TestCancellationCapacityNeverEvictsOldFences(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	carry := make(map[string]string, maxTurnRuns)
	for i := 0; i < maxTurnRuns; i++ {
		carry[fmt.Sprintf("run-%d", i)] = "cancelled"
	}
	refused := false
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateCancelTurn, "overflow-cancel", callbackUpdateCallbacks{onComplete: func(_ any, err error) {
			requireTurnError(t, err, ErrTypeRunCapacity)
			refused = true
			env.UpdateWorkflow(UpdateCancelTurn, "old-cancel", callbackUpdateCallbacks{onComplete: func(value any, err error) {
				if err != nil || value.(*CancelTurnReceipt).State != "cancelled" {
					t.Errorf("old fence: %#v %v", value, err)
				}
				env.CancelWorkflow()
			}}, CancelTurnRequest{Actor: "alice", RunID: "run-0"})
		}}, CancelTurnRequest{Actor: "alice", RunID: "overflow"})
	}, time.Millisecond)
	env.ExecuteWorkflow(SessionWorkflow, Input{SessionID: "ps_capacity", Actor: "alice", RepoURL: "https://github.com/org/app", Ref: "main", TurnRuns: carry})
	if !refused {
		t.Fatal("full retained history was not refused")
	}
}
