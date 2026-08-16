// Package domain projects what the fleet desires and what it observes, and
// what an operator most needs to look at first, into the views the console
// reads.
//
// It owns its own inputs. A projection that reached back for control-plane
// records would make this package import the monolith, and the monolith calls
// this package — that cycle is why reporting stayed inside it. Desired,
// Observed and the overview summaries are deliberately narrow: three fields of
// an environment report, six of a plan step, and a handful of each record an
// overview panel names are all a view needs. Stating exactly that is what lets
// the conversion live on the legacy side where it belongs.
//
// Where a rule belongs to another context — what makes a plan failed, a change
// request awaiting approval, a promotion blocked — it is not restated here. The
// snapshot arrives already classified, so this package cannot hold a second,
// drifting copy of somebody else's vocabulary. Health is the exception, and a
// deliberate one: reading a health value is what a fleet view is for.
//
// Nothing here reads a repository, a clock or a request. The caller supplies
// the moment, so a projection is a function of its inputs and can be tested
// without a fleet.
package domain

import (
	"sort"
	"strings"
	"time"
)

// PageMetadata mirrors the pagination envelope the HTTP layer already serves.
// It is declared here only so a projection can carry its own page; the canonical
// definition stays with pagination, and the two convert because their fields
// are identical.
type PageMetadata struct {
	PageSize       int    `json:"pageSize"`
	Limit          int    `json:"limit"`
	NextCursor     string `json:"nextCursor,omitempty"`
	PreviousCursor string `json:"previousCursor,omitempty"`
	HasPrevious    bool   `json:"hasPrevious"`
}

// Status vocabulary. These are the strings the fleet already speaks; they are
// restated rather than imported so this package depends on nothing.
const (
	HealthHealthy        = "HEALTHY"
	HealthUnhealthy      = "UNHEALTHY"
	HealthUnknown        = "UNKNOWN"
	RolloutUpToDate      = "UP_TO_DATE"
	RolloutOutOfDate     = "OUT_OF_DATE"
	ConfigApplied        = "APPLIED"
	ConfigDrifted        = "DRIFTED"
	ConfigIndeterminate  = "INDETERMINATE"
	LifecycleInstalled   = "INSTALLED"
	LifecycleUninstalled = "UNINSTALLED"
	LifecycleUnknown     = "UNKNOWN"
)

// Desired is one installation a plan calls for.
type Desired struct {
	Environment    string
	InstallationID string
	Product        string
	Namespace      string
	Version        string
	ReleaseDigest  string
	ConfigDigest   string
}

// Observed is one installation an environment reported, carrying the report's
// own timestamp so an entity that did not state its own can fall back to it.
type Observed struct {
	Environment    string
	InstallationID string
	Product        string
	Namespace      string
	Version        string
	ReleaseDigest  string
	ConfigDigest   string
	Health         string
	RolloutStatus  string
	ConfigStatus   string
	Lifecycle      string
	ObservedAt     time.Time
	ReportedAt     time.Time
}

type FleetQuery struct {
	Environment string
	Product     string
	Health      string
	Rollout     string
	Config      string
	Lifecycle   string
	Search      string
	MaxAge      time.Duration
	// SilenceExpected names the environments whose gap in reporting is expected
	// rather than an outage -- Apollo's Intermittent agents setting, where health
	// is read "as of the last time agents connected". An aged report from one of
	// these is shown with its age (ObservationAgeSeconds is still set) but is not
	// counted stale, because for an intermittent environment silence is normal.
	// It does not suppress the never-reported case: "as of last connection"
	// needs a last connection.
	SilenceExpected map[string]bool
}

type FleetInstallation struct {
	Environment           string    `json:"environment"`
	InstallationID        string    `json:"installationId"`
	Product               string    `json:"product"`
	Namespace             string    `json:"namespace"`
	DesiredVersion        string    `json:"desiredVersion,omitempty"`
	ObservedVersion       string    `json:"observedVersion,omitempty"`
	DesiredReleaseDigest  string    `json:"desiredReleaseDigest,omitempty"`
	ObservedReleaseDigest string    `json:"observedReleaseDigest,omitempty"`
	DesiredConfigDigest   string    `json:"desiredConfigDigest,omitempty"`
	ObservedConfigDigest  string    `json:"observedConfigDigest,omitempty"`
	Health                string    `json:"health"`
	RolloutStatus         string    `json:"rolloutStatus"`
	ConfigStatus          string    `json:"configStatus"`
	Lifecycle             string    `json:"lifecycle"`
	ObservedAt            time.Time `json:"observedAt,omitempty"`
	ObservationAgeSeconds int64     `json:"observationAgeSeconds,omitempty"`
	Stale                 bool      `json:"stale"`
}

type FleetSummary struct {
	Total     int            `json:"total"`
	Health    map[string]int `json:"health"`
	Rollout   map[string]int `json:"rollout"`
	Config    map[string]int `json:"config"`
	Lifecycle map[string]int `json:"lifecycle"`
	Stale     int            `json:"stale"`
}

type FleetProjection struct {
	GeneratedAt         time.Time           `json:"generatedAt"`
	MaxResultAgeSeconds int64               `json:"maxResultAgeSeconds"`
	Installations       []FleetInstallation `json:"installations"`
	Summary             FleetSummary        `json:"summary"`
	PageMetadata
}

func BuildFleet(
	desired []Desired, observed []Observed, query FleetQuery, now time.Time,
) FleetProjection {
	items := make(map[string]FleetInstallation)
	for _, step := range desired {
		items[Key(step.Environment, step.InstallationID)] = FleetInstallation{
			Environment: step.Environment, InstallationID: step.InstallationID, Product: step.Product, Namespace: step.Namespace,
			DesiredVersion: step.Version, DesiredReleaseDigest: step.ReleaseDigest, DesiredConfigDigest: step.ConfigDigest,
			Health: HealthUnknown, RolloutStatus: RolloutOutOfDate, ConfigStatus: ConfigIndeterminate,
			Lifecycle: LifecycleUnknown, Stale: true,
		}
	}
	{
		for _, observed := range observed {
			key := Key(observed.Environment, observed.InstallationID)
			item, exists := items[key]
			if !exists {
				item = FleetInstallation{Environment: observed.Environment, InstallationID: observed.InstallationID}
			}
			item.Product, item.Namespace = observed.Product, observed.Namespace
			item.ObservedVersion, item.ObservedReleaseDigest = observed.Version, observed.ReleaseDigest
			item.ObservedConfigDigest = observed.ConfigDigest
			item.Health, item.RolloutStatus = observed.Health, observed.RolloutStatus
			item.ConfigStatus, item.Lifecycle = observed.ConfigStatus, observed.Lifecycle
			item.ObservedAt = observed.ObservedAt
			if item.ObservedAt.IsZero() {
				item.ObservedAt = observed.ReportedAt
			}
			if !item.ObservedAt.IsZero() {
				age := now.Sub(item.ObservedAt)
				if age < 0 {
					age = 0
				}
				item.ObservationAgeSeconds = int64(age / time.Second)
				item.Stale = query.MaxAge > 0 && age > query.MaxAge && !query.SilenceExpected[item.Environment]
			}
			if item.DesiredVersion != "" && (item.DesiredVersion != item.ObservedVersion ||
				item.DesiredReleaseDigest != item.ObservedReleaseDigest) && item.RolloutStatus == RolloutUpToDate {
				item.RolloutStatus = RolloutOutOfDate
			}
			if item.DesiredConfigDigest != "" && item.DesiredConfigDigest != item.ObservedConfigDigest &&
				item.ConfigStatus == ConfigApplied {
				item.ConfigStatus = ConfigDrifted
			}
			items[key] = item
		}
	}
	projection := FleetProjection{
		GeneratedAt: now,
		Summary:     FleetSummary{Health: map[string]int{}, Rollout: map[string]int{}, Config: map[string]int{}, Lifecycle: map[string]int{}},
	}
	for _, item := range items {
		if !fleetMatches(item, query) {
			continue
		}
		projection.Installations = append(projection.Installations, item)
		projection.Summary.Total++
		projection.Summary.Health[item.Health]++
		projection.Summary.Rollout[item.RolloutStatus]++
		projection.Summary.Config[item.ConfigStatus]++
		projection.Summary.Lifecycle[item.Lifecycle]++
		if item.Stale {
			projection.Summary.Stale++
		}
		if item.ObservationAgeSeconds > projection.MaxResultAgeSeconds {
			projection.MaxResultAgeSeconds = item.ObservationAgeSeconds
		}
	}
	sort.Slice(projection.Installations, func(i, j int) bool {
		if projection.Installations[i].Environment != projection.Installations[j].Environment {
			return projection.Installations[i].Environment < projection.Installations[j].Environment
		}
		return projection.Installations[i].InstallationID < projection.Installations[j].InstallationID
	})
	return projection
}

func fleetMatches(item FleetInstallation, query FleetQuery) bool {
	search := strings.ToLower(strings.TrimSpace(query.Search))
	return (search == "" || strings.Contains(strings.ToLower(item.InstallationID), search) ||
		strings.Contains(strings.ToLower(item.Environment), search) || strings.Contains(strings.ToLower(item.Product), search)) &&
		(query.Environment == "" || item.Environment == query.Environment) &&
		(query.Product == "" || item.Product == query.Product) &&
		(query.Health == "" || item.Health == query.Health) &&
		(query.Rollout == "" || item.RolloutStatus == query.Rollout) &&
		(query.Config == "" || item.ConfigStatus == query.Config) &&
		(query.Lifecycle == "" || item.Lifecycle == query.Lifecycle)
}

// Key is the identity of a fleet row. Exported because pagination and action
// routing key on the same pair, and two spellings of one identity is how a page
// boundary quietly stops matching the rows it is meant to cut.
func Key(environment, installation string) string { return environment + "\x00" + installation }
