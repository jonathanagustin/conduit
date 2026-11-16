package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/devserver"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
	planningworkflow "github.com/jonathanagustin/conduit/internal/planning/workflow"
)

func TestCLIRefusesNonLoopbackAddress(t *testing.T) {
	t.Parallel()
	cmd := newRoot()
	cmd.SetArgs([]string{"--address", "10.0.0.1:7233", "plan-status", "ps_1"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "refusing non-loopback") {
		t.Fatalf("expected non-loopback error, got %v", err)
	}
}

func TestCLIPlanningWorkflowEndToEndWithDevServer(t *testing.T) {
	cliPath := strings.TrimSpace(os.Getenv("CONDUIT_TEMPORAL_CLI"))
	if cliPath == "" {
		candidate := filepath.Join("..", "..", ".cache", "temporal", "temporal")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			abs, err := filepath.Abs(candidate)
			if err == nil {
				cliPath = abs
			}
		}
	}
	if cliPath == "" {
		t.Skip("set CONDUIT_TEMPORAL_CLI or run scripts/fetch-temporal-cli.sh to run the live CLI devserver test")
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "temporal.db")
	stateDir := filepath.Join(tmpDir, "state")

	running, err := devserver.Start(context.Background(), devserver.Options{
		CLIPath: cliPath,
		DBPath:  dbPath,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	})
	if err != nil {
		t.Fatalf("start devserver: %v", err)
	}
	t.Cleanup(func() { _ = running.Server.Stop() })

	_, repository, err := activitiesWithRepository(stateDir, "")
	if err != nil {
		t.Fatalf("activitiesWithRepository: %v", err)
	}
	planActs, err := planningActivities(repository, true)
	if err != nil {
		t.Fatalf("planningActivities: %v", err)
	}
	c, err := dial(running.Frontend)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	w := planningworkflow.RegisterWorker(c, planningworkflow.TaskQueue, planActs)
	if err := w.Start(); err != nil {
		t.Fatalf("start worker: %v", err)
	}
	t.Cleanup(func() { w.Stop() })

	execCLI := func(args ...string) (*planningdomain.Session, error) {
		t.Helper()
		var out bytes.Buffer
		cmd := newRoot()
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		fullArgs := append([]string{"--address", running.Frontend, "--state-dir", stateDir}, args...)
		cmd.SetArgs(fullArgs)
		if err := cmd.Execute(); err != nil {
			return nil, err
		}
		var session planningdomain.Session
		if err := json.Unmarshal(out.Bytes(), &session); err != nil {
			t.Fatalf("decode CLI JSON output (%v):\n%s", err, out.String())
		}
		return &session, nil
	}

	// 1. plan-create
	s, err := execCLI("plan-create", "https://github.com/org/sample-app", "main", "--session-id", "ps_cli_1", "--actor", "alice")
	if err != nil {
		t.Fatalf("plan-create: %v", err)
	}
	if s.ID != "ps_cli_1" || s.Status != planningdomain.StatusStarted {
		t.Fatalf("after create: %+v", s)
	}

	// 2. plan-turn 1 (inspects + elicits NeedsInput)
	s, err = execCLI("plan-turn", "ps_cli_1", "Inspect and onboard this service", "--actor", "alice")
	if err != nil {
		t.Fatalf("plan-turn 1: %v", err)
	}
	if s.Status != planningdomain.StatusNeedsInput || len(s.NeedsInput) != 1 {
		t.Fatalf("after turn 1: %+v", s)
	}
	if s.Model != planningdomain.DefaultModelLabel {
		t.Fatalf("model = %q, want %q", s.Model, planningdomain.DefaultModelLabel)
	}

	// 3. plan-answer
	s, err = execCLI("plan-answer", "ps_cli_1", "node_version=20", "--actor", "alice")
	if err != nil {
		t.Fatalf("plan-answer: %v", err)
	}
	if s.Status != planningdomain.StatusPlanning {
		t.Fatalf("after answer: %+v", s)
	}

	// 4. plan-turn 2 (produces the plan while preserving earlier messages #695)
	s, err = execCLI("plan-turn", "ps_cli_1", "Write the packaging plan for Node 20", "--actor", "alice")
	if err != nil {
		t.Fatalf("plan-turn 2: %v", err)
	}
	if s.Status != planningdomain.StatusReady || s.Plan == nil {
		t.Fatalf("after turn 2: %+v", s)
	}
	if len(s.Messages) != 4 {
		t.Fatalf("messages count = %d, want 4 (all earlier messages preserved): %+v", len(s.Messages), s.Messages)
	}

	// 5. plan-accept (commits governance audit and binds plan)
	s, err = execCLI("plan-accept", "ps_cli_1", "--actor", "alice")
	if err != nil {
		t.Fatalf("plan-accept: %v", err)
	}
	if s.Status != planningdomain.StatusAccepted || s.Binding == nil {
		t.Fatalf("after accept: %+v", s)
	}
	if s.Binding.Model != planningdomain.DefaultModelLabel {
		t.Fatalf("binding.Model = %q, want %q", s.Binding.Model, planningdomain.DefaultModelLabel)
	}

	// 6. plan-status
	s, err = execCLI("plan-status", "ps_cli_1", "--actor", "alice")
	if err != nil {
		t.Fatalf("plan-status: %v", err)
	}
	if s.Status != planningdomain.StatusAccepted || len(s.Messages) != 4 {
		t.Fatalf("status query: %+v", s)
	}

	// Verify audit event was persisted in the repository.
	audits, err := repository.ListAuthorizationAudit(context.Background(), "planning-session/ps_cli_1")
	if err != nil || len(audits) != 1 || audits[0].Subject != "alice" {
		t.Fatalf("persisted audits = %+v, err = %v", audits, err)
	}

	execRaw := func(args ...string) ([]byte, error) {
		t.Helper()
		var out, errBuf bytes.Buffer
		cmd := newRoot()
		cmd.SetOut(&out)
		cmd.SetErr(&errBuf)
		fullArgs := append([]string{"--address", running.Frontend, "--state-dir", stateDir}, args...)
		cmd.SetArgs(fullArgs)
		err := cmd.Execute()
		return out.Bytes(), err
	}

	// 7. Create an Excalidraw planning session and verify plan-compute refuses before Accept (COMPUTE_BEFORE_ACCEPT)
	excalidrawSession, err := execCLI("plan-create", "https://github.com/excalidraw/excalidraw", "3a317b55", "--session-id", "ps_excalidraw", "--actor", "alice")
	if err != nil || excalidrawSession.ID != "ps_excalidraw" {
		t.Fatalf("plan-create excalidraw: %+v, err=%v", excalidrawSession, err)
	}
	if _, err := execRaw("plan-compute", "ps_excalidraw", "--actor", "alice"); err == nil || !strings.Contains(err.Error(), planningdomain.CodeComputeBeforeAccept) {
		t.Fatalf("expected COMPUTE_BEFORE_ACCEPT before accept, got %v", err)
	}

	// Complete Excalidraw AI planning turns and Accept
	if _, err := execCLI("plan-turn", "ps_excalidraw", "Inspect Excalidraw for UDS/Zarf packaging", "--actor", "alice"); err != nil {
		t.Fatalf("excalidraw turn 1: %v", err)
	}
	if _, err := execCLI("plan-answer", "ps_excalidraw", "node_version=20", "--actor", "alice"); err != nil {
		t.Fatalf("excalidraw answer: %v", err)
	}
	if _, err := execCLI("plan-turn", "ps_excalidraw", "Generate the Excalidraw packaging plan", "--actor", "alice"); err != nil {
		t.Fatalf("excalidraw turn 2: %v", err)
	}
	if _, err := execCLI("plan-accept", "ps_excalidraw", "--actor", "alice"); err != nil {
		t.Fatalf("excalidraw accept: %v", err)
	}

	// 8. Run plan-compute after Accept: AI writes Excalidraw Dockerfile/zarf.yaml/uds-package.yaml into durable Git workspace branch
	computeBytes, err := execRaw("plan-compute", "ps_excalidraw", "--actor", "alice", "--workspace-id", "wsp_excalidraw", "--environment", "dev")
	if err != nil {
		t.Fatalf("plan-compute after accept: %v\n%s", err, string(computeBytes))
	}
	var computeOut planComputeOutput
	if err := json.Unmarshal(computeBytes, &computeOut); err != nil {
		t.Fatalf("decode plan-compute JSON (%v):\n%s", err, string(computeBytes))
	}
	if computeOut.WorkspaceID != "wsp_excalidraw" || computeOut.Result.Outcome != "PASS" || computeOut.Result.Head == "" {
		t.Fatalf("unexpected plan-compute output: %+v", computeOut)
	}
	if !strings.Contains(computeOut.Result.Archive, "zarf-package-excalidraw") {
		t.Fatalf("expected excalidraw archive, got %q", computeOut.Result.Archive)
	}
	if computeOut.Result.Audit.RepoURL != "https://github.com/excalidraw/excalidraw" || computeOut.Result.Audit.ImageDigest == "" || computeOut.Result.Audit.DockerfileSHA256 == "" {
		t.Fatalf("incomplete compute audit: %+v", computeOut.Result.Audit)
	}

	// 9. Inspect committed Git tree and files via workspace-tree and workspace-file
	treeBytes, err := execRaw("workspace-tree", "wsp_excalidraw", "--actor", "alice")
	if err != nil {
		t.Fatalf("workspace-tree: %v", err)
	}
	if !strings.Contains(string(treeBytes), "Dockerfile") || !strings.Contains(string(treeBytes), "zarf.yaml") || !strings.Contains(string(treeBytes), "uds-package.yaml") {
		t.Fatalf("workspace-tree missing committed package files:\n%s", string(treeBytes))
	}
	fileBytes, err := execRaw("workspace-file", "wsp_excalidraw", "uds-package.yaml", "--actor", "alice")
	if err != nil || !strings.Contains(string(fileBytes), "excalidraw") || !strings.Contains(string(fileBytes), "uds.dev/v1alpha1") {
		t.Fatalf("workspace-file uds-package.yaml: err=%v\n%s", err, string(fileBytes))
	}

	// 10. Commit an operator/AI adjustment on the Git branch via workspace-sync and inspect workspace-diff
	syncBytes, err := execRaw("workspace-sync", "wsp_excalidraw", "pin excalidraw replica notes", "--actor", "alice", "--write", "NOTES.md=Excalidraw packaged for airgap delivery\n")
	if err != nil {
		t.Fatalf("workspace-sync: %v\n%s", err, string(syncBytes))
	}
	diffBytes, err := execRaw("workspace-diff", "wsp_excalidraw", "--actor", "alice")
	if err != nil || !strings.Contains(string(diffBytes), "NOTES.md") || !strings.Contains(string(diffBytes), "zarf.yaml") {
		t.Fatalf("workspace-diff: err=%v\n%s", err, string(diffBytes))
	}

	// 11. Propose a governed ChangeRequest from the durable Git workspace revision (and verify self-approval is refused)
	if _, err := execRaw("workspace-propose", "wsp_excalidraw", "--actor", "alice", "--approve"); err == nil {
		t.Fatal("expected workspace-propose --approve to be refused with self-approval error")
	}
	proposeBytes, err := execRaw("workspace-propose", "wsp_excalidraw", "--actor", "alice", "--title", "Onboard Excalidraw package")
	if err != nil {
		t.Fatalf("workspace-propose: %v\n%s", err, string(proposeBytes))
	}
	if !strings.Contains(string(proposeBytes), "PENDING") {
		t.Fatalf("expected PENDING ChangeRequest receipt, got:\n%s", string(proposeBytes))
	}

	// 12. Run workspace-reap past the idle timeout and verify the workspace compute is reclaimed while preserving Git history
	futureTime := "2099-01-01T00:00:00Z"
	reapBytes, err := execRaw("workspace-reap", "--now", futureTime)
	if err != nil || !strings.Contains(string(reapBytes), "wsp_excalidraw") {
		t.Fatalf("workspace-reap: err=%v\n%s", err, string(reapBytes))
	}
}
