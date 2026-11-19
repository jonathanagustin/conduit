package controlplane

import (
	"context"
	"errors"
	"strings"
	"testing"

	apolloapp "github.com/jonathanagustin/conduit/internal/apollo/application"
	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
)

// Only the three projected operations are implemented: accepting this fixture
// at the port boundary prevents broad aggregate repository requirements.
type reportingProjectionStore struct {
	failure error
	writes  int
}

func (store *reportingProjectionStore) ListPlans(context.Context, string) ([]PlanRecord, error) {
	return nil, store.failure
}
func (store *reportingProjectionStore) GetEnvironmentReport(context.Context, string) (environments.Report, error) {
	return environments.Report{}, store.failure
}
func (store *reportingProjectionStore) PutEnvironmentReport(context.Context, environments.Report) (environments.Report, error) {
	store.writes++
	return environments.Report{}, store.failure
}

func TestEnvironmentReportingProjectionAcceptsNarrowStoreAndPreservesFailures(t *testing.T) {
	failure := errors.New("projection store unavailable")
	store := &reportingProjectionStore{failure: failure}
	port := environmentReportingProjectionPort{repository: store}
	if _, err := port.LatestPlan(t.Context(), "dev"); !errors.Is(err, failure) {
		t.Fatalf("Plan failure changed: %v", err)
	}
	if _, err := port.GetReport(t.Context(), "dev"); !errors.Is(err, failure) {
		t.Fatalf("report read failure changed: %v", err)
	}
	if _, err := port.PutReport(t.Context(), environments.Report{Environment: "dev"}); !errors.Is(err, failure) {
		t.Fatalf("report write failure changed: %v", err)
	}
}

func TestEnvironmentReportingProjectionRefusesNonAtomicStoreWithoutFallback(t *testing.T) {
	store := &reportingProjectionStore{}
	port := environmentReportingProjectionPort{repository: store}
	projected := false
	err := port.UpdateAgentReports(t.Context(), "dev", apolloapp.AgentReportPatch{}, func(apolloapp.AgentReportSnapshot) (*environments.Report, error) {
		projected = true
		return &environments.Report{Environment: "dev"}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "does not support atomic") || projected || store.writes != 0 {
		t.Fatalf("non-atomic fallback: err=%v projected=%v writes=%d", err, projected, store.writes)
	}
}
