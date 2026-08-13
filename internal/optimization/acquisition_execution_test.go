package optimization

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type acquisitionReadFunc func(context.Context, AcquisitionReadRequest) (AcquisitionReadResponse, error)

func (function acquisitionReadFunc) Read(ctx context.Context, request AcquisitionReadRequest) (AcquisitionReadResponse, error) {
	return function(ctx, request)
}

type acquisitionExtractFunc func(AcquisitionReadRequest, AcquisitionReadResponse) ([]ExtractedAcquisitionFact, error)

func (function acquisitionExtractFunc) Extract(request AcquisitionReadRequest, response AcquisitionReadResponse) ([]ExtractedAcquisitionFact, error) {
	return function(request, response)
}

func TestAcquisitionExecutorPersistsOnlyNormalizedBitemporalFacts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "twin")
	store := FileTwinStore{Directory: root}
	body := []byte(`{"count":3,"secret":"must-not-persist"}`)
	executor := AcquisitionExecutor{
		Reader: acquisitionReadFunc(func(_ context.Context, request AcquisitionReadRequest) (AcquisitionReadResponse, error) {
			if request.Method != "GET" || request.Endpoint != "/v1/environments/env-1/reported-state" {
				t.Fatalf("request = %#v", request)
			}
			return AcquisitionReadResponse{StatusCode: 200, ObservedAt: "2026-08-11T12:00:00Z", SourceRevision: "report-r7", Body: body}, nil
		}),
		Extractor: acquisitionExtractFunc(func(_ AcquisitionReadRequest, response AcquisitionReadResponse) ([]ExtractedAcquisitionFact, error) {
			var payload struct {
				Count int64 `json:"count"`
			}
			if err := json.Unmarshal(response.Body, &payload); err != nil {
				return nil, err
			}
			value, _ := json.Marshal(payload.Count)
			return []ExtractedAcquisitionFact{{RequirementID: "observed.replicas", EntityID: "env-1", Attribute: "ready-replicas", Value: value, Unit: "count", Plane: StateObserved, Fidelity: FidelityExact, ValidFrom: response.ObservedAt}}, nil
		}),
		Facts:             store,
		CollectorIdentity: "spoke:env-1",
		Now:               acquisitionClock(time.Date(2026, 8, 11, 12, 0, 1, 0, time.UTC)),
	}
	requirements := []StateRequirement{{ID: "observed.replicas", Plane: StateObserved, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 10}}
	calls := []AcquisitionCall{{ID: "hub.reported-state", Source: "platform-hub", Method: "GET", Endpoint: "/v1/environments/env-1/reported-state", Consistency: "exact-spoke-report", CostUnits: 1, Observations: []FieldObservation{{Requirement: "observed.replicas", Plane: StateObserved, Fidelity: FidelityExact, MaximumStalenessSeconds: 5}}}}
	receipt, err := executor.Execute(context.Background(), requirements, calls)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Complete || !receipt.PlannedComplete || len(receipt.FactIDs) != 1 || receipt.Calls[0].Status != AcquisitionCallComplete {
		t.Fatalf("receipt = %#v", receipt)
	}
	if err := VerifyAcquisitionExecutionReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	fact, err := store.GetFact(receipt.FactIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(fact.Value) != "3" || fact.ValidFrom != "2026-08-11T12:00:00Z" || fact.RecordedAt != "2026-08-11T12:00:02Z" || !validDigest(fact.RequestDigest) || fact.ResponseDigest != sha256Digest(body) {
		t.Fatalf("fact = %#v", fact)
	}
	stored, err := os.ReadFile(filepath.Join(root, "facts", fact.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "must-not-persist") || strings.Contains(string(stored), `\"secret\"`) {
		t.Fatalf("raw response leaked into fact store: %s", stored)
	}
	snapshot, err := BuildTwinSnapshot([]TwinRequirement{{ID: "observed.replicas", EntityID: "env-1", Attribute: "ready-replicas", Unit: "count", Plane: StateObserved, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 10}}, []TemporalFact{fact}, time.Date(2026, 8, 11, 12, 0, 3, 0, time.UTC), time.Date(2026, 8, 11, 12, 0, 3, 0, time.UTC))
	if err != nil || !snapshot.Complete || snapshot.Fields[0].Status != KnowledgeKnown {
		t.Fatalf("snapshot = %#v, error = %v", snapshot, err)
	}
}

func TestAcquisitionExecutorRejectsMutationBeforeInvokingPort(t *testing.T) {
	invocations := 0
	executor := AcquisitionExecutor{
		Reader: acquisitionReadFunc(func(context.Context, AcquisitionReadRequest) (AcquisitionReadResponse, error) {
			invocations++
			return AcquisitionReadResponse{}, nil
		}),
		Extractor: acquisitionExtractFunc(func(AcquisitionReadRequest, AcquisitionReadResponse) ([]ExtractedAcquisitionFact, error) {
			return nil, nil
		}),
		Facts:             FileTwinStore{Directory: t.TempDir()},
		CollectorIdentity: "collector:test",
		Now:               acquisitionClock(time.Now()),
	}
	requirement := []StateRequirement{{ID: "desired.state", Plane: StateDesired, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 10}}
	call := []AcquisitionCall{{ID: "mutate", Source: "provider", Method: "POST", Endpoint: "/state", Consistency: "strong", CostUnits: 1, Observations: []FieldObservation{{Requirement: "desired.state", Plane: StateDesired, Fidelity: FidelityExact, MaximumStalenessSeconds: 1}}}}
	if _, err := executor.Execute(context.Background(), requirement, call); err == nil || !strings.Contains(err.Error(), "read-only GET") {
		t.Fatalf("mutation error = %v", err)
	}
	if invocations != 0 {
		t.Fatalf("mutating port invoked %d times", invocations)
	}
	call[0].Method = "GET"
	call[0].Endpoint = "/state/{tenantID}"
	if _, err := executor.Execute(context.Background(), requirement, call); err == nil || !strings.Contains(err.Error(), "resolved relative endpoint") {
		t.Fatalf("template error = %v", err)
	}
	if invocations != 0 {
		t.Fatalf("unresolved port invoked %d times", invocations)
	}
}

func TestAcquisitionExecutorPreservesUnavailableAndStaleState(t *testing.T) {
	t.Run("unavailable", func(t *testing.T) {
		executor := AcquisitionExecutor{
			Reader: acquisitionReadFunc(func(context.Context, AcquisitionReadRequest) (AcquisitionReadResponse, error) {
				return AcquisitionReadResponse{}, errors.Join(errors.New("credential-bearing provider error is not retained"), AcquisitionReadError{Category: AcquisitionFailureTransport, Retryable: true})
			}),
			Extractor: acquisitionExtractFunc(func(AcquisitionReadRequest, AcquisitionReadResponse) ([]ExtractedAcquisitionFact, error) {
				t.Fatal("extractor invoked")
				return nil, nil
			}),
			Facts:             FileTwinStore{Directory: t.TempDir()},
			CollectorIdentity: "collector:test",
			Now:               acquisitionClock(time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)),
		}
		requirements, calls := oneObservedAcquisition()
		receipt, err := executor.Execute(context.Background(), requirements, calls)
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Complete || receipt.Calls[0].Status != AcquisitionCallUnavailable || receipt.Calls[0].FailureCategory != AcquisitionFailureTransport || !receipt.Calls[0].Retryable || len(receipt.FactIDs) != 0 || len(receipt.UnmetRequirements) != 1 {
			t.Fatalf("receipt = %#v", receipt)
		}
		encoded, _ := json.Marshal(receipt)
		if strings.Contains(string(encoded), "credential-bearing") {
			t.Fatalf("provider error leaked into receipt: %s", encoded)
		}
	})

	t.Run("stale", func(t *testing.T) {
		store := FileTwinStore{Directory: t.TempDir()}
		executor := AcquisitionExecutor{
			Reader: acquisitionReadFunc(func(context.Context, AcquisitionReadRequest) (AcquisitionReadResponse, error) {
				return AcquisitionReadResponse{StatusCode: 200, ObservedAt: "2026-08-11T11:59:59Z", SourceRevision: "r1", Body: []byte(`{"ready":1}`)}, nil
			}),
			Extractor: acquisitionExtractFunc(func(_ AcquisitionReadRequest, response AcquisitionReadResponse) ([]ExtractedAcquisitionFact, error) {
				return []ExtractedAcquisitionFact{{RequirementID: "observed.state", EntityID: "env", Attribute: "ready", Value: json.RawMessage(`1`), Unit: "count", Plane: StateObserved, Fidelity: FidelityExact, ValidFrom: "2026-08-11T11:59:40Z"}}, nil
			}),
			Facts: store, CollectorIdentity: "collector:test", Now: acquisitionClock(time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)),
		}
		requirements, calls := oneObservedAcquisition()
		receipt, err := executor.Execute(context.Background(), requirements, calls)
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Complete || receipt.Calls[0].Status != AcquisitionCallStale || len(receipt.FactIDs) != 1 {
			t.Fatalf("receipt = %#v", receipt)
		}
		fact, err := store.GetFact(receipt.FactIDs[0])
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := BuildTwinSnapshot([]TwinRequirement{{ID: "observed.state", EntityID: "env", Attribute: "ready", Unit: "count", Plane: StateObserved, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 10}}, []TemporalFact{fact}, time.Date(2026, 8, 11, 12, 0, 3, 0, time.UTC), time.Date(2026, 8, 11, 12, 0, 3, 0, time.UTC))
		if err != nil || snapshot.Complete || snapshot.Fields[0].Status != KnowledgeStale {
			t.Fatalf("snapshot = %#v, error = %v", snapshot, err)
		}
	})
}

func TestAcquisitionExecutorRequiresRuntimePlaneAndRequirementEvidence(t *testing.T) {
	requirements := []StateRequirement{
		{ID: "admitted.plan", Plane: StateAdmitted, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 10},
		{ID: "executing.plan", Plane: StateExecuting, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 10},
	}
	calls := []AcquisitionCall{{ID: "hub.plan", Source: "platform-hub", Method: "GET", Endpoint: "/v1/plans/plan-1", Consistency: "repository-read", CostUnits: 1, Observations: []FieldObservation{
		{Requirement: "admitted.plan", Plane: StateAdmitted, Fidelity: FidelityExact, MaximumStalenessSeconds: 1},
		{Requirement: "executing.plan", Plane: StateExecuting, Fidelity: FidelityExact, MaximumStalenessSeconds: 1},
	}}}
	store := FileTwinStore{Directory: t.TempDir()}
	executor := AcquisitionExecutor{
		Reader: acquisitionReadFunc(func(context.Context, AcquisitionReadRequest) (AcquisitionReadResponse, error) {
			return AcquisitionReadResponse{StatusCode: 200, ObservedAt: "2026-08-11T12:00:00Z", SourceRevision: "plan-r2", Body: []byte(`{"status":"APPROVED"}`)}, nil
		}),
		Extractor: acquisitionExtractFunc(func(_ AcquisitionReadRequest, response AcquisitionReadResponse) ([]ExtractedAcquisitionFact, error) {
			return []ExtractedAcquisitionFact{{RequirementID: "admitted.plan", EntityID: "plan-1", Attribute: "approval-status", Value: json.RawMessage(`"APPROVED"`), Unit: "state", Plane: StateAdmitted, Fidelity: FidelityExact, ValidFrom: response.ObservedAt}}, nil
		}),
		Facts: store, CollectorIdentity: "collector:test", Now: acquisitionClock(time.Date(2026, 8, 11, 12, 0, 1, 0, time.UTC)),
	}
	receipt, err := executor.Execute(context.Background(), requirements, calls)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Complete || receipt.Calls[0].Status != AcquisitionCallPartial || len(receipt.Calls[0].SatisfiedRequirements) != 1 || receipt.UnmetRequirements[0] != "executing.plan" {
		t.Fatalf("receipt = %#v", receipt)
	}

	executor.Extractor = acquisitionExtractFunc(func(_ AcquisitionReadRequest, response AcquisitionReadResponse) ([]ExtractedAcquisitionFact, error) {
		return []ExtractedAcquisitionFact{{RequirementID: "admitted.plan", EntityID: "plan-1", Attribute: "approval-status", Value: json.RawMessage(`"APPROVED"`), Unit: "state", Plane: StateExecuting, Fidelity: FidelityExact, ValidFrom: response.ObservedAt}}, nil
	})
	if _, err := executor.Execute(context.Background(), requirements, calls); err == nil || !strings.Contains(err.Error(), "incompatible plane") {
		t.Fatalf("cross-plane error = %v", err)
	}
}

func TestAcquisitionExecutorBoundsResponseBeforeExtraction(t *testing.T) {
	extractorInvocations := 0
	requirements, calls := oneObservedAcquisition()
	executor := AcquisitionExecutor{
		Reader: acquisitionReadFunc(func(context.Context, AcquisitionReadRequest) (AcquisitionReadResponse, error) {
			return AcquisitionReadResponse{StatusCode: 200, ObservedAt: "2026-08-11T12:00:00Z", SourceRevision: "r1", Body: []byte("12345")}, nil
		}),
		Extractor: acquisitionExtractFunc(func(AcquisitionReadRequest, AcquisitionReadResponse) ([]ExtractedAcquisitionFact, error) {
			extractorInvocations++
			return nil, nil
		}),
		Facts: FileTwinStore{Directory: t.TempDir()}, CollectorIdentity: "collector:test", MaximumResponseBytes: 4,
		Now: acquisitionClock(time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)),
	}
	if _, err := executor.Execute(context.Background(), requirements, calls); err == nil || !strings.Contains(err.Error(), "between 1 and 4 bytes") {
		t.Fatalf("response bound error = %v", err)
	}
	if extractorInvocations != 0 {
		t.Fatalf("oversized response reached extractor %d times", extractorInvocations)
	}
}

func TestAcquisitionExecutionReceiptRejectsTampering(t *testing.T) {
	requirements, calls := oneObservedAcquisition()
	executor := AcquisitionExecutor{
		Reader: acquisitionReadFunc(func(context.Context, AcquisitionReadRequest) (AcquisitionReadResponse, error) {
			return AcquisitionReadResponse{}, errors.New("unavailable")
		}),
		Extractor: acquisitionExtractFunc(func(AcquisitionReadRequest, AcquisitionReadResponse) ([]ExtractedAcquisitionFact, error) {
			return nil, nil
		}),
		Facts:             FileTwinStore{Directory: t.TempDir()},
		CollectorIdentity: "collector:test",
		Now:               acquisitionClock(time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)),
	}
	receipt, err := executor.Execute(context.Background(), requirements, calls)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Calls[0].Status = AcquisitionCallComplete
	if err := VerifyAcquisitionExecutionReceipt(receipt); err == nil {
		t.Fatal("accepted tampered acquisition receipt")
	}
}

func oneObservedAcquisition() ([]StateRequirement, []AcquisitionCall) {
	return []StateRequirement{{ID: "observed.state", Plane: StateObserved, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 10}}, []AcquisitionCall{{ID: "observe", Source: "provider", Method: "GET", Endpoint: "/state", Consistency: "source-read", CostUnits: 1, Observations: []FieldObservation{{Requirement: "observed.state", Plane: StateObserved, Fidelity: FidelityExact, MaximumStalenessSeconds: 1}}}}
}

func acquisitionClock(start time.Time) func() time.Time {
	count := 0
	return func() time.Time {
		value := start.Add(time.Duration(count) * time.Second)
		count++
		return value
	}
}
