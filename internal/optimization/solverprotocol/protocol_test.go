package solverprotocol

import (
	"reflect"
	"strings"
	"testing"
)

func TestCompileHiGHSModelDirectTransportFreeContract(t *testing.T) {
	request := protocolTestRequest(t)
	model, err := CompileHiGHSModel(request)
	if err != nil {
		t.Fatal(err)
	}
	if model.RequestID != request.ID || model.ModelDigest != request.Model.Digest || model.Seed != 0 || model.Workers != 1 {
		t.Fatalf("compiled binding = %#v", model)
	}
	if got, want := model.Columns, []HiGHSColumn{
		{Name: "x[job,a]", Kind: HiGHSBinaryColumn, Item: "job", Resource: "a", UpperBound: 1},
		{Name: "x[job,b]", Kind: HiGHSBinaryColumn, Item: "job", Resource: "b", UpperBound: 1},
		{Name: "T", Kind: HiGHSContinuousColumn, UpperBound: model.ConstructiveMakespanUpperBound},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("compiled columns = %#v, want %#v", got, want)
	}
	if len(model.Rows) != 3 || len(model.Objectives) != 1 || model.Objectives[0].Name != "minimize_makespan" {
		t.Fatalf("compiled matrix shape = %#v", model)
	}
}

func TestVerifySolverRequestRejectsIdentityAndDerivedEvidenceTampering(t *testing.T) {
	request := protocolTestRequest(t)
	for _, testCase := range []struct {
		name   string
		mutate func(*SolverRequest)
	}{
		{name: "id", mutate: func(value *SolverRequest) { value.ID = "solreq_" + strings.Repeat("0", 64) }},
		{name: "model", mutate: func(value *SolverRequest) { value.Model.Digest = "sha256:" + strings.Repeat("0", 64) }},
		{name: "seed", mutate: func(value *SolverRequest) { value.Determinism.Seed = 1 }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := request
			testCase.mutate(&candidate)
			if err := VerifySolverRequest(candidate); err == nil {
				t.Fatal("accepted tampered transport-free request")
			}
		})
	}
}

func protocolTestRequest(t *testing.T) SolverRequest {
	t.Helper()
	recommendation := RecommendationRequest{
		APIVersion: EvidenceAPIVersion, Kind: "RecommendationRequest",
		Snapshot:    Snapshot{ID: "snap_" + strings.Repeat("0", 64), Source: "fixture", Revision: "v1", CapturedAt: "2026-08-11T18:00:00Z"},
		Objective:   Objective{Primary: "minimize_makespan"},
		Constraints: []ConstraintEvidence{{ID: "admission", Source: "fixture", Revision: "v1", ObservedAt: "2026-08-11T17:59:00Z"}},
		Problem:     Problem{Resources: []Resource{{ID: "a", Capacity: 1}, {ID: "b", Capacity: 2}}, Items: []WorkItem{{ID: "job", Demand: 1}}},
	}
	modelDigest, err := digestJSON(recommendation.Problem)
	if err != nil {
		t.Fatal(err)
	}
	hardConstraints := struct {
		Evidence  []ConstraintEvidence `json:"evidence"`
		Items     []WorkItem           `json:"items"`
		Resources []Resource           `json:"resources"`
	}{recommendation.Constraints, recommendation.Problem.Items, recommendation.Problem.Resources}
	constraintsDigest, err := digestJSON(hardConstraints)
	if err != nil {
		t.Fatal(err)
	}
	objectiveDigest, err := digestJSON(recommendation.Objective)
	if err != nil {
		t.Fatal(err)
	}
	request := SolverRequest{
		APIVersion: SolverAPIVersion, Kind: "SolverRequest", Recommendation: recommendation,
		Model:      SolverModel{Name: "allocation.makespan", Revision: AllocationModelRevision, Digest: modelDigest, HardConstraintsDigest: constraintsDigest, ObjectiveDigest: objectiveDigest},
		Capability: AllocationBackendCapability, Selection: SolverSelection{PreferredBackend: "highs"},
		Determinism: SolverDeterminism{Workers: 1}, Limits: SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20},
	}
	digest, err := digestJSON(request)
	if err != nil {
		t.Fatal(err)
	}
	request.ID = "solreq_" + strings.TrimPrefix(digest, "sha256:")
	return request
}
