package domain

import "testing"

// Every axis a fleet query can filter on, asserted to filter on itself and to
// leave the others alone. Only the rollout axis had a test, so an axis that
// matched on the wrong field -- or on nothing -- read exactly the same.
func TestFleetQueryFiltersEachAxisIndependently(t *testing.T) {
	t.Parallel()
	item := FleetInstallation{
		Environment: "prod", InstallationID: "api", Product: "app.example:api",
		Health: HealthHealthy, RolloutStatus: RolloutUpToDate,
		ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled,
	}
	for _, axis := range []struct {
		name       string
		matching   FleetQuery
		mismatched FleetQuery
	}{
		{"environment", FleetQuery{Environment: "prod"}, FleetQuery{Environment: "dev"}},
		{"product", FleetQuery{Product: "app.example:api"}, FleetQuery{Product: "app.example:worker"}},
		{"health", FleetQuery{Health: HealthHealthy}, FleetQuery{Health: HealthUnhealthy}},
		{"rollout", FleetQuery{Rollout: RolloutUpToDate}, FleetQuery{Rollout: RolloutOutOfDate}},
		{"config", FleetQuery{Config: ConfigApplied}, FleetQuery{Config: ConfigDrifted}},
		{"lifecycle", FleetQuery{Lifecycle: LifecycleInstalled}, FleetQuery{Lifecycle: LifecycleUninstalled}},
	} {
		t.Run(axis.name, func(t *testing.T) {
			if !fleetMatches(item, axis.matching) {
				t.Errorf("a matching %s was filtered out", axis.name)
			}
			if fleetMatches(item, axis.mismatched) {
				t.Errorf("a mismatched %s was let through", axis.name)
			}
		})
	}
	if !fleetMatches(item, FleetQuery{}) {
		t.Error("an empty query filtered something out")
	}
	for _, search := range []string{" API ", "PROD", "example:api"} {
		if !fleetMatches(item, FleetQuery{Search: search}) {
			t.Errorf("search %q did not match its Fleet coordinate", search)
		}
	}
	if fleetMatches(item, FleetQuery{Search: "absent"}) {
		t.Error("an absent search term matched")
	}
}
