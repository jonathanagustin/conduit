package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

type planningFunc func(context.Context, *domain.Session, string) (*TurnResult, error)

func (f planningFunc) Turn(ctx context.Context, s *domain.Session, message string) (*TurnResult, error) {
	return f(ctx, s, message)
}

func TestTurnSerializesSessionMutationsWithoutBlockingOtherSessions(t *testing.T) {
	entered := make(chan string, 4)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var workers sync.WaitGroup
	defer workers.Wait()
	defer unblock()
	service := NewService(planningFunc(func(ctx context.Context, s *domain.Session, _ string) (*TurnResult, error) {
		entered <- s.ID
		select {
		case <-release:
			return &TurnResult{Plan: &domain.Plan{Text: "new plan"}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}))
	first, err := service.Create(context.Background(), "https://github.com/org/app", "main")
	if err != nil {
		t.Fatal(err)
	}
	other, err := service.Create(context.Background(), "https://github.com/org/other", "main")
	if err != nil {
		t.Fatal(err)
	}
	// A previously reviewed draft would otherwise be eligible for Accept while a
	// newer model turn is still replacing it.
	service.sessions[first.ID].Plan = &domain.Plan{Text: "old plan"}
	service.sessions[first.ID].ResolvedCommit = strings.Repeat("a", 40)
	service.sessions[first.ID].NeedsInput = []domain.Question{{ID: "namespace", Name: "namespace", Type: domain.QuestionString, Required: true}}
	service.sessions[first.ID].Answers = []domain.Answer{{ID: "namespace", Value: "original"}}
	start := func(id string) <-chan error {
		done := make(chan error, 1)
		workers.Add(1)
		go func() { defer workers.Done(); _, err := service.Turn(context.Background(), id, "review"); done <- err }()
		return done
	}
	firstDone := start(first.ID)
	select {
	case id := <-entered:
		if id != first.ID {
			t.Fatal(id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first turn did not start")
	}
	if _, err := service.Answer(context.Background(), first.ID, []domain.Answer{{ID: "namespace", Value: "changed"}}); !errors.Is(err, domain.ErrTurnInProgress) {
		t.Fatalf("concurrent answers: %v", err)
	}
	if _, err := service.Accept(context.Background(), first.ID); !errors.Is(err, domain.ErrTurnInProgress) {
		t.Fatalf("concurrent Accept: %v", err)
	}
	secondDone := start(first.ID)
	select {
	case err := <-secondDone:
		if !errors.Is(err, domain.ErrTurnInProgress) {
			t.Fatalf("concurrent turn: %v", err)
		}
	case <-entered:
		t.Fatal("a second turn reached the model for the same session")
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent turn was blocked instead of rejected")
	}
	snapshot, err := service.Get(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Binding != nil || snapshot.Answers[0].Value != "original" {
		t.Fatal("in-flight session was mutated")
	}
	otherDone := start(other.ID)
	select {
	case id := <-entered:
		if id != other.ID {
			t.Fatal(id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("another session was unnecessarily serialized")
	}
	unblock()
	for _, done := range []<-chan error{firstDone, otherDone} {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.Turn(context.Background(), first.ID, "again"); err != nil {
		t.Fatalf("reservation was not released: %v", err)
	}
}

func TestCancelledTurnReleasesSessionReservation(t *testing.T) {
	entered := make(chan struct{}, 1)
	service := NewService(planningFunc(func(ctx context.Context, _ *domain.Session, _ string) (*TurnResult, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	session, err := service.Create(context.Background(), "https://github.com/org/app", "main")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := service.Turn(ctx, session.ID, "cancelled"); done <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not start")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	service.planner = scriptedPlanner{result: &TurnResult{Plan: &domain.Plan{Text: "recovered"}}}
	got, err := service.Turn(context.Background(), session.ID, "retry")
	if err != nil {
		t.Fatal(err)
	}
	if got.Plan == nil || got.Plan.Text != "recovered" || len(got.Messages) != 1 {
		t.Fatalf("cancelled turn affected retry: %+v", got)
	}
}

func TestChangedAnswersInvalidatePlanButIdenticalAnswersPreserveIt(t *testing.T) {
	service := NewService(scriptedPlanner{result: &TurnResult{Plan: &domain.Plan{Text: "reviewed draft"}, NeedsInput: []domain.Question{{ID: "mode", Name: "mode", Type: domain.QuestionString}}, ResolvedCommit: strings.Repeat("a", 40)}})
	session, err := service.Create(t.Context(), "https://github.com/org/app", "main")
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.Turn(t.Context(), session.ID, "plan")
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.Answer(t.Context(), session.ID, []domain.Answer{{ID: "mode", Value: "one"}})
	if err != nil {
		t.Fatal(err)
	}
	if session.Plan != nil {
		t.Fatal("changed answers left the old draft eligible")
	}
	if _, err := service.Accept(t.Context(), session.ID); !errors.Is(err, domain.ErrPlanRequired) {
		t.Fatalf("stale plan accepted: %v", err)
	}
	session, err = service.Turn(t.Context(), session.ID, "replan")
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.Answer(t.Context(), session.ID, []domain.Answer{{ID: "mode", Value: " one "}})
	if err != nil {
		t.Fatal(err)
	}
	if session.Plan == nil || session.Plan.Text != "reviewed draft" || session.Status != domain.StatusReady {
		t.Fatal("idempotent answers discarded an unchanged plan")
	}
}
