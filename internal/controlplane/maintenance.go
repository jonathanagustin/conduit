package controlplane

import (
	"fmt"
	"strings"
	"time"
)

func validateMaintenancePolicy(environment Environment, releases []Release, entities []Entity, errs *[]ValidationError) {
	validateWeeklyMaintenanceWindow("environment.maintenance.downtime", environment.Maintenance.Downtime, errs)
	validateWeeklyMaintenanceWindow("environment.maintenance.noDowntime", environment.Maintenance.NoDowntime, errs)
	for index, override := range environment.Maintenance.Overrides {
		path := fmt.Sprintf("environment.maintenance.overrides[%d]", index)
		if override.Type != "DOWNTIME" && override.Type != "NO_DOWNTIME" {
			*errs = append(*errs, ValidationError{Path: path + ".type", Message: "must be DOWNTIME or NO_DOWNTIME"})
		}
		if override.StartsAt.IsZero() || !override.EndsAt.After(override.StartsAt) {
			*errs = append(*errs, ValidationError{Path: path, Message: "must have an end after its start"})
		}
		if strings.TrimSpace(override.Reason) == "" || strings.TrimSpace(override.ApprovedBy) == "" {
			*errs = append(*errs, ValidationError{Path: path, Message: "reason and approver are required"})
		}
		if override.InstallationID != "" {
			found := false
			for _, entity := range entities {
				if entity.Name == override.InstallationID {
					found = true
					break
				}
			}
			if !found {
				*errs = append(*errs, ValidationError{Path: path + ".installationId", Message: "must reference an Entity name"})
			}
		}
	}
	for index, release := range releases {
		validateWeeklyMaintenanceWindow(fmt.Sprintf("releases[%d].maintenanceWindow", index), release.MaintenanceWindow, errs)
	}
	for index, entity := range entities {
		validateWeeklyMaintenanceWindow(fmt.Sprintf("entities[%d].maintenance.downtime", index), entity.Maintenance.Downtime, errs)
		validateWeeklyMaintenanceWindow(fmt.Sprintf("entities[%d].maintenance.noDowntime", index), entity.Maintenance.NoDowntime, errs)
	}
}

func planWithinMaintenanceWindow(plan Plan, at time.Time) bool {
	for _, step := range plan.Steps {
		if !maintenanceConstraintAllows(step.InstallationID, step.Maintenance, at) {
			return false
		}
	}
	return true
}

func maintenanceConstraintFor(
	environment Environment,
	entity Entity,
	release Release,
	installationID string,
) MaintenanceConstraint {
	downtime := entity.Maintenance.Downtime
	if downtime == nil {
		downtime = environment.Maintenance.Downtime
	}
	noDowntime := entity.Maintenance.NoDowntime
	if noDowntime == nil {
		noDowntime = environment.Maintenance.NoDowntime
	}
	var product *WeeklyMaintenanceWindow
	if !entity.Maintenance.OverrideProductMaintenanceWindow {
		product = release.MaintenanceWindow
	}
	overrides := make([]MaintenanceOverride, 0)
	for _, override := range environment.Maintenance.Overrides {
		if override.InstallationID == "" || override.InstallationID == installationID || override.InstallationID == entity.Name {
			overrides = append(overrides, override)
		}
	}
	return MaintenanceConstraint{
		RequiresDowntime: entity.RequiresDowntime, Downtime: downtime, NoDowntime: noDowntime,
		Product: product, Overrides: overrides,
	}
}
