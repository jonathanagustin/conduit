package gitstore

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/devworkspace/application"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

type captureCreatedWorkspace struct {
	workspace domain.Workspace
	calls     int
}

func (p *captureCreatedWorkspace) Create(_ context.Context, workspace domain.Workspace) error {
	p.workspace = workspace
	p.calls++
	return domain.ErrNoCluster
}
func (*captureCreatedWorkspace) Delete(context.Context, string) error { return nil }

func TestCreateReturnsPersistedGitCoordinatesAndProvisionsTheSameRecord(t *testing.T) {
	for _, podsEnabled := range []bool{false, true} {
		name := "without pods"
		if podsEnabled {
			name = "with no-cluster provisioner"
		}
		t.Run(name, func(t *testing.T) {
			store, err := Open(Options{RootDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			pods := &captureCreatedWorkspace{}
			service := application.New(application.Dependencies{Records: store, Files: store.Files(), Auditor: &memAudit{}, Pods: pods, PodsEnabled: podsEnabled})
			created, err := service.Create(t.Context(), domain.Caller{Username: "ada"}, "local-git", "east", "Created source")
			if err != nil {
				t.Fatal(err)
			}
			saved, err := store.Get(t.Context(), created.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(created, saved) {
				t.Fatalf("creation receipt differs from persisted record: returned=%+v saved=%+v", created, saved)
			}
			if created.Branch != "workspace/ada/"+created.ID || !domain.ValidCommitID(created.HeadCommit) || created.ProposalRevision != created.HeadCommit || created.BaseCommit != created.HeadCommit || created.SourceDigest != "git:"+created.HeadCommit {
				t.Fatalf("missing recorded Git coordinates: %+v", created)
			}
			actual, err := gitOut(t.Context(), store.RepoDir(created.ID), "rev-parse", "HEAD")
			if err != nil || strings.TrimSpace(actual) != created.HeadCommit {
				t.Fatal("receipt differs from actual Git commit", actual, err)
			}
			if podsEnabled {
				if pods.calls != 1 || !reflect.DeepEqual(pods.workspace, saved) {
					t.Fatalf("provisioner received stale workspace: %+v", pods.workspace)
				}
			} else if pods.calls != 0 {
				t.Fatal("disabled provisioner called")
			}
		})
	}
}
