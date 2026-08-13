package optimization

import (
	"strings"
	"testing"
	"time"
)

const testSnapshotID = "snap_0000000000000000000000000000000000000000000000000000000000000000"

func TestRecommendationIdentityIsReproducibleAndCommitsToEvidence(t *testing.T) {
	request := RecommendationRequest{APIVersion: EvidenceAPIVersion, Kind: "RecommendationRequest", Snapshot: Snapshot{ID: testSnapshotID, Source: "mutation-observer", Revision: "sha256:abc", CapturedAt: "2026-08-08T12:00:00Z"}, Objective: Objective{Primary: "minimize_makespan", Secondary: []string{"minimize_estimated_cost"}}, Constraints: []ConstraintEvidence{{ID: "cpu", Source: "codespace", Revision: "4-vcpu", ObservedAt: "2026-08-08T11:59:00Z"}}, Problem: Problem{Resources: []Resource{{ID: "worker", Capacity: 1}}, Items: []WorkItem{{ID: "opaque-mutant", Demand: 3}}}}
	first, err := Recommend(request, time.Date(2026, 8, 8, 12, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Recommend(request, time.Date(2026, 8, 8, 12, 2, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.CreatedAt == second.CreatedAt {
		t.Fatalf("identity/timestamps = %q/%q %q/%q", first.ID, second.ID, first.CreatedAt, second.CreatedAt)
	}
	request.Snapshot.Revision = "sha256:def"
	changed, err := Recommend(request, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if changed.ID == first.ID {
		t.Fatal("snapshot revision did not change recommendation identity")
	}
}

func TestRecommendationRejectsConstraintFromFutureSnapshot(t *testing.T) {
	request := RecommendationRequest{APIVersion: EvidenceAPIVersion, Kind: "RecommendationRequest", Snapshot: Snapshot{ID: testSnapshotID, Source: "source", Revision: "r", CapturedAt: "2026-08-08T12:00:00Z"}, Objective: Objective{Primary: "minimize_makespan"}, Constraints: []ConstraintEvidence{{ID: "future", Source: "source", Revision: "r", ObservedAt: "2026-08-08T12:00:01Z"}}, Problem: Problem{Resources: []Resource{{ID: "r", Capacity: 1}}, Items: []WorkItem{{ID: "i", Demand: 1}}}}
	if _, err := Recommend(request, time.Now()); err == nil {
		t.Fatal("accepted future constraint evidence")
	}
}

func TestRecommendationRejectsDuplicateSecondaryObjectiveNames(t *testing.T) {
	request := testRecommendationRequest()
	request.Objective.Secondary = []string{"minimize_estimated_cost", "minimize_estimated_cost"}
	request.Problem.CostModel = &CostModel{Currency: "USD", Source: "fixture", AsOf: "2026-08-08T11:00:00Z"}
	for index := range request.Problem.Items {
		request.Problem.Items[index].EstimatedCostMicros = map[string]int64{"amd64": 1, "arm64": 2}
	}
	if _, err := NewSolverRequest(request, SolverSelection{PreferredBackend: "highs"}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20}); err == nil || !strings.Contains(err.Error(), "duplicate secondary objective") {
		t.Fatalf("duplicate secondary objective error = %v", err)
	}
}

func TestRecommendationRejectsNonContentAddressedSnapshot(t *testing.T) {
	request := RecommendationRequest{APIVersion: EvidenceAPIVersion, Kind: "RecommendationRequest", Snapshot: Snapshot{ID: "mutable-current", Source: "source", Revision: "r", CapturedAt: "2026-08-08T12:00:00Z"}, Objective: Objective{Primary: "minimize_makespan"}, Problem: Problem{Resources: []Resource{{ID: "r", Capacity: 1}}, Items: []WorkItem{{ID: "i", Demand: 1}}}}
	if _, err := Recommend(request, time.Now()); err == nil {
		t.Fatal("accepted mutable snapshot coordinate")
	}
}

func TestRecommendationCanonicalizesSemanticSetOrderAndRejectsTampering(t *testing.T) {
	request := RecommendationRequest{APIVersion: EvidenceAPIVersion, Kind: "RecommendationRequest", Snapshot: Snapshot{ID: testSnapshotID, Source: "source", Revision: "r", CapturedAt: "2026-08-08T12:00:00Z"}, Objective: Objective{Primary: "minimize_makespan", Secondary: []string{"minimize_estimated_cost"}}, Constraints: []ConstraintEvidence{{ID: "b", Source: "s", Revision: "2", ObservedAt: "2026-08-08T11:00:00Z"}, {ID: "a", Source: "s", Revision: "1", ObservedAt: "2026-08-08T11:00:00Z"}}, Problem: Problem{Resources: []Resource{{ID: "b", Capacity: 1}, {ID: "a", Capacity: 1}}, Items: []WorkItem{{ID: "i", Demand: 1, Eligible: []string{"b", "a"}}}}}
	first, err := Recommend(request, time.Date(2026, 8, 8, 12, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	request.Constraints[0], request.Constraints[1] = request.Constraints[1], request.Constraints[0]
	request.Problem.Resources[0], request.Problem.Resources[1] = request.Problem.Resources[1], request.Problem.Resources[0]
	request.Problem.Items[0].Eligible[0], request.Problem.Items[0].Eligible[1] = request.Problem.Items[0].Eligible[1], request.Problem.Items[0].Eligible[0]
	second, err := Recommend(request, time.Date(2026, 8, 8, 12, 2, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("semantic reorder changed identity: %s != %s", first.ID, second.ID)
	}
	first.Result.Assignments[0].Resource = "tampered"
	if err := VerifyRecommendation(first); err == nil {
		t.Fatal("accepted tampered recommendation")
	}
}
