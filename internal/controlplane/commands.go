package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type IssueCommandRequest struct {
	DesiredState   DesiredState `json:"desiredState"`
	Type           string       `json:"type"`
	InstallationID string       `json:"installationId"`
	ResourceKind   string       `json:"resourceKind,omitempty"`
	ResourceName   string       `json:"resourceName,omitempty"`
	TargetVersion  string       `json:"targetVersion,omitempty"`
	Rationale      string       `json:"rationale"`
}

func (lifecycle *Lifecycle) IssueCommand(ctx context.Context, request IssueCommandRequest, issuer string) (PlanRecord, error) {
	request.Type = strings.ToUpper(strings.TrimSpace(request.Type))
	request.InstallationID = strings.TrimSpace(request.InstallationID)
	request.ResourceKind = strings.TrimSpace(request.ResourceKind)
	request.ResourceName = strings.TrimSpace(request.ResourceName)
	request.TargetVersion = strings.TrimSpace(request.TargetVersion)
	request.Rationale = strings.TrimSpace(request.Rationale)
	issuer = strings.TrimSpace(issuer)
	if !contains([]string{CommandInstall, CommandUninstall, CommandEnforceConfig, CommandEnforceConfigAndVersion, CommandRestartResource}, request.Type) ||
		request.InstallationID == "" || request.Rationale == "" || issuer == "" {
		return PlanRecord{}, fmt.Errorf("%w: valid command type, installation, issuer, and rationale are required", ErrInvalidTransition)
	}
	if request.Type == CommandRestartResource {
		if !contains([]string{"Deployment", "StatefulSet"}, request.ResourceKind) || request.ResourceName == "" {
			return PlanRecord{}, fmt.Errorf("%w: restart commands require a Deployment or StatefulSet and resource name", ErrInvalidTransition)
		}
	} else if request.ResourceKind != "" || request.ResourceName != "" {
		return PlanRecord{}, fmt.Errorf("%w: resource target is only valid for restart commands", ErrInvalidTransition)
	}
	if request.TargetVersion != "" && request.Type != CommandInstall && request.Type != CommandEnforceConfigAndVersion {
		return PlanRecord{}, fmt.Errorf("%w: target version is only valid for install or enforce config and version", ErrInvalidTransition)
	}

	report, err := lifecycle.repository.GetEnvironmentReport(ctx, request.DesiredState.Environment.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return PlanRecord{}, err
	}
	commandState := cloneJSON(request.DesiredState)
	// Commands deliberately bypass recall eligibility. The selected version is
	// still required to exist as an immutable Release in this state snapshot.
	commandState.Recalls = nil
	commandState.Environment.AutomaticSecurityRecall = false
	for index := range commandState.Releases {
		commandState.Releases[index].Dependencies = nil
		commandState.Releases[index].Incompatibilities = nil
	}
	plan, err := buildPlanWithObserved(commandState, report, lifecycle.now().UTC(), false)
	if err != nil {
		return PlanRecord{}, err
	}
	var target *PlanStep
	for index := range plan.Steps {
		if plan.Steps[index].InstallationID == request.InstallationID {
			target = &plan.Steps[index]
			break
		}
	}
	if target == nil {
		return PlanRecord{}, fmt.Errorf("%w: installation is absent from desired state", ErrNotFound)
	}
	var observed *ReportedEntity
	for index := range report.Entities {
		if report.Entities[index].InstallationID == request.InstallationID {
			observed = &report.Entities[index]
			break
		}
	}
	installed := observed != nil && observed.Lifecycle == LifecycleInstalled
	retargetVersion := request.TargetVersion
	switch request.Type {
	case CommandInstall:
		if installed {
			return PlanRecord{}, fmt.Errorf("%w: install command requires an uninstalled Entity", ErrInvalidTransition)
		}
	case CommandUninstall:
		if !installed {
			return PlanRecord{}, fmt.Errorf("%w: uninstall command requires an installed Entity", ErrInvalidTransition)
		}
		retargetVersion = observed.Version
	case CommandEnforceConfig:
		if !installed {
			return PlanRecord{}, fmt.Errorf("%w: enforce config requires an installed Entity", ErrInvalidTransition)
		}
		retargetVersion = observed.Version
	case CommandEnforceConfigAndVersion:
		if !installed {
			return PlanRecord{}, fmt.Errorf("%w: enforce config and version requires an installed Entity", ErrInvalidTransition)
		}
	case CommandRestartResource:
		if !installed {
			return PlanRecord{}, fmt.Errorf("%w: restart requires an installed Entity", ErrInvalidTransition)
		}
		retargetVersion = observed.Version
	}
	if retargetVersion != "" && target.Version != retargetVersion {
		if !retargetEntityVersion(&commandState, target.Entity, retargetVersion) {
			return PlanRecord{}, fmt.Errorf("%w: target Entity is absent from desired state", ErrNotFound)
		}
		plan, err = buildPlanWithObserved(commandState, report, lifecycle.now().UTC(), false)
		if err != nil {
			return PlanRecord{}, err
		}
		target = nil
		for index := range plan.Steps {
			if plan.Steps[index].InstallationID == request.InstallationID {
				target = &plan.Steps[index]
				break
			}
		}
		if target == nil || target.Version != retargetVersion {
			return PlanRecord{}, fmt.Errorf("%w: target version is unavailable for the Entity", ErrInvalidTransition)
		}
	}
	if request.Type != CommandUninstall && request.Type != CommandRestartResource &&
		securityRecallBlocksCommand(request.DesiredState, *target) {
		return PlanRecord{}, fmt.Errorf("%w: security-recalled Product Release cannot be deployed by an Entity command", ErrInvalidTransition)
	}
	switch request.Type {
	case CommandUninstall:
		target.Action = planActionUninstall
		target.ReleaseDigest = observed.ReleaseDigest
		target.ConfigDigest = observed.ConfigDigest
	case CommandRestartResource:
		target.Action = "restart"
		target.ResourceKind = request.ResourceKind
		target.ResourceName = request.ResourceName
		target.ReleaseDigest = observed.ReleaseDigest
		target.ConfigDigest = observed.ConfigDigest
	}
	target.Position = 1
	target.DependsOn = nil
	plan.Steps = []PlanStep{*target}
	plan.SecretMutations = commandSecretMutations(plan.SecretMutations, target.SecretAccess, target.Namespace)
	if err := validatePlanExecutionTaskBudget(plan); err != nil {
		return PlanRecord{}, err
	}

	plans, err := lifecycle.repository.ListPlans(ctx, plan.Environment)
	if err != nil {
		return PlanRecord{}, err
	}
	for _, existing := range plans {
		if existing.Command != nil && existing.Command.InstallationID == request.InstallationID && planIsActive(existing) {
			return PlanRecord{}, fmt.Errorf("%w: an Entity command is already pending or executing", ErrConflict)
		}
	}
	stateJSON, err := json.Marshal(commandState)
	if err != nil {
		return PlanRecord{}, fmt.Errorf("encode desired state: %w", err)
	}
	stateDigest := sha256.Sum256(stateJSON)
	now := lifecycle.now().UTC()
	identityJSON, _ := json.Marshal(struct {
		Request IssueCommandRequest
		Issuer  string
		At      int64
		Ordinal int
	}{request, issuer, now.UnixNano(), len(plans)})
	identity := sha256.Sum256(identityJSON)
	record := PlanRecord{
		ID: "cmd_" + hex.EncodeToString(identity[:16]), DesiredStateDigest: "sha256:" + hex.EncodeToString(stateDigest[:]),
		DesiredState: pointerTo(commandState), Plan: plan, Status: PlanApproved, RequiredApprovals: 0,
		Command: &EntityCommand{Type: request.Type, InstallationID: request.InstallationID, ResourceKind: request.ResourceKind,
			ResourceName: request.ResourceName, TargetVersion: target.Version, IssuedBy: issuer, Rationale: request.Rationale, IssuedAt: now},
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	return lifecycle.repository.CreatePlan(ctx, record)
}

func securityRecallBlocksCommand(state DesiredState, target PlanStep) bool {
	state = withAutomaticSecurityRecalls(state)
	candidate := Release{Product: target.Product, Version: target.Version}
	for _, recall := range state.Recalls {
		if recall.Source == RecallSourceSecurity && releaseRecalled(candidate, []Recall{recall}, state.Environment.ID) {
			return true
		}
	}
	return false
}

func retargetEntityVersion(state *DesiredState, entityName, version string) bool {
	for index := range state.Entities {
		if state.Entities[index].Name == entityName {
			state.Entities[index].Version = version
			state.Entities[index].ReleaseChannel = ""
			return true
		}
	}
	return false
}

func commandSecretMutations(mutations []SecretMutation, accesses []SecretAccess, namespace string) []SecretMutation {
	wanted := make(map[string]struct{}, len(accesses))
	for _, access := range accesses {
		wanted[access.Assignment] = struct{}{}
	}
	result := make([]SecretMutation, 0, len(mutations))
	for _, mutation := range mutations {
		if mutation.Namespace != namespace {
			continue
		}
		if mutation.Action == SecretDelete {
			mutation.Position = len(result) + 1
			result = append(result, mutation)
			continue
		}
		assignment := mutation.Current
		if assignment == nil {
			assignment = mutation.Previous
		}
		if assignment != nil {
			if _, exists := wanted[assignment.Assignment]; exists {
				mutation.Position = len(result) + 1
				result = append(result, mutation)
			}
		}
	}
	return result
}

func planIsActive(record PlanRecord) bool {
	switch record.Status {
	case PlanAwaitingApproval, PlanApproved, PlanLeased, PlanRunning:
		return true
	default:
		return false
	}
}

func plansOverlap(left, right PlanRecord) bool {
	installations := make(map[string]struct{}, len(left.Plan.Steps))
	for _, step := range left.Plan.Steps {
		installations[step.InstallationID] = struct{}{}
	}
	for _, step := range right.Plan.Steps {
		if _, exists := installations[step.InstallationID]; exists {
			return true
		}
	}
	return false
}
