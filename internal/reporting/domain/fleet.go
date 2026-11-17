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
	Environment    string
	InstallationID string
	Product        string
	Health         string
	Rollout        string
	Config         string
	Lifecycle      string
	Search         string
	MaxAge         time.Duration
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

// The filterable dimensions, named by the query parameter each one is selected
// with. The names are the JSON keys of FleetFacets and the query parameters the
// HTTP layer reads, deliberately: a facet whose key differs from the parameter
// that selects it is a rail that offers a filter the caller cannot apply, and
// nothing else in the system would notice.
const (
	FacetEnvironment = "environment"
	FacetProduct     = "product"
	FacetHealth      = "health"
	FacetRollout     = "rolloutStatus"
	FacetConfig      = "configStatus"
	FacetLifecycle   = "lifecycle"
)

// FleetFacets counts installations per value of each filterable dimension, with
// that dimension's OWN filter excluded from the count.
//
// Summary answers "what is in the result", and every count in it moves when any
// filter moves. That is the right answer for a headline and the wrong one for a
// filter control: selecting health=UNHEALTHY would collapse Summary.Health to a
// single bucket, and a rail reading "HEALTHY 0" beside a fleet that is mostly
// healthy states something false about the fleet in order to state something
// true about the page. Excluding a dimension from its own facet is what makes
// each count answer the question the control actually poses -- "how many would I
// get if I picked this instead" -- which is also the only reading under which
// the counts can be used to navigate.
//
// A dimension nobody has selected is unaffected: its filter is absent, so
// excluding it changes nothing and the facet equals the corresponding Summary
// map. Stale is not here because it is not a filterable dimension; maxAgeSeconds
// decides staleness rather than selecting on it.
//
// CONDUIT-INVENTED: reporting.FleetFacets
// Displaced-by: a capture or published schema showing Apollo's own fleet/installation filter-count response -- its field names, and whether a selected dimension is excluded from its own count
type FleetFacets struct {
	Environment map[string]int `json:"environment"`
	Product     map[string]int `json:"product"`
	Health      map[string]int `json:"health"`
	Rollout     map[string]int `json:"rolloutStatus"`
	Config      map[string]int `json:"configStatus"`
	Lifecycle   map[string]int `json:"lifecycle"`
}

// NewFleetFacets returns facets with every dimension present and empty, so a
// caller that accumulates into them never writes to a nil map, and a dimension
// with no values serialises as {} rather than null.
func NewFleetFacets() FleetFacets {
	return FleetFacets{
		Environment: map[string]int{}, Product: map[string]int{}, Health: map[string]int{},
		Rollout: map[string]int{}, Config: map[string]int{}, Lifecycle: map[string]int{},
	}
}

// Add records one installation's value under one dimension. An unknown
// dimension is ignored rather than panicking: the dimension names are constants
// in this package and a caller cannot invent one without editing it.
func (facets *FleetFacets) Add(dimension, value string) {
	if counts := facets.dimension(dimension); counts != nil {
		counts[value]++
	}
}

func (facets *FleetFacets) dimension(name string) map[string]int {
	switch name {
	case FacetEnvironment:
		return facets.Environment
	case FacetProduct:
		return facets.Product
	case FacetHealth:
		return facets.Health
	case FacetRollout:
		return facets.Rollout
	case FacetConfig:
		return facets.Config
	case FacetLifecycle:
		return facets.Lifecycle
	}
	return nil
}

// AddInstallation records an installation under every dimension at once, which
// is what an item matching the whole query contributes.
func (facets *FleetFacets) AddInstallation(item FleetInstallation) {
	for _, dimension := range fleetDimensions {
		facets.Add(dimension.name, dimension.of(item))
	}
}

// FacetContribution is one installation's contribution to exactly one facet
// dimension: an installation that matches every part of the query except that
// dimension, and so is counted by that dimension's facet and by nothing else.
//
// It carries its Environment because the HTTP layer authorizes per Environment
// and has to drop a contribution the caller may not see -- a facet count is a
// disclosure like any row. It is never serialised: it is scaffolding the caller
// needs to rebuild Facets after filtering, and a client has no use for it.
type FacetContribution struct {
	Environment string
	Dimension   string
	Value       string
}

type FleetProjection struct {
	GeneratedAt         time.Time           `json:"generatedAt"`
	MaxResultAgeSeconds int64               `json:"maxResultAgeSeconds"`
	Installations       []FleetInstallation `json:"installations"`
	Summary             FleetSummary        `json:"summary"`
	Facets              FleetFacets         `json:"facets"`
	// FacetContributions is the near-miss set: one entry per installation that
	// failed exactly one dimension of the query. Facets above already counts
	// them, so a caller that filters nothing can ignore this entirely.
	FacetContributions []FacetContribution `json:"-"`
	PageMetadata
}

// RetainReadable rebuilds the projection to hold only the installations an
// authorization predicate admits, recomputing every derived count -- the
// summary and the facets, near misses included -- over exactly that set.
//
// It is the reporting half of the fleet's authorization. BuildFleet cannot ask
// who the caller is, so the handler passes the question in as mayRead, keyed on
// the Environment because that is the resource a fleet row is authorized
// against. A facet count is a disclosure the same way a row is, so a near-miss
// contribution is admitted only when its Environment is, and the contribution
// set is then dropped: it was scaffolding for this rebuild, not part of the
// response.
func (projection *FleetProjection) RetainReadable(mayRead func(environment string) bool) {
	visible := make([]FleetInstallation, 0, len(projection.Installations))
	projection.Summary = FleetSummary{Health: map[string]int{}, Rollout: map[string]int{}, Config: map[string]int{}, Lifecycle: map[string]int{}}
	projection.Facets = NewFleetFacets()
	projection.MaxResultAgeSeconds = 0
	for _, item := range projection.Installations {
		if !mayRead(item.Environment) {
			continue
		}
		visible = append(visible, item)
		projection.Summary.Total++
		projection.Summary.Health[item.Health]++
		projection.Summary.Rollout[item.RolloutStatus]++
		projection.Summary.Config[item.ConfigStatus]++
		projection.Summary.Lifecycle[item.Lifecycle]++
		// A row in the result matched every selected dimension, so it counts
		// toward every facet: excluding a dimension from its own facet only ever
		// adds near misses, never removes a match.
		projection.Facets.AddInstallation(item)
		if item.Stale {
			projection.Summary.Stale++
		}
		if item.ObservationAgeSeconds > projection.MaxResultAgeSeconds {
			projection.MaxResultAgeSeconds = item.ObservationAgeSeconds
		}
	}
	for _, contribution := range projection.FacetContributions {
		if mayRead(contribution.Environment) {
			projection.Facets.Add(contribution.Dimension, contribution.Value)
		}
	}
	projection.FacetContributions = nil
	projection.Installations = visible
}

// fleetDimension binds a dimension's name to the installation field it reads
// and the query field that selects it. One table drives matching, faceting and
// the near-miss walk, because a filter that reads one field while its facet
// counts another is a rail whose numbers are about a different question than
// the one the click asks.
type fleetDimension struct {
	name     string
	of       func(FleetInstallation) string
	selected func(FleetQuery) string
}

var fleetDimensions = [...]fleetDimension{
	{FacetEnvironment, func(item FleetInstallation) string { return item.Environment },
		func(query FleetQuery) string { return query.Environment }},
	{FacetProduct, func(item FleetInstallation) string { return item.Product },
		func(query FleetQuery) string { return query.Product }},
	{FacetHealth, func(item FleetInstallation) string { return item.Health },
		func(query FleetQuery) string { return query.Health }},
	{FacetRollout, func(item FleetInstallation) string { return item.RolloutStatus },
		func(query FleetQuery) string { return query.Rollout }},
	{FacetConfig, func(item FleetInstallation) string { return item.ConfigStatus },
		func(query FleetQuery) string { return query.Config }},
	{FacetLifecycle, func(item FleetInstallation) string { return item.Lifecycle },
		func(query FleetQuery) string { return query.Lifecycle }},
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
		Facets:      NewFleetFacets(),
	}
	for _, item := range items {
		// Search is not a dimension and has no facet, so an item the search
		// text excludes contributes to nothing. Faceting it would mean offering
		// counts for a filter combination the caller cannot reach from here.
		if !fleetSearchMatches(item, query) {
			continue
		}
		missed, misses := fleetDimensionMisses(item, query)
		if misses > 1 {
			continue
		}
		if misses == 1 {
			// Matches everything but one selected dimension: counted only by
			// that dimension's facet, which is what "excluding its own filter"
			// means item by item. It is not a result, so it is not an
			// installation, not in Summary, and not in MaxResultAgeSeconds.
			dimension := fleetDimensions[missed]
			contribution := FacetContribution{
				Environment: item.Environment, Dimension: dimension.name, Value: dimension.of(item),
			}
			projection.Facets.Add(contribution.Dimension, contribution.Value)
			projection.FacetContributions = append(projection.FacetContributions, contribution)
			continue
		}
		projection.Installations = append(projection.Installations, item)
		projection.Summary.Total++
		projection.Summary.Health[item.Health]++
		projection.Summary.Rollout[item.RolloutStatus]++
		projection.Summary.Config[item.ConfigStatus]++
		projection.Summary.Lifecycle[item.Lifecycle]++
		projection.Facets.AddInstallation(item)
		if item.Stale {
			projection.Summary.Stale++
		}
		if item.ObservationAgeSeconds > projection.MaxResultAgeSeconds {
			projection.MaxResultAgeSeconds = item.ObservationAgeSeconds
		}
	}
	// Items arrive from a map, so the near-miss set would otherwise be in a
	// different order on every call. Nothing downstream depends on the order --
	// it is counted, not shown -- but an unordered slice makes a test that
	// compares one flaky, and a flaky test gets loosened rather than fixed.
	sort.Slice(projection.FacetContributions, func(i, j int) bool {
		left, right := projection.FacetContributions[i], projection.FacetContributions[j]
		if left.Dimension != right.Dimension {
			return left.Dimension < right.Dimension
		}
		if left.Value != right.Value {
			return left.Value < right.Value
		}
		return left.Environment < right.Environment
	})
	sort.Slice(projection.Installations, func(i, j int) bool {
		if projection.Installations[i].Environment != projection.Installations[j].Environment {
			return projection.Installations[i].Environment < projection.Installations[j].Environment
		}
		return projection.Installations[i].InstallationID < projection.Installations[j].InstallationID
	})
	return projection
}

func fleetMatches(item FleetInstallation, query FleetQuery) bool {
	if !fleetSearchMatches(item, query) {
		return false
	}
	_, misses := fleetDimensionMisses(item, query)
	return misses == 0
}

func fleetSearchMatches(item FleetInstallation, query FleetQuery) bool {
	if query.InstallationID != "" && item.InstallationID != query.InstallationID {
		return false
	}
	search := strings.ToLower(strings.TrimSpace(query.Search))
	return search == "" || strings.Contains(strings.ToLower(item.InstallationID), search) ||
		strings.Contains(strings.ToLower(item.Environment), search) ||
		strings.Contains(strings.ToLower(item.Product), search)
}

// fleetDimensionMisses reports how many selected dimensions the item fails and,
// when exactly one does, its index in fleetDimensions. A dimension the query
// does not select cannot be missed, so an unfiltered query misses nothing and
// every matching item counts toward every facet.
func fleetDimensionMisses(item FleetInstallation, query FleetQuery) (int, int) {
	missed, misses := -1, 0
	for index, dimension := range fleetDimensions {
		selected := dimension.selected(query)
		if selected != "" && dimension.of(item) != selected {
			missed, misses = index, misses+1
			if misses > 1 {
				// Two or more misses is neither a result nor a single facet's
				// near miss, so which ones they were stops mattering.
				return -1, misses
			}
		}
	}
	return missed, misses
}

// Key is the identity of a fleet row. Exported because pagination and action
// routing key on the same pair, and two spellings of one identity is how a page
// boundary quietly stops matching the rows it is meant to cut.
func Key(environment, installation string) string { return environment + "\x00" + installation }
