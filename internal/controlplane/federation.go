package controlplane

import (
	"context"
	"errors"
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
// Provenance (reviewed 2026-08-02):
//   - https://www.palantir.com/docs/apollo/managing-environments/environment-connection-settings/
//   - https://www.palantir.com/docs/apollo/managing-environments/environment-edit-source/
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

// EnvironmentFederation is the resolved, defaulted topology for one Environment.
type EnvironmentFederation struct {
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
func (federation EnvironmentFederation) IssuesPlans() bool {
	return federation.ConnectionSetting == ConnectionAgentsConnected
}

// AcceptsAgentConnections reports whether an Agent may lease work from this Hub.
func (federation EnvironmentFederation) AcceptsAgentConnections() bool {
	return federation.ConnectionSetting == ConnectionAgentsConnected
}

// AcceptsRelayedState reports whether observed state may be imported from
// another Hub. Importing relayed state into an Environment whose own Agents
// report here would let a stale Bundle overwrite live observation.
func (federation EnvironmentFederation) AcceptsRelayedState() bool {
	return federation.ConnectionSetting == ConnectionRelayedFromAnotherHub
}

// TracksHealth reports whether the Environment holds observed state at all.
func (federation EnvironmentFederation) TracksHealth() bool {
	return federation.ConnectionSetting == ConnectionAgentsConnected ||
		federation.ConnectionSetting == ConnectionRelayedFromAnotherHub
}

// ParticipatesInPromotion reports whether this Environment's health may count
// toward a Release promotion decision. An Environment that holds no observed
// state cannot vouch for a Release, and silently counting it as healthy would
// let a canary pass on absent evidence.
func (federation EnvironmentFederation) ParticipatesInPromotion() bool {
	return federation.TracksHealth()
}

// AllowsSettingsEdit reports whether Environment settings may be modified here.
func (federation EnvironmentFederation) AllowsSettingsEdit() bool {
	return federation.DeclaredStateSource == SourceEditOnThisHub
}

// AllowsSettingsImport reports whether an incoming Bundle may replace the
// Environment's settings.
func (federation EnvironmentFederation) AllowsSettingsImport() bool {
	return federation.DeclaredStateSource == SourceEditOnAnotherHub ||
		federation.DeclaredStateSource == SourceCopyOfAnotherHub
}

// AllowsBreakGlassEntityConfig reports whether a single Entity's config may be
// overridden locally while the Environment's settings are owned upstream.
func (federation EnvironmentFederation) AllowsBreakGlassEntityConfig() bool {
	return federation.DeclaredStateSource == SourceEditOnThisHub ||
		federation.DeclaredStateSource == SourceEditOnAnotherHub
}

var (
	connectionSettings = []string{
		ConnectionUnknown, ConnectionAgentsConnected,
		ConnectionNoConnectionExpected, ConnectionRelayedFromAnotherHub,
	}
	declaredStateSources = []string{
		SourceEditOnThisHub, SourceEditOnAnotherHub, SourceCopyOfAnotherHub,
	}
)

// resolveFederation normalizes the Environment's declared topology.
//
// An absent ConnectionSetting resolves to AGENTS_CONNECTED rather than to
// UNKNOWN. Apollo defaults a newly created Environment to Unknown and
// transitions it in place once an Agent connects, but Platform's Environment
// state is Git-owned and immutable per generation, so it has nowhere to record
// such a transition. Treating "undeclared" as agents-connected keeps existing
// Environments issuing Plans, while an operator who wants the bootstrap gate
// declares UNKNOWN explicitly and clears it by authoring the next generation.
func resolveFederation(environment Environment) EnvironmentFederation {
	federation := EnvironmentFederation{
		Environment:         environment.ID,
		ConnectionSetting:   strings.TrimSpace(environment.ConnectionSetting),
		DeclaredStateSource: strings.TrimSpace(environment.DeclaredStateSource),
	}
	if federation.ConnectionSetting == "" {
		federation.ConnectionSetting = ConnectionAgentsConnected
	}
	if federation.DeclaredStateSource == "" {
		federation.DeclaredStateSource = SourceEditOnThisHub
	}
	return federation
}

func validateFederation(state DesiredState) []ValidationError {
	errs := []ValidationError{}
	environment := state.Environment
	if setting := strings.TrimSpace(environment.ConnectionSetting); setting != "" && !contains(connectionSettings, setting) {
		errs = append(errs, ValidationError{
			Path:    "environment.connectionSetting",
			Message: "must be one of " + strings.Join(connectionSettings, ", "),
		})
	}
	if source := strings.TrimSpace(environment.DeclaredStateSource); source != "" && !contains(declaredStateSources, source) {
		errs = append(errs, ValidationError{
			Path:    "environment.declaredStateSource",
			Message: "must be one of " + strings.Join(declaredStateSources, ", "),
		})
		return errs
	}
	// A read-only copy exists so a standby Hub can take over unchanged. Allowing
	// even a single local Entity config override would make it diverge from the
	// Hub it is meant to mirror, so break-glass editing is refused here and not
	// merely discouraged.
	federation := resolveFederation(environment)
	if !federation.AllowsBreakGlassEntityConfig() {
		for index, entity := range state.Entities {
			if strings.TrimSpace(entity.ConfigOverrides) == "" {
				continue
			}
			errs = append(errs, ValidationError{
				Path: fmt.Sprintf("entities[%d].configOverrides", index),
				Message: "declared state source " + federation.DeclaredStateSource +
					" does not permit local Entity config overrides",
			})
		}
	}
	return errs
}

// environmentFederation resolves the topology recorded in the Environment's most
// recent reviewed desired state. Callers that only hold an Environment name need
// this; callers that already hold the desired state should use resolveFederation
// directly.
//
// An Environment with no reviewed generation yet has nothing to contradict, so it
// resolves to the defaults.
func environmentFederation(
	ctx context.Context, repository Repository, environment string,
) (EnvironmentFederation, error) {
	plans, err := repository.ListPlans(ctx, environment)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return EnvironmentFederation{}, err
	}
	return resolveFederationFromPlans(environment, plans), nil
}

func resolveFederationFromPlans(environment string, plans []PlanRecord) EnvironmentFederation {
	state, known := latestDesiredStates(plans)[environment]
	if !known {
		return resolveFederation(Environment{ID: environment})
	}
	federation := resolveFederation(state.Environment)
	federation.Present = true
	return federation
}

// nonParticipatingEvidenceEnvironments reports health evidence submitted for
// Environments that cannot vouch for a Release. An Environment configured to
// hold no observed state has nothing to attest with, so counting its evidence
// would let a canary pass on health that was never measured.
func nonParticipatingEvidenceEnvironments(
	ctx context.Context, repository Repository, evidence []PromotionHealthEvidence,
) ([]PromotionBlocker, error) {
	checked := make(map[string]bool, len(evidence))
	blockers := make([]PromotionBlocker, 0)
	for _, entry := range evidence {
		if _, seen := checked[entry.Environment]; seen {
			continue
		}
		federation, err := environmentFederation(ctx, repository, entry.Environment)
		if err != nil {
			return nil, err
		}
		checked[entry.Environment] = federation.ParticipatesInPromotion()
		if !checked[entry.Environment] {
			blockers = append(blockers, PromotionBlocker{
				Kind: PromotionBlockerEnvironmentCannotVouch,
				Message: "environment " + entry.Environment + " cannot participate in promotion with connection setting " +
					federation.ConnectionSetting,
				Subjects: []string{entry.Environment},
			})
		}
	}
	return blockers, nil
}

// FederationError reports that an operation is incompatible with the
// Environment's declared topology. Reason carries the published error name when
// one exists so an operator can match it against the public documentation.
type FederationError struct {
	Environment string
	Reason      string
	Message     string
}

func (err *FederationError) Error() string {
	if err.Reason == "" {
		return fmt.Sprintf("environment %s: %s", err.Environment, err.Message)
	}
	return fmt.Sprintf("environment %s: %s: %s", err.Environment, err.Reason, err.Message)
}

func (err *FederationError) Is(target error) bool { return target == ErrConflict }

// admitSettingsImport rejects a Bundle that would overwrite settings this Hub
// owns. The published behavior is a hard rejection naming the declared state
// source, so an operator must deliberately hand ownership upstream before the
// import can succeed.
//
// An Environment this Hub has never reviewed is admitted: the first Bundle
// carrying it creates it, and the export options decide the edit source it lands
// with. Only a locally authored Environment has settings worth protecting.
func admitSettingsImport(federation EnvironmentFederation) error {
	if !federation.Present || federation.AllowsSettingsImport() {
		return nil
	}
	if !contains(declaredStateSources, federation.DeclaredStateSource) {
		return &FederationError{
			Environment: federation.Environment,
			Reason:      ErrorInvalidTargetDeclaredStateSource,
			Message:     "declared state source " + federation.DeclaredStateSource + " is not a recognized value",
		}
	}
	return &FederationError{
		Environment: federation.Environment,
		Reason:      ErrorCannotImportInEnvironmentWithAuthoritativeDeclaredState,
		Message: "this Hub is authoritative for the Environment's settings; " +
			"switch the edit source to " + SourceEditOnAnotherHub + " before importing",
	}
}
