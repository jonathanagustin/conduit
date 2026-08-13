package controlplane

import (
	"context"
	"fmt"
	"sort"
)

func namespaceDeletionProtected(namespace NamespaceTarget) bool {
	return namespace.DeletionProtected == nil || *namespace.DeletionProtected
}

// validateNamespaceTransition protects the declarative namespace inventory.
// Namespace resources remain Flux/bootstrap-owned; this prevents an approved
// desired-state change from authorizing an unsafe external deletion.
func (lifecycle *Lifecycle) validateNamespaceTransition(
	ctx context.Context, next DesiredState, report EnvironmentReport,
) error {
	plans, err := lifecycle.repository.ListPlans(ctx, next.Environment.ID)
	if err != nil {
		return err
	}
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].CreatedAt.Equal(plans[j].CreatedAt) {
			return plans[i].ID > plans[j].ID
		}
		return plans[i].CreatedAt.After(plans[j].CreatedAt)
	})
	var previous *DesiredState
	var applied *DesiredState
	for index := range plans {
		if plans[index].DesiredState == nil || plans[index].Command != nil {
			continue
		}
		if previous == nil && plans[index].Status != PlanCancelled {
			previous = plans[index].DesiredState
		}
		if applied == nil && plans[index].Status == PlanSucceeded {
			applied = plans[index].DesiredState
		}
	}
	if previous == nil {
		return nil
	}
	previousEntities := make(map[string]Entity, len(previous.Entities))
	for _, entity := range previous.Entities {
		previousEntities[entity.Name] = entity
	}
	for _, entity := range next.Entities {
		if before, exists := previousEntities[entity.Name]; exists && before.Universal == nil && entity.Universal == nil &&
			before.Namespace != entity.Namespace {
			return ValidationErrors{{Path: "entities." + entity.Name + ".namespace", Message: "is immutable after Entity creation"}}
		}
	}

	if applied == nil {
		return nil
	}
	nextNamespaces := make(map[string]NamespaceTarget, len(next.Environment.Namespaces))
	for _, namespace := range next.Environment.Namespaces {
		nextNamespaces[namespace.RID] = namespace
	}
	observed := make(map[string]ReportedEntity, len(report.Entities))
	for _, entity := range report.Entities {
		observed[entity.InstallationID] = entity
	}
	for _, namespace := range applied.Environment.Namespaces {
		if _, retained := nextNamespaces[namespace.RID]; retained {
			continue
		}
		path := "environment.namespaces." + namespace.Name
		if namespaceDeletionProtected(namespace) {
			return ValidationErrors{{Path: path, Message: "must be explicitly unprotected in an approved prior generation before deletion"}}
		}
		for _, entity := range next.Entities {
			if entity.Universal == nil && entity.Namespace == namespace.Name {
				return ValidationErrors{{Path: path, Message: fmt.Sprintf("cannot be deleted while desired Entity %s targets it", entity.Name)}}
			}
		}
		for _, entity := range applied.Entities {
			if entity.Universal != nil || entity.Namespace != namespace.Name {
				continue
			}
			current, exists := observed[entity.Name]
			if !exists || current.Lifecycle != LifecycleUninstalled {
				return ValidationErrors{{Path: path, Message: fmt.Sprintf("cannot be deleted before Entity %s reports uninstalled", entity.Name)}}
			}
		}
		for _, entity := range report.Entities {
			if entity.Namespace == namespace.Name && entity.Lifecycle == LifecycleInstalled {
				return ValidationErrors{{Path: path, Message: fmt.Sprintf("cannot be deleted while reported Entity %s is installed", entity.InstallationID)}}
			}
		}
	}
	return nil
}
