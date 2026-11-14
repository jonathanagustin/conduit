package adapters

import (
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLedgerCloseDrainsOperationAndRejectsLateFilesystemAccess(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ledger")
	ledger, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := ledger.lock()
	if err != nil {
		t.Fatal(err)
	}
	started, done := make(chan struct{}), make(chan error, 1)
	go func() { close(started); done <- ledger.Close() }()
	<-started
	select {
	case err := <-done:
		unlock()
		t.Fatal("Close did not wait for the active operation", err)
	case <-time.After(10 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not drain")
	}
	if err := ledger.Close(); err != nil {
		t.Fatal("Close was not idempotent", err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ledger.ReadApproval(context.Background(), "approval"); !errors.Is(err, os.ErrClosed) {
		t.Fatal("late operation was not closed", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("late activity recreated ledger files", err)
	}
}

func TestLedgerClosePreservesCommittedRecordsAndIndependentOwner(t *testing.T) {
	root := t.TempDir()
	first, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	record := domain.ApprovalRecord{ApprovalID: "approval", Decision: domain.DecisionApproved}
	if err := first.PutApproval(record); err != nil {
		t.Fatal(err)
	}
	peer, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	got, ok, err := peer.ReadApproval(t.Context(), record.ApprovalID)
	if err != nil || !ok || got != record {
		t.Fatal("close lost persisted approval or closed another owner", got, ok, err)
	}
}
