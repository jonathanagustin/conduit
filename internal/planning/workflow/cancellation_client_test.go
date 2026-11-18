package workflow

import (
	"context"
	"errors"
	"testing"

	"go.temporal.io/sdk/client"

	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

type cancellationHandle struct {
	client.WorkflowUpdateHandle
	value any
	err   error
}

func (h cancellationHandle) Get(_ context.Context, out any) error {
	if h.err != nil {
		return h.err
	}
	return (ownershipValue{h.value}).Get(out)
}

type cancellationClient struct {
	ownershipClient
	updates   []client.UpdateWorkflowOptions
	result    any
	err       error
	nilHandle bool
}

func (c *cancellationClient) UpdateWorkflow(_ context.Context, options client.UpdateWorkflowOptions) (client.WorkflowUpdateHandle, error) {
	c.updates = append(c.updates, options)
	if c.err != nil {
		return nil, c.err
	}
	if c.nilHandle {
		return nil, nil
	}
	return cancellationHandle{value: c.result}, nil
}

func TestWorkflowClientBindsLogicalRunToTurnAndAnswer(t *testing.T) {
	backend := &cancellationClient{ownershipClient: ownershipClient{owner: "alice"}, result: domain.Session{ID: "session"}}
	service := NewClientService(backend, "", "alice", nil)
	ctx := application.WithRunScope(context.Background(), application.RunScope{ThreadID: "session", RunID: "exact-logical-run"})
	if _, err := service.Turn(ctx, "session", "inspect"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Answer(ctx, "session", []domain.Answer{{ID: "question", Value: "answer"}}); err != nil {
		t.Fatal(err)
	}
	if len(backend.updates) != 2 {
		t.Fatalf("updates: %#v", backend.updates)
	}
	turn := backend.updates[0].Args[0].(TurnRequest)
	answer := backend.updates[1].Args[0].(AnswerRequest)
	if turn.RunID != "exact-logical-run" || answer.RunID != turn.RunID || turn.Actor != "alice" || answer.Actor != "alice" {
		t.Fatalf("turn=%#v answer=%#v", turn, answer)
	}
	if _, err := service.Turn(context.Background(), "session", "legacy"); err != nil {
		t.Fatal(err)
	}
	if backend.updates[2].Args[0].(TurnRequest).RunID != "" {
		t.Fatal("invented run id for legacy call")
	}
}

func TestWorkflowCancellationWaitsForExactOwnerRunReceipt(t *testing.T) {
	valid := domain.TurnCancellationReceipt{RunID: "run", State: "cancellation_requested", ValueTier: "CONDUIT_NATIVE"}
	for _, tc := range []struct {
		name, actor, run  string
		result            any
		err               error
		nilHandle, wantOK bool
		wantCalls         int
	}{
		{name: "owned", actor: "alice", run: "run", result: valid, wantOK: true, wantCalls: 1},
		{name: "missing caller", run: "run", result: valid},
		{name: "invalid id", actor: "alice", run: " ", result: valid},
		{name: "other run", actor: "alice", run: "run", result: domain.TurnCancellationReceipt{RunID: "other", State: "cancelled", ValueTier: "CONDUIT_NATIVE"}, wantCalls: 1},
		{name: "unknown state", actor: "alice", run: "run", result: domain.TurnCancellationReceipt{RunID: "run", State: "running", ValueTier: "CONDUIT_NATIVE"}, wantCalls: 1},
		{name: "absent receipt", actor: "alice", run: "run", nilHandle: true, wantCalls: 1},
		{name: "forbidden", actor: "bob", run: "run", err: turnRunError(ErrTypeForbidden, "forbidden"), wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &cancellationClient{result: tc.result, err: tc.err, nilHandle: tc.nilHandle}
			receipt, err := NewClientService(backend, "", tc.actor, nil).CancelTurn(context.Background(), "session", tc.run)
			if (err == nil) != tc.wantOK || (receipt != nil) != tc.wantOK {
				t.Fatalf("receipt=%#v err=%v", receipt, err)
			}
			if len(backend.updates) != tc.wantCalls {
				t.Fatalf("updates=%d", len(backend.updates))
			}
			if tc.name == "forbidden" && !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("owner refusal: %v", err)
			}
			if len(backend.updates) > 0 {
				op := backend.updates[0]
				req := op.Args[0].(CancelTurnRequest)
				if op.WorkflowID != WorkflowID("session") || op.UpdateName != UpdateCancelTurn || op.WaitForStage != client.WorkflowUpdateStageCompleted || req.Actor != tc.actor || req.RunID != tc.run {
					t.Fatalf("unsafe cancellation options: %#v %#v", op, req)
				}
			}
		})
	}
}
