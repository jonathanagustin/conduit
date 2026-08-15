package controlplane

import (
	"context"
	"errors"
	"fmt"

	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
)

// The Environment topology is Environments'. Its predicates, defaults, closed
// value sets and the rule that a read-only copy refuses even a break-glass
// override are all decided there now, and are decidable from three declared
// fields. What is left here is the reading -- resolving the topology from the
// Environment's most recent reviewed generation -- and this package's own error
// vocabulary.

// EnvironmentFederation is Environments' resolved topology, aliased so the call
// sites here are spelled as they were.
type EnvironmentFederation = environments.Federation

const (
	ConnectionUnknown               = environments.ConnectionUnknown
	ConnectionAgentsConnected       = environments.ConnectionAgentsConnected
	ConnectionNoConnectionExpected  = environments.ConnectionNoConnectionExpected
	ConnectionRelayedFromAnotherHub = environments.ConnectionRelayedFromAnotherHub

	SourceEditOnThisHub    = environments.SourceEditOnThisHub
	SourceEditOnAnotherHub = environments.SourceEditOnAnotherHub
	SourceCopyOfAnotherHub = environments.SourceCopyOfAnotherHub

	ErrorInvalidTargetDeclaredStateSource                        = environments.ErrorInvalidTargetDeclaredStateSource
	ErrorCannotImportInEnvironmentWithAuthoritativeDeclaredState = environments.ErrorCannotImportInEnvironmentWithAuthoritativeDeclaredState
)

// validateFederation asks Environments and restates its findings in this
// package's vocabulary. The rules and their messages are Environments'; which
// error class a finding belongs to is ours.
func validateFederation(state DesiredState) []ValidationError {
	overrides := make([]string, len(state.Entities))
	for index, entity := range state.Entities {
		overrides[index] = entity.ConfigOverrides
	}
	issues := environments.ValidateSettings(federationSettings(state.Environment), overrides)
	errs := make([]ValidationError, 0, len(issues))
	for _, issue := range issues {
		errs = append(errs, ValidationError{Path: issue.Path, Message: issue.Message})
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
		return environments.ResolveFederation(environments.FederationSettings{Environment: environment})
	}
	federation := environments.ResolveFederation(federationSettings(state.Environment))
	federation.Present = true
	return federation
}

// resolveFederation is the local spelling of Environments' resolver, narrowing
// an Environment to the three fields the topology rules read.
func resolveFederation(environment Environment) EnvironmentFederation {
	return environments.ResolveFederation(federationSettings(environment))
}

// federationSettings narrows an Environment to the three fields the topology
// rules read.
func federationSettings(environment Environment) environments.FederationSettings {
	return environments.FederationSettings{
		Environment:         environment.ID,
		ConnectionSetting:   environment.ConnectionSetting,
		DeclaredStateSource: environment.DeclaredStateSource,
	}
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

// admitSettingsImport asks Environments whether a Bundle may overwrite this
// Hub's settings, and renders a refusal as this package's conflict error.
func admitSettingsImport(federation EnvironmentFederation) error {
	refusal := environments.RefuseSettingsImport(federation)
	if refusal == nil {
		return nil
	}
	return &FederationError{
		Environment: refusal.Environment, Reason: refusal.Reason, Message: refusal.Message,
	}
}
