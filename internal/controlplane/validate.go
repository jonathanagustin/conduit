package controlplane

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	coordinatePattern                  = regexp.MustCompile(`^[a-zA-Z0-9_.-]+:[a-zA-Z0-9_.-]+$`)
	digestPattern                      = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	dnsLabelPattern                    = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	identifierPattern                  = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)
	releaseChannelPattern              = regexp.MustCompile(`^[A-Z][A-Z0-9_-]*$`)
	dependencyGroupPattern             = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	registryHostPattern                = regexp.MustCompile(`^[A-Za-z0-9.-]+(?::[0-9]+)?$`)
	defaultHostnameTemplatePattern     = regexp.MustCompile(`^\{\{\s*default_hostname\s*(?:\(\s*organizationName\s*:\s*"[^"]+"\s*\))?\s*\}\}$`)
	defaultHostnameTemplateFindPattern = regexp.MustCompile(`\{\{\s*default_hostname\s*(?:\(\s*organizationName\s*:\s*"[^"]+"\s*\))?\s*\}\}`)
)

func Validate(state DesiredState) []ValidationError {
	errs := make([]ValidationError, 0)
	add := func(path, message string) { errs = append(errs, ValidationError{Path: path, Message: message}) }

	if state.APIVersion != "platform.delivery/v1alpha1" {
		add("apiVersion", "must be platform.delivery/v1alpha1")
	}
	if state.Kind != "EnvironmentDeployment" {
		add("kind", "must be EnvironmentDeployment")
	}
	if !dnsLabelPattern.MatchString(state.Environment.ID) {
		add("environment.id", "must be a DNS label")
	}
	if strings.TrimSpace(state.Environment.Generation) == "" {
		add("environment.generation", "must pin a Git commit or other immutable generation")
	}
	if state.Environment.DefaultReleaseChannel != "" && !releaseChannelPattern.MatchString(state.Environment.DefaultReleaseChannel) {
		errs = append(errs, ValidationError{Path: "environment.defaultReleaseChannel", Message: "must be a valid Release Channel name"})
	}
	for label, value := range state.Environment.Labels {
		if strings.TrimSpace(label) == "" || strings.TrimSpace(value) == "" {
			add("environment.labels", "label names and values must not be empty")
		}
	}
	errs = append(errs, validateFederation(state)...)
	validateEnvironmentConfig("environment.config", state.Environment.Config, &errs)
	validateHelmExecutionDefaults("environment.helmExecution", state.Environment.HelmExecution, &errs)
	validateVulnerabilityUpgradePolicy("environment.vulnerabilityUpgrades", state.Environment.VulnerabilityUpgrades, &errs)
	validateProductSettings(state.ProductSettings, &errs)
	validateReleaseChannelDefinitions(state.ReleaseChannels, state.Releases, &errs)
	if state.Environment.AutoSuppressionThreshold < 0 {
		add("environment.autoSuppressionThreshold", "must be positive when configured")
	}
	if policy := state.Environment.ArtifactSecurityPolicy; policy != nil {
		if policy.RequiredOutcome != "PASS" && policy.RequiredOutcome != "ANY" {
			add("environment.artifactSecurityPolicy.requiredOutcome", "must be PASS or ANY")
		}
		if policy.MaximumScanAgeSeconds < 0 {
			add("environment.artifactSecurityPolicy.maximumScanAgeSeconds", "must not be negative")
		}
	}
	if policy := state.Environment.ArtifactAvailabilityPolicy; policy != nil && policy.RequiredOutcome != "AVAILABLE" {
		add("environment.artifactAvailabilityPolicy.requiredOutcome", "must be AVAILABLE")
	}
	assignedRegistryHosts := validateArtifactRegistries(state.Environment.ArtifactRegistries, &errs)
	secretAssignments := validateExternalSecrets(state.Environment.Secrets, &errs)
	validatePromotions(state.Promotions, state.Releases, &errs)
	validateMaintenancePolicy(state.Environment, state.Releases, state.Entities, &errs)
	namespaceRIDs := make(map[string]struct{}, len(state.Environment.Namespaces))
	namespaceNames := make(map[string]struct{}, len(state.Environment.Namespaces))
	for i, namespace := range state.Environment.Namespaces {
		prefix := fmt.Sprintf("environment.namespaces[%d]", i)
		if strings.TrimSpace(namespace.RID) == "" {
			add(prefix+".rid", "must not be empty")
		}
		if _, exists := namespaceRIDs[namespace.RID]; exists {
			add(prefix+".rid", "must be unique")
		}
		namespaceRIDs[namespace.RID] = struct{}{}
		if !dnsLabelPattern.MatchString(namespace.Name) {
			add(prefix+".name", "must be a DNS label")
		}
		if _, exists := namespaceNames[namespace.Name]; exists {
			add(prefix+".name", "must be unique")
		}
		namespaceNames[namespace.Name] = struct{}{}
		organizationNames := make(map[string]struct{}, len(namespace.Organizations))
		for organizationIndex, organization := range namespace.Organizations {
			organizationPath := fmt.Sprintf("%s.organizations[%d]", prefix, organizationIndex)
			if strings.TrimSpace(organization.DisplayName) == "" {
				add(organizationPath+".displayName", "must not be empty")
			}
			if _, exists := organizationNames[organization.DisplayName]; exists {
				add(organizationPath+".displayName", "must be unique within the namespace")
			}
			organizationNames[organization.DisplayName] = struct{}{}
			if strings.TrimSpace(organization.PrimaryHostname) == "" || strings.ContainsAny(organization.PrimaryHostname, " /{}") {
				add(organizationPath+".primaryHostname", "must be a hostname without scheme or path")
			}
		}
	}

	releases := make(map[string]Release, len(state.Releases))
	for i, release := range state.Releases {
		prefix := fmt.Sprintf("releases[%d]", i)
		if !coordinatePattern.MatchString(release.Product) {
			add(prefix+".product", "must be a group:name coordinate")
		}
		if !versionPattern.MatchString(release.Version) {
			add(prefix+".version", "must be an orderable semantic version")
		}
		key := release.Product + ":" + release.Version
		if _, exists := releases[key]; exists {
			add(prefix, "duplicates release "+key)
		}
		releases[key] = release
		validateArtifact(prefix+".chart", release.Chart, &errs)
		validateReleaseHelmExecution(prefix, release, state.Environment.HelmExecution, &errs)
		validateArtifactRegistryAssignment(prefix+".chart", release.Chart, assignedRegistryHosts, &errs)
		if len(release.Images) == 0 {
			add(prefix+".images", "must contain at least one immutable image")
		}
		for imageIndex, image := range release.Images {
			validateArtifact(fmt.Sprintf("%s.images[%d]", prefix, imageIndex), image, &errs)
			validateArtifactRegistryAssignment(
				fmt.Sprintf("%s.images[%d]", prefix, imageIndex), image, assignedRegistryHosts, &errs,
			)
		}
		validateVulnerabilityAssessment(prefix, release, nil, &errs)
		validateArtifactAvailability(prefix, release, nil, &errs)
		seenChannels := make(map[string]struct{}, len(release.Channels))
		for channelIndex, channel := range release.Channels {
			channelPath := fmt.Sprintf("%s.channels[%d]", prefix, channelIndex)
			if !releaseChannelPattern.MatchString(channel) {
				add(channelPath, "must be an uppercase Release Channel identifier")
			}
			if _, exists := seenChannels[channel]; exists {
				add(channelPath, "must be unique")
			}
			seenChannels[channel] = struct{}{}
		}
		for label, value := range release.Labels {
			if strings.TrimSpace(label) == "" || strings.TrimSpace(value) == "" {
				add(prefix+".labels", "label names and values must not be empty")
			}
			if label == SecurityScannedLabel || label == SecurityScanOutcomeLabel {
				add(prefix+".labels["+label+"]", "is scanner-owned and must be derived from vulnerabilityAssessment")
			}
		}
		for dependencyIndex, dependency := range release.Dependencies {
			dependencyPath := fmt.Sprintf("%s.dependencies[%d]", prefix, dependencyIndex)
			if !coordinatePattern.MatchString(dependency.Product) {
				add(dependencyPath+".product", "must be a group:name coordinate")
			}
			if !versionPattern.MatchString(dependency.MinimumVersion) {
				add(dependencyPath+".minimumVersion", "must be an orderable semantic version")
			}
			if !versionMatcherPattern.MatchString(dependency.MaximumVersion) {
				add(dependencyPath+".maximumVersion", "must be an Apollo release version matcher")
			}
			if versionPattern.MatchString(dependency.MinimumVersion) &&
				versionMatcherPattern.MatchString(dependency.MaximumVersion) &&
				!versionWithinMaximum(dependency.MinimumVersion, dependency.MaximumVersion) {
				add(dependencyPath, "minimumVersion must not exceed maximumVersion")
			}
		}
		for incompatibilityIndex, incompatibility := range release.Incompatibilities {
			incompatibilityPath := fmt.Sprintf("%s.incompatibilities[%d]", prefix, incompatibilityIndex)
			if !coordinatePattern.MatchString(incompatibility.Product) {
				add(incompatibilityPath+".product", "must be a group:name coordinate")
			}
			if incompatibility.MinimumVersion != "" && !versionPattern.MatchString(incompatibility.MinimumVersion) {
				add(incompatibilityPath+".minimumVersion", "must be an Apollo orderable version")
			}
			if incompatibility.MaximumVersion != "" && !versionMatcherPattern.MatchString(incompatibility.MaximumVersion) {
				add(incompatibilityPath+".maximumVersion", "must be an Apollo release version matcher")
			}
			if incompatibility.MinimumVersion != "" && incompatibility.MaximumVersion != "" &&
				versionPattern.MatchString(incompatibility.MinimumVersion) &&
				versionMatcherPattern.MatchString(incompatibility.MaximumVersion) &&
				!versionWithinMaximum(incompatibility.MinimumVersion, incompatibility.MaximumVersion) {
				add(incompatibilityPath, "minimumVersion must not exceed maximumVersion")
			}
		}
	}
	validateRecalls(state.Recalls, &errs)
	handoffs := make(map[string]struct{}, len(state.OwnershipHandoffs))
	for index, handoff := range state.OwnershipHandoffs {
		path := fmt.Sprintf("ownershipHandoffs[%d]", index)
		if !dnsLabelPattern.MatchString(handoff.InstallationID) {
			add(path+".installationId", "must be a DNS label")
		}
		if strings.TrimSpace(handoff.Rationale) == "" {
			add(path+".rationale", "is required")
		}
		if _, duplicate := handoffs[handoff.InstallationID]; duplicate {
			add(path+".installationId", "must be unique")
		}
		handoffs[handoff.InstallationID] = struct{}{}
	}

	entityNames := make(map[string]struct{}, len(state.Entities))
	entityProductsByNamespace := make(map[string]string, len(state.Entities))
	// One assignment materializes one Kubernetes Secret per namespace, but the
	// key subset comes from each Entity's own requirement. Two Entities sharing
	// a namespace and disagreeing about the keys therefore describe one Secret
	// two different ways, and the Plan can only carry one of them.
	secretKeysByNamespace := make(map[string]secretKeyClaim, len(state.Entities))
	entitiesByProduct := make(map[string][]Entity, len(state.Entities))
	resolvedEntities := make([]Entity, 0, len(state.Entities))
	for i, entity := range state.Entities {
		prefix := fmt.Sprintf("entities[%d]", i)
		if _, handedOff := handoffs[entity.Name]; handedOff {
			add(prefix+".name", "cannot remain managed while an ownership handoff is requested")
		}
		for label, value := range entity.Labels {
			if strings.TrimSpace(label) == "" || strings.TrimSpace(value) == "" {
				add(prefix+".labels", "label names and values must not be empty")
			}
		}
		if !dnsLabelPattern.MatchString(entity.Name) {
			add(prefix+".name", "must be a DNS label")
		}
		if entity.HelmReleaseName != "" &&
			(!dnsLabelPattern.MatchString(entity.HelmReleaseName) || len(entity.HelmReleaseName) > 53) {
			add(prefix+".helmReleaseName", "must be a DNS label no longer than 53 characters")
		}
		if _, exists := entityNames[entity.Name]; exists {
			add(prefix+".name", "must be unique")
		}
		entityNames[entity.Name] = struct{}{}
		productNamespace := entity.Product + "\x00" + entity.Namespace
		if prior, exists := entityProductsByNamespace[productNamespace]; exists {
			add(prefix+".product", "must be unique within a Kubernetes namespace; already used by "+prior)
		}
		entityProductsByNamespace[productNamespace] = entity.Name
		validateEntityDependencySettings(prefix, entity, &errs)
		validateSecretRequirements(prefix+".secretRequirements", entity.SecretRequirements, secretAssignments, &errs)
		validateSecretKeyAgreement(prefix, entity, secretKeysByNamespace, &errs)
		if entity.HealthQuorum != "" && !contains([]string{"UNKNOWN", "ANY", "ALL", "ALL_BUT_ONE", "SIMPLE_MAJORITY", "NO_GUARANTEE"}, entity.HealthQuorum) {
			add(prefix+".healthQuorum", "must be UNKNOWN, ANY, ALL, ALL_BUT_ONE, SIMPLE_MAJORITY, or NO_GUARANTEE")
		}
		validateVersionedConfigOverrides(prefix+".configOverrides", entity.ConfigOverrides, &errs)
		if (entity.Version == "") == (entity.ReleaseChannel == "") {
			add(prefix, "must specify exactly one of version or releaseChannel")
		}
		if entity.ReleaseChannel != "" {
			if !releaseChannelPattern.MatchString(entity.ReleaseChannel) {
				add(prefix+".releaseChannel", "must be an uppercase Release Channel identifier")
			} else if release, found := latestEligibleRelease(
				state.Releases, state.Promotions, state.Recalls, entity.Product, entity.ReleaseChannel, state.Environment,
			); found {
				entity.Version = release.Version
			} else if !catalogHasReleaseOnChannel(state.Releases, state.Promotions, entity.Product, entity.ReleaseChannel) {
				add(prefix+".releaseChannel", "has no Release in the catalog")
			}
		} else if release, exists := releases[entity.Product+":"+entity.Version]; exists &&
			releaseRecalled(release, state.Recalls, state.Environment.ID) {
			add(prefix+".version", "selects a recalled Release")
		}
		entitiesByProduct[entity.Product] = append(entitiesByProduct[entity.Product], entity)
		resolvedEntities = append(resolvedEntities, entity)
		if entity.Universal == nil {
			if !dnsLabelPattern.MatchString(entity.Namespace) {
				add(prefix+".namespace", "must be a DNS label")
			}
			if len(state.Environment.Namespaces) > 0 {
				if _, exists := namespaceNames[entity.Namespace]; !exists {
					add(prefix+".namespace", "must exist in environment namespace inventory")
				}
			}
		} else {
			if entity.Namespace != "" {
				add(prefix+".namespace", "must be empty for a Universal installation")
			}
			if entity.Configuration != nil {
				add(prefix+".configuration", "must be omitted because Universal managedConfig is authoritative")
			}
			if len(state.Environment.Namespaces) == 0 {
				add(prefix+".universal", "requires environment namespace inventory")
			}
			validateUniversalConfiguration(prefix+".universal", *entity.Universal, secretAssignments, &errs)
		}
		if entity.Configuration != nil {
			validateManagedConfiguration(prefix+".configuration", *entity.Configuration, secretAssignments, &errs)
		}
		if len(entity.HelmValues) > 0 {
			var values any
			if json.Unmarshal(entity.HelmValues, &values) != nil {
				add(prefix+".helmValues", "must contain valid JSON")
			} else if _, object := values.(map[string]any); !object {
				add(prefix+".helmValues", "must be a JSON object")
			} else {
				validateValueTemplates(prefix+".helmValues", values, &errs)
			}
		}
		if entity.Version != "" {
			if release, exists := releases[entity.Product+":"+entity.Version]; !exists {
				add(prefix, "references a release absent from the catalog")
			} else {
				validateSelectedReleasePolicies(prefix, release, state.Environment, &errs)
			}
		}
	}

	for i, entity := range resolvedEntities {
		release, exists := releases[entity.Product+":"+entity.Version]
		if !exists {
			continue
		}
		for _, dependency := range release.Dependencies {
			if contains(entity.IgnoredDependencies, dependency.Product) {
				continue
			}
			dependencyCandidates := entitiesByProduct[dependency.Product]
			installedEntity, installed := matchingEntityDependency(entity, dependency.Product, dependencyCandidates)
			if !installed {
				if dependency.Optional {
					continue
				}
				message := "requires product " + dependency.Product
				if len(dependencyCandidates) > 0 {
					message += " in an allowed dependency group"
				}
				add(fmt.Sprintf("entities[%d]", i), message)
				continue
			}
			if versionPattern.MatchString(installedEntity.Version) &&
				versionPattern.MatchString(dependency.MinimumVersion) &&
				compareVersions(installedEntity.Version, dependency.MinimumVersion) < 0 {
				add(fmt.Sprintf("entities[%d]", i), fmt.Sprintf("requires %s >= %s", dependency.Product, dependency.MinimumVersion))
			}
			if versionPattern.MatchString(installedEntity.Version) &&
				versionMatcherPattern.MatchString(dependency.MaximumVersion) &&
				!versionWithinMaximum(installedEntity.Version, dependency.MaximumVersion) {
				add(fmt.Sprintf("entities[%d]", i), fmt.Sprintf("requires %s <= %s", dependency.Product, dependency.MaximumVersion))
			}
		}
		for _, incompatibility := range release.Incompatibilities {
			installedEntity, installed := matchingEntityDependency(entity, incompatibility.Product, entitiesByProduct[incompatibility.Product])
			if !installed ||
				!incompatibilityApplies(installedEntity.Version, incompatibility) {
				continue
			}
			add(fmt.Sprintf("entities[%d]", i), fmt.Sprintf(
				"is incompatible with %s at version %s", incompatibility.Product, installedEntity.Version,
			))
		}
	}

	sort.SliceStable(errs, func(i, j int) bool {
		if errs[i].Path == errs[j].Path {
			return errs[i].Message < errs[j].Message
		}
		return errs[i].Path < errs[j].Path
	})
	return errs
}

func validateReleaseChannelDefinitions(definitions []ReleaseChannelDefinition, releases []Release, errs *[]ValidationError) {
	byName := make(map[string]ReleaseChannelDefinition, len(definitions))
	for index, definition := range definitions {
		path := fmt.Sprintf("releaseChannels[%d]", index)
		if !releaseChannelPattern.MatchString(definition.Name) {
			*errs = append(*errs, ValidationError{Path: path + ".name", Message: "must be an uppercase Release Channel identifier"})
		}
		if _, duplicate := byName[definition.Name]; duplicate {
			*errs = append(*errs, ValidationError{Path: path + ".name", Message: "must be unique"})
		}
		byName[definition.Name] = definition
		for label, values := range definition.LabelRequirements {
			if strings.TrimSpace(label) == "" || len(values) == 0 {
				*errs = append(*errs, ValidationError{Path: path + ".labelRequirements", Message: "must contain non-empty label IDs and accepted values"})
			}
			for _, value := range values {
				if strings.TrimSpace(value) == "" {
					*errs = append(*errs, ValidationError{Path: path + ".labelRequirements." + label, Message: "accepted values must not be empty"})
				}
			}
		}
	}
	for releaseIndex, release := range releases {
		for _, channel := range release.Channels {
			definition, exists := byName[channel]
			if !exists {
				continue
			}
			if definition.Archived {
				*errs = append(*errs, ValidationError{Path: fmt.Sprintf("releases[%d].channels", releaseIndex), Message: "cannot add a Release to archived channel " + channel})
			}
			for label, accepted := range definition.LabelRequirements {
				if !contains(accepted, release.Labels[label]) {
					*errs = append(*errs, ValidationError{Path: fmt.Sprintf("releases[%d].labels.%s", releaseIndex, label), Message: "does not satisfy Release Channel " + channel})
				}
			}
		}
	}
}

func matchingEntityDependency(entity Entity, product string, candidates []Entity) (Entity, bool) {
	for _, candidate := range candidates {
		if candidate.Namespace == entity.Namespace && dependencyGroupsMatch(entity, candidate, product) {
			return candidate, true
		}
	}
	if len(candidates) == 1 && dependencyGroupsMatch(entity, candidates[0], product) {
		return candidates[0], true
	}
	return Entity{}, false
}

func validateHelmExecutionDefaults(path string, defaults *HelmExecutionDefaults, errs *[]ValidationError) {
	if defaults == nil {
		return
	}
	validateRolloutStrategy(path+".defaultRolloutStrategy", defaults.DefaultRolloutStrategy, errs)
	validateReadinessTimeout(path+".installResourceReadinessTimeoutSeconds", defaults.InstallResourceReadinessTimeoutSeconds, errs)
	validateReadinessTimeout(path+".upgradeResourceReadinessTimeoutSeconds", defaults.UpgradeResourceReadinessTimeoutSeconds, errs)
	if defaults.ImagePrePull != nil {
		validateImagePrePullTimeout(path+".imagePrePull.timeoutSeconds", defaults.ImagePrePull.TimeoutSeconds, errs)
	}
}

func validateReleaseHelmExecution(path string, release Release, defaults *HelmExecutionDefaults, errs *[]ValidationError) {
	validateRolloutStrategy(path+".rolloutStrategy", release.RolloutStrategy, errs)
	validateReadinessTimeout(path+".resourceReadinessTimeoutSeconds", release.ResourceReadinessTimeoutSeconds, errs)
	validateImagePrePullTimeout(path+".imagePrePullTimeoutSeconds", release.ImagePrePullTimeoutSeconds, errs)
	if release.ImagePrePullTimeoutSeconds > 0 && defaults != nil && defaults.ImagePrePull != nil && !defaults.ImagePrePull.Enabled {
		*errs = append(*errs, ValidationError{Path: path + ".imagePrePullTimeoutSeconds", Message: "requires environment image pre-pull to be enabled"})
	}
}

func validateRolloutStrategy(path, strategy string, errs *[]ValidationError) {
	if strategy != "" && strategy != RolloutManageRollout && strategy != RolloutApplyChangesNoWait {
		*errs = append(*errs, ValidationError{Path: path, Message: "must be manageRollout or applyChangesNoWait"})
	}
}

func validateReadinessTimeout(path string, seconds int, errs *[]ValidationError) {
	if seconds < 0 || seconds > 24*60*60 {
		*errs = append(*errs, ValidationError{Path: path, Message: "must be between 1 second and 24 hours when configured"})
	}
}

func validateImagePrePullTimeout(path string, seconds int, errs *[]ValidationError) {
	if seconds != 0 && (seconds < 60 || seconds > 90*60) {
		*errs = append(*errs, ValidationError{Path: path, Message: "must be between 1 and 90 minutes when configured"})
	}
}

func validateVulnerabilityUpgradePolicy(path string, policy *VulnerabilityUpgradePolicy, errs *[]ValidationError) {
	if policy == nil {
		return
	}
	if policy.RescanSLASeconds < 0 {
		*errs = append(*errs, ValidationError{Path: path + ".rescanSlaSeconds", Message: "must be non-negative"})
	}
}

func validateProductSettings(settings []ProductSettings, errs *[]ValidationError) {
	seenProducts := make(map[string]struct{}, len(settings))
	for index, setting := range settings {
		path := fmt.Sprintf("productSettings[%d]", index)
		if !coordinatePattern.MatchString(setting.Product) {
			*errs = append(*errs, ValidationError{Path: path + ".product", Message: "must be a group:name coordinate"})
		}
		if _, duplicate := seenProducts[setting.Product]; duplicate {
			*errs = append(*errs, ValidationError{Path: path + ".product", Message: "must be unique"})
		}
		seenProducts[setting.Product] = struct{}{}
		if setting.RampedRollout != nil {
			seenChannels := make(map[string]struct{}, len(setting.RampedRollout.Windows))
			for windowIndex, window := range setting.RampedRollout.Windows {
				windowPath := fmt.Sprintf("%s.rampedRollout.windows[%d]", path, windowIndex)
				if !releaseChannelPattern.MatchString(window.Channel) {
					*errs = append(*errs, ValidationError{Path: windowPath + ".channel", Message: "must be an uppercase Release Channel identifier"})
				}
				if _, duplicate := seenChannels[window.Channel]; duplicate {
					*errs = append(*errs, ValidationError{Path: windowPath + ".channel", Message: "must be unique"})
				}
				seenChannels[window.Channel] = struct{}{}
				if window.WindowSeconds <= 0 || window.WindowSeconds > 31*24*60*60 {
					*errs = append(*errs, ValidationError{Path: windowPath + ".windowSeconds", Message: "must be between 1 second and 31 days"})
				}
			}
		}
		if pipeline := setting.PromotionPipeline; pipeline != nil {
			seenStages := map[string]struct{}{}
			for stageIndex, stage := range pipeline.Stages {
				stagePath := fmt.Sprintf("%s.promotionPipeline.stages[%d]", path, stageIndex)
				if !releaseChannelPattern.MatchString(stage.FromChannel) || !releaseChannelPattern.MatchString(stage.ToChannel) || stage.FromChannel == stage.ToChannel {
					*errs = append(*errs, ValidationError{Path: stagePath, Message: "must name distinct valid source and target Release Channels"})
				}
				key := stage.FromChannel + "\x00" + stage.ToChannel
				if _, duplicate := seenStages[key]; duplicate {
					*errs = append(*errs, ValidationError{Path: stagePath, Message: "must be unique"})
				}
				seenStages[key] = struct{}{}
				if err := validatePromotionStage(stage.Policy); err != nil {
					*errs = append(*errs, ValidationError{Path: stagePath + ".policy", Message: err.Error()})
				}
			}
		}
	}
}

func validateExternalSecrets(assignments []ExternalSecretAssignment, errs *[]ValidationError) map[string]ExternalSecretAssignment {
	byName := make(map[string]ExternalSecretAssignment, len(assignments))
	for index, assignment := range assignments {
		path := fmt.Sprintf("environment.secrets[%d]", index)
		if !dnsLabelPattern.MatchString(assignment.Name) {
			*errs = append(*errs, ValidationError{Path: path + ".name", Message: "must be a DNS label"})
		}
		if _, exists := byName[assignment.Name]; exists {
			*errs = append(*errs, ValidationError{Path: path + ".name", Message: "must be unique"})
		}
		byName[assignment.Name] = assignment
		if assignment.Revision < 1 {
			*errs = append(*errs, ValidationError{Path: path + ".revision", Message: "must be positive"})
		}
		if strings.TrimSpace(assignment.ProviderRef) == "" || strings.ContainsAny(assignment.ProviderRef, "\r\n\x00") {
			*errs = append(*errs, ValidationError{Path: path + ".providerRef", Message: "must be a non-empty opaque single-line reference"})
		}
		if !dnsLabelPattern.MatchString(assignment.KubernetesSecretName) {
			*errs = append(*errs, ValidationError{Path: path + ".kubernetesSecretName", Message: "must be a Kubernetes Secret DNS label"})
		}
		if len(assignment.Keys) == 0 {
			*errs = append(*errs, ValidationError{Path: path + ".keys", Message: "must contain at least one key name"})
		}
		seenKeys := map[string]struct{}{}
		for keyIndex, key := range assignment.Keys {
			keyPath := fmt.Sprintf("%s.keys[%d]", path, keyIndex)
			if !identifierPattern.MatchString(key) {
				*errs = append(*errs, ValidationError{Path: keyPath, Message: "must be a stable key identifier"})
			}
			if _, exists := seenKeys[key]; exists {
				*errs = append(*errs, ValidationError{Path: keyPath, Message: "must be unique"})
			}
			seenKeys[key] = struct{}{}
		}
	}
	return byName
}

func validateSecretRequirements(
	path string, requirements []SecretRequirement, assignments map[string]ExternalSecretAssignment, errs *[]ValidationError,
) {
	seen := map[string]struct{}{}
	for index, requirement := range requirements {
		requirementPath := fmt.Sprintf("%s[%d]", path, index)
		if !identifierPattern.MatchString(requirement.Name) {
			*errs = append(*errs, ValidationError{Path: requirementPath + ".name", Message: "must be a stable identifier"})
		}
		if _, exists := seen[requirement.Name]; exists {
			*errs = append(*errs, ValidationError{Path: requirementPath + ".name", Message: "must be unique within the Entity"})
		}
		seen[requirement.Name] = struct{}{}
		assignment, exists := assignments[requirement.Secret]
		if !exists {
			*errs = append(*errs, ValidationError{Path: requirementPath + ".secret", Message: "must reference an Environment secret assignment"})
			continue
		}
		if len(requirement.Keys) == 0 {
			*errs = append(*errs, ValidationError{Path: requirementPath + ".keys", Message: "must contain at least one required key"})
		}
		seenKeys := map[string]struct{}{}
		for keyIndex, key := range requirement.Keys {
			keyPath := fmt.Sprintf("%s.keys[%d]", requirementPath, keyIndex)
			if !contains(assignment.Keys, key) {
				*errs = append(*errs, ValidationError{Path: keyPath, Message: "is not exposed by Environment secret " + requirement.Secret})
			}
			if _, duplicate := seenKeys[key]; duplicate {
				*errs = append(*errs, ValidationError{Path: keyPath, Message: "must be unique"})
			}
			seenKeys[key] = struct{}{}
		}
	}
}

func validateEntityDependencySettings(path string, entity Entity, errs *[]ValidationError) {
	if entity.Stack != "" && !dependencyGroupPattern.MatchString(entity.Stack) {
		*errs = append(*errs, ValidationError{Path: path + ".stack", Message: "must be a stack identifier"})
	}
	for product, stack := range entity.DependencyStackOverrides {
		if !coordinatePattern.MatchString(product) {
			*errs = append(*errs, ValidationError{Path: path + ".dependencyStackOverrides", Message: "keys must be group:name coordinates"})
		}
		if !dependencyGroupPattern.MatchString(stack) {
			*errs = append(*errs, ValidationError{Path: path + ".dependencyStackOverrides." + product, Message: "must be a stack identifier"})
		}
	}
	seenGroups := make(map[string]struct{}, len(entity.DependencyGroups))
	for index, group := range entity.DependencyGroups {
		if !dependencyGroupPattern.MatchString(group) {
			*errs = append(*errs, ValidationError{Path: fmt.Sprintf("%s.dependencyGroups[%d]", path, index), Message: "must be a dependency group identifier"})
		}
		if _, exists := seenGroups[group]; exists {
			*errs = append(*errs, ValidationError{Path: path + ".dependencyGroups", Message: "must not contain duplicates"})
		}
		seenGroups[group] = struct{}{}
	}

	seenIgnored := make(map[string]struct{}, len(entity.IgnoredDependencies))
	for index, product := range entity.IgnoredDependencies {
		if !coordinatePattern.MatchString(product) {
			*errs = append(*errs, ValidationError{Path: fmt.Sprintf("%s.ignoredDependencies[%d]", path, index), Message: "must be a group:name coordinate"})
		}
		if _, exists := seenIgnored[product]; exists {
			*errs = append(*errs, ValidationError{Path: path + ".ignoredDependencies", Message: "must not contain duplicates"})
		}
		seenIgnored[product] = struct{}{}
	}

	for product, groups := range entity.DependencyGroupOverrides {
		if !coordinatePattern.MatchString(product) {
			*errs = append(*errs, ValidationError{Path: path + ".dependencyGroupOverrides", Message: "keys must be group:name coordinates"})
		}
		if len(groups) == 0 {
			*errs = append(*errs, ValidationError{Path: path + ".dependencyGroupOverrides." + product, Message: "must contain at least one group or wildcard"})
		}
		seen := make(map[string]struct{}, len(groups))
		for _, group := range groups {
			if group != "*" && !dependencyGroupPattern.MatchString(group) {
				*errs = append(*errs, ValidationError{Path: path + ".dependencyGroupOverrides." + product, Message: "must contain dependency group identifiers or *"})
			}
			if _, exists := seen[group]; exists {
				*errs = append(*errs, ValidationError{Path: path + ".dependencyGroupOverrides." + product, Message: "must not contain duplicates"})
			}
			seen[group] = struct{}{}
		}
	}
}

func dependencyGroupsMatch(dependent, candidate Entity, dependencyProduct string) bool {
	if override, exists := dependent.DependencyGroupOverrides[dependencyProduct]; exists {
		if contains(override, "*") {
			return true
		}
		return stringSetsIntersect(override, effectiveDependencyGroups(candidate))
	}
	return stringSetsIntersect(effectiveDependencyGroups(dependent), effectiveDependencyGroups(candidate))
}

func effectiveDependencyGroups(entity Entity) []string {
	if len(entity.DependencyGroups) == 0 {
		return []string{"default"}
	}
	return entity.DependencyGroups
}

func stringSetsIntersect(left, right []string) bool {
	for _, value := range left {
		if contains(right, value) {
			return true
		}
	}
	return false
}

func incompatibilityApplies(version string, incompatibility Incompatibility) bool {
	if !versionPattern.MatchString(version) {
		return false
	}
	if incompatibility.MinimumVersion != "" && compareVersions(version, incompatibility.MinimumVersion) < 0 {
		return false
	}
	return incompatibility.MaximumVersion == "" || versionWithinMaximum(version, incompatibility.MaximumVersion)
}

func validateSelectedReleasePolicies(path string, release Release, environment Environment, errs *[]ValidationError) {
	if policy := environment.ArtifactSecurityPolicy; policy != nil {
		assessment := release.VulnerabilityAssessment
		if assessment == nil {
			*errs = append(*errs, ValidationError{
				Path:    path + ".release.vulnerabilityAssessment",
				Message: "is required by the Environment artifact security policy",
			})
		} else if policy.RequiredOutcome == "PASS" && assessment.Outcome != "PASS" {
			*errs = append(*errs, ValidationError{
				Path:    path + ".release.vulnerabilityAssessment.outcome",
				Message: "must be PASS under the Environment policy",
			})
		}
	}
	if environment.ArtifactAvailabilityPolicy != nil {
		assessment := release.ArtifactAvailability
		if assessment == nil {
			*errs = append(*errs, ValidationError{
				Path:    path + ".release.artifactAvailability",
				Message: "is required by the Environment artifact availability policy",
			})
		} else if assessment.Outcome != "AVAILABLE" {
			*errs = append(*errs, ValidationError{
				Path:    path + ".release.artifactAvailability.outcome",
				Message: "must be AVAILABLE under the Environment policy",
			})
		}
	}
}

func validateVulnerabilityAssessment(
	path string,
	release Release,
	policy *ArtifactSecurityPolicy,
	errs *[]ValidationError,
) {
	assessment := release.VulnerabilityAssessment
	if assessment == nil {
		if policy != nil {
			*errs = append(*errs, ValidationError{Path: path + ".vulnerabilityAssessment", Message: "is required by the Environment artifact security policy"})
		}
		return
	}
	assessmentPath := path + ".vulnerabilityAssessment"
	if strings.TrimSpace(assessment.Provider) == "" {
		*errs = append(*errs, ValidationError{Path: assessmentPath + ".provider", Message: "must not be empty"})
	}
	if strings.TrimSpace(assessment.RunID) == "" {
		*errs = append(*errs, ValidationError{Path: assessmentPath + ".runId", Message: "must not be empty"})
	}
	if assessment.Status != "COMPLETE" {
		*errs = append(*errs, ValidationError{Path: assessmentPath + ".status", Message: "must be COMPLETE"})
	}
	if assessment.Outcome != "PASS" && assessment.Outcome != "FAIL" {
		*errs = append(*errs, ValidationError{Path: assessmentPath + ".outcome", Message: "must be PASS or FAIL"})
	}
	if assessment.ActiveVulnerabilities < 0 || assessment.SuppressedFindings < 0 || assessment.GracePeriodFindings < 0 {
		*errs = append(*errs, ValidationError{Path: assessmentPath, Message: "finding counts must be non-negative"})
	}
	if assessment.Outcome == "PASS" && assessment.ActiveVulnerabilities != 0 {
		*errs = append(*errs, ValidationError{Path: assessmentPath + ".activeVulnerabilities", Message: "must be zero when outcome is PASS"})
	}
	if assessment.Outcome == "FAIL" && assessment.ActiveVulnerabilities == 0 {
		*errs = append(*errs, ValidationError{Path: assessmentPath + ".activeVulnerabilities", Message: "must be positive when outcome is FAIL"})
	}
	if len(assessment.ActiveFindings) > 0 && len(assessment.ActiveFindings) != assessment.ActiveVulnerabilities {
		*errs = append(*errs, ValidationError{
			Path: assessmentPath + ".activeFindings", Message: "must contain one entry for every active vulnerability when supplied",
		})
	}
	seenFindings := make(map[string]struct{}, len(assessment.ActiveFindings))
	for index, finding := range assessment.ActiveFindings {
		findingPath := fmt.Sprintf("%s.activeFindings[%d]", assessmentPath, index)
		id := strings.TrimSpace(finding.ID)
		if id == "" {
			*errs = append(*errs, ValidationError{Path: findingPath + ".id", Message: "must not be empty"})
		}
		if _, duplicate := seenFindings[id]; duplicate && id != "" {
			*errs = append(*errs, ValidationError{Path: findingPath + ".id", Message: "must be unique"})
		}
		seenFindings[id] = struct{}{}
		if math.IsNaN(finding.CVSS) || math.IsInf(finding.CVSS, 0) || finding.CVSS < 0 || finding.CVSS > RiskScoreMaximum {
			*errs = append(*errs, ValidationError{Path: findingPath + ".cvss", Message: "must be a finite score between 0 and 10"})
		}
	}
	validateSuppressedVulnerabilityFindings(assessmentPath, assessment, errs)
	covered := validateDigestCoverage(assessmentPath, assessment.ArtifactDigests, errs)
	validateArtifactDigestCoverage(assessmentPath, release.Images, covered, errs)
	if policy == nil {
		return
	}
	if policy.RequiredOutcome == "PASS" && assessment.Outcome != "PASS" {
		*errs = append(*errs, ValidationError{Path: assessmentPath + ".outcome", Message: "must be PASS under the Environment policy"})
	}
}

func validateSuppressedVulnerabilityFindings(path string, assessment *VulnerabilityAssessment, errs *[]ValidationError) {
	details := assessment.SuppressedFindingDetails
	if len(details) != assessment.SuppressedFindings {
		*errs = append(*errs, ValidationError{
			Path: path + ".suppressedFindingDetails", Message: "must contain one provenance record for every suppressed finding",
		})
	}
	if len(details) == 0 {
		return
	}
	if assessment.AssessedAt.IsZero() {
		*errs = append(*errs, ValidationError{Path: path + ".assessedAt", Message: "is required when findings are suppressed"})
	}
	covered := make(map[string]struct{}, len(assessment.ArtifactDigests))
	for _, digest := range assessment.ArtifactDigests {
		covered[digest] = struct{}{}
	}
	seen := make(map[string]struct{}, len(details))
	for index, detail := range details {
		detailPath := fmt.Sprintf("%s.suppressedFindingDetails[%d]", path, index)
		findingID := strings.TrimSpace(detail.FindingID)
		if findingID == "" {
			*errs = append(*errs, ValidationError{Path: detailPath + ".findingId", Message: "must not be empty"})
		} else if _, duplicate := seen[findingID]; duplicate {
			*errs = append(*errs, ValidationError{Path: detailPath + ".findingId", Message: "must be unique"})
		}
		seen[findingID] = struct{}{}
		if strings.TrimSpace(detail.VulnerabilityID) == "" {
			*errs = append(*errs, ValidationError{Path: detailPath + ".vulnerabilityId", Message: "must not be empty"})
		}
		if !digestPattern.MatchString(detail.ArtifactDigest) {
			*errs = append(*errs, ValidationError{Path: detailPath + ".artifactDigest", Message: "must be a sha256 digest"})
		} else if _, exists := covered[detail.ArtifactDigest]; !exists {
			*errs = append(*errs, ValidationError{Path: detailPath + ".artifactDigest", Message: "must identify an assessed Release artifact"})
		}
		validateVulnerabilitySuppressionProvenance(detailPath+".suppression", detail.Suppression, assessment.AssessedAt, errs)
	}
}

func validateVulnerabilitySuppressionProvenance(
	path string, provenance VulnerabilitySuppressionProvenance, assessedAt time.Time, errs *[]ValidationError,
) {
	require := func(field, value string) {
		if strings.TrimSpace(value) == "" {
			*errs = append(*errs, ValidationError{Path: path + "." + field, Message: "must not be empty"})
		}
	}
	require("providerId", provenance.ProviderID)
	require("rationale", provenance.Rationale)
	require("requestedBy", provenance.RequestedBy)
	require("approvedBy", provenance.ApprovedBy)
	require("changeRequestId", provenance.ChangeRequestID)

	switch provenance.Scope {
	case VulnerabilitySuppressionGlobal:
		if strings.TrimSpace(provenance.ScopeValue) != "" {
			*errs = append(*errs, ValidationError{Path: path + ".scopeValue", Message: "must be empty for GLOBAL scope"})
		}
	case VulnerabilitySuppressionScanner, VulnerabilitySuppressionImage,
		VulnerabilitySuppressionImagePrefix, VulnerabilitySuppressionDistribution:
		require("scopeValue", provenance.ScopeValue)
	default:
		*errs = append(*errs, ValidationError{Path: path + ".scope", Message: "must be GLOBAL, SCANNER, IMAGE, IMAGE_PREFIX, or DISTRIBUTION"})
	}
	switch provenance.Category {
	case VulnerabilitySuppressionFalsePositive, VulnerabilitySuppressionWontFix,
		VulnerabilitySuppressionVendorDependency, VulnerabilitySuppressionPlannedFix:
	default:
		*errs = append(*errs, ValidationError{Path: path + ".category", Message: "must be FALSE_POSITIVE, WONT_FIX, VENDOR_DEPENDENCY, or PLANNED_FIX"})
	}
	if provenance.CreatedAt.IsZero() {
		*errs = append(*errs, ValidationError{Path: path + ".createdAt", Message: "must not be empty"})
	}
	if provenance.ApprovedAt.IsZero() {
		*errs = append(*errs, ValidationError{Path: path + ".approvedAt", Message: "must not be empty"})
	}
	if provenance.ValidUntil.IsZero() {
		*errs = append(*errs, ValidationError{Path: path + ".validUntil", Message: "must not be empty"})
	}
	if !provenance.CreatedAt.IsZero() && !provenance.ApprovedAt.IsZero() && provenance.ApprovedAt.Before(provenance.CreatedAt) {
		*errs = append(*errs, ValidationError{Path: path + ".approvedAt", Message: "must not precede creation"})
	}
	if !provenance.ApprovedAt.IsZero() && !assessedAt.IsZero() && provenance.ApprovedAt.After(assessedAt) {
		*errs = append(*errs, ValidationError{Path: path + ".approvedAt", Message: "must not follow the assessment"})
	}
	if !provenance.ValidUntil.IsZero() && !assessedAt.IsZero() && !provenance.ValidUntil.After(assessedAt) {
		*errs = append(*errs, ValidationError{Path: path + ".validUntil", Message: "must be active when the assessment completed"})
	}
}

func validateArtifactAvailability(
	path string,
	release Release,
	policy *ArtifactAvailabilityPolicy,
	errs *[]ValidationError,
) {
	assessment := release.ArtifactAvailability
	if assessment == nil {
		if policy != nil {
			*errs = append(*errs, ValidationError{
				Path: path + ".artifactAvailability", Message: "is required by the Environment artifact availability policy",
			})
		}
		return
	}
	assessmentPath := path + ".artifactAvailability"
	if strings.TrimSpace(assessment.Provider) == "" {
		*errs = append(*errs, ValidationError{Path: assessmentPath + ".provider", Message: "must not be empty"})
	}
	if strings.TrimSpace(assessment.RunID) == "" {
		*errs = append(*errs, ValidationError{Path: assessmentPath + ".runId", Message: "must not be empty"})
	}
	if assessment.Status != "COMPLETE" {
		*errs = append(*errs, ValidationError{Path: assessmentPath + ".status", Message: "must be COMPLETE"})
	}
	if assessment.Outcome != "AVAILABLE" && assessment.Outcome != "UNAVAILABLE" {
		*errs = append(*errs, ValidationError{Path: assessmentPath + ".outcome", Message: "must be AVAILABLE or UNAVAILABLE"})
	}
	covered := validateDigestCoverage(assessmentPath, assessment.ArtifactDigests, errs)
	validateArtifactDigestCoverage(assessmentPath, releaseArtifacts(release), covered, errs)
	if policy != nil && assessment.Outcome != "AVAILABLE" {
		*errs = append(*errs, ValidationError{Path: assessmentPath + ".outcome", Message: "must be AVAILABLE under the Environment policy"})
	}
}

func validateArtifactRegistries(
	assignments []ArtifactRegistryAssignment, errs *[]ValidationError,
) map[string]string {
	hosts := make(map[string]string)
	ids := make(map[string]struct{}, len(assignments))
	for index, assignment := range assignments {
		path := fmt.Sprintf("environment.artifactRegistries[%d]", index)
		if !identifierPattern.MatchString(assignment.ID) {
			*errs = append(*errs, ValidationError{Path: path + ".id", Message: "must be a stable identifier"})
		}
		if _, exists := ids[assignment.ID]; exists {
			*errs = append(*errs, ValidationError{Path: path + ".id", Message: "must be unique"})
		}
		ids[assignment.ID] = struct{}{}
		if !registryHostPattern.MatchString(assignment.URL) {
			*errs = append(*errs, ValidationError{Path: path + ".url", Message: "must be a registry host with an optional port and no scheme"})
		}
		if assignment.Revision < 1 {
			*errs = append(*errs, ValidationError{Path: path + ".revision", Message: "must be positive"})
		}
		authentication := assignment.Authentication
		if authentication == "" {
			authentication = "NONE"
		}
		if authentication != "NONE" && authentication != "EXTERNAL" {
			*errs = append(*errs, ValidationError{Path: path + ".authentication", Message: "must be NONE or EXTERNAL"})
		}
		if authentication == "EXTERNAL" && strings.TrimSpace(assignment.CredentialRef) == "" {
			*errs = append(*errs, ValidationError{Path: path + ".credentialRef", Message: "is required for EXTERNAL authentication"})
		}
		if authentication == "NONE" && assignment.CredentialRef != "" {
			*errs = append(*errs, ValidationError{Path: path + ".credentialRef", Message: "must be empty for NONE authentication"})
		}
		if strings.ContainsAny(assignment.CredentialRef, "\r\n\x00") {
			*errs = append(*errs, ValidationError{Path: path + ".credentialRef", Message: "must be an opaque single-line reference"})
		}
		if assignment.ImagePullSecretName != "" && !dnsLabelPattern.MatchString(assignment.ImagePullSecretName) {
			*errs = append(*errs, ValidationError{Path: path + ".imagePullSecretName", Message: "must be a Kubernetes Secret DNS label"})
		}
		registerRegistryHost(path+".url", assignment.URL, assignment.ID, hosts, errs)
		seenMirrors := make(map[string]struct{}, len(assignment.Mirrors))
		for mirrorIndex, mirror := range assignment.Mirrors {
			mirrorPath := fmt.Sprintf("%s.mirrors[%d]", path, mirrorIndex)
			if !registryHostPattern.MatchString(mirror) {
				*errs = append(*errs, ValidationError{Path: mirrorPath, Message: "must be a registry host with an optional port"})
			}
			if _, exists := seenMirrors[mirror]; exists {
				*errs = append(*errs, ValidationError{Path: mirrorPath, Message: "must be unique"})
			}
			seenMirrors[mirror] = struct{}{}
			registerRegistryHost(mirrorPath, mirror, assignment.ID, hosts, errs)
		}
	}
	return hosts
}

func registerRegistryHost(
	path, host, assignmentID string, hosts map[string]string, errs *[]ValidationError,
) {
	if prior, exists := hosts[host]; exists && prior != assignmentID {
		*errs = append(*errs, ValidationError{
			Path: path, Message: "overlaps Artifact Registry assignment " + prior + "; mirror selection must be deterministic",
		})
		return
	}
	hosts[host] = assignmentID
}

func validateArtifactRegistryAssignment(
	path string, artifact Artifact, assignedRegistryHosts map[string]string, errs *[]ValidationError,
) {
	if len(assignedRegistryHosts) == 0 || strings.TrimSpace(artifact.Repository) == "" {
		return
	}
	host := artifactRegistryHost(artifact.Repository)
	if _, exists := assignedRegistryHosts[host]; !exists {
		*errs = append(*errs, ValidationError{
			Path: path + ".repository", Message: "registry " + host + " is not assigned to the Environment",
		})
	}
}

func artifactRegistryHost(repository string) string {
	repository = strings.TrimPrefix(strings.TrimSpace(repository), "oci://")
	if slash := strings.IndexByte(repository, '/'); slash >= 0 {
		return repository[:slash]
	}
	return repository
}

func validateDigestCoverage(path string, digests []string, errs *[]ValidationError) map[string]struct{} {
	covered := make(map[string]struct{}, len(digests))
	for index, digest := range digests {
		itemPath := fmt.Sprintf("%s.artifactDigests[%d]", path, index)
		if !digestPattern.MatchString(digest) {
			*errs = append(*errs, ValidationError{Path: itemPath, Message: "must be an immutable sha256 digest"})
		}
		if _, exists := covered[digest]; exists {
			*errs = append(*errs, ValidationError{Path: itemPath, Message: "must be unique"})
		}
		covered[digest] = struct{}{}
	}
	return covered
}

func validateArtifactDigestCoverage(
	path string, required []Artifact, covered map[string]struct{}, errs *[]ValidationError,
) {
	missing := make(map[string]struct{}, len(required))
	allowed := make(map[string]struct{}, len(required))
	for _, artifact := range required {
		allowed[artifact.Digest] = struct{}{}
		if _, exists := covered[artifact.Digest]; exists {
			continue
		}
		if _, alreadyReported := missing[artifact.Digest]; alreadyReported {
			continue
		}
		missing[artifact.Digest] = struct{}{}
		*errs = append(*errs, ValidationError{
			Path: path + ".artifactDigests", Message: "does not cover release artifact " + artifact.Digest,
		})
	}
	for digest := range covered {
		if _, exists := allowed[digest]; !exists {
			*errs = append(*errs, ValidationError{
				Path: path + ".artifactDigests", Message: "contains digest outside the assessed artifact set " + digest,
			})
		}
	}
}

func validateUniversalConfiguration(
	path string, config UniversalInstallationConfiguration, secrets map[string]ExternalSecretAssignment, errs *[]ValidationError,
) {
	validateManagedConfiguration(path+".managedConfig", config.ManagedConfig, secrets, errs)
	allow := validateIdentifierList(path+".enrollmentAllowlist", config.EnrollmentAllowlist, errs)
	deny := validateIdentifierList(path+".enrollmentDenylist", config.EnrollmentDenylist, errs)
	validateIdentifierList(path+".namespaceDenylist", config.NamespaceDenylist, errs)
	for enrollment := range allow {
		if _, denied := deny[enrollment]; denied {
			*errs = append(*errs, ValidationError{Path: path, Message: "an enrollment cannot be both allowed and denied: " + enrollment})
		}
	}
}

func validateIdentifierList(path string, values []string, errs *[]ValidationError) map[string]struct{} {
	seen := make(map[string]struct{}, len(values))
	for i, value := range values {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		if !identifierPattern.MatchString(value) {
			*errs = append(*errs, ValidationError{Path: itemPath, Message: "must be a non-empty stable identifier"})
		}
		if _, exists := seen[value]; exists {
			*errs = append(*errs, ValidationError{Path: itemPath, Message: "must be unique"})
		}
		seen[value] = struct{}{}
	}
	return seen
}

func validateManagedConfiguration(
	path string, config ManagedInstallationConfiguration, secrets map[string]ExternalSecretAssignment, errs *[]ValidationError,
) {
	validateTemplatedIdentifierList(path+".cbacMarkings", config.CBACMarkings, errs)
	shapes := make(map[string]string)
	validateShapeMap := func(mappingPath string, mapping map[string]json.RawMessage) {
		for shapeID, value := range mapping {
			shapePath := mappingPath + "[" + shapeID + "]"
			if !identifierPattern.MatchString(shapeID) {
				*errs = append(*errs, ValidationError{Path: shapePath, Message: "shape id must be a stable identifier"})
			}
			if prior, exists := shapes[shapeID]; exists {
				*errs = append(*errs, ValidationError{Path: shapePath, Message: "shape is already resolved by " + prior})
			}
			shapes[shapeID] = mappingPath
			var decoded any
			if len(value) == 0 || json.Unmarshal(value, &decoded) != nil {
				*errs = append(*errs, ValidationError{Path: shapePath, Message: "must contain valid JSON"})
				continue
			}
			validateValueTemplates(shapePath, decoded, errs)
		}
	}
	validateShapeMap(path+".inputMapping", config.InputMapping)
	validateShapeMap(path+".presetInputMapping", config.PresetInputMapping)

	for shapeID, input := range config.SecretInputMapping {
		shapePath := path + ".secretInputMapping[" + shapeID + "]"
		if !identifierPattern.MatchString(shapeID) {
			*errs = append(*errs, ValidationError{Path: shapePath, Message: "shape id must be a stable identifier"})
		}
		if prior, exists := shapes[shapeID]; exists {
			*errs = append(*errs, ValidationError{Path: shapePath, Message: "shape is already resolved by " + prior})
		}
		shapes[shapeID] = path + ".secretInputMapping"
		validateSecretInput(shapePath, input, secrets, errs)
	}
}

func validateSecretInput(
	path string, input SecretResolvedInput, secrets map[string]ExternalSecretAssignment, errs *[]ValidationError,
) {
	populated := 0
	if input.EdgePipelineMagritteSource != nil {
		populated++
	}
	if input.Markings != nil {
		populated++
	}
	if input.NamedCredential != nil {
		populated++
	}
	if populated != 1 {
		*errs = append(*errs, ValidationError{Path: path, Message: "must populate exactly one secret-backed input variant"})
		return
	}
	switch input.Type {
	case "edgePipelineMagritteSource":
		if input.EdgePipelineMagritteSource == nil {
			*errs = append(*errs, ValidationError{Path: path, Message: "type does not match populated variant"})
			return
		}
		validateSecretName(path+".edgePipelineMagritteSource.secretName", input.EdgePipelineMagritteSource.SecretName, errs)
		validateSecretAssignmentReference(path+".edgePipelineMagritteSource.secretName", input.EdgePipelineMagritteSource.SecretName, "", secrets, errs)
		var decoded any
		if len(input.EdgePipelineMagritteSource.Config) == 0 || json.Unmarshal(input.EdgePipelineMagritteSource.Config, &decoded) != nil {
			*errs = append(*errs, ValidationError{Path: path + ".edgePipelineMagritteSource.config", Message: "must contain valid JSON"})
		} else {
			validateValueTemplates(path+".edgePipelineMagritteSource.config", decoded, errs)
		}
		if len(input.EdgePipelineMagritteSource.TaskConfig) > 0 {
			if json.Unmarshal(input.EdgePipelineMagritteSource.TaskConfig, &decoded) != nil {
				*errs = append(*errs, ValidationError{Path: path + ".edgePipelineMagritteSource.taskConfig", Message: "must contain valid JSON"})
			} else {
				validateValueTemplates(path+".edgePipelineMagritteSource.taskConfig", decoded, errs)
			}
		}
	case "markings":
		if input.Markings == nil {
			*errs = append(*errs, ValidationError{Path: path, Message: "type does not match populated variant"})
			return
		}
		validateSecretName(path+".markings.secretName", input.Markings.SecretName, errs)
		validateTemplatedIdentifier(path+".markings.secretKeyName", input.Markings.SecretKeyName, "secret key name", errs)
		validateSecretAssignmentReference(path+".markings.secretName", input.Markings.SecretName, input.Markings.SecretKeyName, secrets, errs)
	case "namedCredential":
		if input.NamedCredential == nil {
			*errs = append(*errs, ValidationError{Path: path, Message: "type does not match populated variant"})
			return
		}
		validateSecretName(path+".namedCredential.secretName", input.NamedCredential.SecretName, errs)
		validateSecretAssignmentReference(path+".namedCredential.secretName", input.NamedCredential.SecretName, "", secrets, errs)
	default:
		*errs = append(*errs, ValidationError{Path: path + ".type", Message: "must be edgePipelineMagritteSource, markings, or namedCredential"})
	}
}

func validateSecretAssignmentReference(
	path, secretName, keyName string, secrets map[string]ExternalSecretAssignment, errs *[]ValidationError,
) {
	if strings.Contains(secretName, "{{") {
		return
	}
	assignment, exists := secrets[secretName]
	if !exists {
		*errs = append(*errs, ValidationError{Path: path, Message: "must reference an Environment secret assignment"})
		return
	}
	if keyName != "" && !strings.Contains(keyName, "{{") && !contains(assignment.Keys, keyName) {
		*errs = append(*errs, ValidationError{Path: path, Message: "secret assignment does not expose key " + keyName})
	}
}

func validateSecretName(path, name string, errs *[]ValidationError) {
	validateTemplatedIdentifier(path, name, "secret name", errs)
}

func validateTemplatedIdentifierList(path string, values []string, errs *[]ValidationError) {
	seen := make(map[string]struct{}, len(values))
	for i, value := range values {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		validateTemplatedIdentifier(itemPath, value, "stable identifier", errs)
		if _, exists := seen[value]; exists {
			*errs = append(*errs, ValidationError{Path: itemPath, Message: "must be unique"})
		}
		seen[value] = struct{}{}
	}
}

func validateTemplatedIdentifier(path, value, label string, errs *[]ValidationError) {
	validateValueTemplates(path, value, errs)
	withoutTemplates := defaultHostnameTemplateFindPattern.ReplaceAllString(value, "template")
	if !identifierPattern.MatchString(withoutTemplates) {
		*errs = append(*errs, ValidationError{Path: path, Message: "must be a non-empty " + label})
	}
}

func validateValueTemplates(path string, value any, errs *[]ValidationError) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			validateValueTemplates(path+"."+key, child, errs)
		}
	case []any:
		for i, child := range typed {
			validateValueTemplates(fmt.Sprintf("%s[%d]", path, i), child, errs)
		}
	case string:
		remaining := typed
		for {
			start := strings.Index(remaining, "{{")
			if start == -1 {
				if strings.Contains(remaining, "}}") {
					*errs = append(*errs, ValidationError{Path: path, Message: "contains an unmatched template terminator"})
				}
				return
			}
			endOffset := strings.Index(remaining[start+2:], "}}")
			if endOffset == -1 {
				*errs = append(*errs, ValidationError{Path: path, Message: "contains an unterminated template"})
				return
			}
			end := start + 2 + endOffset + 2
			template := remaining[start:end]
			if !defaultHostnameTemplatePattern.MatchString(template) {
				*errs = append(*errs, ValidationError{Path: path, Message: "contains an unsupported enrollment template: " + template})
			}
			remaining = remaining[end:]
		}
	}
}

func validateArtifact(path string, artifact Artifact, errs *[]ValidationError) {
	add := func(field, message string) {
		*errs = append(*errs, ValidationError{Path: path + "." + field, Message: message})
	}
	if strings.TrimSpace(artifact.Repository) == "" {
		add("repository", "must not be empty")
	}
	if artifact.Tag == "" || artifact.Tag == "latest" {
		add("tag", "must be a specific, non-latest tag")
	}
	if !digestPattern.MatchString(artifact.Digest) {
		add("digest", "must be a lowercase sha256 digest")
	}
}

// secretKeyClaim records which Entity first claimed a key subset for one
// assignment in one namespace, so a disagreement can name both sides.
type secretKeyClaim struct {
	entity string
	keys   []string
}

// validateSecretKeyAgreement refuses two Entities in the same namespace that
// require different key subsets of the same assignment.
//
// buildSecretMutations keys desired secrets on assignment and namespace, so a
// disagreement is not merged or reported: one Entity's requirement silently
// overwrites the other's, and the Plan materializes a Secret missing keys the
// losing Entity declared. Its Plan step still advertises the requirement, so the
// Spoke installs believing it was satisfied and the workload starts against a
// Secret without a key it asked for.
//
// Refused rather than unioned. A union would satisfy both Entities, but it
// would also put every key either workload needs into the one Secret they
// share, which widens secret material by default to resolve an ambiguity the
// author is better placed to settle.
func validateSecretKeyAgreement(
	prefix string, entity Entity, claims map[string]secretKeyClaim, errs *[]ValidationError,
) {
	for index, requirement := range entity.SecretRequirements {
		keys := append([]string(nil), requirement.Keys...)
		sort.Strings(keys)
		scope := requirement.Secret + "\x00" + entity.Namespace
		claimed, exists := claims[scope]
		if !exists {
			claims[scope] = secretKeyClaim{entity: entity.Name, keys: keys}
			continue
		}
		if equalStrings(claimed.keys, keys) {
			continue
		}
		*errs = append(*errs, ValidationError{
			Path: fmt.Sprintf("%s.secretRequirements[%d].keys", prefix, index),
			Message: fmt.Sprintf(
				"must match the keys Entity %s requires from secret %s in namespace %s (%s); "+
					"one Kubernetes Secret is materialized per assignment per namespace",
				claimed.entity, requirement.Secret, entity.Namespace, strings.Join(claimed.keys, ", "),
			),
		})
	}
}
