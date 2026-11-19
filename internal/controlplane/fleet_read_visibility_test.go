package controlplane

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fleetReadSnapshots struct {
	plans                  []PlanRecord
	reports                []EnvironmentReport
	planError, reportError error
	calls                  []string
	contexts               []context.Context
}

func (r *fleetReadSnapshots) ListPlans(ctx context.Context, environment string) ([]PlanRecord, error) {
	r.calls = append(r.calls, "plans:"+environment)
	r.contexts = append(r.contexts, ctx)
	return r.plans, r.planError
}
func (r *fleetReadSnapshots) ListEnvironmentReports(ctx context.Context) ([]EnvironmentReport, error) {
	r.calls = append(r.calls, "reports")
	r.contexts = append(r.contexts, ctx)
	return r.reports, r.reportError
}

var _ FleetProjectionReader = (*fleetReadSnapshots)(nil)

type fleetGrantReadCounter struct {
	Repository
	calls int
}

func (r *fleetGrantReadCounter) ListResourceGrants(ctx context.Context, scope string) ([]ResourceGrant, error) {
	r.calls++
	return r.Repository.ListResourceGrants(ctx, scope)
}

func TestFleetVisibilityPreservesCarrierSpaceAndMemoizedDecisions(t *testing.T) {
	for _, tc := range []struct {
		name        string
		principal   Principal
		dev, hidden bool
	}{
		{"outsider", Principal{Username: "outsider"}, false, false},
		{"reader", Principal{Username: "reader", Teams: []string{"readers"}}, true, false},
		{"carrier administrator", Principal{Username: "root", OnBehalfOf: &Principal{Username: "outsider"}}, true, true},
		{"represented administrator", Principal{Username: "robot", ServiceAccount: true, OnBehalfOf: &Principal{Username: "root"}}, false, false},
		{"delegated reader", Principal{Username: "robot", ServiceAccount: true, OnBehalfOf: &Principal{Username: "reader", Teams: []string{"readers"}}}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := NewMemoryRepository()
			if _, err := repository.PutResourceGrant(t.Context(), ResourceGrant{ID: "read-dev", Revision: 1, Team: "readers", ResourceRID: environmentRID("dev"), Operations: []string{OperationReadResource}}); err != nil {
				t.Fatal(err)
			}
			counter := &fleetGrantReadCounter{Repository: repository}
			governance := NewGovernanceService(counter)
			options := HandlerOptions{BootstrapAdministrators: []string{"root"}}
			visible := fleetReadVisibility(t.Context(), governance, tc.principal, options, indexSpace{})
			if visible("dev") != tc.dev || visible("hidden") != tc.hidden {
				t.Fatal("native carrier authority changed")
			}
			calls := counter.calls
			for range 4 {
				if visible("dev") != tc.dev || visible("hidden") != tc.hidden {
					t.Fatal("memoized decision changed")
				}
			}
			if calls != counter.calls {
				t.Fatal("repeated Plan/report Environments were reauthorized")
			}
			private := fleetReadVisibility(t.Context(), governance, tc.principal, options, indexSpace{id: "private", environments: map[string]struct{}{"dev": {}}})
			calls = counter.calls
			if private("hidden") || counter.calls != calls {
				t.Fatal("Space exclusion queried or widened resource authority")
			}
			if private("dev") != tc.dev {
				t.Fatal("Space membership became authority or lost a permitted read")
			}
		})
	}
}

func TestFleetProjectionAcceptsOnlySnapshotReadsBeforeFacets(t *testing.T) {
	now := time.Date(2026, 11, 19, 4, 0, 0, 0, time.UTC)
	source := &fleetReadSnapshots{plans: []PlanRecord{
		{ID: "p1", Plan: Plan{Environment: "dev", Steps: []PlanStep{{InstallationID: "app", Product: "visible:app", Version: "1", Namespace: "visible"}}}},
		{ID: "p2", Plan: Plan{Environment: "hidden", Steps: []PlanStep{{InstallationID: "secret", Product: "hidden:app", Version: "2", Namespace: "hidden"}}}},
	}, reports: []EnvironmentReport{
		{Environment: "dev", ObservedAt: now, Entities: []ReportedEntity{{InstallationID: "app", Product: "visible:app", Version: "1", Namespace: "visible", Health: HealthHealthy, ObservedAt: now}}},
		{Environment: "hidden", ObservedAt: now, Entities: []ReportedEntity{{InstallationID: "secret", Product: "hidden:app", Version: "2", Namespace: "hidden", Health: HealthUnhealthy, ObservedAt: now}}},
	}}
	repository := NewMemoryRepository()
	governance := NewGovernanceService(repository)
	retain := fleetReadVisibility(t.Context(), governance, Principal{Username: "root"}, HandlerOptions{BootstrapAdministrators: []string{"root"}}, indexSpace{id: "private", environments: map[string]struct{}{"dev": {}}})
	projection, err := buildFleetProjection(t.Context(), source, FleetQuery{Health: HealthUnhealthy}, now, retain)
	if err != nil {
		t.Fatal(err)
	}
	// A healthy visible row is a near miss for this health query. The hidden
	// unhealthy row must affect neither the result nor the counterfactual facet.
	if len(projection.Installations) != 0 || projection.Summary.Total != 0 || projection.Facets.Health[HealthHealthy] != 1 || projection.Facets.Health[HealthUnhealthy] != 0 || projection.Facets.Environment["hidden"] != 0 || projection.Facets.Product["hidden:app"] != 0 {
		t.Fatalf("hidden source reached counts or facets: %#v", projection)
	}
	if !reflect.DeepEqual(source.calls, []string{"plans:", "reports"}) {
		t.Fatalf("snapshot reads=%v", source.calls)
	}
	for _, ctx := range source.contexts {
		if ctx != t.Context() {
			t.Fatal("original read context lost")
		}
	}
	all, err := BuildFleetProjection(t.Context(), source, FleetQuery{}, now)
	if err != nil || all.Summary.Total != 2 {
		t.Fatalf("unfiltered native projection changed: %#v %v", all, err)
	}
}

func TestFleetProjectionSnapshotErrorsRemainOrdered(t *testing.T) {
	sentinel := errors.New("snapshot unavailable")
	for _, stage := range []string{"plans", "reports"} {
		source := &fleetReadSnapshots{}
		want := []string{"plans:"}
		if stage == "plans" {
			source.planError = sentinel
		} else {
			source.reportError = sentinel
			want = append(want, "reports")
		}
		got, err := BuildFleetProjection(t.Context(), source, FleetQuery{}, time.Now())
		if !errors.Is(err, sentinel) || !reflect.DeepEqual(got, FleetProjection{}) || !reflect.DeepEqual(source.calls, want) {
			t.Fatalf("%s result=%#v error=%v calls=%v", stage, got, err, source.calls)
		}
	}
}
