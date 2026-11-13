package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/spaces/domain"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is not set")
	}
	store, err := New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
func seedSpaces(t *testing.T, store *Store) (domain.Space, domain.Space) {
	t.Helper()
	ctx := t.Context()
	if _, err := store.db.ExecContext(ctx, `TRUNCATE conduit_space_bindings,conduit_spaces`); err != nil {
		t.Fatal(err)
	}
	first, err := domain.New("space_11111111111111111111111111111111", "First", "", "admin", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	second, err := domain.New("space_22222222222222222222222222222222", "Second", "", "admin", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSpace(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSpace(ctx, second); err != nil {
		t.Fatal(err)
	}
	return first, second
}

func TestIndependentPoolsCannotReassignResourceAndLegacySnapshotsCannotEraseSpaces(t *testing.T) {
	ctx := t.Context()
	first := newStore(t)
	second := newStore(t)
	a, b := seedSpaces(t, first)
	resource := domain.Resource{Kind: domain.Product, ID: "com.example:application"}
	var successes atomic.Int32
	var group sync.WaitGroup
	for i := range 20 {
		group.Go(func() {
			store := first
			space := a
			if i%2 != 0 {
				store = second
				space = b
			}
			binding, err := domain.NewBinding(space.ID, resource, "publisher", time.Now())
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := store.BindResource(ctx, binding); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, domain.ErrConflict) {
				t.Errorf("claim: %v", err)
			}
		})
	}
	group.Wait()
	if successes.Load() != 10 {
		t.Fatalf("claims acknowledged for more than one Space: %d", successes.Load())
	}
	saved, err := first.GetBinding(ctx, resource)
	if err != nil {
		t.Fatal(err)
	}
	loser := a
	if saved.SpaceID == a.ID {
		loser = b
	}
	bad, _ := domain.NewBinding(loser.ID, resource, "replacement", time.Now())
	if _, err := second.BindResource(ctx, bad); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("assignment moved: %v", err)
	}
	retry, _ := domain.NewBinding(saved.SpaceID, resource, "replacement", time.Now())
	again, err := second.BindResource(ctx, retry)
	if err != nil || again != saved {
		t.Fatalf("idempotent retry replaced provenance: %#v, %v", again, err)
	}
	// A legacy writer serializes the delivery component without a Space field.
	// The independent tables must retain identity and grouping through that write.
	core, err := controlplane.NewPostgresRepository(ctx, os.Getenv("CONDUIT_TEST_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	team := controlplane.Team{Name: "space-snapshot-writer", Revision: 1}
	if previous, err := core.GetTeam(ctx, team.Name); err == nil {
		team.Revision = previous.Revision + 1
	} else if !errors.Is(err, controlplane.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := core.PutTeam(ctx, team); err != nil {
		t.Fatal(err)
	}
	if err := core.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := newStore(t)
	stored, err := reopened.GetSpace(ctx, a.ID)
	if err != nil || stored != a {
		t.Fatalf("identity lost after snapshot write/reopen: %#v, %v", stored, err)
	}
	retained, err := reopened.GetBinding(ctx, resource)
	if err != nil || retained != saved {
		t.Fatalf("assignment lost after snapshot write/reopen: %#v, %v", retained, err)
	}
	if _, err := first.CreateSpace(ctx, a); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("identity overwrite allowed: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := first.ListSpaces(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read: %v", err)
	}
}

func TestCorruptBindingCannotClaimAnotherSpaceAndUnknownSpaceCannotBeAssigned(t *testing.T) {
	ctx := t.Context()
	store := newStore(t)
	a, b := seedSpaces(t, store)
	resource := domain.Resource{Kind: domain.Environment, ID: "production"}
	binding, _ := domain.NewBinding(a.ID, resource, "admin", time.Now())
	if _, err := store.BindResource(ctx, binding); err != nil {
		t.Fatal(err)
	}
	forged := binding
	forged.SpaceID = b.ID
	document, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE conduit_space_bindings SET document=$1 WHERE resource_kind=$2 AND resource_id=$3`, document, resource.Kind, resource.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetBinding(ctx, resource); err == nil {
		t.Fatal("document/index mismatch changed membership")
	}
	if values, err := store.ListBindings(ctx, a.ID); err == nil || values != nil {
		t.Fatalf("corruption became a known roster: %#v, %v", values, err)
	}
	missing, _ := domain.NewBinding("space_ffffffffffffffffffffffffffffffff", domain.Resource{Kind: domain.Bundle, ID: "future"}, "admin", time.Now())
	if _, err := store.BindResource(ctx, missing); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown Space accepted: %v", err)
	}
}
