package optimization

import (
	"testing"
	"time"
)

func TestMeasureScheduleAccuracyIncludesQueueingAndRequiresExactAssignments(t *testing.T) {
	request := RecommendationRequest{APIVersion: EvidenceAPIVersion, Kind: "RecommendationRequest", Snapshot: Snapshot{ID: testSnapshotID, Source: "observer", Revision: "r", CapturedAt: "2026-08-08T12:00:00Z"}, Objective: Objective{Primary: "minimize_makespan"}, Problem: Problem{MakespanUnit: "seconds", Resources: []Resource{{ID: "a", Capacity: 1}, {ID: "b", Capacity: 1}}, Items: []WorkItem{{ID: "i1", Demand: 2}, {ID: "i2", Demand: 2}}}}
	recommendation, err := Recommend(request, time.Date(2026, 8, 8, 12, 0, 1, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	assignments := recommendation.Result.Assignments
	outcome := ScheduleOutcome{RecommendationID: recommendation.ID, Source: "runner", Revision: "run-1", ObservedAt: "2026-08-08T12:01:00Z", Assignments: []ObservedAssignment{{Item: assignments[0].Item, Resource: assignments[0].Resource, StartedAt: "2026-08-08T12:00:10Z", FinishedAt: "2026-08-08T12:00:12Z"}, {Item: assignments[1].Item, Resource: assignments[1].Resource, StartedAt: "2026-08-08T12:00:11Z", FinishedAt: "2026-08-08T12:00:14Z"}}}
	got, err := MeasureScheduleAccuracy(recommendation, outcome)
	if err != nil {
		t.Fatal(err)
	}
	if got.PredictedMakespanSeconds != 2 || got.RealizedMakespanSeconds != 4 || got.SignedErrorSeconds != 2 || got.AbsoluteErrorPPMRounded != "500000" || got.OutcomeRevision != "run-1" {
		t.Fatalf("accuracy = %#v", got)
	}
	outcome.Assignments[0].Resource = "wrong"
	if _, err := MeasureScheduleAccuracy(recommendation, outcome); err == nil {
		t.Fatal("accepted outcome for wrong resource")
	}
}
