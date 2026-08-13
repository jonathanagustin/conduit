package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

var healthCheckIDPattern = regexp.MustCompile(`^[A-Z][A-Z_]*$`)

func latestHealthTargets(ctx context.Context, repository Repository, environment string) ([]HealthTarget, error) {
	plans, err := repository.ListPlans(ctx, environment)
	if err != nil {
		return nil, err
	}
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].CreatedAt.Equal(plans[j].CreatedAt) {
			return plans[i].ID > plans[j].ID
		}
		return plans[i].CreatedAt.After(plans[j].CreatedAt)
	})
	for _, record := range plans {
		if record.Status != PlanSucceeded {
			continue
		}
		targets := make([]HealthTarget, 0, len(record.Plan.Steps))
		for _, step := range record.Plan.Steps {
			targets = append(targets, HealthTarget{
				InstallationID: step.InstallationID, Entity: step.Entity, Product: step.Product,
				Version: step.Version, Namespace: step.Namespace, Quorum: effectiveHealthQuorum(step.HealthQuorum),
			})
		}
		return targets, nil
	}
	return []HealthTarget{}, nil
}

func applyHealthObservations(
	ctx context.Context, repository Repository, environment, agentID string, observations []HealthObservation, now time.Time,
) (EnvironmentReport, error) {
	targets, err := latestHealthTargets(ctx, repository, environment)
	if err != nil {
		return EnvironmentReport{}, err
	}
	allowed := make(map[string]HealthTarget, len(targets))
	for _, target := range targets {
		allowed[target.InstallationID] = target
	}
	if len(observations) == 0 {
		return EnvironmentReport{}, ValidationErrors{{Path: "observations", Message: "must contain at least one health observation"}}
	}
	seen := map[string]struct{}{}
	for index := range observations {
		path := fmt.Sprintf("observations[%d]", index)
		target, exists := allowed[observations[index].InstallationID]
		if !exists {
			return EnvironmentReport{}, ValidationErrors{{Path: path + ".installationId", Message: "is not in the latest succeeded Plan"}}
		}
		if _, duplicate := seen[observations[index].InstallationID]; duplicate {
			return EnvironmentReport{}, ValidationErrors{{Path: path + ".installationId", Message: "must be unique"}}
		}
		seen[observations[index].InstallationID] = struct{}{}
		observations[index].Details.Quorum = target.Quorum
		if err := normalizeHealthDetails(path+".details", &observations[index].Details); err != nil {
			return EnvironmentReport{}, err
		}
		encoded, _ := json.Marshal(observations[index].Details)
		if len(encoded) > 10_000 {
			return EnvironmentReport{}, ValidationErrors{{Path: path + ".details", Message: "must not exceed 10,000 bytes"}}
		}
	}
	report, err := repository.GetEnvironmentReport(ctx, environment)
	if err != nil {
		return EnvironmentReport{}, err
	}
	byInstallation := make(map[string]int, len(report.Entities))
	for index, entity := range report.Entities {
		byInstallation[entity.InstallationID] = index
	}
	now = now.UTC()
	for _, observation := range observations {
		index, exists := byInstallation[observation.InstallationID]
		if !exists {
			return EnvironmentReport{}, fmt.Errorf("%w: installed Entity %s is absent from reported state", ErrConflict, observation.InstallationID)
		}
		details := cloneJSON(observation.Details)
		report.Entities[index].HealthDetails = &details
		report.Entities[index].Health = healthResult(details)
		report.Entities[index].ObservedAt = now
	}
	report.AgentID = agentID
	report.ObservedAt = now
	return repository.PutEnvironmentReport(ctx, report)
}

func normalizeHealthDetails(path string, details *EntityHealthDetails) error {
	seenNodes := map[string]struct{}{}
	for nodeIndex := range details.Nodes {
		node := &details.Nodes[nodeIndex]
		node.Result = HealthHealthy
		nodePath := fmt.Sprintf("%s.nodes[%d]", path, nodeIndex)
		if strings.TrimSpace(node.NodeID) == "" || strings.TrimSpace(node.ProductVersion) == "" {
			return ValidationErrors{{Path: nodePath, Message: "nodeId and productVersion are required"}}
		}
		if _, duplicate := seenNodes[node.NodeID]; duplicate {
			return ValidationErrors{{Path: nodePath + ".nodeId", Message: "must be unique"}}
		}
		seenNodes[node.NodeID] = struct{}{}
		seenContainers := map[string]struct{}{}
		for containerIndex := range node.Containers {
			container := &node.Containers[containerIndex]
			containerPath := fmt.Sprintf("%s.containers[%d]", nodePath, containerIndex)
			if strings.TrimSpace(container.ContainerID) == "" {
				return ValidationErrors{{Path: containerPath + ".containerId", Message: "is required"}}
			}
			if _, duplicate := seenContainers[container.ContainerID]; duplicate {
				return ValidationErrors{{Path: containerPath + ".containerId", Message: "must be unique within the node"}}
			}
			seenContainers[container.ContainerID] = struct{}{}
			result := HealthHealthy
			if !container.Liveness || !container.Readiness {
				result = HealthError
			}
			seenChecks := map[string]struct{}{}
			for checkIndex := range container.Checks {
				check := &container.Checks[checkIndex]
				checkPath := fmt.Sprintf("%s.checks[%d]", containerPath, checkIndex)
				if !healthCheckIDPattern.MatchString(check.ID) ||
					!contains([]string{HealthHealthy, HealthRepairing, HealthWarning, HealthError, HealthTerminal}, check.Result) {
					return ValidationErrors{{Path: checkPath, Message: "id and result must use the health contract"}}
				}
				if _, duplicate := seenChecks[check.ID]; duplicate {
					return ValidationErrors{{Path: checkPath + ".id", Message: "must be unique within the container"}}
				}
				seenChecks[check.ID] = struct{}{}
				if check.Params == nil {
					check.Params = map[string]any{}
				}
				result = worseHealth(result, check.Result)
			}
			container.Result = result
			node.Result = worseHealth(node.Result, result)
		}
		if len(node.Containers) == 0 {
			node.Result = HealthUnknown
		}
	}
	sort.Slice(details.Nodes, func(i, j int) bool { return details.Nodes[i].NodeID < details.Nodes[j].NodeID })
	return nil
}

func healthResult(details EntityHealthDetails) string {
	if len(details.Nodes) == 0 {
		return HealthUnknown
	}
	healthy, worst := 0, HealthHealthy
	for _, node := range details.Nodes {
		if node.Result == HealthHealthy {
			healthy++
		}
		worst = worseHealth(worst, node.Result)
	}
	required := len(details.Nodes)
	switch details.Quorum {
	case "ANY", "NO_GUARANTEE":
		required = 1
	case "ALL_BUT_ONE":
		required = len(details.Nodes) - 1
		if required < 1 {
			required = 1
		}
	case "SIMPLE_MAJORITY":
		required = len(details.Nodes)/2 + 1
	case "UNKNOWN":
		return HealthIndeterminate
	}
	if healthy >= required {
		return HealthHealthy
	}
	return worst
}
