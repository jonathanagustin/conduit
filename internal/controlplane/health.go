package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
	"sort"
	"time"
)

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

// The health rules are Environments'. What is left here is the repository work
// around them and the translation of a finding into this package's validation
// vocabulary.
func normalizeHealthDetails(path string, details *EntityHealthDetails) error {
	if issue := environments.NormalizeEntityHealth(path, details); issue != nil {
		return ValidationErrors{{Path: issue.Path, Message: issue.Message}}
	}
	return nil
}

func healthResult(details EntityHealthDetails) string {
	return environments.EntityHealthResult(details)
}
