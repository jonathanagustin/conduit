package application

import (
	"context"
	"fmt"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

type stubWriter struct {
	calls     int
	proposals []domain.PackageProposal
}

func (stub *stubWriter) ProposePackage(context.Context, domain.PackagingContext) (domain.PackageProposal, domain.Provenance, error) {
	i := stub.calls
	if i >= len(stub.proposals) {
		i = len(stub.proposals) - 1
	}
	stub.calls++
	return stub.proposals[i], domain.Provenance{Adapter: "stub"}, nil
}

type stubStore struct {
	commits []map[string]string
}

func (store *stubStore) Prepare(context.Context, string, string, string, string) (string, string, error) {
	return "/tmp/ws", "workspace/demo", nil
}
func (store *stubStore) Commit(_ context.Context, _ string, files map[string]string, _ string) (string, error) {
	copied := map[string]string{}
	for k, v := range files {
		copied[k] = v
	}
	store.commits = append(store.commits, copied)
	return fmt.Sprintf("%040d", len(store.commits)), nil
}

type stubChecks struct {
	failTimes int
	seen      int
	lints     int
	creates   int
}

func (checks *stubChecks) Dockerfile(string, string) string { return "" }
func (checks *stubChecks) PackageCR(string) string          { return "" }
func (checks *stubChecks) Lint(context.Context, string) error {
	checks.lints++
	if checks.seen < checks.failTimes {
		checks.seen++
		return fmt.Errorf("zarf dev lint: component image is not pinned")
	}
	return nil
}
func (checks *stubChecks) Create(context.Context, string, string) (string, error) {
	checks.creates++
	return "/tmp/ws/out/zarf-package-static-web.tar.zst", nil
}

func TestWritePackageRepairsLintFailuresAndThenPasses(t *testing.T) {
	writer := &stubWriter{proposals: []domain.PackageProposal{
		{Source: "name: web\n", Zarf: "apiVersion: zarf.dev/v1alpha1\n", CR: "apiVersion: uds.dev/v1alpha1\nkind: Package\n", Dockerfile: "FROM scratch\nUSER 65532\n", Dockerignore: ".git\n", Rationale: "first draft"},
		{Source: "name: web\n", Zarf: "apiVersion: zarf.dev/v1alpha1\n", CR: "apiVersion: uds.dev/v1alpha1\nkind: Package\n", Dockerfile: "FROM scratch\nUSER 65532\n", Dockerignore: ".git\n", Rationale: "pin the image"},
	}}
	store := &stubStore{}
	checks := &stubChecks{failTimes: 1}
	result, err := (WorkspacePackager{Writer: writer, Store: store, Checks: checks}).WritePackage(context.Background(), "/tmp/ws", "https://git.example/web.git", "main", "workspace/demo", domain.PackagingContext{RunID: "run-1"})
	if err != nil || result.Error != "" {
		t.Fatalf("result = %#v %v", result, err)
	}
	if result.Outcome != string(domain.OutcomePass) || len(result.Attempts) != 2 || checks.creates != 1 {
		t.Fatalf("result = %#v checks=%#v", result, checks)
	}
	if result.Attempts[0].Outcome != string(domain.OutcomeFailRequired) || result.Attempts[1].Outcome != string(domain.OutcomePass) {
		t.Fatalf("attempts = %#v", result.Attempts)
	}
	if writer.calls != 2 || len(store.commits) != 2 {
		t.Fatalf("calls=%d commits=%d", writer.calls, len(store.commits))
	}
	if _, ok := store.commits[0]["Dockerfile"]; !ok || store.commits[0]["zarf.yaml"] == "" || store.commits[0]["uds-package.yaml"] == "" {
		t.Fatalf("files = %#v", store.commits[0])
	}
	if result.Provenance.RepoURL != "https://git.example/web.git" || result.Provenance.ResolvedCommit == "" || result.Provenance.DockerfileSHA256 == "" {
		t.Fatalf("provenance = %#v", result.Provenance)
	}
}

func TestWritePackageStopsAfterTwoRepairs(t *testing.T) {
	writer := &stubWriter{proposals: []domain.PackageProposal{
		{Source: "name: web\n", Zarf: "bad", CR: "apiVersion: uds.dev/v1alpha1\nkind: Package\n", Rationale: "try again"},
	}}
	checks := &stubChecks{failTimes: 10}
	result, err := (WorkspacePackager{Writer: writer, Store: &stubStore{}, Checks: checks}).WritePackage(context.Background(), "/tmp/ws", "", "", "workspace/demo", domain.PackagingContext{})
	if err != nil || result.Outcome != string(domain.OutcomeFailRequired) || len(result.Attempts) != 3 || checks.creates != 0 {
		t.Fatalf("result = %#v checks=%#v %v", result, checks, err)
	}
	if writer.calls != 3 {
		t.Fatalf("calls = %d", writer.calls)
	}
}
