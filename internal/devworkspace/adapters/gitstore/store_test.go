package gitstore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/devworkspace/application"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

type memAudit struct {
	events []auditItem
}

type auditItem struct {
	subject, action, resource, decision, reason string
}

func (a *memAudit) Record(_ context.Context, subject, action, resource, decision, reason string) error {
	a.events = append(a.events, auditItem{subject, action, resource, decision, reason})
	return nil
}

func TestGitStoreReadsCommittedStateOnlyAndExcludesFilesOutsideRepo(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	store, err := Open(Options{RootDir: tmpDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	ws := domain.Touch(domain.Workspace{
		ID:            "wsp_git_1",
		ProfileID:     "prof-node",
		EnvironmentID: "env-dev",
		Owner:         "alice",
		Title:         "Sample Service",
		Status:        domain.StatusRunning,
	}, time.Now().UTC())
	if err := store.Save(ctx, ws); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Write a file outside /repo (e.g., in the workspace volume root /scratch)
	outsidePath := filepath.Join(store.WorkspaceDir(ws.ID), "scratch", "notes.txt")
	if err := os.MkdirAll(filepath.Dir(outsidePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outsidePath, []byte("scratch outside /repo"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write an uncommitted file inside /repo
	uncommittedPath := filepath.Join(store.RepoDir(ws.ID), "uncommitted.txt")
	if err := os.WriteFile(uncommittedPath, []byte("dirty working tree"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Tree and Read must only see committed Git state (README.md), NOT uncommitted.txt or scratch/notes.txt
	tree, err := store.Tree().List(ctx, ws.ID, ".", "", 50)
	if err != nil {
		t.Fatalf("Tree.List: %v", err)
	}
	if len(tree.Entries) != 1 || tree.Entries[0].Path != "README.md" {
		t.Fatalf("expected only committed README.md, got %#v", tree.Entries)
	}
	if _, err := store.Tree().Read(ctx, ws.ID, "uncommitted.txt", ""); err != domain.ErrMissing {
		t.Fatalf("expected ErrMissing for uncommitted file, got %v", err)
	}

	// Diff before Sync must be empty even with dirty working-tree files
	diffBefore, err := store.Tree().Diff(ctx, ws.ID)
	if err != nil {
		t.Fatalf("Diff before sync: %v", err)
	}
	if len(diffBefore.Files) != 0 {
		t.Fatalf("expected 0 diff files before commit, got %#v", diffBefore.Files)
	}

	// Sync commits /repo files to the workspace branch
	synced, err := store.Sync(ctx, ws.ID, "commit working tree")
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if synced.HeadCommit == "" || synced.HeadCommit == synced.BaseCommit {
		t.Fatalf("expected new HeadCommit after Sync, got base=%q head=%q", synced.BaseCommit, synced.HeadCommit)
	}

	// Now committed state includes uncommitted.txt and Diff shows it
	view, err := store.Tree().Read(ctx, ws.ID, "uncommitted.txt", "")
	if err != nil || view.Content != "dirty working tree" {
		t.Fatalf("Read after Sync: %#v, err=%v", view, err)
	}
	diffAfter, err := store.Tree().Diff(ctx, ws.ID)
	if err != nil || len(diffAfter.Files) != 1 || diffAfter.Files[0].Path != "uncommitted.txt" {
		t.Fatalf("Diff after Sync: %#v, err=%v", diffAfter, err)
	}
}

func TestGitStoreRefusesSecretFilesOnSyncAndWriteAndCommit(t *testing.T) {
	ctx := context.Background()
	store, err := Open(Options{RootDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ws := domain.Touch(domain.Workspace{
		ID:            "wsp_sec_1",
		ProfileID:     "prof-node",
		EnvironmentID: "env-dev",
		Owner:         "alice",
		Title:         "Secrets Check",
	}, time.Now().UTC())
	if err := store.Save(ctx, ws); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteAndCommit(ctx, ws.ID, map[string]string{".env": "TOKEN=123"}, "bad commit"); err == nil {
		t.Fatal("expected WriteAndCommit to reject .env secret file")
	}
	if err := os.WriteFile(filepath.Join(store.RepoDir(ws.ID), "tls.key"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Sync(ctx, ws.ID, "sync secret"); err == nil {
		t.Fatal("expected Sync to reject tls.key secret file")
	}
}

func TestIdleReapPushesPreservationCommitAndRetainsVolumeOnPushFailure(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	bareUpstream := filepath.Join(tmpDir, "upstream.git")
	if out, err := exec.CommandContext(ctx, "git", "init", "--bare", bareUpstream).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, string(out))
	}

	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	store, err := Open(Options{
		RootDir:         filepath.Join(tmpDir, "store"),
		UpstreamDir:     bareUpstream,
		RetentionWindow: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	audit := &memAudit{}
	svc := application.New(application.Dependencies{
		Records: store,
		Files:   store.Tree(),
		Runtime: store,
		Pods:    store.Pods(),
		Auditor: audit,
		Now:     func() time.Time { return now },
		Idle:    30 * time.Minute,
	})

	// 1. Workspace with reachable upstream remote: dirty /repo file gets committed to a preservation commit and pushed on idle reap
	wsPush := domain.Workspace{
		ID:            "wsp_push_ok",
		ProfileID:     "prof-1",
		EnvironmentID: "env-dev",
		Owner:         "alice",
		Title:         "Push OK",
		Status:        domain.StatusRunning,
		LastUsed:      now.Add(-45 * time.Minute),
		UpdatedAt:     now.Add(-45 * time.Minute),
		CreatedAt:     now.Add(-time.Hour),
	}
	if err := store.Save(ctx, wsPush); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.RepoDir(wsPush.ID), "zarf.yaml"), []byte("kind: ZarfPackageConfig\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 2. Workspace with unreachable remote: dirty /repo file gets committed locally, push fails, volume is preserved for 7 days
	wsPreserve := domain.Workspace{
		ID:            "wsp_push_fail",
		ProfileID:     "prof-1",
		EnvironmentID: "env-dev",
		Owner:         "bob",
		Title:         "Push Fail Preserve Volume",
		RepoURL:       filepath.Join(tmpDir, "nonexistent-upstream.git"),
		Status:        domain.StatusRunning,
		LastUsed:      now.Add(-45 * time.Minute),
		UpdatedAt:     now.Add(-45 * time.Minute),
		CreatedAt:     now.Add(-time.Hour),
	}
	if err := store.Save(ctx, wsPreserve); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.RepoDir(wsPreserve.ID), "Dockerfile"), []byte("FROM scratch\nUSER 65532\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reaped, err := svc.Reap(ctx, now)
	if err != nil || len(reaped) != 2 {
		t.Fatalf("Reap: reaped=%v, err=%v", reaped, err)
	}

	// Verify wsp_push_ok pushed its preservation commit to bareUpstream
	afterPush, err := store.Get(ctx, wsPush.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterPush.VolumePreserved {
		t.Fatalf("expected VolumePreserved=false when push succeeded, got %#v", afterPush)
	}
	refOut, err := exec.CommandContext(ctx, "git", "--git-dir", bareUpstream, "rev-parse", "refs/heads/"+afterPush.Branch).CombinedOutput()
	if err != nil || strings.TrimSpace(string(refOut)) != afterPush.HeadCommit {
		t.Fatalf("upstream branch ref = %q (err=%v), want %q", strings.TrimSpace(string(refOut)), err, afterPush.HeadCommit)
	}

	// Verify wsp_push_fail marked VolumePreserved=true for the 7-day retention window
	afterPreserve, err := store.Get(ctx, wsPreserve.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantUntil := now.Add(7 * 24 * time.Hour)
	if !afterPreserve.VolumePreserved || !afterPreserve.PreservedUntil.Equal(wantUntil) {
		t.Fatalf("afterPreserve = %#v, want VolumePreserved=true until %v", afterPreserve, wantUntil)
	}
	if _, err := os.Stat(store.RepoDir(wsPreserve.ID)); err != nil {
		t.Fatalf("preserved volume should still exist before retention expiry: %v", err)
	}

	// Advance clock past the 7-day retention window and run Reap again
	afterRetention := now.Add(7*24*time.Hour + time.Minute)
	if _, err := svc.Reap(ctx, afterRetention); err != nil {
		t.Fatalf("Reap after retention: %v", err)
	}
	expiredWS, err := store.Get(ctx, wsPreserve.ID)
	if err != nil {
		t.Fatal(err)
	}
	if expiredWS.VolumePreserved || !expiredWS.PreservedUntil.IsZero() {
		t.Fatalf("expected VolumePreserved cleared after retention expiry, got %#v", expiredWS)
	}
	if _, err := os.Stat(store.WorkspaceDir(wsPreserve.ID)); !os.IsNotExist(err) {
		t.Fatalf("expected preserved volume directory deleted after retention window, stat err = %v", err)
	}
}
