package controlplane

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// Intentionally has no aggregate Repository embedding or write capability.
type overviewReadOnly struct {
	plans      func(context.Context, string) ([]PlanRecord, error)
	change     func(context.Context, string) (ChangeRequest, error)
	changes    func(context.Context, string) ([]ChangeRequest, error)
	reports    func(context.Context) ([]EnvironmentReport, error)
	promotions func(context.Context, string) ([]PromotionRecord, error)
}

func (r overviewReadOnly) ListPlans(ctx context.Context, filter string) ([]PlanRecord, error) {
	return r.plans(ctx, filter)
}
func (r overviewReadOnly) GetChangeRequest(ctx context.Context, id string) (ChangeRequest, error) {
	return r.change(ctx, id)
}
func (r overviewReadOnly) ListChangeRequests(ctx context.Context, filter string) ([]ChangeRequest, error) {
	return r.changes(ctx, filter)
}
func (r overviewReadOnly) ListEnvironmentReports(ctx context.Context) ([]EnvironmentReport, error) {
	return r.reports(ctx)
}
func (r overviewReadOnly) ListPromotions(ctx context.Context, filter string) ([]PromotionRecord, error) {
	return r.promotions(ctx, filter)
}

var _ OverviewProjectionReader = overviewReadOnly{}

func TestOverviewAcceptsOnlySnapshotReadsAndPreservesLookupFailure(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(t.Context(), key{}, "original")
	calls := []string{}
	check := func(c context.Context, name, filter string) {
		t.Helper()
		if c != ctx || c.Value(key{}) != "original" || filter != "" {
			t.Fatalf("context/filter changed: %s %q", name, filter)
		}
		calls = append(calls, name)
	}
	failure := errors.New("snapshot unavailable")
	r := overviewReadOnly{
		plans: func(c context.Context, f string) ([]PlanRecord, error) {
			check(c, "plans", f)
			return []PlanRecord{{ID: "failed", ChangeRequestID: "change", Plan: Plan{Environment: "private"}, Status: PlanFailed}}, nil
		},
		change: func(c context.Context, id string) (ChangeRequest, error) {
			if c != ctx || id != "change" {
				t.Fatal("lookup changed")
			}
			calls = append(calls, "change")
			return ChangeRequest{}, failure
		},
		changes: func(c context.Context, f string) ([]ChangeRequest, error) {
			check(c, "changes", f)
			return nil, failure
		},
		reports: func(c context.Context) ([]EnvironmentReport, error) { check(c, "reports", ""); return nil, failure },
		promotions: func(c context.Context, f string) ([]PromotionRecord, error) {
			check(c, "promotions", f)
			return nil, failure
		},
	}
	gov := NewGovernanceService(NewMemoryRepository())
	source := newOverviewSource(ctx, r, gov, Principal{Username: "root"}, HandlerOptions{BootstrapAdministrators: []string{"root"}})
	if !reflect.DeepEqual(calls, []string{"plans"}) {
		t.Fatalf("eager reads=%v", calls)
	}
	plans, err := source.VisiblePlans(ctx)
	if err != nil || len(plans) != 0 {
		t.Fatalf("failed Change Request lookup widened Plan visibility: %v %v", plans, err)
	}
	if _, err = source.VisibleChangeRequests(ctx); !errors.Is(err, failure) {
		t.Fatalf("changes error=%v", err)
	}
	if _, err = source.VisibleInstallations(ctx, time.Minute, time.Now()); !errors.Is(err, failure) {
		t.Fatalf("reports error=%v", err)
	}
	if _, err = source.VisiblePromotions(ctx); !errors.Is(err, failure) {
		t.Fatalf("promotions error=%v", err)
	}
	if !reflect.DeepEqual(calls, []string{"plans", "change", "changes", "reports", "promotions"}) {
		t.Fatalf("reads=%v", calls)
	}
	calls = nil
	r.plans = func(c context.Context, f string) ([]PlanRecord, error) { check(c, "plans", f); return nil, failure }
	source = newOverviewSource(ctx, r, gov, Principal{}, HandlerOptions{})
	if _, err = source.VisiblePlans(ctx); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if _, err = source.VisibleInstallations(ctx, time.Minute, time.Now()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"plans"}) {
		t.Fatalf("failed snapshot was reread: %v", calls)
	}
}
