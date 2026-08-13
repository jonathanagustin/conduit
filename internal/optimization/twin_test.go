package optimization

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBuildTwinSnapshotSeparatesValidAndKnowledgeTime(t *testing.T) {
	base := TemporalFact{EntityID: "release-a", Attribute: "status", Value: json.RawMessage(`"healthy"`), Unit: "state", Plane: StateObserved, ValidFrom: "2026-08-08T10:00:00Z", RecordedAt: "2026-08-08T10:01:00Z", Source: "spoke", SourceRevision: "1", RequestDigest: "sha256:req", ResponseDigest: "sha256:res", CollectorIdentity: "spiffe://observer", Fidelity: FidelityExact}
	healthy, err := NewTemporalFact(base)
	if err != nil {
		t.Fatal(err)
	}
	base.Value = json.RawMessage(`"failed"`)
	base.SourceRevision = "2"
	base.RecordedAt = "2026-08-08T10:03:00Z"
	failed, err := NewTemporalFact(base)
	if err != nil {
		t.Fatal(err)
	}
	requirement := []TwinRequirement{{ID: "release-status", EntityID: "release-a", Attribute: "status", Unit: "state", Plane: StateObserved, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 600}}
	validAt := time.Date(2026, 8, 8, 10, 2, 0, 0, time.UTC)
	early, err := BuildTwinSnapshot(requirement, []TemporalFact{healthy, failed}, validAt, time.Date(2026, 8, 8, 10, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if early.Fields[0].Status != KnowledgeKnown || string(early.Fields[0].Value) != `"healthy"` {
		t.Fatalf("early = %#v", early)
	}
	late, err := BuildTwinSnapshot(requirement, []TemporalFact{healthy, failed}, validAt, time.Date(2026, 8, 8, 10, 4, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if late.Fields[0].Status != KnowledgeConflicting || len(late.Fields[0].FactIDs) != 2 {
		t.Fatalf("late = %#v", late)
	}
}

func TestBuildTwinSnapshotReportsStaleAndRejectsTamper(t *testing.T) {
	fact, err := NewTemporalFact(TemporalFact{EntityID: "user", Attribute: "latency", Value: json.RawMessage(`125`), Unit: "ms", Plane: StateExperienced, ValidFrom: "2026-08-08T10:00:00Z", RecordedAt: "2026-08-08T10:00:01Z", Source: "rum", SourceRevision: "1", RequestDigest: "sha256:req", ResponseDigest: "sha256:res", CollectorIdentity: "collector", Fidelity: FidelityExact})
	if err != nil {
		t.Fatal(err)
	}
	requirement := []TwinRequirement{{ID: "latency", EntityID: "user", Attribute: "latency", Unit: "ms", Plane: StateExperienced, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 30}}
	snapshot, err := BuildTwinSnapshot(requirement, []TemporalFact{fact}, time.Date(2026, 8, 8, 10, 1, 0, 0, time.UTC), time.Date(2026, 8, 8, 10, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Fields[0].Status != KnowledgeStale {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	fact.Value = json.RawMessage(`126`)
	if err := VerifyTemporalFact(fact); err == nil {
		t.Fatal("accepted tampered fact")
	}
}

func TestBuildTwinSnapshotCompletenessAllowsExplicitNotApplicableOnly(t *testing.T) {
	applicable := false
	snapshot, err := BuildTwinSnapshot([]TwinRequirement{{ID: "external-cost", EntityID: "system", Attribute: "external-cost", Unit: "USD", Plane: StateBilled, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 60, Applicable: &applicable}}, nil, time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC), time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Complete || snapshot.Fields[0].Status != KnowledgeNotApplicable {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if err := VerifyTwinSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	reference, err := RecommendationSnapshot(snapshot, "twin-store", "r1")
	if err != nil || reference.ID != snapshot.ID {
		t.Fatalf("reference = %#v err=%v", reference, err)
	}
	applicable = true
	incomplete, err := BuildTwinSnapshot([]TwinRequirement{{ID: "external-cost", EntityID: "system", Attribute: "external-cost", Unit: "USD", Plane: StateBilled, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 60, Applicable: &applicable}}, nil, time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC), time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecommendationSnapshot(incomplete, "twin-store", "r1"); err == nil {
		t.Fatal("accepted incomplete recommendation snapshot")
	}
}

func TestProviderEvidenceConformancePreservesZeroAndConflictAsDistinctStates(t *testing.T) {
	validAt := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	base := TemporalFact{EntityID: "environment/dev", Attribute: "ready", Value: json.RawMessage(`0`), Unit: "count", Plane: StateObserved, ValidFrom: validAt.Format(time.RFC3339Nano), RecordedAt: validAt.Format(time.RFC3339Nano), Source: "kubernetes.nodes", SourceRevision: "rv:1", RequestDigest: "sha256:req1", ResponseDigest: "sha256:res1", CollectorIdentity: "collector:kubernetes", Fidelity: FidelityExact}
	zero, err := NewTemporalFact(base)
	if err != nil {
		t.Fatal(err)
	}
	requirement := []TwinRequirement{{ID: "nodes.ready", EntityID: "environment/dev", Attribute: "ready", Unit: "count", Plane: StateObserved, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 30}}
	snapshot, err := BuildTwinSnapshot(requirement, []TemporalFact{zero}, validAt, validAt.Add(time.Second))
	if err != nil || snapshot.Fields[0].Status != KnowledgeKnown || string(snapshot.Fields[0].Value) != "0" || !snapshot.Complete {
		t.Fatalf("zero evidence was treated as missing: %#v, %v", snapshot, err)
	}
	conflicting := base
	conflicting.Value = json.RawMessage(`1`)
	conflicting.Source = "prometheus.scalar"
	conflicting.SourceRevision = "query:2"
	conflicting.RequestDigest = "sha256:req2"
	conflicting.ResponseDigest = "sha256:res2"
	one, err := NewTemporalFact(conflicting)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = BuildTwinSnapshot(requirement, []TemporalFact{zero, one}, validAt, validAt.Add(time.Second))
	if err != nil || snapshot.Fields[0].Status != KnowledgeConflicting || snapshot.Complete {
		t.Fatalf("conflicting evidence was collapsed: %#v, %v", snapshot, err)
	}
}
