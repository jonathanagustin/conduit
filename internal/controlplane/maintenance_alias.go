package controlplane

import (
	"fmt"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/maintenance"
)

// Deciding when work may happen now lives in internal/delivery/maintenance.
// This file is the translation: this package's windows and constraints go in,
// Delivery's answer comes back, and its problem reports are restated as
// ValidationErrors with the paths callers already expect.

// deliveryWindow converts one of this package's windows into Delivery's own.
// The two carry the same two fields; the conversion exists so that Delivery
// need not import Catalog, where WeeklyMaintenanceWindow happens to live
// because a Release may declare one.
func deliveryWindow(window *WeeklyMaintenanceWindow) *maintenance.Window {
	if window == nil {
		return nil
	}
	return &maintenance.Window{TimeZone: window.TimeZone, Intervals: window.Intervals}
}

func deliveryConstraint(constraint MaintenanceConstraint) maintenance.Constraint {
	overrides := make([]maintenance.Override, 0, len(constraint.Overrides))
	for _, override := range constraint.Overrides {
		overrides = append(overrides, maintenance.Override{
			InstallationID: override.InstallationID,
			Type:           override.Type,
			StartsAt:       override.StartsAt,
			EndsAt:         override.EndsAt,
		})
	}
	return maintenance.Constraint{
		RequiresDowntime: constraint.RequiresDowntime,
		Downtime:         deliveryWindow(constraint.Downtime),
		NoDowntime:       deliveryWindow(constraint.NoDowntime),
		Product:          deliveryWindow(constraint.Product),
		Overrides:        overrides,
	}
}

func weeklyWindowContains(window *WeeklyMaintenanceWindow, at time.Time) bool {
	return maintenance.WindowContains(deliveryWindow(window), at)
}

func maintenanceConstraintAllows(installationID string, constraint MaintenanceConstraint, at time.Time) bool {
	return maintenance.ConstraintAllows(installationID, deliveryConstraint(constraint), at)
}

// validateWeeklyMaintenanceWindow restates Delivery's problem reports as
// ValidationErrors. Delivery reports a field and an index because it has no
// business knowing what a JSON path looks like; the path is assembled here,
// unchanged from what callers saw before.
func validateWeeklyMaintenanceWindow(path string, window *WeeklyMaintenanceWindow, errs *[]ValidationError) {
	for _, problem := range maintenance.ValidateWindow(deliveryWindow(window)) {
		fieldPath := path + "." + problem.Field
		if problem.Index >= 0 {
			fieldPath = fmt.Sprintf("%s.%s[%d]", path, problem.Field, problem.Index)
		}
		*errs = append(*errs, ValidationError{Path: fieldPath, Message: problem.Message})
	}
}
