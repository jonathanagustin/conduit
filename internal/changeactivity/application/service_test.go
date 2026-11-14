package application

import (
	"context"
	"errors"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/changeactivity/adapters/memory"
	"github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"github.com/jonathanagustin/conduit/internal/changeactivity/ports"
	"strings"
	"testing"
	"time"
)

type authority struct{ writeErr, readErr error }

func (a *authority) RequireAnnotation(context.Context, string) (ports.Writer, error) {
	return ports.Writer{Author: "effective-author", Actor: "actual-carrier"}, a.writeErr
}
func (a *authority) RequireRead(context.Context, string) error { return a.readErr }

type failingStore struct {
	ports.Store
	listErr error
	rows    []domain.Comment
}

func (s failingStore) List(context.Context, string) ([]domain.Comment, error) {
	return s.rows, s.listErr
}
func TestDiscussionRequiresAuthorityAndPreservesExactTextWithoutReviewState(t *testing.T) {
	store := memory.New()
	a := &authority{writeErr: domain.ErrForbidden}
	service, err := New(store, a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Append(t.Context(), "crq_thread", "text"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal(err)
	}
	rows, _ := store.List(t.Context(), "crq_thread")
	if len(rows) != 0 {
		t.Fatal("denied annotation persisted")
	}
	a.writeErr = nil
	now := time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	sequence := 0
	service.newID = func() (string, error) { sequence++; return fmt.Sprintf("cmt_%032x", sequence), nil }
	first, err := service.Append(t.Context(), "crq_thread", "  first\nsecond\tline  ")
	if err != nil {
		t.Fatal(err)
	}
	if first.Author != "effective-author" || first.Actor != "actual-carrier" || first.Text != "  first\nsecond\tline  " || first.ValueTier != "CONDUIT_NATIVE" {
		t.Fatal(first)
	}
	second, err := service.Append(t.Context(), "crq_thread", "same timestamp")
	if err != nil {
		t.Fatal(err)
	}
	rows, err = service.List(t.Context(), "crq_thread")
	if err != nil || len(rows) != 2 || rows[0] != first || rows[1] != second {
		t.Fatal("stable discussion order changed", rows, err)
	}
	a.readErr = domain.ErrForbidden
	if _, err := service.List(t.Context(), "crq_thread"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal(err)
	}
	a.readErr = nil
	for _, text := range []string{"", " \t\n", strings.Repeat("x", domain.MaxTextBytes+1), "contains\x00nul"} {
		if _, err := service.Append(t.Context(), "crq_thread", text); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("invalid discussion text accepted", err)
		}
	}
	rows, _ = store.List(t.Context(), "crq_thread")
	if len(rows) != 2 {
		t.Fatal("invalid input mutated discussion")
	}
	failure := errors.New("storage unavailable")
	service.store = failingStore{listErr: failure}
	if _, err := service.List(t.Context(), "crq_thread"); !errors.Is(err, failure) {
		t.Fatal("storage failure flattened into empty discussion", err)
	}
	foreign := first
	foreign.RequestID = "crq_foreign"
	service.store = failingStore{rows: []domain.Comment{foreign}}
	if _, err := service.List(t.Context(), "crq_thread"); !errors.Is(err, domain.ErrCorrupt) {
		t.Fatal("foreign discussion returned", err)
	}
	service.store = failingStore{rows: []domain.Comment{first, first}}
	if _, err := service.List(t.Context(), "crq_thread"); !errors.Is(err, domain.ErrCorrupt) {
		t.Fatal("duplicate discussion returned", err)
	}
}
