package controlplane

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type concreteInstallation struct {
	ID           string
	Entity       Entity
	Namespace    string
	NamespaceRID string
	ConfigDigest string
	Values       []byte
	SecretAccess []SecretAccess
}

func BuildPlan(state DesiredState) (Plan, error) {
	return BuildPlanWithObserved(state, EnvironmentReport{}, time.Now().UTC())
}

func BuildPlanWithObserved(state DesiredState, observed EnvironmentReport, now time.Time) (Plan, error) {
	return buildPlanWithObserved(state, observed, now, true)
}

// buildPlanWithObserved can defer the execution-evidence budget while a
// caller is deriving a narrower command Plan from the complete Environment.
// Every executable result must pass validatePlanExecutionTaskBudget after its
// final step and mutation projection is known.
func buildPlanWithObserved(state DesiredState, observed EnvironmentReport, now time.Time, enforceExecutionBudget bool) (Plan, error) {
	state = withAutomaticSecurityRecalls(state)
	if errs := Validate(state); len(errs) > 0 {
		return Plan{}, fmt.Errorf("desired state has %d validation error(s)", len(errs))
	}
	resolvedState, err := resolveDesiredStateWithObserved(state, observed, now)
	if err != nil {
		return Plan{}, err
	}
	state = resolvedState
	releases := make(map[string]Release, len(state.Releases))
	for _, release := range state.Releases {
		releases[release.Product+":"+release.Version] = release
	}
	installations, ignored, err := expandInstallations(state)
	if err != nil {
		return Plan{}, err
	}
	secretMutations, err := buildSecretMutations(installations, observed.Secrets)
	if err != nil {
		return Plan{}, err
	}

	byProduct := make(map[string][]concreteInstallation)
	byID := make(map[string]concreteInstallation, len(installations))
	for _, installation := range installations {
		byProduct[installation.Entity.Product] = append(byProduct[installation.Entity.Product], installation)
		byID[installation.ID] = installation
	}

	dependencies := make(map[string][]string, len(installations))
	dependents := make(map[string][]string, len(installations))
	indegree := make(map[string]int, len(installations))
	for _, installation := range installations {
		indegree[installation.ID] = 0
		release := releases[installation.Entity.Product+":"+installation.Entity.Version]
		for _, dependency := range release.Dependencies {
			if contains(installation.Entity.IgnoredDependencies, dependency.Product) {
				continue
			}
			dependencyInstallation, found := matchingDependency(installation, dependency.Product, byProduct[dependency.Product])
			if !found {
				if dependency.Optional {
					continue
				}
				return Plan{}, fmt.Errorf("installation %s has no %s dependency in namespace %s", installation.ID, dependency.Product, installation.Namespace)
			}
			dependencies[installation.ID] = append(dependencies[installation.ID], dependencyInstallation.ID)
			dependents[dependencyInstallation.ID] = append(dependents[dependencyInstallation.ID], installation.ID)
			indegree[installation.ID]++
		}
		sort.Strings(dependencies[installation.ID])
	}

	ready := make([]string, 0)
	for id, degree := range indegree {
		if degree == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)

	ordered := make([]string, 0, len(installations))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		ordered = append(ordered, id)
		sort.Strings(dependents[id])
		for _, dependent := range dependents[id] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				ready = append(ready, dependent)
				sort.Strings(ready)
			}
		}
	}
	if len(ordered) != len(installations) {
		return Plan{}, fmt.Errorf("product dependency cycle detected")
	}

	steps := make([]PlanStep, 0, len(ordered))
	for position, id := range ordered {
		installation := byID[id]
		release := releases[installation.Entity.Product+":"+installation.Entity.Version]
		releaseDigest, err := releaseContentDigest(release)
		if err != nil {
			return Plan{}, err
		}
		chart, chartAccess := resolveRuntimeArtifact(release.Chart, state.Environment.ArtifactRegistries)
		images := make([]Artifact, 0, len(release.Images))
		accessByAssignment := map[string]RegistryAccess{}
		if chartAccess != nil {
			accessByAssignment[chartAccess.AssignmentID] = *chartAccess
		}
		for _, image := range release.Images {
			resolvedImage, access := resolveRuntimeArtifact(image, state.Environment.ArtifactRegistries)
			images = append(images, resolvedImage)
			if access != nil {
				accessByAssignment[access.AssignmentID] = *access
			}
		}
		registryAccess := make([]RegistryAccess, 0, len(accessByAssignment))
		for _, access := range accessByAssignment {
			registryAccess = append(registryAccess, access)
		}
		sort.Slice(registryAccess, func(i, j int) bool { return registryAccess[i].AssignmentID < registryAccess[j].AssignmentID })
		steps = append(steps, PlanStep{
			Position:         position + 1,
			Action:           planActionReconcile,
			InstallationID:   installation.ID,
			Entity:           installation.Entity.Name,
			HelmReleaseName:  installation.Entity.HelmReleaseName,
			Product:          installation.Entity.Product,
			Version:          installation.Entity.Version,
			ReleaseDigest:    releaseDigest,
			ReleaseChannel:   installation.Entity.ReleaseChannel,
			Namespace:        installation.Namespace,
			NamespaceRID:     installation.NamespaceRID,
			ConfigDigest:     installation.ConfigDigest,
			Values:           append([]byte(nil), installation.Values...),
			Chart:            chart,
			Images:           images,
			RegistryAccess:   registryAccess,
			SecretAccess:     append([]SecretAccess(nil), installation.SecretAccess...),
			HealthQuorum:     effectiveHealthQuorum(installation.Entity.HealthQuorum),
			RampedRollout:    rampedRolloutConstraint(state, installation),
			RecallRollOff:    installation.Entity.recallRollOff,
			RecallIDs:        append([]string(nil), installation.Entity.recallIDs...),
			UpgradeRationale: installation.Entity.upgradeRationale,
			DependsOn:        dependencies[id],
			Maintenance:      maintenanceConstraintFor(state.Environment, installation.Entity, release, installation.ID),
			HelmExecution:    stepHelmExecution(state.Environment.HelmExecution, release),
		})
	}
	plan := Plan{
		Environment:              state.Environment.ID,
		Generation:               state.Environment.Generation,
		AutoSuppressionThreshold: state.Environment.AutoSuppressionThreshold,
		SecretMutations:          secretMutations,
		Steps:                    steps,
		IgnoredNamespaces:        ignored,
	}
	if enforceExecutionBudget {
		if err := validatePlanExecutionTaskBudget(plan); err != nil {
			return Plan{}, err
		}
	}
	return plan, nil
}

// stepHelmExecution is the execution policy a step carries. A Zarf package or
// UDS bundle is not executed by Helm: its CLI always waits for the deployment,
// so the Environment's Helm defaults (a no-wait rollout among them) must not
// reach the step, where they would change what a successful report looks like.
func stepHelmExecution(defaults *HelmExecutionDefaults, release Release) HelmExecutionPolicy {
	if release.Chart.IsPackage() {
		return HelmExecutionPolicy{RolloutStrategy: RolloutManageRollout}
	}
	return effectiveHelmExecution(defaults, release)
}

func effectiveHelmExecution(defaults *HelmExecutionDefaults, release Release) HelmExecutionPolicy {
	policy := HelmExecutionPolicy{
		RolloutStrategy:            RolloutManageRollout,
		ImagePrePull:               true,
		ImagePrePullTimeoutSeconds: 15 * 60,
	}
	if defaults != nil {
		if defaults.DefaultRolloutStrategy != "" {
			policy.RolloutStrategy = defaults.DefaultRolloutStrategy
		}
		policy.InstallResourceReadinessTimeoutSeconds = defaults.InstallResourceReadinessTimeoutSeconds
		policy.UpgradeResourceReadinessTimeoutSeconds = defaults.UpgradeResourceReadinessTimeoutSeconds
		if defaults.ImagePrePull != nil {
			policy.ImagePrePull = defaults.ImagePrePull.Enabled
			policy.ImagePrePullTimeoutSeconds = 0
			if policy.ImagePrePull {
				policy.ImagePrePullTimeoutSeconds = defaults.ImagePrePull.TimeoutSeconds
				if policy.ImagePrePullTimeoutSeconds == 0 {
					policy.ImagePrePullTimeoutSeconds = 15 * 60
				}
			}
		}
	}
	if release.RolloutStrategy != "" {
		policy.RolloutStrategy = release.RolloutStrategy
	}
	if release.ResourceReadinessTimeoutSeconds > 0 {
		policy.InstallResourceReadinessTimeoutSeconds = release.ResourceReadinessTimeoutSeconds
		policy.UpgradeResourceReadinessTimeoutSeconds = release.ResourceReadinessTimeoutSeconds
	}
	if release.ImagePrePullTimeoutSeconds > 0 && policy.ImagePrePull {
		policy.ImagePrePullTimeoutSeconds = release.ImagePrePullTimeoutSeconds
	}
	return policy
}

func buildSecretMutations(installations []concreteInstallation, reported []ReportedSecret) ([]SecretMutation, error) {
	desiredByName := make(map[string]struct {
		Namespace string
		Access    SecretAccess
	})
	for _, installation := range installations {
		for _, access := range installation.SecretAccess {
			key := access.Assignment + "\x00" + installation.Namespace
			desiredByName[key] = struct {
				Namespace string
				Access    SecretAccess
			}{Namespace: installation.Namespace, Access: access}
		}
	}
	reportedByName := make(map[string]struct {
		Namespace string
		Access    SecretAccess
	}, len(reported))
	for _, secret := range reported {
		key := secret.Assignment + "\x00" + secret.Namespace
		reportedByName[key] = struct {
			Namespace string
			Access    SecretAccess
		}{Namespace: secret.Namespace, Access: SecretAccess{
			Assignment: secret.Assignment, Revision: secret.Revision, ProviderRef: secret.ProviderRef,
			KubernetesSecretName: secret.KubernetesSecretName, Keys: append([]string(nil), secret.Keys...),
		}}
	}
	names := make([]string, 0, len(desiredByName)+len(reportedByName))
	for key := range desiredByName {
		names = append(names, key)
	}
	for key := range reportedByName {
		if _, exists := desiredByName[key]; !exists {
			names = append(names, key)
		}
	}
	sort.Strings(names)
	mutations := make([]SecretMutation, 0, len(names))
	for _, key := range names {
		currentRecord, desiredExists := desiredByName[key]
		previousRecord, reportedExists := reportedByName[key]
		current, previous := currentRecord.Access, previousRecord.Access
		namespace := currentRecord.Namespace
		if !desiredExists {
			namespace = previousRecord.Namespace
		}
		mutation := SecretMutation{Position: len(mutations) + 1, Namespace: namespace}
		switch {
		case desiredExists && !reportedExists:
			mutation.Action, mutation.Current = SecretCreate, pointerTo(current)
		case !desiredExists && reportedExists:
			mutation.Action, mutation.Previous = SecretDelete, pointerTo(previous)
		case current.Revision < previous.Revision:
			return nil, fmt.Errorf("secret assignment %s in %s revision %d is older than reported revision %d", current.Assignment, namespace, current.Revision, previous.Revision)
		case current.Revision == previous.Revision &&
			(current.ProviderRef != previous.ProviderRef || current.KubernetesSecretName != previous.KubernetesSecretName || !equalStrings(current.Keys, previous.Keys)):
			return nil, fmt.Errorf("secret assignment %s in %s changed metadata without incrementing revision", current.Assignment, namespace)
		case current.Revision == previous.Revision:
			mutation.Action, mutation.Current, mutation.Previous = SecretVerify, pointerTo(current), pointerTo(previous)
		default:
			mutation.Action, mutation.Current, mutation.Previous = SecretReplace, pointerTo(current), pointerTo(previous)
		}
		mutations = append(mutations, mutation)
	}
	return mutations, nil
}

// rampedRolloutConstraint paces one installation inside its channel's rollout
// window.
//
// The window comes from the Product's ramped-rollout settings when they cover the
// Entity's channel, and otherwise from the promotion stage that delivered the
// version. Product settings win because they can pace a channel no promotion
// pipeline targets, which a stage-scoped window cannot express.
//
// Pacing still needs an instant to measure from. That is the promotion that put
// this version on the Entity's channel, or failing that the newest promotion of
// the version anywhere: a version that was never promoted has no fleet-wide
// arrival instant, so it is not paced.
func rampedRolloutConstraint(state DesiredState, installation concreteInstallation) *RampedRolloutConstraint {
	channel := installation.Entity.ReleaseChannel
	if channel == "" {
		return nil
	}
	var onChannel, anywhere *PromotionRecord
	for index := range state.Promotions {
		promotion := &state.Promotions[index]
		if promotion.Status != PromotionPassed || promotion.Product != installation.Entity.Product ||
			promotion.Version != installation.Entity.Version {
			continue
		}
		if promotion.ToChannel == channel {
			onChannel = newerPromotion(onChannel, promotion)
		}
		anywhere = newerPromotion(anywhere, promotion)
	}

	anchor := onChannel
	windowSeconds, configured := rolloutWindowFor(state.ProductSettings, installation.Entity.Product, channel)
	if !configured {
		if onChannel == nil || onChannel.Policy.RolloutWindowSeconds <= 0 {
			return nil
		}
		windowSeconds = onChannel.Policy.RolloutWindowSeconds
	} else if anchor == nil {
		anchor = anywhere
	}
	if anchor == nil || windowSeconds <= 0 {
		return nil
	}

	digest := sha256.Sum256([]byte(state.Environment.ID + "\x00" + installation.ID + "\x00" + anchor.ID))
	window := time.Duration(windowSeconds) * time.Second
	// Map the stable installation identity uniformly into the immutable rollout
	// window. This preserves pacing across Hub restarts and Plan regeneration.
	fraction := binary.BigEndian.Uint64(digest[:8])
	offset := time.Duration((uint64(window) / 1_000_000) * (fraction % 1_000_000))
	return &RampedRolloutConstraint{
		PromotionID: anchor.ID, WindowSeconds: windowSeconds,
		StartedAt: anchor.EvaluatedAt, NotBefore: anchor.EvaluatedAt.Add(offset),
	}
}

// newerPromotion picks the later of two promotion decisions, breaking ties on ID
// so Plan regeneration is deterministic.
func newerPromotion(current, candidate *PromotionRecord) *PromotionRecord {
	if current == nil || candidate.EvaluatedAt.After(current.EvaluatedAt) ||
		(candidate.EvaluatedAt.Equal(current.EvaluatedAt) && candidate.ID > current.ID) {
		return candidate
	}
	return current
}

func effectiveHealthQuorum(quorum string) string {
	if quorum == "" {
		return "ALL"
	}
	return quorum
}

func resolveRuntimeArtifact(artifact Artifact, assignments []ArtifactRegistryAssignment) (Artifact, *RegistryAccess) {
	source := artifactRegistryHost(artifact.Repository)
	for _, assignment := range assignments {
		if source != assignment.URL && !contains(assignment.Mirrors, source) {
			continue
		}
		resolved := artifact
		if source != assignment.URL {
			prefix := ""
			repository := strings.TrimSpace(artifact.Repository)
			if strings.HasPrefix(repository, "oci://") {
				prefix, repository = "oci://", strings.TrimPrefix(repository, "oci://")
			}
			resolved.Repository = prefix + assignment.URL + strings.TrimPrefix(repository, source)
		}
		return resolved, &RegistryAccess{
			AssignmentID: assignment.ID, Revision: assignment.Revision, SourceRegistry: source,
			RuntimeRegistry: assignment.URL, CredentialRef: assignment.CredentialRef,
			ImagePullSecretName: assignment.ImagePullSecretName,
		}
	}
	return artifact, nil
}

func expandInstallations(state DesiredState) ([]concreteInstallation, []IgnoredNamespace, error) {
	installations := make([]concreteInstallation, 0, len(state.Entities))
	ignored := make([]IgnoredNamespace, 0)
	for _, entity := range state.Entities {
		secretAccess, err := resolveSecretAccess(state.Environment, entity)
		if err != nil {
			return nil, nil, err
		}
		resolvedHelmValues, err := resolveDeploymentHelmValues(state.Environment, entity)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve deployment values for %s: %w", entity.Name, err)
		}
		if entity.Universal == nil {
			var namespaceTarget *NamespaceTarget
			for i := range state.Environment.Namespaces {
				if state.Environment.Namespaces[i].Name == entity.Namespace {
					namespaceTarget = &state.Environment.Namespaces[i]
					break
				}
			}
			values, digest, err := resolvedExecutionConfiguration(entity.Configuration, resolvedHelmValues, namespaceTarget)
			if err != nil {
				return nil, nil, err
			}
			digest = configurationDigestWithSecrets(digest, secretAccess)
			installations = append(installations, concreteInstallation{
				ID: entity.Name, Entity: entity, Namespace: entity.Namespace, ConfigDigest: digest, Values: values,
				SecretAccess: secretAccess,
			})
			continue
		}

		namespaces := append([]NamespaceTarget(nil), state.Environment.Namespaces...)
		sort.Slice(namespaces, func(i, j int) bool { return namespaces[i].RID < namespaces[j].RID })
		for _, namespace := range namespaces {
			if reason := universalEligibilityIgnoreReason(entity, namespace); reason != "" {
				ignored = append(ignored, IgnoredNamespace{Entity: entity.Name, NamespaceRID: namespace.RID, Reason: reason})
				continue
			}
			values, digest, err := resolvedExecutionConfiguration(&entity.Universal.ManagedConfig, resolvedHelmValues, &namespace)
			if err != nil {
				return nil, nil, fmt.Errorf("resolve configuration for %s in %s: %w", entity.Name, namespace.RID, err)
			}
			digest = configurationDigestWithSecrets(digest, secretAccess)
			if current, exists := namespace.Installed[entity.Product]; exists &&
				current.Version == entity.Version && current.ConfigDigest == digest {
				ignored = append(ignored, IgnoredNamespace{
					Entity: entity.Name, NamespaceRID: namespace.RID, Reason: IgnoreAlreadyOnTarget,
				})
				continue
			}
			installations = append(installations, concreteInstallation{
				ID: entity.Name + "@" + namespace.RID, Entity: entity, Namespace: namespace.Name,
				NamespaceRID: namespace.RID, ConfigDigest: digest, Values: values, SecretAccess: secretAccess,
			})
		}
	}
	sort.Slice(ignored, func(i, j int) bool {
		if ignored[i].Entity == ignored[j].Entity {
			return ignored[i].NamespaceRID < ignored[j].NamespaceRID
		}
		return ignored[i].Entity < ignored[j].Entity
	})
	return installations, ignored, nil
}

func resolveSecretAccess(environment Environment, entity Entity) ([]SecretAccess, error) {
	assignments := make(map[string]ExternalSecretAssignment, len(environment.Secrets))
	for _, assignment := range environment.Secrets {
		assignments[assignment.Name] = assignment
	}
	accesses := make([]SecretAccess, 0, len(entity.SecretRequirements))
	for _, requirement := range entity.SecretRequirements {
		assignment, exists := assignments[requirement.Secret]
		if !exists {
			return nil, fmt.Errorf("Entity %s secret requirement %s references missing assignment %s", entity.Name, requirement.Name, requirement.Secret)
		}
		keys := append([]string(nil), requirement.Keys...)
		sort.Strings(keys)
		accesses = append(accesses, SecretAccess{
			Requirement: requirement.Name, Assignment: assignment.Name, Revision: assignment.Revision,
			ProviderRef: assignment.ProviderRef, KubernetesSecretName: assignment.KubernetesSecretName, Keys: keys,
		})
	}
	sort.Slice(accesses, func(i, j int) bool { return accesses[i].Requirement < accesses[j].Requirement })
	return accesses, nil
}

func configurationDigestWithSecrets(configurationDigest string, accesses []SecretAccess) string {
	if len(accesses) == 0 {
		return configurationDigest
	}
	// ProviderRef is intentionally excluded. Rotation is authorized through the
	// explicit immutable revision, not by diffing provider-specific identifiers.
	coordinates := make([]struct {
		Requirement string   `json:"requirement"`
		Assignment  string   `json:"assignment"`
		Revision    int      `json:"revision"`
		SecretName  string   `json:"secretName"`
		Keys        []string `json:"keys"`
	}, 0, len(accesses))
	for _, access := range accesses {
		coordinates = append(coordinates, struct {
			Requirement string   `json:"requirement"`
			Assignment  string   `json:"assignment"`
			Revision    int      `json:"revision"`
			SecretName  string   `json:"secretName"`
			Keys        []string `json:"keys"`
		}{access.Requirement, access.Assignment, access.Revision, access.KubernetesSecretName, access.Keys})
	}
	encoded, _ := json.Marshal(coordinates)
	digest := sha256.Sum256(append([]byte(configurationDigest+"\x00"), encoded...))
	return fmt.Sprintf("sha256:%x", digest)
}

func universalEligibilityIgnoreReason(entity Entity, namespace NamespaceTarget) string {
	config := entity.Universal
	if contains(config.NamespaceDenylist, namespace.RID) {
		return IgnoreNamespaceExplicitlyDenied
	}
	if namespace.EnrollmentRID == "" {
		return IgnoreNoOwningEnrollment
	}
	if contains(config.EnrollmentDenylist, namespace.EnrollmentRID) {
		return IgnoreEnrollmentExplicitlyDenied
	}
	if len(config.EnrollmentAllowlist) > 0 && !contains(config.EnrollmentAllowlist, namespace.EnrollmentRID) {
		return IgnoreEnrollmentNotAllowed
	}
	if namespace.Trashed {
		return IgnoreNamespaceTrashed
	}
	if !namespace.OntologyCountValid {
		return IgnoreInvalidOntologyCount
	}
	return ""
}

func matchingDependency(
	installation concreteInstallation,
	dependencyProduct string,
	candidates []concreteInstallation,
) (concreteInstallation, bool) {
	// An override names the stack a dependency lives on, which is an explicit
	// statement that it is not co-located. Honoring it while still demanding the
	// same namespace would make the override unusable, so the override replaces
	// the co-location requirement rather than adding to it.
	targetStack, overridden := dependencyStack(installation.Entity, dependencyProduct)
	for _, candidate := range candidates {
		if !dependencyGroupsMatch(installation.Entity, candidate.Entity, dependencyProduct) ||
			candidate.Entity.Stack != targetStack {
			continue
		}
		if overridden || candidate.Namespace == installation.Namespace {
			return candidate, true
		}
	}
	if len(candidates) == 1 && candidates[0].NamespaceRID == "" &&
		candidates[0].Entity.Stack == targetStack &&
		dependencyGroupsMatch(installation.Entity, candidates[0].Entity, dependencyProduct) {
		return candidates[0], true
	}
	return concreteInstallation{}, false
}

// dependencyStack reports which stack must satisfy one dependency, and whether an
// override chose it. Without an override a dependency is resolved on the
// depending Entity's own stack.
func dependencyStack(entity Entity, dependencyProduct string) (string, bool) {
	if override, ok := entity.DependencyStackOverrides[dependencyProduct]; ok {
		return strings.TrimSpace(override), true
	}
	return entity.Stack, false
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
