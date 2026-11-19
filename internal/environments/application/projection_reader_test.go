package application

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jonathanagustin/conduit/internal/environments/domain"
)

type projectionFixture struct {
	inventory map[string]domain.CompatibilityProjection
	lifecycle map[string]domain.ReleaseLifecycle
	failure   error
	calls     int
	context   context.Context
}

func (source *projectionFixture) InventoryByEnvironment(ctx context.Context) (map[string]domain.CompatibilityProjection, error) {
	source.calls++
	source.context = ctx
	return source.inventory, source.failure
}
func (source *projectionFixture) LifecycleByEnvironment(ctx context.Context) (map[string]domain.ReleaseLifecycle, error) {
	source.calls++
	source.context = ctx
	return source.lifecycle, source.failure
}

func TestProjectionReaderOrdersBySourceEnvironmentAndOwnsReturnedSlices(t *testing.T) {
	source := &projectionFixture{inventory: map[string]domain.CompatibilityProjection{
		"a": {ID: "z-wire-id", Entities: []domain.ProjectedEntity{{Name: "first", Version: "1"}}, Namespaces: []domain.ProjectedNamespace{{RID: "same", Name: "first"}}},
		"z": {ID: "a-wire-id", Entities: []domain.ProjectedEntity{{Name: "last"}}},
	}}
	reader := ProjectionReader{Source: source}
	result, err := reader.EnvironmentInventory(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[0].ID != "z-wire-id" || result[1].ID != "a-wire-id" {
		t.Fatalf("source grouping order changed: %+v", result)
	}
	result[0].Entities[0].Name = "changed"
	result[0].Namespaces[0].Name = "changed"
	if source.inventory["a"].Entities[0].Name != "first" || source.inventory["a"].Namespaces[0].Name != "first" {
		t.Fatal("returned inventory aliases source snapshots")
	}
	source.inventory = map[string]domain.CompatibilityProjection{"new": {ID: "new"}}
	next, err := reader.EnvironmentInventory(t.Context())
	if err != nil || len(next) != 1 || next[0].ID != "new" || source.calls != 2 {
		t.Fatalf("stale inventory reused: %+v %v calls=%d", next, err, source.calls)
	}
}

func TestProjectionReaderRetainsStableLifecycleOrderAndRecallOwnership(t *testing.T) {
	source := &projectionFixture{lifecycle: map[string]domain.ReleaseLifecycle{
		"z": {Recalls: []domain.ProjectedRecall{{Product: "last"}}, Promotions: []domain.ProjectedPromotion{{Product: "last", Passed: false}}},
		"a": {Recalls: []domain.ProjectedRecall{{Product: "first", Reason: "first-reason", ExcludedVersions: []string{"2"}}, {Product: "second"}}, Promotions: []domain.ProjectedPromotion{{Product: "first", Passed: true}}},
	}}
	result, err := (ProjectionReader{Source: source}).ReleaseLifecycle(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	products := []string{}
	for _, recall := range result.Recalls {
		products = append(products, recall.Product)
	}
	if !reflect.DeepEqual(products, []string{"first", "second", "last"}) || result.Promotions[0].Product != "first" || !result.Promotions[0].Passed || result.Promotions[1].Passed {
		t.Fatalf("lifecycle order or facts changed: %+v", result)
	}
	result.Recalls[0].ExcludedVersions[0] = "changed"
	if source.lifecycle["a"].Recalls[0].ExcludedVersions[0] != "2" {
		t.Fatal("recall exclusions alias the source")
	}
}

func TestProjectionReaderPreservesFailureAndContextInsteadOfEmptySuccess(t *testing.T) {
	failure := errors.New("source unavailable")
	source := &projectionFixture{failure: failure}
	reader := ProjectionReader{Source: source}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if _, err := reader.EnvironmentInventory(ctx); !errors.Is(err, failure) {
		t.Fatalf("inventory failure changed: %v", err)
	}
	if _, err := reader.ReleaseLifecycle(ctx); !errors.Is(err, failure) {
		t.Fatalf("lifecycle failure changed: %v", err)
	}
	if source.calls != 2 || source.context != ctx {
		t.Fatal("source read or original context omitted")
	}
}

func TestProjectionReaderDistinguishesEmptySourceFromUnavailable(t *testing.T) {
	reader := ProjectionReader{Source: &projectionFixture{}}
	inventory, err := reader.EnvironmentInventory(t.Context())
	if err != nil || inventory == nil || len(inventory) != 0 {
		t.Fatalf("empty inventory contract changed: %#v %v", inventory, err)
	}
	lifecycle, err := reader.ReleaseLifecycle(t.Context())
	if err != nil || lifecycle.Recalls != nil || lifecycle.Promotions != nil {
		t.Fatalf("empty lifecycle contract changed: %+v %v", lifecycle, err)
	}
	if _, err := (ProjectionReader{}).EnvironmentInventory(t.Context()); !errors.Is(err, ErrProjectionSourceUnavailable) {
		t.Fatal("missing source reported as empty inventory", err)
	}
	if _, err := (ProjectionReader{}).ReleaseLifecycle(t.Context()); !errors.Is(err, ErrProjectionSourceUnavailable) {
		t.Fatal("missing source reported as empty lifecycle", err)
	}
}

func TestProjectionReaderOrderIsStableAcrossRepeatedSourceMaps(t *testing.T) {
	source := &projectionFixture{inventory: map[string]domain.CompatibilityProjection{"z": {ID: "wire-a"}, "a": {ID: "wire-z"}, "b": {ID: "wire-b"}}}
	reader := ProjectionReader{Source: source}
	for range 100 {
		inventory, err := reader.EnvironmentInventory(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, item := range inventory {
			ids = append(ids, item.ID)
		}
		if !reflect.DeepEqual(ids, []string{"wire-z", "wire-b", "wire-a"}) {
			t.Fatalf("map iteration changed source grouping order: %v", ids)
		}
	}
	if source.calls != 100 {
		t.Fatalf("source reads were cached: %d", source.calls)
	}
}
