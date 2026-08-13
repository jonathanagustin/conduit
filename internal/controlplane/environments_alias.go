package controlplane

import (
	"fmt"

	"github.com/jonathanagustin/conduit/internal/environments"
)

// How an Environment describes itself — its profile, and the desired
// configuration governing what may be installed into it — now lives in
// internal/environments. These aliases keep this package's call sites spelled
// as they were.
type (
	EnvironmentProfile = environments.Profile

	ImagePrePullPolicy         = environments.ImagePrePullPolicy
	HelmExecutionDefaults      = environments.HelmExecutionDefaults
	HelmExecutionPolicy        = environments.HelmExecutionPolicy
	ArtifactSecurityPolicy     = environments.ArtifactSecurityPolicy
	ArtifactAvailabilityPolicy = environments.ArtifactAvailabilityPolicy
	VulnerabilityUpgradePolicy = environments.VulnerabilityUpgradePolicy
	ArtifactRegistryAssignment = environments.ArtifactRegistryAssignment
	RegistryAccess             = environments.RegistryAccess
	ExternalSecretAssignment   = environments.ExternalSecretAssignment
	SecretRequirement          = environments.SecretRequirement
	RampedRolloutConstraint    = environments.RampedRolloutConstraint
	Organization               = environments.Organization
	InstalledTarget            = environments.InstalledTarget
	NamespaceTarget            = environments.NamespaceTarget
)

const EnvironmentProfileSourceOperatorAPI = environments.SourceOperatorAPI

func environmentProfilesEqual(left, right EnvironmentProfile) bool {
	return environments.ProfilesEqual(left, right)
}

func environmentProfileProposalIdentity(profile EnvironmentProfile) string {
	return environments.ProposalIdentity(profile)
}

func environmentProfilePolicyResource(profile EnvironmentProfile) map[string]any {
	return environments.PolicyResource(profile)
}

func sortedEnvironmentProfiles(profiles []EnvironmentProfile) { environments.SortProfiles(profiles) }

// validateEnvironmentProfile restates Environments' ErrInvalid as this
// package's ErrConflict, which is what writeLifecycleError turns into a 409.
// Environments is right not to know that; out here the status code has to keep
// coming out the same.
func validateEnvironmentProfile(profile EnvironmentProfile) error {
	if err := environments.ValidateProfile(profile); err != nil {
		return fmt.Errorf("%w: %w", ErrConflict, err)
	}
	return nil
}
