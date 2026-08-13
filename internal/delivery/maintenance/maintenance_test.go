package maintenance

import (
	"testing"
	"time"
)

// 2026-08-13 is a Thursday. 14:00 UTC.
var thursdayAfternoon = time.Date(2026, 8, 13, 14, 0, 0, 0, time.UTC)

func window(zone string, intervals ...string) *Window {
	return &Window{TimeZone: zone, Intervals: intervals}
}

// Declaring no window is declaring no restriction. Reading nil as "nothing
// fits" would forbid all work wherever a window was simply not set.
func TestNilWindowPermitsEverything(t *testing.T) {
	t.Parallel()
	if !WindowContains(nil, thursdayAfternoon) {
		t.Fatal("a nil window must contain every moment")
	}
}

// The opposite direction on purpose: a window whose zone cannot be resolved has
// not been evaluated, and calling that permission would run work outside every
// window ever declared.
func TestUnresolvableTimeZonePermitsNothing(t *testing.T) {
	t.Parallel()
	if WindowContains(window("Mars/Olympus", "MONDAY/00:00-SUNDAY/23:59"), thursdayAfternoon) {
		t.Fatal("a window whose time zone cannot be loaded must contain nothing")
	}
}

func TestWindowContainsOnlyItsOwnHours(t *testing.T) {
	t.Parallel()
	inside := window("UTC", "THURSDAY/13:00-THURSDAY/15:00")
	if !WindowContains(inside, thursdayAfternoon) {
		t.Error("14:00 Thursday must fall inside 13:00-15:00 Thursday")
	}
	outside := window("UTC", "THURSDAY/15:00-THURSDAY/16:00")
	if WindowContains(outside, thursdayAfternoon) {
		t.Error("14:00 Thursday must fall outside 15:00-16:00 Thursday")
	}
}

// The interval is half-open: it includes its start and excludes its end, so two
// adjacent windows do not both claim the boundary minute.
func TestIntervalIncludesItsStartAndExcludesItsEnd(t *testing.T) {
	t.Parallel()
	if !WindowContains(window("UTC", "THURSDAY/14:00-THURSDAY/15:00"), thursdayAfternoon) {
		t.Error("a moment exactly at the start must be inside")
	}
	if WindowContains(window("UTC", "THURSDAY/13:00-THURSDAY/14:00"), thursdayAfternoon) {
		t.Error("a moment exactly at the end must be outside")
	}
}

// A window running Saturday into Monday wraps the end of the week. Treating
// start > end as an empty range would silently close every weekend window.
func TestWindowWrapsTheEndOfTheWeek(t *testing.T) {
	t.Parallel()
	weekend := window("UTC", "SATURDAY/22:00-MONDAY/06:00")
	sunday := time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC)
	if !WindowContains(weekend, sunday) {
		t.Error("Sunday 03:00 must fall inside a Saturday-to-Monday window")
	}
	if WindowContains(weekend, thursdayAfternoon) {
		t.Error("Thursday must fall outside a Saturday-to-Monday window")
	}
}

// The week starts on Monday, which is Apollo's convention and not Go's, where
// Sunday is zero. Getting this wrong shifts every window by a day.
func TestWeekStartsOnMonday(t *testing.T) {
	t.Parallel()
	sunday := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	if !WindowContains(window("UTC", "SUNDAY/11:00-SUNDAY/13:00"), sunday) {
		t.Fatal("Sunday noon must fall inside a Sunday window")
	}
	if WindowContains(window("UTC", "MONDAY/11:00-MONDAY/13:00"), sunday) {
		t.Fatal("Sunday noon must not fall inside a Monday window")
	}
}

// The window is evaluated in its own zone, not the caller's.
func TestWindowIsEvaluatedInItsOwnTimeZone(t *testing.T) {
	t.Parallel()
	// 14:00 UTC is 10:00 in New York.
	if !WindowContains(window("America/New_York", "THURSDAY/09:00-THURSDAY/11:00"), thursdayAfternoon) {
		t.Error("the window's zone must decide, not UTC")
	}
	if WindowContains(window("America/New_York", "THURSDAY/13:00-THURSDAY/15:00"), thursdayAfternoon) {
		t.Error("the caller's clock must not be used directly")
	}
}

// An unparseable interval is skipped rather than treated as matching. A typo
// that opened the window permanently would be the dangerous failure.
func TestUnparseableIntervalIsSkipped(t *testing.T) {
	t.Parallel()
	if WindowContains(window("UTC", "NOTADAY/13:00-THURSDAY/15:00"), thursdayAfternoon) {
		t.Fatal("a malformed interval must not open the window")
	}
	mixed := window("UTC", "NOTADAY/13:00-THURSDAY/15:00", "THURSDAY/13:00-THURSDAY/15:00")
	if !WindowContains(mixed, thursdayAfternoon) {
		t.Fatal("a malformed interval must not close a window a valid one opens")
	}
}

func TestConstraintRequiringDowntimeUsesTheDowntimeWindow(t *testing.T) {
	t.Parallel()
	constraint := Constraint{
		RequiresDowntime: true,
		Downtime:         window("UTC", "THURSDAY/13:00-THURSDAY/15:00"),
		NoDowntime:       window("UTC", "FRIDAY/13:00-FRIDAY/15:00"),
	}
	if !ConstraintAllows("inst", constraint, thursdayAfternoon) {
		t.Error("work requiring downtime must be allowed inside the downtime window")
	}
	constraint.Downtime = window("UTC", "FRIDAY/13:00-FRIDAY/15:00")
	if ConstraintAllows("inst", constraint, thursdayAfternoon) {
		t.Error("work requiring downtime must not run outside the downtime window")
	}
}

// Work that needs no downtime may use either window: a downtime window is a
// superset of what no-downtime work is allowed to do.
func TestNoDowntimeWorkMayUseEitherWindow(t *testing.T) {
	t.Parallel()
	viaDowntime := Constraint{Downtime: window("UTC", "THURSDAY/13:00-THURSDAY/15:00")}
	if !ConstraintAllows("inst", viaDowntime, thursdayAfternoon) {
		t.Error("no-downtime work must be allowed inside a downtime window")
	}
	viaNoDowntime := Constraint{NoDowntime: window("UTC", "THURSDAY/13:00-THURSDAY/15:00")}
	if !ConstraintAllows("inst", viaNoDowntime, thursdayAfternoon) {
		t.Error("no-downtime work must be allowed inside a no-downtime window")
	}
}

// A Release declaring when it may be installed is not overruled by the
// Environment being open.
func TestProductWindowBindsRegardlessOfTheEnvironment(t *testing.T) {
	t.Parallel()
	constraint := Constraint{
		Downtime: window("UTC", "THURSDAY/13:00-THURSDAY/15:00"),
		Product:  window("UTC", "FRIDAY/13:00-FRIDAY/15:00"),
	}
	if ConstraintAllows("inst", constraint, thursdayAfternoon) {
		t.Fatal("the Release's own window must bind even when the Environment is open")
	}
}

// An override is an operator overruling the schedule. A schedule that could
// overrule it back would make the override pointless.
func TestDowntimeOverrideOpensAClosedSchedule(t *testing.T) {
	t.Parallel()
	constraint := Constraint{
		RequiresDowntime: true,
		Downtime:         window("UTC", "FRIDAY/13:00-FRIDAY/15:00"),
		Overrides: []Override{{
			Type:     OverrideDowntime,
			StartsAt: thursdayAfternoon.Add(-time.Hour),
			EndsAt:   thursdayAfternoon.Add(time.Hour),
		}},
	}
	if !ConstraintAllows("inst", constraint, thursdayAfternoon) {
		t.Fatal("a live downtime override must permit work the schedule forbids")
	}
}

// A no-downtime override does not authorize work that needs downtime.
func TestNoDowntimeOverrideDoesNotAuthorizeDowntimeWork(t *testing.T) {
	t.Parallel()
	override := Override{
		Type:     OverrideNoDowntime,
		StartsAt: thursdayAfternoon.Add(-time.Hour),
		EndsAt:   thursdayAfternoon.Add(time.Hour),
	}
	needsDowntime := Constraint{RequiresDowntime: true, Downtime: window("UTC", "FRIDAY/13:00-FRIDAY/15:00"), Overrides: []Override{override}}
	if ConstraintAllows("inst", needsDowntime, thursdayAfternoon) {
		t.Error("a no-downtime override must not authorize work requiring downtime")
	}
	noDowntimeNeeded := Constraint{Downtime: window("UTC", "FRIDAY/13:00-FRIDAY/15:00"), Overrides: []Override{override}}
	if !ConstraintAllows("inst", noDowntimeNeeded, thursdayAfternoon) {
		t.Error("a no-downtime override must authorize work that needs no downtime")
	}
}

func TestOverrideIsBoundedAndTargeted(t *testing.T) {
	t.Parallel()
	closed := Constraint{RequiresDowntime: true, Downtime: window("UTC", "FRIDAY/13:00-FRIDAY/15:00")}

	lapsed := closed
	lapsed.Overrides = []Override{{Type: OverrideDowntime, StartsAt: thursdayAfternoon.Add(-2 * time.Hour), EndsAt: thursdayAfternoon.Add(-time.Hour)}}
	if ConstraintAllows("inst", lapsed, thursdayAfternoon) {
		t.Error("an override that has ended must not permit work")
	}

	// The end is exclusive, matching the windows.
	endingNow := closed
	endingNow.Overrides = []Override{{Type: OverrideDowntime, StartsAt: thursdayAfternoon.Add(-time.Hour), EndsAt: thursdayAfternoon}}
	if ConstraintAllows("inst", endingNow, thursdayAfternoon) {
		t.Error("an override ending exactly now must not permit work")
	}

	elsewhere := closed
	elsewhere.Overrides = []Override{{InstallationID: "other", Type: OverrideDowntime, StartsAt: thursdayAfternoon.Add(-time.Hour), EndsAt: thursdayAfternoon.Add(time.Hour)}}
	if ConstraintAllows("inst", elsewhere, thursdayAfternoon) {
		t.Error("an override naming another installation must not permit work here")
	}

	// An override naming no installation covers them all.
	fleetWide := closed
	fleetWide.Overrides = []Override{{Type: OverrideDowntime, StartsAt: thursdayAfternoon.Add(-time.Hour), EndsAt: thursdayAfternoon.Add(time.Hour)}}
	if !ConstraintAllows("inst", fleetWide, thursdayAfternoon) {
		t.Error("an override naming no installation must cover every one")
	}
}

func TestValidateWindowReportsEveryProblem(t *testing.T) {
	t.Parallel()
	if ValidateWindow(nil) != nil {
		t.Error("a nil window has nothing wrong with it")
	}
	if problems := ValidateWindow(window("UTC", "MONDAY/00:00-TUESDAY/00:00")); problems != nil {
		t.Errorf("a valid window must report nothing, got %v", problems)
	}
	if problems := ValidateWindow(window("Mars/Olympus")); len(problems) != 2 {
		t.Errorf("a bad zone and no intervals are two problems, got %v", problems)
	}
}

// A zero-length interval parses cleanly and permits nothing, so it reads as a
// declared window while behaving as if none were declared.
func TestZeroLengthIntervalIsRejected(t *testing.T) {
	t.Parallel()
	problems := ValidateWindow(window("UTC", "MONDAY/09:00-MONDAY/09:00"))
	if len(problems) != 1 || problems[0].Index != 0 || problems[0].Field != FieldIntervals {
		t.Fatalf("want one interval problem at index 0, got %v", problems)
	}
}

func TestValidateWindowPointsAtTheOffendingInterval(t *testing.T) {
	t.Parallel()
	problems := ValidateWindow(window("UTC", "MONDAY/00:00-TUESDAY/00:00", "NOTADAY/00:00-TUESDAY/00:00"))
	if len(problems) != 1 || problems[0].Index != 1 {
		t.Fatalf("want one problem at index 1, got %v", problems)
	}
}
