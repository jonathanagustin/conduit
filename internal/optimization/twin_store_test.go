package optimization

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestFileTwinStorePersistsReplayableFactsAndSnapshots(t *testing.T) {
	fact, err := NewTemporalFact(TemporalFact{EntityID: "system", Attribute: "users", Value: json.RawMessage(`42`), Unit: "count", Plane: StateExperienced, ValidFrom: "2026-08-08T10:00:00Z", RecordedAt: "2026-08-08T10:00:01Z", Source: "analytics", SourceRevision: "r1", RequestDigest: "sha256:req", ResponseDigest: "sha256:res", CollectorIdentity: "collector", Fidelity: FidelityExact})
	if err != nil {
		t.Fatal(err)
	}
	requirements := []TwinRequirement{{ID: "users", EntityID: "system", Attribute: "users", Unit: "count", Plane: StateExperienced, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 60}}
	snapshot, err := BuildTwinSnapshot(requirements, []TemporalFact{fact}, time.Date(2026, 8, 8, 10, 0, 30, 0, time.UTC), time.Date(2026, 8, 8, 10, 0, 30, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	store := FileTwinStore{Directory: filepath.Join(t.TempDir(), "twin")}
	if err := store.SaveFact(fact); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	loadedFact, err := store.GetFact(fact.ID)
	if err != nil || loadedFact.ID != fact.ID {
		t.Fatalf("fact = %#v err=%v", loadedFact, err)
	}
	if loadedFact.KnowledgeStatus != KnowledgeKnown || loadedFact.Uncertainty != "none" {
		t.Fatalf("fact metadata = %#v", loadedFact)
	}
	loadedSnapshot, err := store.GetSnapshot(snapshot.ID)
	if err != nil || loadedSnapshot.ID != snapshot.ID {
		t.Fatalf("snapshot = %#v err=%v", loadedSnapshot, err)
	}
	factPath := filepath.Join(store.Directory, "facts", fact.ID+".json")
	encoded, err := os.ReadFile(factPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(factPath, append(encoded, []byte("{}\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetFact(fact.ID); err == nil {
		t.Fatal("accepted trailing input in immutable fact record")
	}
}

func TestFileTwinStoreConcurrentIdempotentIngestion(t *testing.T) {
	fact, err := NewTemporalFact(TemporalFact{EntityID: "system", Attribute: "capacity", Value: json.RawMessage(`8`), Unit: "cpu", Plane: StateObserved, ValidFrom: "2026-08-08T10:00:00Z", RecordedAt: "2026-08-08T10:00:01Z", Source: "inventory", SourceRevision: "r1", RequestDigest: "sha256:req", ResponseDigest: "sha256:res", CollectorIdentity: "collector", Fidelity: FidelityExact})
	if err != nil {
		t.Fatal(err)
	}
	store := FileTwinStore{Directory: filepath.Join(t.TempDir(), "twin")}
	var wait sync.WaitGroup
	errorsSeen := make(chan error, 16)
	for range 16 {
		wait.Add(1)
		go func() { defer wait.Done(); errorsSeen <- store.SaveFact(fact) }()
	}
	wait.Wait()
	close(errorsSeen)
	for saveErr := range errorsSeen {
		if saveErr != nil {
			t.Fatal(saveErr)
		}
	}
	if _, err := store.GetFact(fact.ID); err != nil {
		t.Fatal(err)
	}
}
