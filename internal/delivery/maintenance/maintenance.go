// Package maintenance owns when work is allowed to happen: the weekly windows
// an operator declares, the overrides that suspend them, and the question every
// Plan step has to answer before it runs — may this installation be touched
// now?
//
// It is Delivery's, not the Environment's. An Environment declares a window and
// a Release may declare one too, but deciding whether a given moment falls
// inside them, and which of several windows governs, is part of delivering a
// change rather than part of describing where it lands.
//
// The Window declared here is deliberately this package's own rather than the
// catalogue's. The two are the same two fields today, and converting is one
// function on the caller's side; sharing the type instead would put an edge
// between Delivery and Catalog for nothing, and the direction would be wrong.
//
// This package imports nothing.
package maintenance

import (
	"regexp"
	"strconv"
	"time"
)

// Window is a recurring weekly period, in a named time zone, during which work
// may happen. Intervals are DAY/HH:MM-DAY/HH:MM and may wrap the week's end.
type Window struct {
	TimeZone  string
	Intervals []string
}

// Override suspends the ordinary windows for a bounded period. An override with
// no InstallationID applies to every installation the constraint covers.
type Override struct {
	InstallationID string
	Type           string
	StartsAt       time.Time
	EndsAt         time.Time
}

// Override types.
const (
	OverrideDowntime   = "DOWNTIME"
	OverrideNoDowntime = "NO_DOWNTIME"
)

// Constraint is every window bearing on one installation at once: the
// Environment's downtime and no-downtime windows, the Release's own window, and
// any overrides in force.
type Constraint struct {
	RequiresDowntime bool
	Downtime         *Window
	NoDowntime       *Window
	Product          *Window
	Overrides        []Override
}

var weeklyIntervalPattern = regexp.MustCompile(
	`^(MONDAY|TUESDAY|WEDNESDAY|THURSDAY|FRIDAY|SATURDAY|SUNDAY)/([0-9]{2}):([0-9]{2})-` +
		`(MONDAY|TUESDAY|WEDNESDAY|THURSDAY|FRIDAY|SATURDAY|SUNDAY)/([0-9]{2}):([0-9]{2})$`,
)

// The week starts on Monday, which is Apollo's convention and not Go's.
var weekdayOffsets = map[string]int{
	"MONDAY": 0, "TUESDAY": 1, "WEDNESDAY": 2, "THURSDAY": 3,
	"FRIDAY": 4, "SATURDAY": 5, "SUNDAY": 6,
}

// interval is a half-open range of minutes-since-Monday-midnight. start > end
// means the interval wraps the end of the week.
type interval struct {
	start int
	end   int
}

func parseInterval(value string) (interval, bool) {
	parts := weeklyIntervalPattern.FindStringSubmatch(value)
	if parts == nil {
		return interval{}, false
	}
	startHour, _ := strconv.Atoi(parts[2])
	startMinute, _ := strconv.Atoi(parts[3])
	endHour, _ := strconv.Atoi(parts[5])
	endMinute, _ := strconv.Atoi(parts[6])
	if startHour > 23 || endHour > 23 || startMinute > 59 || endMinute > 59 {
		return interval{}, false
	}
	return interval{
		start: weekdayOffsets[parts[1]]*24*60 + startHour*60 + startMinute,
		end:   weekdayOffsets[parts[4]]*24*60 + endHour*60 + endMinute,
	}, true
}

// WindowContains reports whether a moment falls inside a window.
//
// A nil window contains everything: declaring no window is declaring no
// restriction, not declaring a window nothing fits in. An unloadable time zone
// contains nothing, which is the opposite direction on purpose — a window whose
// zone cannot be resolved has not been evaluated, and treating that as
// permission would run work outside every window ever declared.
func WindowContains(window *Window, at time.Time) bool {
	if window == nil {
		return true
	}
	location, err := time.LoadLocation(window.TimeZone)
	if err != nil {
		return false
	}
	local := at.In(location)
	weekday := (int(local.Weekday()) + 6) % 7 // Go Sunday=0; Apollo week starts Monday.
	position := weekday*24*60 + local.Hour()*60 + local.Minute()
	for _, value := range window.Intervals {
		parsed, ok := parseInterval(value)
		if !ok {
			continue
		}
		if parsed.start < parsed.end && position >= parsed.start && position < parsed.end {
			return true
		}
		if parsed.start > parsed.end && (position >= parsed.start || position < parsed.end) {
			return true
		}
	}
	return false
}

// ConstraintAllows reports whether one installation may be worked on now.
//
// Overrides are checked first and can only permit, never forbid: an operator
// opening a window is overruling the schedule, and a schedule that could
// overrule them back would make the override pointless. A NO_DOWNTIME override
// does not authorize work that requires downtime, which is why it is only
// honoured when the constraint does not require any.
//
// The Release's own window then binds unconditionally — a Release declaring
// when it may be installed is not overridden by the Environment being open —
// after which the Environment's windows apply, downtime or otherwise.
func ConstraintAllows(installationID string, constraint Constraint, at time.Time) bool {
	for _, override := range constraint.Overrides {
		if override.InstallationID != "" && override.InstallationID != installationID {
			continue
		}
		if at.Before(override.StartsAt) || !at.Before(override.EndsAt) {
			continue
		}
		if override.Type == OverrideDowntime || (!constraint.RequiresDowntime && override.Type == OverrideNoDowntime) {
			return true
		}
	}

	if !WindowContains(constraint.Product, at) {
		return false
	}
	if constraint.RequiresDowntime {
		return WindowContains(constraint.Downtime, at)
	}
	return WindowContains(constraint.NoDowntime, at) || WindowContains(constraint.Downtime, at)
}

// WindowProblem is one thing wrong with a declared window. Index is the
// offending interval's position, or -1 when the problem is the window itself.
type WindowProblem struct {
	Field   string
	Index   int
	Message string
}

// Field names a WindowProblem can carry.
const (
	FieldTimeZone  = "timeZone"
	FieldIntervals = "intervals"
)

// ValidateWindow reports everything wrong with a window rather than stopping at
// the first problem, so an operator fixing one does not discover the next on
// the following attempt.
func ValidateWindow(window *Window) []WindowProblem {
	if window == nil {
		return nil
	}
	var problems []WindowProblem
	if _, err := time.LoadLocation(window.TimeZone); err != nil {
		problems = append(problems, WindowProblem{Field: FieldTimeZone, Index: -1, Message: "must be an IANA time zone"})
	}
	if len(window.Intervals) == 0 {
		problems = append(problems, WindowProblem{Field: FieldIntervals, Index: -1, Message: "must not be empty"})
	}
	for index, value := range window.Intervals {
		// A zero-length interval is rejected as well as an unparseable one. It
		// parses cleanly and permits nothing, so it reads as a declared window
		// while behaving as if none were declared.
		parsed, ok := parseInterval(value)
		if !ok || parsed.start == parsed.end {
			problems = append(problems, WindowProblem{
				Field:   FieldIntervals,
				Index:   index,
				Message: "must use DAY/HH:MM-DAY/HH:MM with distinct endpoints",
			})
		}
	}
	return problems
}
