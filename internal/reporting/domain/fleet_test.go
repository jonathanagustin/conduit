package domain

import (
	"testing"
	"time"
)

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
		{"installation", FleetQuery{InstallationID: "api"}, FleetQuery{InstallationID: "API"}},
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

// TestExpectedSilenceSuppressesStalenessButNotAge is the #25 display half. An
// aged report from an environment whose gaps are expected -- Apollo's
// Intermittent agents -- is shown with its age but not counted stale, while the
// same age on an ordinary environment is stale. The age itself is reported
// either way, because "as of the last connection" still needs the age.
func TestExpectedSilenceSuppressesStalenessButNotAge(t *testing.T) {
	t.Parallel()
	base := time.Unix(1_000_000, 0)
	now := base
	desired := []Desired{
		{Environment: "prod", InstallationID: "api", Product: "app:api", Version: "1"},
		{Environment: "edge", InstallationID: "api", Product: "app:api", Version: "1"},
	}
	// Both reported an hour ago; MaxAge is five minutes.
	observed := []Observed{
		{Environment: "prod", InstallationID: "api", Product: "app:api", Version: "1", Health: HealthHealthy, ObservedAt: base.Add(-time.Hour)},
		{Environment: "edge", InstallationID: "api", Product: "app:api", Version: "1", Health: HealthHealthy, ObservedAt: base.Add(-time.Hour)},
	}
	query := FleetQuery{MaxAge: 5 * time.Minute, SilenceExpected: map[string]bool{"edge": true}}

	fleet := BuildFleet(desired, observed, query, now)
	byEnv := map[string]FleetInstallation{}
	for _, item := range fleet.Installations {
		byEnv[item.Environment] = item
	}

	if !byEnv["prod"].Stale {
		t.Error("an aged report from a continuously-reporting environment was not stale")
	}
	if byEnv["edge"].Stale {
		t.Error("an aged report from an intermittent environment was counted stale; its silence is expected")
	}
	// The age is still reported for the intermittent one -- it is shown, not hidden.
	if byEnv["edge"].ObservationAgeSeconds != 3600 {
		t.Errorf("intermittent age = %d, want the real 3600s so it is shown as of last connection", byEnv["edge"].ObservationAgeSeconds)
	}
	if fleet.Summary.Stale != 1 {
		t.Errorf("stale summary = %d, want only the continuously-reporting environment counted", fleet.Summary.Stale)
	}
}

// The property that distinguishes a facet from a summary: a facet excludes its
// own filter, so selecting a value does not zero out its siblings.
//
// This is the whole reason the shape exists. Summary.Health is the health of the
// result, and once health is selected it has exactly one non-zero bucket; a
// filter control built on it would read "HEALTHY 0" beside a fleet that is
// mostly healthy and offer nothing to switch to.
func TestFleetFacetsExcludeTheirOwnDimension(t *testing.T) {
	t.Parallel()
	observed := []Observed{
		{Environment: "prod", InstallationID: "api", Product: "app.example:api",
			Health: HealthHealthy, RolloutStatus: RolloutUpToDate, ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled},
		{Environment: "prod", InstallationID: "worker", Product: "app.example:worker",
			Health: HealthHealthy, RolloutStatus: RolloutOutOfDate, ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled},
		{Environment: "dev", InstallationID: "api", Product: "app.example:api",
			Health: HealthUnhealthy, RolloutStatus: RolloutUpToDate, ConfigStatus: ConfigDrifted, Lifecycle: LifecycleInstalled},
	}
	now := time.Unix(1_700_000_000, 0).UTC()

	unfiltered := BuildFleet(nil, observed, FleetQuery{}, now)
	if got, want := unfiltered.Facets.Health[HealthHealthy], 2; got != want {
		t.Errorf("unfiltered HEALTHY facet = %d, want %d", got, want)
	}
	if got, want := unfiltered.Facets.Health[HealthUnhealthy], 1; got != want {
		t.Errorf("unfiltered UNHEALTHY facet = %d, want %d", got, want)
	}
	// With nothing selected there is nothing to exclude, so a facet is the
	// corresponding summary map. A difference here would mean the near-miss
	// walk is counting items the result already counted.
	if len(unfiltered.FacetContributions) != 0 {
		t.Errorf("an unfiltered query produced %d near misses; nothing can miss a filter that is absent",
			len(unfiltered.FacetContributions))
	}

	selected := BuildFleet(nil, observed, FleetQuery{Health: HealthUnhealthy}, now)
	if got, want := len(selected.Installations), 1; got != want {
		t.Fatalf("selecting UNHEALTHY returned %d installations, want %d", got, want)
	}
	if got, want := selected.Summary.Health[HealthHealthy], 0; got != want {
		t.Errorf("Summary.Health[HEALTHY] = %d, want %d -- the summary describes the result", got, want)
	}
	if got, want := selected.Facets.Health[HealthHealthy], 2; got != want {
		t.Errorf("Facets.Health[HEALTHY] = %d, want %d -- the facet excludes its own filter", got, want)
	}
	// Every other dimension still narrows: only the one unhealthy installation
	// is in the result, so its Environment is the only one the environment
	// facet can count.
	if got, want := selected.Facets.Environment["dev"], 1; got != want {
		t.Errorf("Facets.Environment[dev] = %d, want %d", got, want)
	}
	if got, want := selected.Facets.Environment["prod"], 0; got != want {
		t.Errorf("Facets.Environment[prod] = %d, want %d -- a dimension nobody selected is not excluded", got, want)
	}
}

// Two filters at once. An item that misses both is counted by neither facet,
// which is what keeps a facet a count of "pick this instead of the current
// value" rather than "pick this and drop everything else too".
func TestFleetFacetsCountOnlySingleDimensionNearMisses(t *testing.T) {
	t.Parallel()
	observed := []Observed{
		{Environment: "prod", InstallationID: "api", Health: HealthHealthy, RolloutStatus: RolloutUpToDate},
		{Environment: "prod", InstallationID: "worker", Health: HealthUnhealthy, RolloutStatus: RolloutUpToDate},
		{Environment: "dev", InstallationID: "api", Health: HealthUnhealthy, RolloutStatus: RolloutOutOfDate},
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	projection := BuildFleet(nil, observed,
		FleetQuery{Health: HealthHealthy, Rollout: RolloutUpToDate}, now)

	if got, want := projection.Summary.Total, 1; got != want {
		t.Fatalf("total = %d, want %d", got, want)
	}
	// prod/worker misses health only, so it is the one near miss the health
	// facet counts. dev/api misses both, so it is counted nowhere.
	if got, want := projection.Facets.Health[HealthUnhealthy], 1; got != want {
		t.Errorf("Facets.Health[UNHEALTHY] = %d, want %d", got, want)
	}
	if got, want := projection.Facets.Rollout[RolloutOutOfDate], 0; got != want {
		t.Errorf("Facets.Rollout[OUT_OF_DATE] = %d, want %d -- dev/api also misses health", got, want)
	}
	if got, want := len(projection.FacetContributions), 1; got != want {
		t.Fatalf("near misses = %d, want %d", got, want)
	}
	if projection.FacetContributions[0].Environment != "prod" {
		t.Errorf("a near miss must carry its Environment so the caller can authorize it, got %q",
			projection.FacetContributions[0].Environment)
	}
}

// Search is not a dimension, so it narrows facets rather than being excluded
// from one. Faceting it would offer a count for a state the rail cannot reach.
func TestFleetFacetsAreNarrowedBySearch(t *testing.T) {
	t.Parallel()
	observed := []Observed{
		{Environment: "prod", InstallationID: "api", Health: HealthHealthy},
		{Environment: "prod", InstallationID: "ledger", Health: HealthUnhealthy},
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	projection := BuildFleet(nil, observed, FleetQuery{Search: "ledger", Health: HealthHealthy}, now)

	if got, want := len(projection.Installations), 0; got != want {
		t.Fatalf("installations = %d, want %d", got, want)
	}
	if got, want := projection.Facets.Health[HealthUnhealthy], 1; got != want {
		t.Errorf("Facets.Health[UNHEALTHY] = %d, want %d -- the searched-for row is a health near miss", got, want)
	}
	if got, want := projection.Facets.Health[HealthHealthy], 0; got != want {
		t.Errorf("Facets.Health[HEALTHY] = %d, want %d -- search excludes it from every facet", got, want)
	}
}

// RetainReadable is the authorization rebuild the Hub applies after BuildFleet:
// it drops rows an operator may not read AND rebuilds every derived count over
// what is left, so a facet cannot leak the size of an Environment the caller
// cannot see. The predicate stands in for the Hub's per-Environment check.
func TestRetainReadableRebuildsEveryCountOverTheReadableSet(t *testing.T) {
	t.Parallel()
	observed := []Observed{
		{Environment: "prod", InstallationID: "api", Product: "app.example:api",
			Health: HealthHealthy, RolloutStatus: RolloutUpToDate, ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled},
		{Environment: "prod", InstallationID: "worker", Product: "app.example:worker",
			Health: HealthUnhealthy, RolloutStatus: RolloutUpToDate, ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled},
		{Environment: "secret", InstallationID: "vault", Product: "app.example:vault",
			Health: HealthHealthy, RolloutStatus: RolloutUpToDate, ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled},
	}
	now := time.Unix(1_700_000_000, 0).UTC()

	// Selecting HEALTHY leaves the unhealthy prod row as a near miss, so the
	// contribution walk is exercised too, not only the row walk.
	projection := BuildFleet(nil, observed, FleetQuery{Health: HealthHealthy}, now)

	// Sanity: before authorization, both Environments' healthy rows and the
	// unhealthy near miss are present.
	if got, want := projection.Summary.Total, 2; got != want {
		t.Fatalf("pre-authorization total = %d, want %d", got, want)
	}
	if got := projection.Facets.Environment["secret"]; got == 0 {
		t.Fatalf("test setup: expected the secret Environment to appear before authorization")
	}

	projection.RetainReadable(func(environment string) bool { return environment != "secret" })

	// The secret row is gone from the result and from every count.
	for _, item := range projection.Installations {
		if item.Environment == "secret" {
			t.Fatalf("a row from an unreadable Environment survived RetainReadable")
		}
	}
	if got, want := projection.Summary.Total, 1; got != want {
		t.Errorf("Summary.Total = %d, want %d -- only the readable healthy row remains", got, want)
	}
	if got, want := projection.Facets.Environment["secret"], 0; got != want {
		t.Errorf("Facets.Environment[secret] = %d, want %d -- a facet must not disclose an unreadable Environment's size", got, want)
	}
	// The unhealthy prod near miss is readable, so the HEALTHY facet still
	// excludes its own filter and counts it.
	if got, want := projection.Facets.Health[HealthUnhealthy], 1; got != want {
		t.Errorf("Facets.Health[UNHEALTHY] = %d, want %d -- a readable near miss is still counted", got, want)
	}
	// And the scaffolding is dropped from the response.
	if projection.FacetContributions != nil {
		t.Errorf("FacetContributions survived the rebuild; it is scaffolding, not response")
	}
}

// An administrator-shaped predicate admits everything, so RetainReadable must
// change nothing about the counts a full read produced.
func TestRetainReadableWithATotalPredicateIsIdentityOnCounts(t *testing.T) {
	t.Parallel()
	observed := []Observed{
		{Environment: "prod", InstallationID: "api", Product: "app.example:api",
			Health: HealthHealthy, RolloutStatus: RolloutUpToDate, ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled},
		{Environment: "dev", InstallationID: "api", Product: "app.example:api",
			Health: HealthUnhealthy, RolloutStatus: RolloutOutOfDate, ConfigStatus: ConfigDrifted, Lifecycle: LifecycleInstalled},
	}
	now := time.Unix(1_700_000_000, 0).UTC()

	reference := BuildFleet(nil, observed, FleetQuery{Health: HealthHealthy}, now)
	rebuilt := BuildFleet(nil, observed, FleetQuery{Health: HealthHealthy}, now)
	rebuilt.RetainReadable(func(string) bool { return true })

	if got, want := rebuilt.Summary.Total, reference.Summary.Total; got != want {
		t.Errorf("total after an admit-all rebuild = %d, want %d", got, want)
	}
	if got, want := rebuilt.Facets.Health[HealthUnhealthy], reference.Facets.Health[HealthUnhealthy]; got != want {
		t.Errorf("UNHEALTHY facet after an admit-all rebuild = %d, want %d", got, want)
	}
	if len(rebuilt.Installations) != len(reference.Installations) {
		t.Errorf("admit-all left %d installations, want %d", len(rebuilt.Installations), len(reference.Installations))
	}
}
