package gitstore

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/devworkspace/application"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

func forkIdentityFixture(t *testing.T, withOrigin bool) (*Store, domain.Workspace, *application.Service, string) {
	t.Helper()
	root := t.TempDir()
	upstream := ""
	if withOrigin {
		upstream = filepath.Join(root, "upstream.git")
		if err := runGit(t.Context(), "", "init", "--bare", upstream); err != nil {
			t.Fatal(err)
		}
	}
	store, err := Open(Options{RootDir: filepath.Join(root, "workspaces"), UpstreamDir: upstream})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(t.Context(), domain.Workspace{ID: "wsp_fork_parent", Owner: "ada", EnvironmentID: "east", Status: domain.StatusStopped, ProposalRevision: "rev-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteAndCommit(t.Context(), "wsp_fork_parent", map[string]string{"code.txt": "reviewed source"}, "fix: reviewed parent source"); err != nil {
		t.Fatal(err)
	}
	parent, err := store.Get(t.Context(), "wsp_fork_parent")
	if err != nil {
		t.Fatal(err)
	}
	service := application.New(application.Dependencies{Records: store, Files: store.Files(), Auditor: &memAudit{}})
	return store, parent, service, upstream
}

func TestForkReturnsItsOwnRecordedGitBranch(t *testing.T) {
	store, parent, service, _ := forkIdentityFixture(t, true)
	child, err := service.Fork(t.Context(), domain.Caller{Username: "ada"}, parent.ID, "Fork")
	if err != nil {
		t.Fatal(err)
	}
	actual, err := gitOut(t.Context(), store.RepoDir(child.ID), "symbolic-ref", "--short", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if child.Branch == parent.Branch || child.Branch != strings.TrimSpace(actual) {
		t.Fatalf("child branch=%q actual=%q parent=%q", child.Branch, actual, parent.Branch)
	}
	after, err := store.Get(t.Context(), parent.ID)
	if err != nil || !reflect.DeepEqual(parent, after) {
		t.Fatal("fork changed parent metadata", err)
	}
}

func TestForkPinsTheRecordedHeadInsteadOfAnUnrecordedParentCommit(t *testing.T) {
	store, parent, service, _ := forkIdentityFixture(t, true)
	if err := runGit(t.Context(), store.RepoDir(parent.ID), "commit", "--allow-empty", "-m", "chore: unrecorded outside commit"); err != nil {
		t.Fatal(err)
	}
	child, err := service.Fork(t.Context(), domain.Caller{Username: "ada"}, parent.ID, "Pinned fork")
	if err != nil {
		t.Fatal(err)
	}
	actual, err := gitOut(t.Context(), store.RepoDir(child.ID), "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(actual) != parent.HeadCommit || child.HeadCommit != parent.HeadCommit {
		t.Fatalf("fork copied unrecorded head: recorded=%s child=%s actual=%s", parent.HeadCommit, child.HeadCommit, actual)
	}
}

func TestForkPreservationPushesToTheActualUpstreamNotTheParentCheckout(t *testing.T) {
	store, parent, service, upstream := forkIdentityFixture(t, true)
	child, err := service.Fork(t.Context(), domain.Caller{Username: "ada"}, parent.ID, "Upstream fork")
	if err != nil {
		t.Fatal(err)
	}
	origin, err := gitOut(t.Context(), store.RepoDir(child.ID), "remote", "get-url", "origin")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(origin) != upstream {
		t.Fatalf("fork origin=%q want actual upstream=%q", origin, upstream)
	}
	preserved, err := store.PreserveOnIdle(t.Context(), child, time.Now().UTC())
	if err != nil || preserved.VolumePreserved {
		t.Fatal("local fixture upstream push failed", err)
	}
	pushed, err := gitOut(t.Context(), "", "--git-dir", upstream, "rev-parse", "refs/heads/"+child.Branch)
	if err != nil || strings.TrimSpace(pushed) != child.HeadCommit {
		t.Fatal("fork branch was not observed in the isolated bare upstream", err)
	}
}

func TestNoOriginForkDoesNotInventAPushDestination(t *testing.T) {
	store, parent, service, _ := forkIdentityFixture(t, false)
	child, err := service.Fork(t.Context(), domain.Caller{Username: "ada"}, parent.ID, "No upstream")
	if err != nil {
		t.Fatal(err)
	}
	remotes, err := gitOut(t.Context(), store.RepoDir(child.ID), "remote")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(remotes) != "" {
		t.Fatal("fork invented local-parent upstream", remotes)
	}
	preserved, err := store.PreserveOnIdle(t.Context(), child, time.Now().UTC())
	if !errors.Is(err, domain.ErrBranchNotPushed) || !preserved.VolumePreserved || preserved.PreservedUntil.IsZero() {
		t.Fatal("missing upstream did not preserve child volume", err)
	}
}

func TestFreshForkDoesNotInheritExpiredParentPreservation(t *testing.T) {
	store, parent, service, _ := forkIdentityFixture(t, false)
	parent.VolumePreserved = true
	parent.PreservedUntil = time.Now().UTC().Add(-time.Hour)
	if err := store.Save(t.Context(), parent); err != nil {
		t.Fatal(err)
	}
	child, err := service.Fork(t.Context(), domain.Caller{Username: "ada"}, parent.ID, "Fresh fork")
	if err != nil {
		t.Fatal(err)
	}
	if child.VolumePreserved || !child.PreservedUntil.IsZero() {
		t.Fatal("fresh fork inherited parent retention deadline", child.PreservedUntil)
	}
}

func TestForkCopyRefusesStaleHeadBeforeCreatingCheckout(t *testing.T) {
	store, parent, _, _ := forkIdentityFixture(t, false)
	childID := "wsp_stale_child"
	if err := store.CopyAt(t.Context(), parent.ID, childID, strings.Repeat("b", 40)); !errors.Is(err, domain.ErrStale) {
		t.Fatalf("stale copy error=%v", err)
	}
	if _, err := os.Stat(store.RepoDir(childID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale copy created checkout: %v", err)
	}
}

func TestForkCopyRejectsInvalidDestination(t *testing.T) {
	store, parent, _, _ := forkIdentityFixture(t, false)
	for _, id := range []string{"../escaped", parent.ID} {
		if err := store.CopyAt(t.Context(), parent.ID, id, parent.HeadCommit); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("destination %q error=%v", id, err)
		}
	}
}
