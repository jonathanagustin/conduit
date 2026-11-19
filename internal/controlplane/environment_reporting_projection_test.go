package controlplane

import (
	"context"
	"errors"
	reportadapter "github.com/jonathanagustin/conduit/internal/apollo/adapters/reporting"
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
	err := reportadapter.New(port).UpdateAgentReports(t.Context(), "dev", apolloapp.AgentReportPatch{}, func(apolloapp.AgentReportSnapshot) (*environments.Report, error) {
		projected = true
		return &environments.Report{Environment: "dev"}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "does not support atomic") || projected || store.writes != 0 {
		t.Fatalf("non-atomic fallback: err=%v projected=%v writes=%d", err, projected, store.writes)
	}
}

// This transaction-local source cannot write reports or any other aggregate.
// Requiring Repository again makes the operation invocation fail compilation.
type reportSnapshotReadOnly struct {
	plans   []PlanRecord
	report  environments.Report
	failure error
	reads   []string
}

func (source *reportSnapshotReadOnly) ListPlans(_ context.Context, environment string) ([]PlanRecord, error) {
	source.reads = append(source.reads, "plans:"+environment)
	return source.plans, source.failure
}
func (source *reportSnapshotReadOnly) GetEnvironmentReport(_ context.Context, environment string) (environments.Report, error) {
	source.reads = append(source.reads, "report:"+environment)
	return source.report, source.failure
}

type atomicProjectionReadFixture struct {
	reportingProjectionStore
	source *reportSnapshotReadOnly
	result *environments.Report
}

func (store *atomicProjectionReadFixture) updateReportProjection(_ context.Context, _ string, operation reportProjectionOperation) error {
	_, report, err := operation(store.source, persistedReportProjectionInputs{})
	if err == nil {
		store.result = report
	}
	return err
}

func TestAtomicReportProjectionUsesReadOnlyTransactionSnapshot(t *testing.T) {
	source := &reportSnapshotReadOnly{
		plans:  []PlanRecord{{ID: "plan", Plan: Plan{Environment: "dev", Steps: []PlanStep{{InstallationID: "install", Product: "app", Version: "1.2.3", Namespace: "ns"}}}}},
		report: environments.Report{Environment: "dev"},
	}
	store := &atomicProjectionReadFixture{source: source}
	port := environmentReportingProjectionPort{repository: store}
	calls := 0
	projected := &environments.Report{Environment: "dev", AgentID: "agent"}
	err := reportadapter.New(port).UpdateAgentReports(t.Context(), "dev", apolloapp.AgentReportPatch{}, func(snapshot apolloapp.AgentReportSnapshot) (*environments.Report, error) {
		calls++
		if snapshot.Environment != "dev" || snapshot.Plan.ID != "plan" || len(snapshot.Plan.Steps) != 1 || snapshot.Plan.Steps[0].InstallationID != "install" || snapshot.Report.Environment != "dev" {
			t.Fatalf("wrong transaction snapshot: %+v", snapshot)
		}
		return projected, nil
	})
	if err != nil || calls != 1 || store.result != projected || store.writes != 0 {
		t.Fatalf("projection changed: %v calls=%d result=%+v writes=%d", err, calls, store.result, store.writes)
	}
	if strings.Join(source.reads, ",") != "plans:dev,report:dev" {
		t.Fatalf("unexpected reads %v", source.reads)
	}
}

func TestAtomicReportProjectionPreservesReadFailureBeforeProjection(t *testing.T) {
	failure := errors.New("transaction snapshot unavailable")
	source := &reportSnapshotReadOnly{failure: failure}
	store := &atomicProjectionReadFixture{source: source}
	projected := false
	err := reportadapter.New(environmentReportingProjectionPort{repository: store}).UpdateAgentReports(t.Context(), "dev", apolloapp.AgentReportPatch{}, func(apolloapp.AgentReportSnapshot) (*environments.Report, error) {
		projected = true
		return &environments.Report{}, nil
	})
	if err != failure || projected || store.result != nil || store.writes != 0 {
		t.Fatalf("failed read committed projection: %v projected=%v result=%+v writes=%d", err, projected, store.result, store.writes)
	}
	if strings.Join(source.reads, ",") != "plans:dev" {
		t.Fatalf("continued after failure: %v", source.reads)
	}
}
