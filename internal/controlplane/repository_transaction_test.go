package controlplane

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileRepositoryTransactionUnderReadOnlyParent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory read-only mode bits")
	}

	parent := t.TempDir()
	dataDir := filepath.Join(parent, "state")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	// Simulate a writable PVC mounted inside a readOnlyRootFilesystem container.
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatalf("chmod parent read-only: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(parent, 0o700)
	})

	repo, err := NewFileRepository(dataDir)
	if err != nil {
		t.Fatalf("new file repository: %v", err)
	}
	ctx := context.Background()

	// Commit a transaction with a nested transaction inside.
	err = repo.Transaction(ctx, func(tx Repository) error {
		if _, err := tx.PutTeam(ctx, Team{Name: "ops", DisplayName: "Ops", Revision: 1}); err != nil {
			return err
		}
		nestedTx, ok := tx.(*FileRepository)
		if !ok {
			t.Fatalf("expected *FileRepository inside Transaction, got %T", tx)
		}
		return nestedTx.Transaction(ctx, func(nested Repository) error {
			_, err := nested.PutTeam(ctx, Team{Name: "sec", DisplayName: "Sec", Revision: 1})
			return err
		})
	})
	if err != nil {
		t.Fatalf("transaction under read-only parent failed: %v", err)
	}

	if _, err := repo.GetTeam(ctx, "ops"); err != nil {
		t.Fatalf("expected committed team ops: %v", err)
	}
	if _, err := repo.GetTeam(ctx, "sec"); err != nil {
		t.Fatalf("expected nested committed team sec: %v", err)
	}

	// Verify rollback leaves previous state intact and cleans up staging dirs.
	sentinel := errors.New("abort transaction")
	err = repo.Transaction(ctx, func(tx Repository) error {
		if _, err := tx.PutTeam(ctx, Team{Name: "rolled-back", DisplayName: "Rolled Back", Revision: 1}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got: %v", err)
	}
	if _, err := repo.GetTeam(ctx, "rolled-back"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected rolled-back team to be absent, got: %v", err)
	}

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatalf("read dataDir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".txn-") {
			t.Fatalf("leftover transaction staging directory: %s", entry.Name())
		}
	}
}
