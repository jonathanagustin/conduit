package filestore_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/spaces/adapters/filestore"
	"github.com/jonathanagustin/conduit/internal/spaces/domain"
)

func TestSpaceSurvivesReopenAndConcurrentCreateCannotReplaceIdentity(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root := t.TempDir()
	first, err := filestore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := filestore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	value, err := domain.New("space_0123456789abcdef0123456789abcdef", "Original", "", "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var group sync.WaitGroup
	for i := range 20 {
		group.Go(func() {
			store := first
			if i%2 != 0 {
				store = second
			}
			_, err := store.CreateSpace(ctx, value)
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, domain.ErrConflict) {
				t.Errorf("create: %v", err)
			}
		})
	}
	group.Wait()
	if wins.Load() != 1 {
		t.Fatalf("successful identity creations = %d", wins.Load())
	}
	reopened, err := filestore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := reopened.GetSpace(ctx, value.ID)
	if err != nil || saved != value {
		t.Fatalf("record lost on reopen: %#v, %v", saved, err)
	}
	info, err := os.Stat(filepath.Join(root, value.ID+".json"))
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatal("Space record is not private")
	}
	changed := value
	changed.CreatedBy = "mallory"
	changed.Name = "Replacement"
	if _, err := second.CreateSpace(ctx, changed); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("identity overwritten: %v", err)
	}
	saved, err = first.GetSpace(ctx, value.ID)
	if err != nil || saved != value {
		t.Fatal("failed duplicate changed the record")
	}
}

func TestCorruptSpaceIdentityDoesNotBecomeAValidGrouping(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store, err := filestore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	const id = "space_0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(filepath.Join(root, id+".json"), []byte(`{"id":"space_ffffffffffffffffffffffffffffffff","name":"Other","createdBy":"alice","createdAt":"2026-11-13T12:00:00Z","valueTier":"CONDUIT_NATIVE"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSpace(t.Context(), id); err == nil {
		t.Fatal("mismatched identity accepted")
	}
	if values, err := store.ListSpaces(t.Context()); err == nil || values != nil {
		t.Fatalf("corruption became a collection: %#v, %v", values, err)
	}
	if _, err := store.GetSpace(t.Context(), "../outside"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("invalid identity was treated as a path: %v", err)
	}
}

func TestConcurrentSpaceBindingSurvivesReopenAndPreservesFirstAssignment(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root := t.TempDir()
	first, err := filestore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := filestore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := domain.New("space_11111111111111111111111111111111", "A", "", "admin", time.Now())
	b, _ := domain.New("space_22222222222222222222222222222222", "B", "", "admin", time.Now())
	if _, err := first.CreateSpace(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err := first.CreateSpace(ctx, b); err != nil {
		t.Fatal(err)
	}
	resource := domain.Resource{Kind: domain.Product, ID: "com.example:application"}
	var wins atomic.Int32
	var group sync.WaitGroup
	for i := range 20 {
		group.Go(func() {
			store := first
			space := a
			if i%2 != 0 {
				store = second
				space = b
			}
			value, _ := domain.NewBinding(space.ID, resource, "publisher", time.Now())
			if _, err := store.BindResource(ctx, value); err == nil {
				wins.Add(1)
			} else if !errors.Is(err, domain.ErrConflict) {
				t.Errorf("claim: %v", err)
			}
		})
	}
	group.Wait()
	if wins.Load() != 10 {
		t.Fatalf("assignments accepted across Space boundary: %d", wins.Load())
	}
	original, err := first.GetBinding(ctx, resource)
	if err != nil {
		t.Fatal(err)
	}
	loser := a
	if original.SpaceID == a.ID {
		loser = b
	}
	conflict, _ := domain.NewBinding(loser.ID, resource, "replacement", time.Now())
	if _, err := second.BindResource(ctx, conflict); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("assignment replaced: %v", err)
	}
	reopened, err := filestore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := reopened.GetBinding(ctx, resource)
	if err != nil || saved != original {
		t.Fatalf("assignment lost: %#v, %v", saved, err)
	}
	retry, _ := domain.NewBinding(saved.SpaceID, resource, "replacement", time.Now())
	saved, err = second.BindResource(ctx, retry)
	if err != nil || saved != original {
		t.Fatal("idempotent assignment replaced provenance")
	}
}
