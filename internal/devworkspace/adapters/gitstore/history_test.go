package gitstore

import (
	"errors"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoryUsesRealCommitsBoundsTheGraphAndLeavesDirtyFilesUntouched(t *testing.T) {
	store, err := Open(Options{RootDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(t.Context(), domain.Workspace{ID: "wsp_history", Owner: "ada", Branch: "workspace/ada/history", ProposalRevision: "rev-1"}); err != nil {
		t.Fatal(err)
	}
	var row domain.Workspace
	for i := range 23 {
		row, err = store.WriteAndCommit(t.Context(), "wsp_history", map[string]string{"code.txt": fmt.Sprint(i)}, fmt.Sprintf("fix: recorded change %d <literal>", i))
		if err != nil {
			t.Fatal(err)
		}
	}
	dirty := filepath.Join(store.RepoDir(row.ID), "dirty.txt")
	if err := os.WriteFile(dirty, []byte("uncommitted source"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := gitOut(t.Context(), store.RepoDir(row.ID), "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	history, err := store.History(t.Context(), row.ID, row.HeadCommit)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Entries) != domain.MaxHistoryEntries || !history.Truncated || history.HeadCommit != row.HeadCommit || history.Entries[0].Commit != row.HeadCommit || history.Entries[0].Subject != "fix: recorded change 22 <literal>" {
		t.Fatalf("history=%+v", history)
	}
	for _, entry := range history.Entries {
		if !domain.ValidCommitID(entry.Commit) || entry.CommittedAt.IsZero() {
			t.Fatal("invalid recorded metadata", entry)
		}
	}
	after, err := gitOut(t.Context(), store.RepoDir(row.ID), "status", "--porcelain")
	if err != nil || before != after {
		t.Fatal("history changed working tree", err)
	}
	if _, err := store.History(t.Context(), row.ID, "HEAD~1"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("revision syntax accepted", err)
	}
	if _, err := store.History(t.Context(), row.ID, history.Entries[1].Commit); !errors.Is(err, domain.ErrStale) {
		t.Fatal("stale recorded head accepted", err)
	}
	if err := runGit(t.Context(), store.RepoDir(row.ID), "commit", "--allow-empty", "-m", "chore: external unrecorded head"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.History(t.Context(), row.ID, row.HeadCommit); !errors.Is(err, domain.ErrStale) {
		t.Fatal("unrecorded Git HEAD accepted", err)
	}
}

func TestHistoryDeclinesOversizedGitMetadataInsteadOfReturningAPartialHistory(t *testing.T) {
	store, err := Open(Options{RootDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(t.Context(), domain.Workspace{ID: "wsp_large_history", Owner: "ada"}); err != nil {
		t.Fatal(err)
	}
	row, err := store.WriteAndCommit(t.Context(), "wsp_large_history", map[string]string{"code.txt": "recorded"}, "fix: "+strings.Repeat("x", 70000))
	if err != nil {
		t.Fatal(err)
	}
	history, err := store.History(t.Context(), row.ID, row.HeadCommit)
	if err == nil || len(history.Entries) != 0 {
		t.Fatal("oversized metadata appeared complete", err)
	}
}
