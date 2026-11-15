package domain

import (
	"reflect"
	"testing"
	"time"
)

func TestFleetObservationFreshnessAndDriftBoundaries(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name            string
		change          func(*Desired, *Observed, *FleetQuery)
		age             int64
		stale           bool
		rollout, config string
	}{
		{name: "matching report", age: 10, rollout: RolloutUpToDate, config: ConfigApplied},
		{name: "version mismatch", change: func(_ *Desired, o *Observed, _ *FleetQuery) { o.Version = "older" }, age: 10, rollout: RolloutOutOfDate, config: ConfigApplied},
		{name: "release mismatch", change: func(_ *Desired, o *Observed, _ *FleetQuery) { o.ReleaseDigest = "wrong" }, age: 10, rollout: RolloutOutOfDate, config: ConfigApplied},
		{name: "reported rollout in progress", change: func(_ *Desired, o *Observed, _ *FleetQuery) { o.Version = "older"; o.RolloutStatus = "ROLLING" }, age: 10, rollout: "ROLLING", config: ConfigApplied},
		{name: "no desired version", change: func(d *Desired, o *Observed, _ *FleetQuery) { d.Version = ""; o.ReleaseDigest = "wrong" }, age: 10, rollout: RolloutUpToDate, config: ConfigApplied},
		{name: "configuration mismatch", change: func(_ *Desired, o *Observed, _ *FleetQuery) { o.ConfigDigest = "wrong" }, age: 10, rollout: RolloutUpToDate, config: ConfigDrifted},
		{name: "configuration in progress", change: func(_ *Desired, o *Observed, _ *FleetQuery) { o.ConfigDigest = "wrong"; o.ConfigStatus = "APPLYING" }, age: 10, rollout: RolloutUpToDate, config: "APPLYING"},
		{name: "no desired configuration", change: func(d *Desired, o *Observed, _ *FleetQuery) { d.ConfigDigest = ""; o.ConfigDigest = "wrong" }, age: 10, rollout: RolloutUpToDate, config: ConfigApplied},
		{name: "exact stale boundary", change: func(_ *Desired, o *Observed, _ *FleetQuery) { o.ObservedAt = now.Add(-5 * time.Minute) }, age: 300, rollout: RolloutUpToDate, config: ConfigApplied},
		{name: "past stale boundary", change: func(_ *Desired, o *Observed, _ *FleetQuery) { o.ObservedAt = now.Add(-5*time.Minute - time.Second) }, age: 301, stale: true, rollout: RolloutUpToDate, config: ConfigApplied},
		{name: "zero maximum age", change: func(_ *Desired, o *Observed, q *FleetQuery) { o.ObservedAt = now.Add(-time.Hour); q.MaxAge = 0 }, age: 3600, rollout: RolloutUpToDate, config: ConfigApplied},
		{name: "negative maximum age", change: func(_ *Desired, o *Observed, q *FleetQuery) {
			o.ObservedAt = now.Add(-time.Hour)
			q.MaxAge = -time.Second
		}, age: 3600, rollout: RolloutUpToDate, config: ConfigApplied},
		{name: "expected silence", change: func(_ *Desired, o *Observed, q *FleetQuery) {
			o.ObservedAt = now.Add(-time.Hour)
			q.SilenceExpected = map[string]bool{"prod": true}
		}, age: 3600, rollout: RolloutUpToDate, config: ConfigApplied},
		{name: "future observation", change: func(_ *Desired, o *Observed, _ *FleetQuery) { o.ObservedAt = now.Add(time.Minute) }, rollout: RolloutUpToDate, config: ConfigApplied},
		{name: "report time fallback", change: func(_ *Desired, o *Observed, _ *FleetQuery) {
			o.ObservedAt = time.Time{}
			o.ReportedAt = now.Add(-40 * time.Second)
		}, age: 40, rollout: RolloutUpToDate, config: ConfigApplied},
		{name: "no last connection", change: func(_ *Desired, o *Observed, q *FleetQuery) {
			o.ObservedAt = time.Time{}
			o.ReportedAt = time.Time{}
			q.SilenceExpected = map[string]bool{"prod": true}
		}, stale: true, rollout: RolloutUpToDate, config: ConfigApplied},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := Desired{Environment: "prod", InstallationID: "api", Product: "app", Version: "1", ReleaseDigest: "release", ConfigDigest: "config"}
			o := Observed{Environment: "prod", InstallationID: "api", Product: "app", Version: "1", ReleaseDigest: "release", ConfigDigest: "config", Health: HealthHealthy, RolloutStatus: RolloutUpToDate, ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled, ObservedAt: now.Add(-10 * time.Second)}
			q := FleetQuery{MaxAge: 5 * time.Minute}
			if test.change != nil {
				test.change(&d, &o, &q)
			}
			fleet := BuildFleet([]Desired{d}, []Observed{o}, q, now)
			if len(fleet.Installations) != 1 {
				t.Fatalf("installations: %+v", fleet)
			}
			row := fleet.Installations[0]
			if row.ObservationAgeSeconds != test.age || row.Stale != test.stale || row.RolloutStatus != test.rollout || row.ConfigStatus != test.config {
				t.Fatalf("observation: %+v", row)
			}
			if fleet.MaxResultAgeSeconds != test.age {
				t.Fatalf("maximum age: %d", fleet.MaxResultAgeSeconds)
			}
		})
	}
}

func TestFleetSummariesCountEveryStatusBeforeAndAfterAuthorization(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	observed := []Observed{
		{Environment: "allow", InstallationID: "one", Health: HealthHealthy, RolloutStatus: RolloutUpToDate, ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled, ObservedAt: now.Add(-20 * time.Second)},
		{Environment: "allow", InstallationID: "two", Health: HealthUnhealthy, RolloutStatus: RolloutOutOfDate, ConfigStatus: ConfigDrifted, Lifecycle: LifecycleUninstalled, ObservedAt: now.Add(-30 * time.Second)},
		{Environment: "deny", InstallationID: "secret", Health: HealthUnhealthy, RolloutStatus: RolloutOutOfDate, ConfigStatus: ConfigDrifted, Lifecycle: LifecycleUninstalled, ObservedAt: now.Add(-time.Hour)},
	}
	fleet := BuildFleet(nil, observed, FleetQuery{MaxAge: 25 * time.Second}, now)
	before := FleetSummary{Total: 3, Health: map[string]int{HealthHealthy: 1, HealthUnhealthy: 2}, Rollout: map[string]int{RolloutUpToDate: 1, RolloutOutOfDate: 2}, Config: map[string]int{ConfigApplied: 1, ConfigDrifted: 2}, Lifecycle: map[string]int{LifecycleInstalled: 1, LifecycleUninstalled: 2}, Stale: 2}
	if !reflect.DeepEqual(fleet.Summary, before) || fleet.MaxResultAgeSeconds != 3600 {
		t.Fatalf("full summary: %+v age %d", fleet.Summary, fleet.MaxResultAgeSeconds)
	}
	fleet.RetainReadable(func(environment string) bool { return environment == "allow" })
	after := FleetSummary{Total: 2, Health: map[string]int{HealthHealthy: 1, HealthUnhealthy: 1}, Rollout: map[string]int{RolloutUpToDate: 1, RolloutOutOfDate: 1}, Config: map[string]int{ConfigApplied: 1, ConfigDrifted: 1}, Lifecycle: map[string]int{LifecycleInstalled: 1, LifecycleUninstalled: 1}, Stale: 1}
	if !reflect.DeepEqual(fleet.Summary, after) || fleet.MaxResultAgeSeconds != 30 {
		t.Fatalf("readable summary: %+v age %d", fleet.Summary, fleet.MaxResultAgeSeconds)
	}
	fleet.RetainReadable(func(string) bool { return false })
	if fleet.Summary.Total != 0 || fleet.Summary.Stale != 0 || len(fleet.Summary.Health) != 0 || fleet.MaxResultAgeSeconds != 0 || len(fleet.Installations) != 0 {
		t.Fatalf("unreadable counts survived: %+v", fleet)
	}
}

func TestFleetNearMissesHaveStableDimensionValueAndEnvironmentOrder(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	observed := []Observed{
		{Environment: "z", InstallationID: "config-applied", Health: HealthUnhealthy, ConfigStatus: ConfigApplied},
		{Environment: "a", InstallationID: "config-unknown", Health: HealthUnhealthy, ConfigStatus: ConfigIndeterminate},
		{Environment: "z", InstallationID: "health-healthy-z", Health: HealthHealthy, ConfigStatus: ConfigDrifted},
		{Environment: "a", InstallationID: "health-healthy-a", Health: HealthHealthy, ConfigStatus: ConfigDrifted},
		{Environment: "b", InstallationID: "health-unknown", Health: HealthUnknown, ConfigStatus: ConfigDrifted},
		{Environment: "hidden", InstallationID: "two-misses", Health: HealthHealthy, ConfigStatus: ConfigApplied},
		{Environment: "a", InstallationID: "match", Health: HealthUnhealthy, ConfigStatus: ConfigDrifted},
	}
	expected := []FacetContribution{
		{Environment: "z", Dimension: FacetConfig, Value: ConfigApplied},
		{Environment: "a", Dimension: FacetConfig, Value: ConfigIndeterminate},
		{Environment: "a", Dimension: FacetHealth, Value: HealthHealthy},
		{Environment: "z", Dimension: FacetHealth, Value: HealthHealthy},
		{Environment: "b", Dimension: FacetHealth, Value: HealthUnknown},
	}
	for i := 0; i < 20; i++ {
		fleet := BuildFleet(nil, observed, FleetQuery{Health: HealthUnhealthy, Config: ConfigDrifted}, now)
		if !reflect.DeepEqual(fleet.FacetContributions, expected) || fleet.Summary.Total != 1 || len(fleet.Installations) != 1 || fleet.Installations[0].InstallationID != "match" {
			t.Fatalf("near-miss ordering/filtering: %+v", fleet)
		}
	}
}
