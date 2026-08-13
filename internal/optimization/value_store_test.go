package optimization

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type failingValueTemporary struct {
	writeErr error
	written  int
	synced   bool
	closed   bool
}

func (*failingValueTemporary) Chmod(os.FileMode) error { return nil }
func (file *failingValueTemporary) Write([]byte) (int, error) {
	return file.written, file.writeErr
}
func (file *failingValueTemporary) Sync() error  { file.synced = true; return nil }
func (file *failingValueTemporary) Close() error { file.closed = true; return nil }

func TestValueAssessmentTemporaryPreservesWriteFailure(t *testing.T) {
	want := errors.New("disk full")
	file := &failingValueTemporary{writeErr: want}
	if err := writeValueAssessmentTemporary(file, []byte("record")); !errors.Is(err, want) {
		t.Fatalf("write error = %v, want %v", err, want)
	}
	if file.synced || !file.closed {
		t.Fatalf("failed write lifecycle synced=%t closed=%t", file.synced, file.closed)
	}

	file = &failingValueTemporary{written: 2}
	if err := writeValueAssessmentTemporary(file, []byte("record")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write error = %v", err)
	}
}

func TestFileValueAssessmentStorePersistsImmutableReplayableFrontier(t *testing.T) {
	criteria := []ValueCriterion{{ID: "benefit", Stakeholder: "users", UserSegment: "active users", Function: "finish work", Criticality: "important", Measure: "tasks", Unit: "count", Scale: "CARDINAL", Baseline: "8", Target: "10", Direction: ValueMaximize}}
	estimate := ValueEstimate{Value: "10", Source: "trial", SourceRevision: "run-1", AsOf: "2026-08-08T00:00:00Z", HorizonSeconds: 3600, Distribution: "point"}
	record, err := NewValueAssessmentRecord("snap_0000000000000000000000000000000000000000000000000000000000000000", criteria, []ValueAlternative{testValueAlternative("candidate", map[string]ValueEstimate{"benefit": estimate})}, time.Date(2026, 8, 8, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	store := FileValueAssessmentStore{Directory: filepath.Join(t.TempDir(), "value")}
	if err := store.Save(record); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(record); err != nil {
		t.Fatalf("idempotent save: %v", err)
	}
	loaded, err := store.Get(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != record.ID || len(loaded.Assessment.ParetoEfficient) != 1 {
		t.Fatalf("loaded = %#v", loaded)
	}
	record.Assessment.ParetoEfficient = []string{"forged"}
	if err := store.Save(record); err == nil {
		t.Fatal("stored forged frontier")
	}
	encoded, err := os.ReadFile(filepath.Join(store.Directory, loaded.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	encoded[len(encoded)-2] ^= 1
	if err := os.WriteFile(filepath.Join(store.Directory, loaded.ID+".json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(loaded.ID); err == nil {
		t.Fatal("loaded tampered frontier")
	}
}
