package filejournal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jonathanagustin/conduit/internal/liveevents/domain"
)

func TestCommitRollbackRestartAndDetachedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.json")
	journal, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var escaped *domain.State
	if err := journal.Update(context.Background(), func(state *domain.State) error {
		state.SourcePosition = 7
		state.Producers["source"] = domain.ProducerCheckpoint{Position: 3, Digest: "saved"}
		escaped = state
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	escaped.SourcePosition = 99
	escaped.Producers["source"] = domain.ProducerCheckpoint{Position: 99}
	snapshot, err := journal.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SourcePosition != 7 || snapshot.Producers["source"].Position != 3 {
		t.Fatal("callback alias changed committed state")
	}
	snapshot.Producers["source"] = domain.ProducerCheckpoint{Position: 100}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wanted := errors.New("refuse transaction")
	if err := journal.Update(context.Background(), func(state *domain.State) error {
		state.SourcePosition = 8
		delete(state.Producers, "source")
		return wanted
	}); !errors.Is(err, wanted) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("failed update changed disk")
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	restored, _ := reopened.Snapshot(context.Background())
	if restored.SourcePosition != 7 || restored.Producers["source"].Position != 3 {
		t.Fatal("restart lost committed transaction")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%v", info.Mode())
	}
}
func TestCancellationAndCorruptionDoNotBecomeAnEmptyJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.json")
	journal, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if err := journal.Update(ctx, func(*domain.State) error { called = true; return nil }); !errors.Is(err, context.Canceled) || called {
		t.Fatal("canceled callback ran")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled update created a journal")
	}
	if err := os.WriteFile(path, []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("incomplete journal became an empty state")
	}
	if err := os.WriteFile(path, []byte(`{"version":99,"streams":{},"producers":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("unknown schema became an empty journal")
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"streams":{},"producers":{}} {}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("trailing state accepted")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("publicly readable journal accepted")
	}
	link := filepath.Join(filepath.Dir(path), "linked.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(link); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("symlink journal accepted")
	}
}
