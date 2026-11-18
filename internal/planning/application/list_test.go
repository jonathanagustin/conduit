package application

import (
	"context"
	"errors"
	"testing"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

func TestListIsOwnerStoreScopedSortedAndDoesNotAliasSessions(t *testing.T) {
	a, b := NewService(nil), NewService(nil)
	first, err := a.Create(context.Background(), "https://github.com/org/one", "main")
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.Create(context.Background(), "https://github.com/org/two", "main")
	if err != nil {
		t.Fatal(err)
	}
	other, err := b.Create(context.Background(), "https://github.com/private/other", "main")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := a.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID >= rows[1].ID {
		t.Fatalf("unexpected index: %#v", rows)
	}
	for _, row := range rows {
		if row.ID == other.ID {
			t.Fatal("cross-store session exposed")
		}
	}
	rows[0].Profile.Name = "tampered"
	rows[0].Status = "tampered"
	got, err := a.Get(context.Background(), rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Profile.Name != domain.ProfileOnboard || got.Status != domain.StatusStarted {
		t.Fatal("list aliases stored session")
	}
	if first.ID == second.ID {
		t.Fatal("fixture ids collide")
	}
	empty, err := NewService(nil).List(context.Background())
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty: %#v %v", empty, err)
	}
}

func TestListRefusesOversizedInventoryAndCancelledContext(t *testing.T) {
	service := NewService(nil)
	for i := 0; i <= domain.MaxIndexedExecutions; i++ {
		service.sessions[string(rune(i+1))] = &domain.Session{ID: "fixture"}
	}
	if rows, err := service.List(context.Background()); !errors.Is(err, domain.ErrIndexTooLarge) || rows != nil {
		t.Fatalf("oversized returned %#v %v", rows, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewService(nil).List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}
