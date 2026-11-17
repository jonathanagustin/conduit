package composition

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/appgen/adapters/recorded"
	appgenapp "github.com/jonathanagustin/conduit/internal/appgen/application"
	appgendomain "github.com/jonathanagustin/conduit/internal/appgen/domain"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/devworkspace/adapters/gitstore"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/workspacepkg"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	planningapp "github.com/jonathanagustin/conduit/internal/planning/application"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
	appgenskill "github.com/jonathanagustin/conduit/skills/appgen"
)

type testAppgenSandbox struct {
	builds int
	fail   bool
}

func (s *testAppgenSandbox) Build(_ context.Context, _ appgenapp.BuildRequest) (appgenapp.BuildResult, error) {
	s.builds++
	if s.fail {
		return appgenapp.BuildResult{Passed: false, Reason: "container build failed"}, nil
	}
	return appgenapp.BuildResult{Passed: true, Reason: "image built without network", ImageID: "sha256:appgen"}, nil
}

func (s *testAppgenSandbox) Check(_ context.Context, _ appgenapp.BuildResult, _ appgenapp.BuildRequest) (appgenapp.HealthResult, error) {
	return appgenapp.HealthResult{Passed: true, Reason: "GET /health returned 200"}, nil
}

type testAppgenPublisher struct {
	pushes int
}

func (p *testAppgenPublisher) Push(_ context.Context, _, name string, _ []byte) (string, string, string, error) {
	p.pushes++
	return "127.0.0.1:5000/" + name + ":0.1.0", "sha256:" + strings.Repeat("a", 64), "sha256:" + strings.Repeat("b", 64), nil
}

func TestAppgenProfileStartTurnReadyAcceptAndBinding(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	handler := planningHandler(http.NotFoundHandler(), repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}, nil)

	createBody := `{"profile":"appgen","problemStatement":"Build an HTTP status dashboard for cluster health","appName":"cluster-status"}`
	resp := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions", createBody)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create appgen session: %d %s", resp.Code, resp.Body.String())
	}
	var session planningdomain.Session
	if err := json.Unmarshal(resp.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Profile.Name != planningdomain.ProfileAppgen {
		t.Fatalf("expected profile appgen, got %+v", session.Profile)
	}
	if len(session.NeedsInput) != 0 {
		t.Fatalf("expected no NeedsInput when problemStatement and appName are supplied, got %+v", session.NeedsInput)
	}

	turnResp := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions/"+session.ID+"/turns", `{"message":"include a /health endpoint"}`)
	if turnResp.Code != http.StatusOK {
		t.Fatalf("turn: %d %s", turnResp.Code, turnResp.Body.String())
	}
	if err := json.Unmarshal(turnResp.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Status != planningdomain.StatusReady || session.Plan == nil || session.Plan.Text == "" {
		t.Fatalf("expected ready session with plan, got %+v", session)
	}
	if !strings.Contains(session.Plan.Text, `"healthCheckPath":"/health"`) || strings.Contains(session.Plan.Text, "/healthz") || !strings.Contains(session.Plan.Text, `"sourceRule":"exactly one source file"`) {
		t.Fatalf("plan does not state the source rule and health path: %s", session.Plan.Text)
	}
	if session.ResolvedCommit != planningdomain.NoRepositoryCommit {
		t.Fatalf("expected resolvedCommit %q, got %q", planningdomain.NoRepositoryCommit, session.ResolvedCommit)
	}

	acceptResp := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions/"+session.ID+"/accept", `{}`)
	if acceptResp.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", acceptResp.Code, acceptResp.Body.String())
	}
	if err := json.Unmarshal(acceptResp.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Binding == nil {
		t.Fatal("expected binding on accepted session")
	}
	if session.Binding.Profile.Name != planningdomain.ProfileAppgen || session.Binding.Profile.Hash != appgenskill.SHA256() {
		t.Fatalf("unexpected bound profile: %+v", session.Binding.Profile)
	}
	if session.Binding.ResolvedCommit != planningdomain.NoRepositoryCommit {
		t.Fatalf("unexpected bound resolvedCommit: %q", session.Binding.ResolvedCommit)
	}
	if session.Binding.Intake.ProblemStatement != "Build an HTTP status dashboard for cluster health" || session.Binding.Intake.AppName != "cluster-status" {
		t.Fatalf("unexpected bound intake: %+v", session.Binding.Intake)
	}
}

func TestAppgenProfileMissingAppNameRaisesNeedsInput(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	handler := planningHandler(http.NotFoundHandler(), repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}, nil)

	createBody := `{"profile":"appgen","problemStatement":"Build an incident timeline viewer"}`
	resp := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions", createBody)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.Code, resp.Body.String())
	}
	var session planningdomain.Session
	if err := json.Unmarshal(resp.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if len(session.NeedsInput) != 1 || session.NeedsInput[0].ID != "app_name" {
		t.Fatalf("expected NeedsInput for app_name, got %+v", session.NeedsInput)
	}

	turnResp := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions/"+session.ID+"/turns", `{}`)
	if turnResp.Code != http.StatusOK {
		t.Fatalf("turn: %d %s", turnResp.Code, turnResp.Body.String())
	}
	if err := json.Unmarshal(turnResp.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Status != planningdomain.StatusNeedsInput || session.Plan != nil {
		t.Fatalf("expected needs_input without plan before answering app_name, got %+v", session)
	}

	ansResp := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions/"+session.ID+"/answers", `{"answers":[{"id":"app_name","value":"incident-timeline"}]}`)
	if ansResp.Code != http.StatusOK {
		t.Fatalf("answers: %d %s", ansResp.Code, ansResp.Body.String())
	}

	turnResp2 := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions/"+session.ID+"/turns", `{}`)
	if turnResp2.Code != http.StatusOK {
		t.Fatalf("turn after answer: %d %s", turnResp2.Code, turnResp2.Body.String())
	}
	if err := json.Unmarshal(turnResp2.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Status != planningdomain.StatusReady || session.Plan == nil || session.Intake.AppName != "incident-timeline" {
		t.Fatalf("expected ready session with updated appName, got %+v", session)
	}
}

func writeZarfStub(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "zarf")
	// This fixture simulates both creation and definition inspection. Real
	// archive parsing is qualified by packagingtools' native Zarf cases.
	script := `#!/bin/sh
set -eu
out=
prev=
arch=$(uname -m)
case "$arch" in x86_64) arch=amd64;; aarch64) arch=arm64;; esac
for arg in "$@"; do
  if [ "$prev" = "--output" ]; then out=$arg; fi
  if [ "$prev" = "--architecture" ]; then arch=$arg; fi
  prev=$arg
done
if [ "$2" = "inspect" ]; then
  awk -v arch="$arch" '/^metadata:/ { print; print "  architecture: " arch; next } { print }' zarf.yaml
  exit 0
fi
if [ -n "$out" ]; then
  name=$(awk '$1 == "name:" { gsub(/["\047]/, "", $2); print $2; exit }' zarf.yaml)
  version=$(awk '$1 == "version:" { gsub(/["\047]/, "", $2); print $2; exit }' zarf.yaml)
  mkdir -p "$out"
  printf 'simulated archive\n' > "$out/zarf-package-$name-$arch-$version.tar.zst"
fi
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAppgenProfileRefusesComputeBeforeAcceptAndGeneratesBeforePackaging(t *testing.T) {
	gitRoot := t.TempDir()
	handoffDir := t.TempDir()
	t.Setenv("DEVWORKSPACE_GIT_ROOT", gitRoot)
	t.Setenv("PACKAGING_FIXTURE_DIR", handoffDir)
	t.Setenv("ZARF_BIN", writeZarfStub(t))
	sandbox := &testAppgenSandbox{}
	publisher := &testAppgenPublisher{}
	repository := controlplane.NewMemoryRepository()
	governanceSvc := controlplane.NewGovernanceService(repository)
	if _, err := governanceSvc.PutTeam(t.Context(), controlplane.Team{
		Name: "release-reviewers", DisplayName: "Release reviewers",
		AdminUsers: []string{"alice", "bob"}, MemberUsers: []string{"alice", "bob"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governanceSvc.PutPolicy(t.Context(), controlplane.ApprovalPolicy{
		ID: "release-review", DisplayName: "Release review", Enabled: true,
		Expression: `type != "" ? require_team("release-reviewers", "release review") : no_op()`,
	}); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHub(
		repository,
		controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true},
		Options{
			AppGenerator:    recorded.Adapter{},
			AppgenSandbox:   sandbox,
			AppgenPublisher: publisher,
		},
	)
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}

	createBody := `{"profile":"appgen","problemStatement":"Create a service catalog portal","appName":"hello-http"}`
	resp := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions", createBody)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.Code, resp.Body.String())
	}
	var session planningdomain.Session
	if err := json.Unmarshal(resp.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	path := "/v1/planning-sessions/" + session.ID

	// No generation or packaging runs before Accept.
	preAccept := planningRequest(t, handler, "alice", http.MethodPost, path+"/compute", `{}`)
	if preAccept.Code != http.StatusConflict || !strings.Contains(preAccept.Body.String(), "COMPUTE_BEFORE_ACCEPT") {
		t.Fatalf("expected 409 COMPUTE_BEFORE_ACCEPT before turn/accept, got %d %s", preAccept.Code, preAccept.Body.String())
	}
	if sandbox.builds != 0 || publisher.pushes != 0 {
		t.Fatalf("sandbox/publisher should not run before Accept, got builds=%d pushes=%d", sandbox.builds, publisher.pushes)
	}

	if r := planningRequest(t, handler, "alice", http.MethodPost, path+"/turns", `{}`); r.Code != http.StatusOK {
		t.Fatalf("turn: %d %s", r.Code, r.Body.String())
	}
	preAcceptAfterTurn := planningRequest(t, handler, "alice", http.MethodPost, path+"/compute", `{}`)
	if preAcceptAfterTurn.Code != http.StatusConflict || !strings.Contains(preAcceptAfterTurn.Body.String(), "COMPUTE_BEFORE_ACCEPT") {
		t.Fatalf("expected 409 COMPUTE_BEFORE_ACCEPT after turn before accept, got %d %s", preAcceptAfterTurn.Code, preAcceptAfterTurn.Body.String())
	}
	if sandbox.builds != 0 || publisher.pushes != 0 {
		t.Fatalf("sandbox/publisher should not run before Accept, got builds=%d pushes=%d", sandbox.builds, publisher.pushes)
	}

	if r := planningRequest(t, handler, "alice", http.MethodPost, path+"/accept", `{}`); r.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", r.Code, r.Body.String())
	}
	computeResp := planningRequest(t, handler, "alice", http.MethodPost, path+"/compute", `{}`)
	if computeResp.Code != http.StatusAccepted {
		t.Fatalf("compute after accept: %d %s", computeResp.Code, computeResp.Body.String())
	}
	if sandbox.builds != 1 || publisher.pushes != 1 {
		t.Fatalf("expected 1 build and 1 push after Accept+Compute, got builds=%d pushes=%d", sandbox.builds, publisher.pushes)
	}

	// Verify GET /v1/app-generations on the same NewHub handler exposes the generated record and passing gates.
	genListResp := planningRequest(t, handler, "alice", http.MethodGet, "/v1/app-generations", "")
	if genListResp.Code != http.StatusOK {
		t.Fatalf("list app-generations: %d %s", genListResp.Code, genListResp.Body.String())
	}
	var genList struct {
		Items []appgenapp.Result `json:"items"`
	}
	if err := json.Unmarshal(genListResp.Body.Bytes(), &genList); err != nil {
		t.Fatal(err)
	}
	if len(genList.Items) != 1 || genList.Items[0].Name != "hello-http" || len(genList.Items[0].Gates) != 6 {
		t.Fatalf("unexpected app-generations list: %+v", genList.Items)
	}
	for _, g := range genList.Items[0].Gates {
		if g.Outcome != appgendomain.OutcomePass {
			t.Fatalf("expected gate %s to pass, got %+v", g.Name, g)
		}
	}

	// Verify both generated application files and packaged manifests exist in the workspace and handoff dir.
	if _, err := os.Stat(filepath.Join(handoffDir, "sources", "hello-http-1.txt")); err != nil {
		t.Fatalf("missing packaging handoff source file: %v", err)
	}
	store, err := gitstore.Open(gitstore.Options{RootDir: gitRoot})
	if err != nil {
		t.Fatal(err)
	}
	repoDir := store.RepoDir("wsp_" + session.ID)
	for _, wantFile := range []string{"main.go", "values.yaml", "Dockerfile", ".dockerignore", "zarf.yaml", "uds-package.yaml", "package-source.yaml", "chart/Chart.yaml", "chart/values.yaml", "chart/templates/deployment.yaml", "chart/templates/service.yaml"} {
		if _, err := os.Stat(filepath.Join(repoDir, wantFile)); err != nil {
			t.Fatalf("missing expected generated/packaged file %s: %v", wantFile, err)
		}
	}

	ws, err := store.Get(t.Context(), "wsp_"+session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ws.ValidationOutcome != string(packagingdomain.OutcomePass) {
		t.Fatalf("expected workspace validation outcome PASS, got %q", ws.ValidationOutcome)
	}
	crBytes, err := os.ReadFile(filepath.Join(repoDir, "uds-package.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	checks := workspacepkg.Checks{}
	if reason := checks.PackageCR(string(crBytes)); reason != "" {
		t.Fatalf("PackageCR gate failed: %s", reason)
	}
	pinned := "127.0.0.1:5000/hello-http@sha256:" + strings.Repeat("a", 64)
	values, err := os.ReadFile(filepath.Join(repoDir, "chart/values.yaml"))
	if err != nil || !strings.Contains(string(values), pinned) {
		t.Fatalf("chart values = %s %v", values, err)
	}
	zarf, err := os.ReadFile(filepath.Join(repoDir, "zarf.yaml"))
	if err != nil || !strings.Contains(string(zarf), pinned) || !strings.Contains(string(zarf), `path: "chart"`) {
		t.Fatalf("zarf = %s %v", zarf, err)
	}
	if !strings.Contains(string(crBytes), `service: "hello-http"`) || !strings.Contains(string(crBytes), "expose:") {
		t.Fatalf("package = %s", crBytes)
	}
	outEntries, err := os.ReadDir(filepath.Join(repoDir, "out"))
	if err != nil || len(outEntries) == 0 {
		t.Fatalf("expected zarf package create archive in %s/out, got entries=%d err=%v", repoDir, len(outEntries), err)
	}

	// Verify a governed ChangeRequest was opened and requires a second reviewer.
	crs, err := repository.ListChangeRequests(t.Context(), "")
	if err != nil || len(crs) != 1 {
		t.Fatalf("expected 1 ChangeRequest from appgen compute, got %d (err=%v)", len(crs), err)
	}
	if crs[0].Status != controlplane.ChangePendingApprovals {
		t.Fatalf("expected ChangeRequest status %q, got %q", controlplane.ChangePendingApprovals, crs[0].Status)
	}
	if _, _, err := governanceSvc.Approve(t.Context(), crs[0].ID, controlplane.Principal{Username: "alice", Teams: []string{"release-reviewers"}}, "self-approve"); !errors.Is(err, controlplane.ErrForbidden) {
		t.Fatalf("expected author self-approval to be forbidden, got %v", err)
	}

	// Verify GET /v1/dev-workspaces/wsp_<id> and GET /v1/dev-workspaces/wsp_<id>/diff on the NewHub handler.
	wsResp := planningRequest(t, handler, "alice", http.MethodGet, "/v1/dev-workspaces/wsp_"+session.ID, "")
	if wsResp.Code != http.StatusOK {
		t.Fatalf("get dev-workspace: %d %s", wsResp.Code, wsResp.Body.String())
	}
	var wsView struct {
		WorkspaceID       string `json:"workspaceId"`
		HeadCommit        string `json:"headCommit"`
		ApprovalID        string `json:"approvalId"`
		ValidationOutcome string `json:"validationOutcome"`
	}
	if err := json.Unmarshal(wsResp.Body.Bytes(), &wsView); err != nil {
		t.Fatal(err)
	}
	if wsView.WorkspaceID != "wsp_"+session.ID || wsView.HeadCommit == "" || wsView.HeadCommit == planningdomain.NoRepositoryCommit || wsView.ApprovalID != crs[0].ID || wsView.ValidationOutcome != "PASS" {
		t.Fatalf("unexpected dev-workspace view: %+v (want approvalId=%s)", wsView, crs[0].ID)
	}

	diffResp := planningRequest(t, handler, "alice", http.MethodGet, "/v1/dev-workspaces/wsp_"+session.ID+"/diff", "")
	if diffResp.Code != http.StatusOK {
		t.Fatalf("get dev-workspace diff: %d %s", diffResp.Code, diffResp.Body.String())
	}
	var diffView struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(diffResp.Body.Bytes(), &diffView); err != nil {
		t.Fatal(err)
	}
	diffPaths := make(map[string]bool, len(diffView.Files))
	for _, f := range diffView.Files {
		diffPaths[f.Path] = true
	}
	for _, wantPath := range []string{"Dockerfile", "zarf.yaml", "uds-package.yaml", "main.go", "package-source.yaml"} {
		if !diffPaths[wantPath] {
			t.Fatalf("dev-workspace diff missing %q in %+v", wantPath, diffView.Files)
		}
	}
}

func TestAppgenProfileFailsClosedWhenSandboxBuildFails(t *testing.T) {
	t.Setenv("DEVWORKSPACE_GIT_ROOT", t.TempDir())
	t.Setenv("PACKAGING_FIXTURE_DIR", t.TempDir())
	sandbox := &testAppgenSandbox{fail: true}
	publisher := &testAppgenPublisher{}
	repository := controlplane.NewMemoryRepository()
	handler, err := NewHub(
		repository,
		controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true},
		Options{
			AppGenerator:    recorded.Adapter{},
			AppgenSandbox:   sandbox,
			AppgenPublisher: publisher,
		},
	)
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	resp := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions", `{"profile":"appgen","problemStatement":"Create a service catalog portal","appName":"hello-http"}`)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.Code, resp.Body.String())
	}
	var session planningdomain.Session
	if err := json.Unmarshal(resp.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	path := "/v1/planning-sessions/" + session.ID
	if r := planningRequest(t, handler, "alice", http.MethodPost, path+"/turns", `{}`); r.Code != http.StatusOK {
		t.Fatalf("turn: %d %s", r.Code, r.Body.String())
	}
	if r := planningRequest(t, handler, "alice", http.MethodPost, path+"/accept", `{}`); r.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", r.Code, r.Body.String())
	}
	computeResp := planningRequest(t, handler, "alice", http.MethodPost, path+"/compute", `{}`)
	if computeResp.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway when sandbox build fails, got %d %s", computeResp.Code, computeResp.Body.String())
	}
	if publisher.pushes != 0 {
		t.Fatalf("expected 0 pushes when sandbox build fails, got %d", publisher.pushes)
	}
	crs, err := repository.ListChangeRequests(t.Context(), "")
	if err != nil || len(crs) != 0 {
		t.Fatalf("expected 0 ChangeRequests when sandbox build fails, got %d (err=%v)", len(crs), err)
	}
}

func TestAppgenProfileFailsClosedWhenZarfLintOrCreateFails(t *testing.T) {
	gitRoot := t.TempDir()
	t.Setenv("DEVWORKSPACE_GIT_ROOT", gitRoot)
	t.Setenv("PACKAGING_FIXTURE_DIR", t.TempDir())
	t.Setenv("ZARF_BIN", filepath.Join(t.TempDir(), "missing-zarf-binary"))
	sandbox := &testAppgenSandbox{}
	publisher := &testAppgenPublisher{}
	repository := controlplane.NewMemoryRepository()
	handler, err := NewHub(
		repository,
		controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true},
		Options{
			AppGenerator:    recorded.Adapter{},
			AppgenSandbox:   sandbox,
			AppgenPublisher: publisher,
		},
	)
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	resp := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions", `{"profile":"appgen","problemStatement":"Create a service catalog portal","appName":"hello-http"}`)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.Code, resp.Body.String())
	}
	var session planningdomain.Session
	if err := json.Unmarshal(resp.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	path := "/v1/planning-sessions/" + session.ID
	if r := planningRequest(t, handler, "alice", http.MethodPost, path+"/turns", `{}`); r.Code != http.StatusOK {
		t.Fatalf("turn: %d %s", r.Code, r.Body.String())
	}
	if r := planningRequest(t, handler, "alice", http.MethodPost, path+"/accept", `{}`); r.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", r.Code, r.Body.String())
	}
	computeResp := planningRequest(t, handler, "alice", http.MethodPost, path+"/compute", `{}`)
	if computeResp.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway when Zarf lint/create fails, got %d %s", computeResp.Code, computeResp.Body.String())
	}
	store, err := gitstore.Open(gitstore.Options{RootDir: gitRoot})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(t.Context(), "wsp_"+session.ID); err == nil {
		t.Fatal("expected workspace not to be recorded as PASS when Zarf lint/create fails")
	}
	crs, err := repository.ListChangeRequests(t.Context(), "")
	if err != nil || len(crs) != 0 {
		t.Fatalf("expected 0 ChangeRequests when Zarf lint/create fails, got %d (err=%v)", len(crs), err)
	}
}

func TestAppgenProfileSkillChangeUpdatesBoundProfileHash(t *testing.T) {
	svcA := planningapp.NewService(nil).WithAppgenSkillSource(appgenskill.Source)
	svcB := planningapp.NewService(nil).WithAppgenSkillSource(appgenskill.Source + "\n# additional convention\n")

	sessA, err := svcA.CreateWithInput(t.Context(), planningdomain.CreateInput{
		Profile:          planningdomain.ProfileAppgen,
		ProblemStatement: "Build a telemetry collector",
		AppName:          "telemetry-collector",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svcA.Turn(t.Context(), sessA.ID, ""); err != nil {
		t.Fatal(err)
	}
	accA, err := svcA.Accept(t.Context(), sessA.ID)
	if err != nil {
		t.Fatal(err)
	}

	sessB, err := svcB.CreateWithInput(t.Context(), planningdomain.CreateInput{
		Profile:          planningdomain.ProfileAppgen,
		ProblemStatement: "Build a telemetry collector",
		AppName:          "telemetry-collector",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svcB.Turn(t.Context(), sessB.ID, ""); err != nil {
		t.Fatal(err)
	}
	accB, err := svcB.Accept(t.Context(), sessB.ID)
	if err != nil {
		t.Fatal(err)
	}

	if accA.Binding.Profile.Hash == "" || accB.Binding.Profile.Hash == "" {
		t.Fatalf("expected non-empty profile hashes: %q vs %q", accA.Binding.Profile.Hash, accB.Binding.Profile.Hash)
	}
	if accA.Binding.Profile.Hash == accB.Binding.Profile.Hash {
		t.Fatalf("changing appgen skill source did not change bound profile hash: %q", accA.Binding.Profile.Hash)
	}
}
