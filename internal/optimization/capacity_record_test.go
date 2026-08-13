package optimization

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSubstratePreflightRecordIsCanonicalAndContentAddressed(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 1, 0, 0, time.UTC)
	request := validSubstratePreflight()
	request.Dependencies[0], request.Dependencies[1] = request.Dependencies[1], request.Dependencies[0]
	record, err := NewSubstratePreflightRecord("dev-use2", "node-pool-r43", request, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(record.ID, "spf_") || record.Request.Dependencies[0].ID != "cni" {
		t.Fatalf("record = %#v", record)
	}
	if record.APIVersion != "optimization.platform.example/substrate-preflight/v1alpha2" {
		t.Fatalf("apiVersion = %q", record.APIVersion)
	}
	other, err := NewSubstratePreflightRecord("dev-use2", "node-pool-r43", validSubstratePreflight(), now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if other.ID != record.ID {
		t.Fatalf("same evidence ids = %q, %q", record.ID, other.ID)
	}
	if err := VerifySubstratePreflightRecord(record); err != nil {
		t.Fatal(err)
	}
}

func TestSubstratePreflightStorePersistsAcrossRestartAndRejectsTampering(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "preflight")
	record, err := NewSubstratePreflightRecord("dev-use2", "node-pool-r43", validSubstratePreflight(), time.Date(2026, 8, 9, 12, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	store := FileSubstratePreflightStore{Directory: directory}
	if err := store.Save(record); err != nil {
		t.Fatal(err)
	}
	loaded, err := (FileSubstratePreflightStore{Directory: directory}).Get(record.ID)
	if err != nil || loaded.ID != record.ID {
		t.Fatalf("loaded = %#v, error = %v", loaded, err)
	}
	path := filepath.Join(directory, record.ID+".json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("record mode = %o", info.Mode().Perm())
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	document["unexpected"] = true
	encoded, _ = json.Marshal(document)
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(record.ID); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown-field error = %v", err)
	}
}

func TestSubstratePreflightStoreRejectsTrailingJSONAndUnsafeID(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "preflight")
	record, err := NewSubstratePreflightRecord("dev-use2", "node-pool-r43", validSubstratePreflight(), time.Date(2026, 8, 9, 12, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	store := FileSubstratePreflightStore{Directory: directory}
	if err := store.Save(record); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, record.ID+".json")
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, []byte("{}\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(record.ID); err == nil || !strings.Contains(err.Error(), "trailing JSON") {
		t.Fatalf("trailing-json error = %v", err)
	}
	if _, err := store.Get("../" + record.ID); err == nil || !strings.Contains(err.Error(), "invalid substrate preflight id") {
		t.Fatalf("unsafe-id error = %v", err)
	}
}

func TestSubstratePreflightRecordRejectsUnsafeCoordinatesAndPredatedCreation(t *testing.T) {
	request := validSubstratePreflight()
	if _, err := NewSubstratePreflightRecord("../escape", "proposal", request, time.Date(2026, 8, 9, 12, 1, 0, 0, time.UTC)); err == nil {
		t.Fatal("accepted unsafe environment")
	}
	if _, err := NewSubstratePreflightRecord("dev-use2", "proposal", request, time.Date(2026, 8, 9, 11, 59, 0, 0, time.UTC)); err == nil || !strings.Contains(err.Error(), "predate") {
		t.Fatalf("predated error = %v", err)
	}
}

func TestSubstratePreflightRecordRejectsAssessmentTampering(t *testing.T) {
	record, err := NewSubstratePreflightRecord("dev-use2", "proposal", validSubstratePreflight(), time.Date(2026, 8, 9, 12, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	record.Assessment.Eligible = false
	if err := VerifySubstratePreflightRecord(record); err == nil || !strings.Contains(err.Error(), "does not match replay") {
		t.Fatalf("tamper error = %v", err)
	}
}
