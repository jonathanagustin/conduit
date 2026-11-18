package application

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

func scopedRun(id, run string) context.Context {
	return WithRunScope(context.Background(), RunScope{ThreadID: id, RunID: run})
}

func TestLocalCancellationFencesLateResultAndExactRun(t *testing.T) {
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	service := NewService(planningFunc(func(ctx context.Context, _ *domain.Session, _ string) (*TurnResult, error) {
		entered <- ctx
		<-release
		return &TurnResult{Plan: &domain.Plan{Text: "late plan"}, Messages: []domain.Message{{Role: domain.RoleModel, Text: "late output"}}}, nil
	}))
	session, err := service.Create(context.Background(), "https://github.com/org/app", "main")
	if err != nil {
		t.Fatal(err)
	}
	before := domain.CloneSession(session)
	if receipt, err := service.CancelTurn(context.Background(), session.ID, "old"); err != nil || receipt.State != "cancelled" {
		t.Fatalf("before-start: %#v %v", receipt, err)
	}
	done := make(chan error, 1)
	go func() { _, err := service.Turn(scopedRun(session.ID, "new"), session.ID, "new request"); done <- err }()
	active := <-entered
	if _, err := service.CancelTurn(context.Background(), session.ID, "old"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-active.Done():
		t.Error("stale cancellation stopped new run")
	default:
	}
	receipt, err := service.CancelTurn(context.Background(), session.ID, "new")
	if err != nil || receipt.State != "cancellation_requested" {
		t.Fatalf("active receipt: %#v %v", receipt, err)
	}
	select {
	case <-active.Done():
	default:
		t.Error("model context was not cancelled")
	}
	close(release)
	if err := <-done; !errors.Is(err, domain.ErrRunCancelled) {
		t.Errorf("late completion: %v", err)
	}
	got, err := service.Get(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, before) {
		t.Fatalf("cancelled turn changed session: %#v", got)
	}
	if receipt, err := service.CancelTurn(context.Background(), session.ID, "new"); err != nil || receipt.State != "cancelled" {
		t.Fatalf("terminal receipt: %#v %v", receipt, err)
	}
	if _, err := service.Turn(scopedRun(session.ID, "new"), session.ID, "retry old id"); !errors.Is(err, domain.ErrRunCancelled) {
		t.Fatalf("reused cancelled id: %v", err)
	}
	service.planner = scriptedPlanner{result: &TurnResult{Plan: &domain.Plan{Text: "fresh plan"}}}
	if _, err := service.Turn(scopedRun(session.ID, "fresh"), session.ID, "fresh request"); err != nil {
		t.Fatal(err)
	}
	if receipt, err := service.CancelTurn(context.Background(), session.ID, "fresh"); err != nil || receipt.State != "completed" {
		t.Fatalf("completed receipt: %#v %v", receipt, err)
	}
	if _, err := service.Turn(scopedRun(session.ID, "fresh"), session.ID, "reuse"); !errors.Is(err, domain.ErrRunConflict) {
		t.Fatalf("reused completed id: %v", err)
	}
}

func TestLocalCancellationCapacityAnswersAndOwnerStore(t *testing.T) {
	service := NewService(scriptedPlanner{result: &TurnResult{}})
	session, err := service.Create(context.Background(), "https://github.com/org/app", "main")
	if err != nil {
		t.Fatal(err)
	}
	peer := NewService(nil)
	if _, err := peer.CancelTurn(context.Background(), session.ID, "run"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign session: %v", err)
	}
	for i := 0; i < domain.MaxTurnRuns; i++ {
		if _, err := service.CancelTurn(context.Background(), session.ID, fmt.Sprintf("run-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.CancelTurn(context.Background(), session.ID, "overflow"); !errors.Is(err, domain.ErrRunCapacity) {
		t.Fatalf("cancellation capacity: %v", err)
	}
	if _, err := service.Turn(scopedRun(session.ID, "overflow"), session.ID, "overflow"); !errors.Is(err, domain.ErrRunCapacity) {
		t.Fatalf("turn capacity: %v", err)
	}
	service.sessions[session.ID].NeedsInput = []domain.Question{{ID: "q", Type: domain.QuestionString}}
	if _, err := service.Answer(scopedRun(session.ID, "run-0"), session.ID, []domain.Answer{{ID: "q", Value: "answer"}}); !errors.Is(err, domain.ErrRunCancelled) {
		t.Fatalf("cancelled answer: %v", err)
	}
	got, _ := service.Get(context.Background(), session.ID)
	if len(got.Answers) != 0 {
		t.Fatal("cancelled answer was recorded")
	}
	if receipt, err := service.CancelTurn(context.Background(), session.ID, "run-0"); err != nil || receipt.State != "cancelled" {
		t.Fatalf("old fence evicted: %#v %v", receipt, err)
	}
}
