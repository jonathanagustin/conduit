package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jonathanagustin/conduit/internal/spaces/adapters/memory"
	"github.com/jonathanagustin/conduit/internal/spaces/application"
	"github.com/jonathanagustin/conduit/internal/spaces/domain"
)

type authority struct {
	subject      string
	createErr    error
	read         func(string) error
	resourceRead func(domain.Resource) error
}

func (a authority) RequireCreation(context.Context) (string, error) { return a.subject, a.createErr }
func (a authority) RequireRead(_ context.Context, id string) error {
	if a.read != nil {
		return a.read(id)
	}
	return nil
}

func TestCreatePersistsDistinctNativeIdentityWithAuthenticatedProvenance(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := memory.New()
	service, err := application.New(store, authority{subject: "operator-alice"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Create(ctx, " Production ", "environment grouping")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(ctx, "Production", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || !domain.ValidID(first.ID) || first.CreatedBy != "operator-alice" || first.Name != "Production" || first.ValueTier != "CONDUIT_NATIVE" || first.CreatedAt.IsZero() {
		t.Fatalf("invalid persisted identity/provenance: %#v", first)
	}
	saved, err := store.GetSpace(ctx, first.ID)
	if err != nil || saved != first {
		t.Fatalf("stored Space differs: %#v, %v", saved, err)
	}
	denied, err := application.New(store, authority{createErr: domain.ErrForbidden})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := denied.Create(ctx, "Unauthorized", ""); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("denial lost: %v", err)
	}
	all, err := store.ListSpaces(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("unauthorized create persisted a record: %v, %v", all, err)
	}
}

func TestSpaceReadsIntersectCallerAuthorityAndDoNotHideFailures(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := memory.New()
	admin, _ := application.New(store, authority{subject: "admin"})
	alice, err := admin.Create(ctx, "Alice", "")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := admin.Create(ctx, "Bob", "")
	if err != nil {
		t.Fatal(err)
	}
	reader, _ := application.New(store, authority{read: func(id string) error {
		if id == alice.ID {
			return nil
		}
		return domain.ErrForbidden
	}})
	values, err := reader.List(ctx)
	if err != nil || len(values) != 1 || values[0].ID != alice.ID {
		t.Fatalf("read leaked another grouping: %#v, %v", values, err)
	}
	if _, err := reader.Get(ctx, bob.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("direct read bypassed filtering: %v", err)
	}
	unavailable := errors.New("authorization store unavailable")
	broken, _ := application.New(store, authority{read: func(string) error { return unavailable }})
	if values, err := broken.List(ctx); !errors.Is(err, unavailable) || values != nil {
		t.Fatalf("failed read became an empty/partial answer: %#v, %v", values, err)
	}
}

func (a authority) RequireBinding(context.Context, string, domain.Resource) (string, error) {
	return a.subject, a.createErr
}
func (a authority) RequireResourceRead(_ context.Context, resource domain.Resource) error {
	if a.resourceRead != nil {
		return a.resourceRead(resource)
	}
	return nil
}

func TestSpaceScopeUsesStoredBindingWithoutGrantingUnrelatedResourceReads(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := memory.New()
	admin, _ := application.New(store, authority{subject: "admin"})
	first, err := admin.Create(ctx, "First", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := admin.Create(ctx, "Second", "")
	if err != nil {
		t.Fatal(err)
	}
	own := domain.Resource{Kind: domain.Environment, ID: "prod-a"}
	other := domain.Resource{Kind: domain.Product, ID: "com.example:private"}
	binding, err := admin.Bind(ctx, first.ID, own)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Bind(ctx, first.ID, other); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Bind(ctx, second.ID, own); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("resource moved across Space boundary: %v", err)
	}
	reader, _ := application.New(store, authority{subject: "alice", resourceRead: func(resource domain.Resource) error {
		if resource == own {
			return nil
		}
		return domain.ErrForbidden
	}})
	if err := reader.RequireScope(ctx, first.ID, own); err != nil {
		t.Fatal(err)
	}
	if err := reader.RequireScope(ctx, second.ID, own); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("wrong Space selected: %v", err)
	}
	if _, err := reader.Lookup(ctx, other); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Space selection granted resource visibility: %v", err)
	}
	visible, err := reader.ListBindings(ctx, first.ID)
	if err != nil || len(visible) != 1 || visible[0] != binding {
		t.Fatalf("roster disclosed a hidden resource: %#v, %v", visible, err)
	}
	again, err := admin.Bind(ctx, first.ID, own)
	if err != nil || again != binding {
		t.Fatalf("idempotent assignment replaced provenance: %#v, %v", again, err)
	}
}
