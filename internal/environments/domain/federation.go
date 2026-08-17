package domain

import (
	"fmt"
	"strings"
)

// An Environment participates in exactly one Hub topology. Two independent
// settings describe that participation, and both gate behavior rather than only
// labelling it.
//
// ConnectionSetting answers "where does observed state come from?" and so
// decides whether Plans may be issued at all, whether an Agent may lease work,
// and whether the Environment can contribute health to a promotion decision.
//
// DeclaredStateSource answers "which Hub owns the settings?" and so decides
// whether this Hub may modify them and whether an incoming Bundle may overwrite
// them.
//
// Provenance (reviewed 2026-08-02, re-crawled 2026-08-15, connection settings
// re-read in full 2026-08-15):
//   - https://www.palantir.com/docs/apollo/managing-environments/environment-connection-settings/
//   - https://www.palantir.com/docs/apollo/managing-environments/environment-edit-source/
//
// The published page lists five connection settings and, for each, the exact
// characteristics used by the predicates below. "Intermittent agents" is
// identical to "Connected agents" in every published characteristic -- agents
// connect directly, Plans are issued, Bundle import is disallowed, Release
// Channel promotion is supported -- with a single difference: gaps in reported
// state are EXPECTED and are not treated as an outage, and health is shown "as
// of the last time agents connected."
//
// That difference splits into two halves. The operational half -- accepting
// agents, issuing Plans, tracking health, participating in promotion -- is
// pure published conformance and is honoured below via ExpectsDirectAgents so a
// bare-constant addition cannot silently produce an Environment that accepts no
// agents and issues no Plans. The staleness half -- that an expected gap on an
// intermittent Environment must not read as an outage or a stale promotion
// evidence -- is a behaviour change to promotion evidence that threads the
// connection setting into the reporting fleet projection, and is tracked
// separately (issue #65, formerly #25). SilenceIsExpected is the domain
// predicate both halves consult -- this comment named an
// ExpectsContinuousReporting that does not exist and called it unused, while
// the inverted SilenceIsExpected below is what the code actually calls. The
// fleet projection consumes it on both the console overview and the fleet API;
// the promotion-evidence half remains open on #65.
const (
	// ConnectionUnknown is the un-configured state. An Environment in it is not
	// operational: no Agent may connect and no Plan is issued.
	ConnectionUnknown = "UNKNOWN"
	// ConnectionAgentsConnected receives observed state from Agents that connect
	// to this Hub, and expects them to report continuously -- a gap is an outage.
	ConnectionAgentsConnected = "AGENTS_CONNECTED"
	// ConnectionIntermittentAgents receives observed state from Agents that
	// connect directly to this Hub, but expects them to connect only periodically.
	// Operationally it is Connected agents: Plans are issued, promotion is
	// supported, Bundle import is disallowed. It differs only in that a gap in
	// reported state is expected rather than an outage, and health reflects the
	// last connection.
	ConnectionIntermittentAgents = "INTERMITTENT_AGENTS"
	// ConnectionNoConnectionExpected intentionally holds no observed state. It
	// models an Environment that is exported to another Hub and whose reported
	// state cannot be relayed back.
	ConnectionNoConnectionExpected = "NO_CONNECTION_EXPECTED"
	// ConnectionRelayedFromAnotherHub receives observed state from Agents
	// connected to a different Hub, carried in a Bundle. Health is visible here
	// and can gate promotion, but this Hub never issues the Plans.
	ConnectionRelayedFromAnotherHub = "RELAYED_FROM_ANOTHER_HUB"

	// SourceEditOnThisHub makes this Hub authoritative for the Environment's
	// settings. It is the default for a newly created Environment so that an
	// imported Bundle cannot silently overwrite local settings.
	SourceEditOnThisHub = "EDIT_ON_THIS_HUB"
	// SourceEditOnAnotherHub imports settings from a trusted upstream Hub, while
	// still allowing per-Entity config edits here for break-glass recovery.
	SourceEditOnAnotherHub = "EDIT_ON_ANOTHER_HUB"
	// SourceCopyOfAnotherHub is a read-only copy that always matches the upstream
	// Hub. Not even per-Entity break-glass edits are permitted, which is what
	// makes it usable as a standby ready to take over unchanged.
	SourceCopyOfAnotherHub = "COPY_OF_ANOTHER_HUB"
)

// Published rejection reasons for an import that would overwrite settings this
// Hub owns. The public documentation names both; Platform distinguishes them so
// the failure says whether the target is deliberately authoritative or simply
// carries an unusable value.
const (
	ErrorInvalidTargetDeclaredStateSource                        = "InvalidTargetDeclaredStateSource"
	ErrorCannotImportInEnvironmentWithAuthoritativeDeclaredState = "CannotImportInEnvironmentWithAuthoritativeDeclaredStateSource"
)

// Federation is the resolved, defaulted topology for one Environment.
type Federation struct {
	Environment         string `json:"environment"`
	ConnectionSetting   string `json:"connectionSetting"`
	DeclaredStateSource string `json:"declaredStateSource"`
	// Present reports whether this Hub holds a locally reviewed generation for
	// the Environment. A Hub that has never seen the Environment has no settings
	// to protect, so the first Bundle carrying it is a creation rather than an
	// overwrite.
	Present bool `json:"present"`
}

// ExpectsDirectAgents reports whether Agents connect straight to this Hub. Both
// Connected and Intermittent settings do; they differ only in whether a gap is
// an outage, which is SilenceIsExpected's question, not this one. The
// operational predicates below are written against this rather than against a
// single constant so that Intermittent is not accidentally excluded from any of
// them.
func (federation Federation) ExpectsDirectAgents() bool {
	return federation.ConnectionSetting == ConnectionAgentsConnected ||
		federation.ConnectionSetting == ConnectionIntermittentAgents
}

// SilenceIsExpected reports whether a gap in reported state is normal rather
// than a problem. It is true only for Intermittent agents, where agents connect
// periodically by design, so the reporting fleet projection shows an aged
// report with its age but does not count it stale (issue #25).
//
// It is deliberately narrower than "not continuously reporting". A Connected
// agents gap is an outage. A Relayed-from-another-Hub gap is also a concern,
// but a different one -- it means the bundle import that mirrors the upstream
// state has stopped, so the mirror is diverging -- and an operator wants that
// flagged, not suppressed. Only Intermittent has a gap that means nothing is
// wrong, so only Intermittent is here.
func (federation Federation) SilenceIsExpected() bool {
	return federation.ConnectionSetting == ConnectionIntermittentAgents
}

// IssuesPlans reports whether this Hub may compute and hand out work. Only a
// Hub that Agents connect to directly issues Plans; a relayed or
// no-connection Environment is driven elsewhere, and issuing a Plan here would
// create two writers for one Environment.
func (federation Federation) IssuesPlans() bool {
	return federation.ExpectsDirectAgents()
}

// AcceptsAgentConnections reports whether an Agent may lease work from this Hub.
func (federation Federation) AcceptsAgentConnections() bool {
	return federation.ExpectsDirectAgents()
}

// AcceptsRelayedState reports whether observed state may be imported from
// another Hub. Importing relayed state into an Environment whose own Agents
// report here would let a stale Bundle overwrite live observation. Intermittent
// agents report directly, so like Connected it does not accept relayed state.
func (federation Federation) AcceptsRelayedState() bool {
	return federation.ConnectionSetting == ConnectionRelayedFromAnotherHub
}

// TracksHealth reports whether the Environment holds observed state at all.
func (federation Federation) TracksHealth() bool {
	return federation.ExpectsDirectAgents() ||
		federation.ConnectionSetting == ConnectionRelayedFromAnotherHub
}

// ParticipatesInPromotion reports whether this Environment's health may count
// toward a Release promotion decision. An Environment that holds no observed
// state cannot vouch for a Release, and silently counting it as healthy would
// let a canary pass on absent evidence.
//
// Present is half of that, and it used to be missing. An Environment this Hub
// has never reviewed resolves to the defaulted topology -- which is
// AGENTS_CONNECTED, a deliberate divergence from Apollo's UNKNOWN -- so it
// answered TracksHealth() true and vouched. The promotion evidence naming it is
// caller-supplied, so an environment name this Hub had never seen was enough to
// make health evidence count, which is the "absent evidence" case stated
// directly above.
//
// The two halves are different questions and both must hold: Present asks
// whether this Hub has ever reviewed a generation for the Environment, and
// TracksHealth asks whether its declared topology observes health at all. An
// Environment can be genuinely known and still not track health.
//
// Note the asymmetry with RefuseSettingsImport below, which admits a
// not-Present Environment on purpose: a Hub that has never seen an Environment
// has no settings worth protecting, so the first Bundle creates it. Absence is
// permissive there and refusing here, because creating an Environment and
// attesting to a Release's health are not the same act.
func (federation Federation) ParticipatesInPromotion() bool {
	return federation.Present && federation.TracksHealth()
}

// AllowsSettingsEdit reports whether Environment settings may be modified here.
func (federation Federation) AllowsSettingsEdit() bool {
	return federation.DeclaredStateSource == SourceEditOnThisHub
}

// AllowsSettingsImport reports whether an incoming Bundle may replace the
// Environment's settings.
func (federation Federation) AllowsSettingsImport() bool {
	return federation.DeclaredStateSource == SourceEditOnAnotherHub ||
		federation.DeclaredStateSource == SourceCopyOfAnotherHub
}

// AllowsBreakGlassEntityConfig reports whether a single Entity's config may be
// overridden locally while the Environment's settings are owned upstream.
func (federation Federation) AllowsBreakGlassEntityConfig() bool {
	return federation.DeclaredStateSource == SourceEditOnThisHub ||
		federation.DeclaredStateSource == SourceEditOnAnotherHub
}

// The closed sets. Exported because a caller validating a settings document
// renders them into the message an operator reads.
var (
	ConnectionSettings = []string{
		ConnectionUnknown, ConnectionAgentsConnected, ConnectionIntermittentAgents,
		ConnectionNoConnectionExpected, ConnectionRelayedFromAnotherHub,
	}
	DeclaredStateSources = []string{
		SourceEditOnThisHub, SourceEditOnAnotherHub, SourceCopyOfAnotherHub,
	}
)

// FederationSettings is what an Environment declares about its topology, and it
// is the whole of what these rules read.
//
// Taking three fields rather than an Environment is what keeps this package from
// needing to know what else an Environment has on it.
type FederationSettings struct {
	Environment         string
	ConnectionSetting   string
	DeclaredStateSource string
}

// ResolveFederation normalizes the Environment's declared topology.
//
// An absent ConnectionSetting resolves to AGENTS_CONNECTED rather than to
// UNKNOWN. Apollo defaults a newly created Environment to Unknown and
// transitions it in place once an Agent connects, but Platform's Environment
// state is Git-owned and immutable per generation, so it has nowhere to record
// such a transition. Treating "undeclared" as agents-connected keeps existing
// Environments issuing Plans, while an operator who wants the bootstrap gate
// declares UNKNOWN explicitly and clears it by authoring the next generation.
func ResolveFederation(settings FederationSettings) Federation {
	federation := Federation{
		Environment:         settings.Environment,
		ConnectionSetting:   strings.TrimSpace(settings.ConnectionSetting),
		DeclaredStateSource: strings.TrimSpace(settings.DeclaredStateSource),
	}
	if federation.ConnectionSetting == "" {
		federation.ConnectionSetting = ConnectionAgentsConnected
	}
	if federation.DeclaredStateSource == "" {
		federation.DeclaredStateSource = SourceEditOnThisHub
	}
	return federation
}

// ValidateSettings reports what is wrong with an Environment's declared
// topology, and with the Entity config overrides that topology permits.
//
// entityConfigOverrides is indexed the way the caller's entities are, so the
// path in a returned Issue names the entity the caller can find. A read-only
// copy exists so a standby Hub can take over unchanged; allowing even a single
// local override would make it diverge from the Hub it mirrors, so break-glass
// editing is refused there rather than merely discouraged.
func ValidateSettings(settings FederationSettings, entityConfigOverrides []string) []Issue {
	issues := []Issue{}
	if setting := strings.TrimSpace(settings.ConnectionSetting); setting != "" && !contains(ConnectionSettings, setting) {
		issues = append(issues, Issue{
			Path:    "environment.connectionSetting",
			Message: "must be one of " + strings.Join(ConnectionSettings, ", "),
		})
	}
	if source := strings.TrimSpace(settings.DeclaredStateSource); source != "" && !contains(DeclaredStateSources, source) {
		issues = append(issues, Issue{
			Path:    "environment.declaredStateSource",
			Message: "must be one of " + strings.Join(DeclaredStateSources, ", "),
		})
		return issues
	}
	federation := ResolveFederation(settings)
	if federation.AllowsBreakGlassEntityConfig() {
		return issues
	}
	for index, overrides := range entityConfigOverrides {
		if strings.TrimSpace(overrides) == "" {
			continue
		}
		issues = append(issues, Issue{
			Path: fmt.Sprintf("entities[%d].configOverrides", index),
			Message: "declared state source " + federation.DeclaredStateSource +
				" does not permit local Entity config overrides",
		})
	}
	return issues
}

// ImportRefusal is why a Bundle may not overwrite this Environment's settings.
// Reason carries the published error name so an operator can match it against
// the public documentation.
type ImportRefusal struct {
	Environment string
	Reason      string
	Message     string
}

// RefuseSettingsImport reports why an incoming Bundle may not replace this
// Environment's settings, or nil when it may.
//
// The published behavior is a hard rejection naming the declared state source,
// so an operator must deliberately hand ownership upstream before the import can
// succeed. An Environment this Hub has never reviewed is admitted: the first
// Bundle carrying it creates it, and the export options decide the edit source
// it lands with. Only a locally authored Environment has settings worth
// protecting.
func RefuseSettingsImport(federation Federation) *ImportRefusal {
	if !federation.Present || federation.AllowsSettingsImport() {
		return nil
	}
	if !contains(DeclaredStateSources, federation.DeclaredStateSource) {
		return &ImportRefusal{
			Environment: federation.Environment,
			Reason:      ErrorInvalidTargetDeclaredStateSource,
			Message:     "declared state source " + federation.DeclaredStateSource + " is not a recognized value",
		}
	}
	return &ImportRefusal{
		Environment: federation.Environment,
		Reason:      ErrorCannotImportInEnvironmentWithAuthoritativeDeclaredState,
		Message: "this Hub is authoritative for the Environment's settings; " +
			"switch the edit source to " + SourceEditOnAnotherHub + " before importing",
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
