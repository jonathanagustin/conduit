package application

import (
	"context"
	"fmt"
	"io"
	"strings"
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
	commits        []map[string]string
	selectedDir    string
	selectedCommit string
	selectErr      error
}

func (store *stubStore) Prepare(context.Context, string, string, string, string) (string, string, error) {
	return "/tmp/ws", "workspace/demo", nil
}
func (store *stubStore) SelectCommit(_ context.Context, dir, commit string) error {
	store.selectedDir, store.selectedCommit = dir, commit
	return store.selectErr
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

type stubBuilder struct {
	failFirst bool
	calls     int
}

func (b *stubBuilder) BuildImage(_ context.Context, _ string, logOut io.Writer) (string, error) {
	b.calls++
	if b.failFirst && b.calls == 1 {
		_, _ = fmt.Fprintln(logOut, "STEP 2/4: RUN npm ci -> EACCES")
		return "", fmt.Errorf("image build failed")
	}
	_, _ = fmt.Fprintln(logOut, "STEP 4/4: built ghcr.io/example/web:v1")
	return "ghcr.io/example/web:v1", nil
}

type stubInspector struct {
	inspected domain.ImageInspect
	calls     int
}

func (ins *stubInspector) InspectImage(_ context.Context, ref string) (domain.ImageInspect, error) {
	ins.calls++
	out := ins.inspected
	out.Ref = ref
	return out, nil
}

type stubWorkspaceAuditor struct {
	records []WorkspaceAudit
}

func (aud *stubWorkspaceAuditor) RecordWorkspacePackage(_ context.Context, audit WorkspaceAudit) error {
	aud.records = append(aud.records, audit)
	return nil
}

func TestWritePackageStreamsBuildLogsInspectsImageAndRecordsCompleteAudit(t *testing.T) {
	writer := &stubWriter{proposals: []domain.PackageProposal{
		{
			Source:       "name: web\n",
			Zarf:         "apiVersion: zarf.dev/v1alpha1\n",
			CR:           "apiVersion: uds.dev/v1alpha1\nkind: Package\n",
			Dockerfile:   "FROM node:20-slim\nRUN npm ci\nUSER 65532\n",
			Dockerignore: ".git\nnode_modules\n",
			Rationale:    "initial dockerfile",
		},
		{
			Source:       "name: web\n",
			Zarf:         "apiVersion: zarf.dev/v1alpha1\n",
			CR:           "apiVersion: uds.dev/v1alpha1\nkind: Package\n",
			Dockerfile:   "FROM node:20-slim\nUSER 65532\nCOPY --chown=65532:65532 . .\n",
			Dockerignore: ".git\nnode_modules\n",
			Rationale:    "fix build permissions",
		},
	}}
	store := &stubStore{}
	checks := &stubChecks{}
	builder := &stubBuilder{failFirst: true}
	inspector := &stubInspector{
		inspected: domain.ImageInspect{
			Digest:       "sha256:8888888888888888888888888888888888888888888888888888888888888888",
			User:         "65532",
			ExposedPorts: []string{"8080/tcp"},
		},
	}
	auditor := &stubWorkspaceAuditor{}
	var streamed strings.Builder

	packager := WorkspacePackager{
		Writer:    writer,
		Store:     store,
		Checks:    checks,
		Builder:   builder,
		Inspector: inspector,
		Auditor:   auditor,
		LogWriter: &streamed,
	}

	result, err := packager.WritePackage(
		context.Background(),
		"/tmp/ws",
		"https://git.example/web.git",
		"main",
		"workspace/demo",
		domain.PackagingContext{RunID: "run-audit-1", ImageRef: "ghcr.io/example/web:v1"},
	)
	if err != nil || result.Outcome != string(domain.OutcomePass) {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	if builder.calls != 2 || inspector.calls != 1 {
		t.Fatalf("builder.calls = %d, inspector.calls = %d", builder.calls, inspector.calls)
	}
	if !strings.Contains(streamed.String(), "STEP 2/4: RUN npm ci -> EACCES") || !strings.Contains(streamed.String(), "STEP 4/4: built ghcr.io/example/web:v1") {
		t.Fatalf("streamed logs missing expected build output:\n%s", streamed.String())
	}
	if result.ImageInspect == nil || result.ImageInspect.User != "65532" {
		t.Fatalf("imageInspect = %#v", result.ImageInspect)
	}
	if len(auditor.records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(auditor.records))
	}
	audit := auditor.records[0]
	meta := audit.Metadata()
	for _, requiredKey := range []string{
		"repo",
		"repoUrl",
		"commit",
		"resolvedCommit",
		"dockerfileSha256",
		"imageDigest",
		"skillSha256",
		"toolCalls",
		"promptSha256",
		"responseSha256",
	} {
		val, ok := meta[requiredKey]
		if !ok || val == "" {
			t.Fatalf("missing or empty required audit key %q in %#v", requiredKey, meta)
		}
	}
	if audit.ImageDigest != "sha256:8888888888888888888888888888888888888888888888888888888888888888" {
		t.Fatalf("audit.ImageDigest = %q", audit.ImageDigest)
	}
	if len(audit.ToolCalls) != 1 || audit.ToolCalls[0] != "inspect_image" {
		t.Fatalf("audit.ToolCalls = %#v, want [inspect_image]", audit.ToolCalls)
	}
}
