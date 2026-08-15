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
// Provenance (reviewed 2026-08-02, re-crawled 2026-08-15):
//   - https://www.palantir.com/docs/apollo/managing-environments/environment-connection-settings/
//   - https://www.palantir.com/docs/apollo/managing-environments/environment-edit-source/
//
// KNOWN DIVERGENCE, found by the 2026-08-15 re-crawl. Apollo now publishes
// FIVE connection settings; the four below were the whole published set on
// 2026-08-02. The new one is "Intermittent agents": Agents connect directly and
// Plans ARE issued, health is displayed as of the last connection, Bundle
// import stays disallowed, and it supports Release Channel promotion. What
// separates it from Connected agents is that gaps in reported state are
// expected and are NOT an outage.
//
// It is deliberately not added as a bare constant, because the predicates below
// are written as equality against ConnectionAgentsConnected. Adding the state
// without revisiting each of them would produce an Environment that accepts no
// Agents, issues no Plans and cannot promote -- the opposite of what Apollo
// publishes. The staleness rule is the harder half: Conduit reads a stale
// report as HealthUnknown, which is right for a continuously-connected
// Environment and wrong for an intermittent one, where the same silence is
// expected. Closing this is a behaviour change to promotion evidence and needs
// deciding, not defaulting.
const (
	// ConnectionUnknown is the un-configured state. An Environment in it is not
	// operational: no Agent may connect and no Plan is issued.
	ConnectionUnknown = "UNKNOWN"
	// ConnectionAgentsConnected receives observed state from Agents that connect
	// to this Hub.
	ConnectionAgentsConnected = "AGENTS_CONNECTED"
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

// IssuesPlans reports whether this Hub may compute and hand out work. Only a
// Hub that Agents connect to directly issues Plans; a relayed or
// no-connection Environment is driven elsewhere, and issuing a Plan here would
// create two writers for one Environment.
func (federation Federation) IssuesPlans() bool {
	return federation.ConnectionSetting == ConnectionAgentsConnected
}

// AcceptsAgentConnections reports whether an Agent may lease work from this Hub.
func (federation Federation) AcceptsAgentConnections() bool {
	return federation.ConnectionSetting == ConnectionAgentsConnected
}

// AcceptsRelayedState reports whether observed state may be imported from
// another Hub. Importing relayed state into an Environment whose own Agents
// report here would let a stale Bundle overwrite live observation.
func (federation Federation) AcceptsRelayedState() bool {
	return federation.ConnectionSetting == ConnectionRelayedFromAnotherHub
}

// TracksHealth reports whether the Environment holds observed state at all.
func (federation Federation) TracksHealth() bool {
	return federation.ConnectionSetting == ConnectionAgentsConnected ||
		federation.ConnectionSetting == ConnectionRelayedFromAnotherHub
}

// ParticipatesInPromotion reports whether this Environment's health may count
// toward a Release promotion decision. An Environment that holds no observed
// state cannot vouch for a Release, and silently counting it as healthy would
// let a canary pass on absent evidence.
func (federation Federation) ParticipatesInPromotion() bool {
	return federation.TracksHealth()
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
		ConnectionUnknown, ConnectionAgentsConnected,
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
