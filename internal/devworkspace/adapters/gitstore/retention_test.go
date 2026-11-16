package gitstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/devworkspace/application"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

type expiryRecorder struct {
	beforeErr, afterErr error
	before, after       int
}

func (a *expiryRecorder) BeforeExpire(context.Context, domain.Workspace) error {
	a.before++
	return a.beforeErr
}
func (a *expiryRecorder) AfterExpire(context.Context, domain.Workspace, error) error {
	a.after++
	return a.afterErr
}

func retentionFixture(t *testing.T) (*Store, domain.Workspace, time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	store, err := Open(Options{RootDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ws := domain.Workspace{ID: "wsp_retention", Owner: "operator", Status: domain.StatusStopped, VolumePreserved: true, PreservedUntil: now}
	store.rows[ws.ID] = ws
	if err := store.saveCatalogLocked(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.RepoDir(ws.ID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.RepoDir(ws.ID), "valuable"), []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	return store, ws, now
}

func TestRetentionSkipsActiveAndUnexpiredWorkspaces(t *testing.T) {
	for _, status := range []string{domain.StatusRunning, domain.StatusStarting, domain.StatusInitializing, domain.StatusStopping} {
		t.Run(string(status), func(t *testing.T) {
			store, ws, now := retentionFixture(t)
			ws.Status = status
			store.rows[ws.ID] = ws
			audit := &expiryRecorder{}
			got, err := store.ExpirePreservedVolumes(context.Background(), now.Add(time.Hour), audit)
			if err != nil || len(got) != 0 || audit.before != 0 {
				t.Fatalf("expiry: %v %v %+v", got, err, audit)
			}
			if _, err := os.Stat(store.RepoDir(ws.ID)); err != nil {
				t.Fatal(err)
			}
		})
	}
	store, _, now := retentionFixture(t)
	got, err := store.ExpirePreservedVolumes(context.Background(), now.Add(-time.Nanosecond), &expiryRecorder{})
	if err != nil || len(got) != 0 {
		t.Fatalf("early expiry: %v %v", got, err)
	}
}

func TestRetentionFailuresKeepRetryMetadata(t *testing.T) {
	for _, failure := range []string{"cancel", "intent", "remove", "completion", "catalog", "missing-audit"} {
		t.Run(failure, func(t *testing.T) {
			store, ws, now := retentionFixture(t)
			audit := &expiryRecorder{}
			ctx := context.Background()
			sentinel := errors.New("injected failure")
			originalRemove := store.removeVolume
			originalCatalog := store.catalogPath
			switch failure {
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "intent":
				audit.beforeErr = sentinel
			case "remove":
				store.removeVolume = func(string) error { return sentinel }
			case "completion":
				audit.afterErr = sentinel
			case "catalog":
				store.catalogPath = t.TempDir()
			}
			var got []domain.Workspace
			var err error
			if failure == "missing-audit" {
				got, err = store.ExpirePreservedVolumes(ctx, now, nil)
			} else {
				got, err = store.ExpirePreservedVolumes(ctx, now, audit)
			}
			if err == nil || len(got) != 0 {
				t.Fatalf("expected failure: %v %v", got, err)
			}
			row, _ := store.Get(context.Background(), ws.ID)
			if row != ws {
				t.Fatalf("retry metadata changed: %+v", row)
			}
			if failure != "completion" && failure != "catalog" {
				if _, err := os.Stat(store.RepoDir(ws.ID)); err != nil {
					t.Fatal(err)
				}
			}
			store.removeVolume = originalRemove
			store.catalogPath = originalCatalog
			got, err = store.ExpirePreservedVolumes(context.Background(), now, &expiryRecorder{})
			if err != nil || len(got) != 1 {
				t.Fatalf("retry: %v %v", got, err)
			}
			reopened, err := Open(Options{RootDir: store.rootDir})
			if err != nil {
				t.Fatal(err)
			}
			row, _ = reopened.Get(context.Background(), ws.ID)
			if row.VolumePreserved || !row.PreservedUntil.IsZero() {
				t.Fatalf("expiry not persisted: %+v", row)
			}
			if _, err := os.Stat(store.WorkspaceDir(ws.ID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("volume remains: %v", err)
			}
		})
	}
}

func TestRetentionRemovalConfinedToStore(t *testing.T) {
	store, ws, now := retentionFixture(t)
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "valuable")
	if err := os.WriteFile(sentinel, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(store.rootDir, "workspaces")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(store.rootDir, "workspaces")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(outside, ws.ID), 0700); err != nil {
		t.Fatal(err)
	}
	got, err := store.ExpirePreservedVolumes(context.Background(), now, &expiryRecorder{})
	if err == nil || len(got) != 0 {
		t.Fatalf("escaped root: %v %v", got, err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, ws.ID)); err != nil {
		t.Fatal(err)
	}
	if err := store.removeWorkspaceVolume("../../outside"); err == nil {
		t.Fatal("accepted unsafe ID")
	}
}

func TestPreservationCatalogFailurePreventsComputeCleanup(t *testing.T) {
	store, _, now := retentionFixture(t)
	ws := domain.Workspace{ID: "wsp_running", Owner: "operator", Status: domain.StatusRunning, LastUsed: now.Add(-time.Hour)}
	if err := store.Save(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	store.runtimes[ws.ID] = domain.Runtime{WorkspaceID: ws.ID, Status: domain.StatusRunning}
	original, _ := store.Get(context.Background(), ws.ID)
	store.catalogPath = t.TempDir()
	service := application.New(application.Dependencies{Records: store, Files: store.Files(), Runtime: store, Auditor: &memAudit{}, Idle: time.Minute, Now: func() time.Time { return now }})
	got, err := service.Reap(context.Background(), now)
	if err == nil || len(got) != 0 {
		t.Fatalf("cleanup continued: %v %v", got, err)
	}
	if store.runtimes[ws.ID].Status != domain.StatusRunning {
		t.Fatal("compute stopped without durable preservation")
	}
	row, _ := store.Get(context.Background(), ws.ID)
	if row != original {
		t.Fatalf("catalog failure leaked state: %+v", row)
	}
}

func TestRetentionSkipsRuntimeStartedBeforeCatalogUpdate(t *testing.T) {
	store, ws, now := retentionFixture(t)
	store.runtimes[ws.ID] = domain.Runtime{WorkspaceID: ws.ID, Status: domain.StatusRunning}
	got, err := store.ExpirePreservedVolumes(context.Background(), now, &expiryRecorder{})
	if err != nil || len(got) != 0 {
		t.Fatalf("active runtime expiry: %v %v", got, err)
	}
	if _, err := os.Stat(store.RepoDir(ws.ID)); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteFailureRetainsWorkspaceRecord(t *testing.T) {
	for _, failure := range []string{"remove", "catalog"} {
		t.Run(failure, func(t *testing.T) {
			store, ws, _ := retentionFixture(t)
			if failure == "remove" {
				store.removeVolume = func(string) error { return errors.New("failed removal") }
			} else {
				store.catalogPath = t.TempDir()
			}
			if err := store.Delete(context.Background(), ws.ID); err == nil {
				t.Fatal("delete falsely succeeded")
			}
			row, err := store.Get(context.Background(), ws.ID)
			if err != nil || row != ws {
				t.Fatalf("record lost: %+v %v", row, err)
			}
		})
	}
}

func TestCatalogRejectsUnsafeAndDuplicateIDs(t *testing.T) {
	for _, raw := range []string{`[{"id":"../outside"}]`, `[{"id":"wsp_duplicate"},{"id":"wsp_duplicate"}]`} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "workspaces.json"), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(Options{RootDir: root}); err == nil {
			t.Fatalf("accepted catalog: %s", raw)
		}
	}
}

func TestPreservationWithoutOriginIsDurablyRetained(t *testing.T) {
	store, _, now := retentionFixture(t)
	ws := domain.Workspace{ID: "wsp_no_origin", Owner: "operator", Status: domain.StatusRunning}
	if err := store.Save(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.RepoDir(ws.ID), "notes.txt"), []byte("work"), 0600); err != nil {
		t.Fatal(err)
	}
	row, err := store.PreserveOnIdle(context.Background(), ws, now)
	if !errors.Is(err, domain.ErrBranchNotPushed) || !row.VolumePreserved || !row.PreservedUntil.Equal(now.Add(store.RetentionWindow())) {
		t.Fatalf("unpreserved branch: %+v %v", row, err)
	}
	reopened, err := Open(Options{RootDir: store.rootDir})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := reopened.Get(context.Background(), ws.ID)
	if err != nil || persisted != row {
		t.Fatalf("not persisted: %+v %v", persisted, err)
	}
	subject, err := gitOut(context.Background(), store.RepoDir(ws.ID), "log", "-1", "--format=%s")
	if err != nil || strings.TrimSpace(subject) != "chore(workspace): preserve idle work" {
		t.Fatalf("subject: %q %v", subject, err)
	}
}

func TestSaveDoesNotRecreateExpiredRepository(t *testing.T) {
	store, ws, now := retentionFixture(t)
	if _, err := store.ExpirePreservedVolumes(context.Background(), now, &expiryRecorder{}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), ws); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing repository reinitialized: %v", err)
	}
	if _, err := os.Stat(store.RepoDir(ws.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("repository recreated: %v", err)
	}
}
