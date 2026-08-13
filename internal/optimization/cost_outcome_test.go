package optimization

import (
	"testing"
	"time"
)

func TestMeasureCostAccuracyRequiresCompleteMatchingEvidence(t *testing.T) {
	request := RecommendationRequest{APIVersion: EvidenceAPIVersion, Kind: "RecommendationRequest", Snapshot: Snapshot{ID: testSnapshotID, Source: "observer", Revision: "r", CapturedAt: "2026-08-08T00:00:00Z"}, Objective: Objective{Primary: "minimize_makespan", Secondary: []string{"minimize_estimated_cost"}}, Problem: Problem{CostModel: &CostModel{Currency: "USD", Source: "price-list-v7", AsOf: "2026-08-08T00:00:00Z"}, Resources: []Resource{{ID: "worker-a", Capacity: 1}}, Items: []WorkItem{{ID: "opaque-1", Demand: 1, EstimatedCostMicros: map[string]int64{"worker-a": 200_003}}}}}
	recommendation, err := Recommend(request, time.Date(2026, 8, 8, 0, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	got, err := MeasureCostAccuracy(recommendation, OutcomeCostEvidence{RecommendationID: recommendation.ID, Currency: "USD", Source: "billing-export-42", Revision: "sha256:42", ObservedAt: "2026-08-09T00:00:00Z", Costs: []RealizedCost{{Item: "opaque-1", Resource: "worker-a", CostMicros: 250_003}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.SignedErrorMicros != 50_000 || got.AbsoluteErrorMicros != 50_000 || got.AbsoluteErrorPartsPerMillion != "199997" {
		t.Fatalf("accuracy = %#v", got)
	}

	_, err = MeasureCostAccuracy(recommendation, OutcomeCostEvidence{RecommendationID: recommendation.ID, Currency: "USD", Source: "billing-export-42", Revision: "sha256:42", ObservedAt: "2026-08-09T00:00:00Z", Costs: []RealizedCost{{Item: "opaque-1", Resource: "worker-b", CostMicros: 250_003}}})
	if err == nil {
		t.Fatal("accepted cost attributed to a different assignment")
	}
	_, err = MeasureCostAccuracy(recommendation, OutcomeCostEvidence{RecommendationID: recommendation.ID, Currency: "USD", Source: "billing-export-42", Revision: "sha256:42", ObservedAt: "2026-08-09T00:00:00Z"})
	if err == nil {
		t.Fatal("accepted partial realized cost evidence")
	}
}
