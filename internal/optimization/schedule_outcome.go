package optimization

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

type ObservedAssignment struct {
	Item       string `json:"item"`
	Resource   string `json:"resource"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt"`
}

type ScheduleOutcome struct {
	RecommendationID string               `json:"recommendationId"`
	Source           string               `json:"source"`
	Revision         string               `json:"revision"`
	ObservedAt       string               `json:"observedAt"`
	Assignments      []ObservedAssignment `json:"assignments"`
}

type ScheduleAccuracy struct {
	RecommendationID         string  `json:"recommendationId"`
	OutcomeSource            string  `json:"outcomeSource"`
	OutcomeRevision          string  `json:"outcomeRevision"`
	PredictedMakespanSeconds float64 `json:"predictedMakespanSeconds"`
	RealizedMakespanSeconds  float64 `json:"realizedMakespanSeconds"`
	SignedErrorSeconds       float64 `json:"signedErrorSeconds"`
	AbsoluteErrorSeconds     float64 `json:"absoluteErrorSeconds"`
	AbsoluteErrorPPMRounded  string  `json:"absoluteErrorPpmRounded,omitempty"`
	AssignmentCount          int     `json:"assignmentCount"`
}

// MeasureScheduleAccuracy requires complete evidence matching every opaque
// item/resource assignment. Makespan is elapsed wall time from the earliest
// observed start to latest finish, including real scheduling gaps.
func MeasureScheduleAccuracy(recommendation Recommendation, outcome ScheduleOutcome) (ScheduleAccuracy, error) {
	if err := VerifyRecommendation(recommendation); err != nil {
		return ScheduleAccuracy{}, err
	}
	if recommendation.Request.Problem.MakespanUnit != "seconds" {
		return ScheduleAccuracy{}, errors.New("schedule accuracy requires makespanUnit seconds")
	}
	if outcome.RecommendationID != recommendation.ID {
		return ScheduleAccuracy{}, errors.New("outcome recommendation id does not match")
	}
	if outcome.Source == "" || outcome.Revision == "" {
		return ScheduleAccuracy{}, errors.New("outcome source and revision are required")
	}
	observedAt, err := time.Parse(time.RFC3339, outcome.ObservedAt)
	if err != nil {
		return ScheduleAccuracy{}, fmt.Errorf("outcome observedAt must be RFC3339: %w", err)
	}
	createdAt, _ := time.Parse(time.RFC3339Nano, recommendation.CreatedAt)
	want := make(map[string]string, len(recommendation.Result.Assignments))
	for _, assignment := range recommendation.Result.Assignments {
		want[assignment.Item] = assignment.Resource
	}
	seen := make(map[string]struct{}, len(outcome.Assignments))
	var earliest, latest time.Time
	for _, assignment := range outcome.Assignments {
		resource, ok := want[assignment.Item]
		if !ok || resource != assignment.Resource {
			return ScheduleAccuracy{}, fmt.Errorf("observed assignment does not match recommendation %q/%q", assignment.Item, assignment.Resource)
		}
		if _, exists := seen[assignment.Item]; exists {
			return ScheduleAccuracy{}, fmt.Errorf("duplicate observed assignment %q", assignment.Item)
		}
		seen[assignment.Item] = struct{}{}
		started, parseErr := time.Parse(time.RFC3339Nano, assignment.StartedAt)
		if parseErr != nil {
			return ScheduleAccuracy{}, fmt.Errorf("assignment %q startedAt: %w", assignment.Item, parseErr)
		}
		finished, parseErr := time.Parse(time.RFC3339Nano, assignment.FinishedAt)
		if parseErr != nil {
			return ScheduleAccuracy{}, fmt.Errorf("assignment %q finishedAt: %w", assignment.Item, parseErr)
		}
		if finished.Before(started) {
			return ScheduleAccuracy{}, fmt.Errorf("assignment %q finishes before it starts", assignment.Item)
		}
		if started.Before(createdAt) {
			return ScheduleAccuracy{}, fmt.Errorf("assignment %q starts before recommendation", assignment.Item)
		}
		if finished.After(observedAt) {
			return ScheduleAccuracy{}, fmt.Errorf("assignment %q finishes after outcome observation", assignment.Item)
		}
		if earliest.IsZero() || started.Before(earliest) {
			earliest = started
		}
		if latest.IsZero() || finished.After(latest) {
			latest = finished
		}
	}
	if len(seen) != len(want) {
		return ScheduleAccuracy{}, fmt.Errorf("outcome covers %d of %d assignments", len(seen), len(want))
	}
	realized := 0.0
	if len(seen) > 0 {
		realized = latest.Sub(earliest).Seconds()
	}
	predicted := recommendation.Result.RoundedMakespan
	signed := realized - predicted
	absolute := math.Abs(signed)
	metric := ScheduleAccuracy{RecommendationID: recommendation.ID, OutcomeSource: outcome.Source, OutcomeRevision: outcome.Revision, PredictedMakespanSeconds: predicted, RealizedMakespanSeconds: realized, SignedErrorSeconds: signed, AbsoluteErrorSeconds: absolute, AssignmentCount: len(seen)}
	if realized > 0 {
		metric.AbsoluteErrorPPMRounded = ratioPartsPerMillion(absolute, realized)
	}
	return metric, nil
}

func ratioPartsPerMillion(numerator, denominator float64) string {
	// Inputs are bounded finite schedule values. Formatting a rounded integer is
	// explicit measurement precision rather than an exact monetary calculation.
	return fmt.Sprintf("%.0f", numerator*1_000_000/denominator)
}

// StableObservedAssignments sorts outcome evidence without interpreting IDs.
func StableObservedAssignments(assignments []ObservedAssignment) []ObservedAssignment {
	result := append([]ObservedAssignment(nil), assignments...)
	sort.Slice(result, func(i, j int) bool { return result[i].Item < result[j].Item })
	return result
}
