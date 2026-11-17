package composition

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	planningapp "github.com/jonathanagustin/conduit/internal/planning/application"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
)

type shaPlanner struct{ sha string }

func (planner shaPlanner) Turn(context.Context, *planningdomain.Session, string) (*planningapp.TurnResult, error) {
	return &planningapp.TurnResult{
		Plan:           &planningdomain.Plan{Text: "Package the fixture from the workspace source."},
		ResolvedCommit: planner.sha,
		Model:          "deterministic",
		Profile:        planningdomain.ProfileRef{Name: planningdomain.ProfileOnboard, Version: "1", Hash: strings.Repeat("c", 64)},
	}, nil
}

func TestGovernedOnboardPackagingFromSource(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not installed")
	}
	if _, err := exec.LookPath("zarf"); err != nil && strings.TrimSpace(os.Getenv("ZARF_BIN")) == "" {
		t.Skip("zarf is not installed")
	}
	remote := newTestGitRemote(t, "fixtureapp", map[string]string{
		"source.txt": "hello from source\n",
		"Dockerfile": "FROM ubuntu:latest\nUSER root\n",
	})
	gitRoot := t.TempDir()
	t.Setenv("CONDUIT_IMAGE_BUILDER", "docker")
	t.Setenv("CONDUIT_PACKAGING_BUILD", "scratch")
	t.Setenv("DEVWORKSPACE_GIT_ROOT", gitRoot)
	repository := controlplane.NewMemoryRepository()
	opts := controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		BootstrapAdministrators:              []string{"alice"},
	}
	spaces := assembleDevWorkspace(repository, opts, os.Getenv)
	handler := planningHandlerWithOptions(
		mountDevWorkspace(http.NotFoundHandler(), repository, opts, spaces),
		repository, opts, shaPlanner{sha: remote.defaultSHA}, nil, nil, nil, os.Getenv,
	)
	created := planningRequest(t, handler, "alice", "POST", "/v1/planning-sessions", `{"repoUrl":"`+remote.repoURL+`","ref":"main"}`)
	var session planningdomain.Session
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &session) != nil {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	turned := planningRequest(t, handler, "alice", "POST", "/v1/planning-sessions/"+session.ID+"/turns", `{"message":"package it"}`)
	if turned.Code != http.StatusOK {
		t.Fatalf("turn: %d %s", turned.Code, turned.Body.String())
	}
	accepted := planningRequest(t, handler, "alice", "POST", "/v1/planning-sessions/"+session.ID+"/accept", `{}`)
	if accepted.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", accepted.Code, accepted.Body.String())
	}
	computed := planningRequest(t, handler, "alice", "POST", "/v1/planning-sessions/"+session.ID+"/compute", `{}`)
	if computed.Code != http.StatusAccepted && computed.Code != http.StatusOK {
		t.Fatalf("compute: %d %s", computed.Code, computed.Body.String())
	}
	wsID := "wsp_" + session.ID
	workspace := planningRequest(t, handler, "alice", "GET", "/v1/dev-workspaces/"+wsID, "")
	var row map[string]any
	if workspace.Code != http.StatusOK || json.Unmarshal(workspace.Body.Bytes(), &row) != nil {
		t.Fatalf("workspace: %d %s", workspace.Code, workspace.Body.String())
	}
	digest, _ := row["artifactDigest"].(string)
	observed, err := exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", "conduit/fixtureapp:local").Output()
	if err != nil {
		t.Fatalf("docker inspect: %v", err)
	}
	if digest == "" || digest != strings.TrimSpace(string(observed)) || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("artifact digest %q, engine id %q", digest, strings.TrimSpace(string(observed)))
	}
	repo := filepath.Join(gitRoot, "workspaces", wsID, "repo")
	dockerfile, err := os.ReadFile(filepath.Join(repo, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(dockerfile), "ubuntu") || !strings.Contains(string(dockerfile), "FROM scratch") || !strings.Contains(string(dockerfile), "COPY source.txt") {
		t.Fatalf("dockerfile was not written in the workspace:\n%s", dockerfile)
	}
	zarf, err := os.ReadFile(filepath.Join(repo, "zarf.yaml"))
	if err != nil || !strings.Contains(string(zarf), digest) {
		t.Fatalf("zarf.yaml digest: %v\n%s", err, zarf)
	}
	cr, err := os.ReadFile(filepath.Join(repo, "uds-package.yaml"))
	if err != nil || !strings.Contains(string(cr), digest) || !strings.Contains(string(cr), "kind: Package") {
		t.Fatalf("package cr: %v\n%s", err, cr)
	}
	matches, err := filepath.Glob(filepath.Join(repo, "out", "create-*", "zarf-package-*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("archive: %v %v", matches, err)
	}
	info, err := os.Stat(matches[0])
	if err != nil || info.Size() == 0 {
		t.Fatalf("archive size: %v", err)
	}
	logText, err := os.ReadFile(filepath.Join(gitRoot, "logs", session.ID+".log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"pinned image digest " + digest, "zarf dev lint passed", "zarf package create", "change request "} {
		if !strings.Contains(string(logText), line) {
			t.Fatalf("log missing %q:\n%s", line, logText)
		}
	}
	requests, err := repository.ListChangeRequests(context.Background(), "")
	if err != nil || len(requests) != 1 || !strings.Contains(requests[0].Description, digest) {
		t.Fatalf("change requests: %+v %v", requests, err)
	}
	if row["validationOutcome"] != "PASS" {
		t.Fatalf("validation outcome %#v", row["validationOutcome"])
	}
}
