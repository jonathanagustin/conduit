package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"
)

func weeklyWindow(intervals ...string) *WeeklyMaintenanceWindow {
	return &WeeklyMaintenanceWindow{TimeZone: "UTC", Intervals: intervals}
}

func TestWeeklyMaintenanceWindowUsesHalfOpenCrossWeekIntervals(t *testing.T) {
	window := weeklyWindow("FRIDAY/22:00-MONDAY/06:00")
	for _, at := range []time.Time{
		time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC), // Saturday
		time.Date(2026, time.August, 3, 5, 59, 0, 0, time.UTC), // Monday
	} {
		if !weeklyWindowContains(window, at) {
			t.Fatalf("window should contain %s", at)
		}
	}
	if weeklyWindowContains(window, time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC)) {
		t.Fatal("window must exclude its end")
	}
}

func TestValidateMaintenanceWindowsAndOverrides(t *testing.T) {
	state := foundation()
	state.Environment.Maintenance.Downtime = &WeeklyMaintenanceWindow{
		TimeZone: "Not/AZone", Intervals: []string{"MONDAY/25:00-MONDAY/26:00"},
	}
	state.Environment.Maintenance.Overrides = []MaintenanceOverride{{
		Type: "INVALID", StartsAt: time.Now(), EndsAt: time.Now(),
	}}
	errs := Validate(state)
	wanted := map[string]bool{
		"environment.maintenance.downtime.timeZone":     false,
		"environment.maintenance.downtime.intervals[0]": false,
		"environment.maintenance.overrides[0].type":     false,
		"environment.maintenance.overrides[0]":          false,
	}
	for _, validationError := range errs {
		if _, exists := wanted[validationError.Path]; exists {
			wanted[validationError.Path] = true
		}
	}
	for path, found := range wanted {
		if !found {
			t.Fatalf("missing maintenance validation at %s: %#v", path, errs)
		}
	}
}

func TestLeaseWaitsForResolvedMaintenanceWindow(t *testing.T) {
	state := foundation()
	state.Environment.Maintenance.Downtime = weeklyWindow("MONDAY/10:00-MONDAY/12:00")
	for index := range state.Entities {
		state.Entities[index].RequiresDowntime = true
	}

	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	outside := time.Date(2026, time.August, 4, 11, 0, 0, 0, time.UTC) // Tuesday
	lifecycle.now = func() time.Time { return outside }
	record, err := lifecycle.CreatePlan(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.ApprovePlan(context.Background(), record.ID, "operator", "reviewed"); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.LeaseNextPlan(context.Background(), "dev-use2", "agent", time.Minute); !errors.Is(err, ErrNoPlanAvailable) {
		t.Fatalf("outside-window lease = %v", err)
	}

	inside := time.Date(2026, time.August, 3, 11, 0, 0, 0, time.UTC) // Monday
	lifecycle.now = func() time.Time { return inside }
	if _, err := lifecycle.LeaseNextPlan(context.Background(), "dev-use2", "agent", time.Minute); err != nil {
		t.Fatalf("inside-window lease = %v", err)
	}
}

func TestMaintenanceResolutionIntersectsProductAndHonorsApprovedOverride(t *testing.T) {
	at := time.Date(2026, time.August, 3, 11, 0, 0, 0, time.UTC)
	constraint := MaintenanceConstraint{
		RequiresDowntime: true,
		Downtime:         weeklyWindow("MONDAY/10:00-MONDAY/12:00"),
		Product:          weeklyWindow("TUESDAY/10:00-TUESDAY/12:00"),
	}
	if maintenanceConstraintAllows("api", constraint, at) {
		t.Fatal("disjoint Product and Entity windows must block")
	}
	constraint.Overrides = []MaintenanceOverride{{
		Type: "DOWNTIME", StartsAt: at.Add(-time.Hour), EndsAt: at.Add(time.Hour),
		Reason: "emergency patch", ApprovedBy: "change-manager",
	}}
	if !maintenanceConstraintAllows("api", constraint, at) {
		t.Fatal("active approved downtime override must allow execution")
	}
}

func TestNoDowntimeChangeCanRunInsideDowntimeWindow(t *testing.T) {
	at := time.Date(2026, time.August, 3, 11, 0, 0, 0, time.UTC)
	constraint := MaintenanceConstraint{
		NoDowntime: weeklyWindow("TUESDAY/10:00-TUESDAY/12:00"),
		Downtime:   weeklyWindow("MONDAY/10:00-MONDAY/12:00"),
	}
	if !maintenanceConstraintAllows("api", constraint, at) {
		t.Fatal("no-downtime change should run during a downtime window")
	}
}
