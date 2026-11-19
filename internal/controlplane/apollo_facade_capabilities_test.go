package controlplane

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
)

// Named backend delegation exposes only the seven required capabilities, never
// the aggregate Repository's promoted lifecycle/lease/governance operations.
type facadeProjectionStore struct {
	backend                                                    *MemoryRepository
	catalogFailure                                             error
	catalogReads, reportWrites, assetWrites, publicationWrites int
}

func (s *facadeProjectionStore) ListProductPublications(ctx context.Context, product string) ([]catalog.Publication, error) {
	s.catalogReads++
	if s.catalogFailure != nil {
		return nil, s.catalogFailure
	}
	return s.backend.ListProductPublications(ctx, product)
}
func (s *facadeProjectionStore) CreateProductPublication(ctx context.Context, p catalog.Publication) (catalog.Publication, error) {
	s.publicationWrites++
	return s.backend.CreateProductPublication(ctx, p)
}
func (s *facadeProjectionStore) GetCatalogRetirement(ctx context.Context, product, version string) (CatalogRetirement, error) {
	return s.backend.GetCatalogRetirement(ctx, product, version)
}
func (s *facadeProjectionStore) PutBlob(ctx context.Context, kind, digest string, body io.Reader) (BlobDescriptor, bool, error) {
	s.assetWrites++
	return s.backend.PutBlob(ctx, kind, digest, body)
}
func (s *facadeProjectionStore) ListPlans(ctx context.Context, environment string) ([]PlanRecord, error) {
	return s.backend.ListPlans(ctx, environment)
}
func (s *facadeProjectionStore) GetEnvironmentReport(ctx context.Context, environment string) (EnvironmentReport, error) {
	return s.backend.GetEnvironmentReport(ctx, environment)
}
func (s *facadeProjectionStore) PutEnvironmentReport(ctx context.Context, r EnvironmentReport) (EnvironmentReport, error) {
	s.reportWrites++
	return s.backend.PutEnvironmentReport(ctx, r)
}

var _ compatibilityProjectionRepository = (*facadeProjectionStore)(nil)

type atomicFacadeProjectionStore struct {
	*facadeProjectionStore
	failure     error
	calls       int
	context     context.Context
	environment string
}

func (s *atomicFacadeProjectionStore) updateReportProjection(ctx context.Context, environment string, _ reportProjectionOperation) error {
	s.calls++
	s.context = ctx
	s.environment = environment
	return s.failure
}

func TestCompatibilityFacadeComposesNarrowCapabilitiesAndKeepsAtomicRefusal(t *testing.T) {
	backend := NewMemoryRepository()
	store := &facadeProjectionStore{backend: backend}
	api := newApolloPublicAPI(store, NewGovernanceService(backend), HandlerOptions{})
	if api.initializationError != nil || api.application == nil || store.catalogReads != 1 {
		t.Fatalf("initialization=%v reads=%d", api.initializationError, store.catalogReads)
	}
	err := api.application.PutEntityHealth(t.Context(), "dev", nil)
	if err == nil || !strings.Contains(err.Error(), "does not support atomic") || store.reportWrites != 0 || store.assetWrites != 0 || store.publicationWrites != 0 {
		t.Fatalf("non-atomic fallback: %v writes=%d", err, store.reportWrites)
	}
	failure := errors.New("catalog fixture unavailable")
	store.catalogFailure = failure
	api = newApolloPublicAPI(store, NewGovernanceService(backend), HandlerOptions{})
	if !errors.Is(api.initializationError, failure) || api.application != nil || store.catalogReads != 2 {
		t.Fatalf("initialization failure lost: %v", api.initializationError)
	}
}

func TestCompatibilityFacadePreservesDynamicAtomicCapabilityAndContext(t *testing.T) {
	backend := NewMemoryRepository()
	failure := errors.New("atomic fixture unavailable")
	store := &atomicFacadeProjectionStore{facadeProjectionStore: &facadeProjectionStore{backend: backend}, failure: failure}
	api := newApolloPublicAPI(store, NewGovernanceService(backend), HandlerOptions{})
	if api.initializationError != nil {
		t.Fatal(api.initializationError)
	}
	ctx := t.Context()
	err := api.application.PutEntityHealth(ctx, "dev", nil)
	if !errors.Is(err, failure) || store.calls != 1 || store.context != ctx || store.environment != "dev" || store.reportWrites != 0 {
		t.Fatalf("dynamic capability/context lost: %v calls=%d environment=%q", err, store.calls, store.environment)
	}
}

type memoryAtomicFacadeStore struct{ *facadeProjectionStore }

func (s *memoryAtomicFacadeStore) updateReportProjection(ctx context.Context, environment string, operation reportProjectionOperation) error {
	return s.backend.updateReportProjection(ctx, environment, operation)
}

func TestCompatibilityFacadeKeepsRealAtomicReportPersistence(t *testing.T) {
	backend := NewMemoryRepository()
	if _, err := backend.CreatePlan(t.Context(), PlanRecord{ID: "plan", Plan: Plan{Environment: "dev", Steps: []PlanStep{{InstallationID: "app", Product: "native:app", Version: "1.2.3", Namespace: "ns"}}}}); err != nil {
		t.Fatal(err)
	}
	store := &memoryAtomicFacadeStore{&facadeProjectionStore{backend: backend}}
	api := newApolloPublicAPI(store, NewGovernanceService(backend), HandlerOptions{})
	if api.initializationError != nil {
		t.Fatal(api.initializationError)
	}
	if err := api.application.PutEntityHealth(t.Context(), "dev", nil); err != nil {
		t.Fatal(err)
	}
	report, err := backend.GetEnvironmentReport(t.Context(), "dev")
	if err != nil || report.AgentID != "apollo-public-api" || len(report.Entities) != 1 || report.Entities[0].InstallationID != "app" || report.Entities[0].Product != "native:app" || report.Entities[0].Version != "1.2.3" || report.Entities[0].Health != "UNKNOWN" || store.reportWrites != 0 {
		t.Fatalf("atomic projection not persisted: %+v %v fallback=%d", report, err, store.reportWrites)
	}
}
