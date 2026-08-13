package optimization

import (
	"testing"
	"time"
)

func TestMeasureValueAccuracyRequiresCompleteMatchingRealization(t *testing.T) {
	criterion := ValueCriterion{ID: "completion", Stakeholder: "users", UserSegment: "active users", Function: "finish work", Criticality: "important", Measure: "completed tasks", Unit: "count", Scale: "CARDINAL", Baseline: "8", Target: "12", Direction: ValueMaximize}
	prediction := ValueEstimate{Value: "10", LowerBound: "8", UpperBound: "12", Source: "experiment", SourceRevision: "r1", AsOf: "2026-08-08T00:00:00Z", HorizonSeconds: 3600, Distribution: "confidence-95"}
	record, err := NewValueAssessmentRecord(testSnapshotID, []ValueCriterion{criterion}, []ValueAlternative{testValueAlternative("candidate", map[string]ValueEstimate{"completion": prediction})}, time.Date(2026, 8, 8, 0, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	realized := ValueEstimate{Value: "11", Source: "product-analytics", SourceRevision: "r2", AsOf: "2026-08-08T01:01:00Z", HorizonSeconds: 3600, Distribution: "point"}
	got, err := MeasureValueAccuracy(record, ValueOutcomeEvidence{AssessmentID: record.ID, AlternativeID: "candidate", ObservedAt: "2026-08-08T01:01:00Z", Values: []RealizedValue{{CriterionID: "completion", Estimate: realized}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].SignedError != "1" || got.Entries[0].AbsoluteError != "1" {
		t.Fatalf("accuracy = %#v", got)
	}
	if _, err := MeasureValueAccuracy(record, ValueOutcomeEvidence{AssessmentID: record.ID, AlternativeID: "candidate", ObservedAt: "2026-08-08T01:01:00Z"}); err == nil {
		t.Fatal("accepted partial realized value")
	}
}
