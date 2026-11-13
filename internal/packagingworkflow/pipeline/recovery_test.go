package pipeline

import (
	"bytes"
	"context"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	sdkworkflow "go.temporal.io/sdk/workflow"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/devserver"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/fixtures"
)

// TestDevServerRestartRecoversWaitAndHandoff is the localhost crash evidence.
// It is skipped when the pinned CLI is not on disk. CI does not download it.
func TestDevServerRestartRecoversWaitAndHandoff(t *testing.T) {
	cli := temporalCLI(t)
	checkCLIVersion(t, cli)
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture")
	if err := fixtures.Materialize(fixture); err != nil {
		t.Fatal(err)
	}
	ledger, err := adapters.Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	hold := filepath.Join(dir, "hold")
	pause := filepath.Join(dir, "pause-after-submit")
	if err := os.WriteFile(hold, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pause, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "temporal.db")
	var logs bytes.Buffer
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	running, err := devserver.Start(ctx, devserver.Options{CLIPath: cli, DBPath: db, Stdout: &logs, Stderr: &logs})
	if err != nil {
		t.Fatalf("start dev server: %v\n%s", err, logs.String())
	}
	startup := time.Since(started)
	assertLoopback(t, running.Frontend)
	assertLoopback(t, running.UI)
	logListeners(t, running)
	t.Logf("dev server startup %s frontend %s ui %s", startup, running.Frontend, running.UI)

	id, err := fixtures.Identity("sample-app-restart")
	if err != nil {
		t.Fatal(err)
	}
	acts := Activities{FixtureDir: fixture, Ledger: ledger, HoldFile: hold, PauseAfterSubmitFile: pause}
	workerOne := startWorker(t, ctx, running.Server.Client(), acts)
	run, err := running.Server.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        id.WorkflowID(),
		TaskQueue: TaskQueue,
	}, WorkflowName, Input{Identity: id, ApprovalWait: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 15*time.Second, func() bool { return fileExists(hold + ".ready") })
	// The hold file keeps Assess in flight. Stop the worker, then let it proceed.
	workerOne.Stop()
	if err := os.Remove(hold); err != nil {
		t.Fatal(err)
	}
	workerTwo := startWorker(t, ctx, running.Server.Client(), Activities{FixtureDir: fixture, Ledger: ledger, PauseAfterSubmitFile: pause})
	status := waitPhase(t, ctx, running.Server.Client(), id.WorkflowID(), domain.PhaseReview)
	if status.SourceDigest != id.SourceDigest || status.ArtifactDigest == "" {
		t.Fatalf("status after activity restart = %#v", status)
	}
	workerTwo.Stop()
	if err := running.Server.Stop(); err != nil {
		t.Fatal(err)
	}
	logs.Reset()
	running, err = devserver.Start(ctx, devserver.Options{CLIPath: cli, DBPath: db, Stdout: &logs, Stderr: &logs})
	if err != nil {
		t.Fatalf("restart dev server: %v\n%s", err, logs.String())
	}
	defer running.Server.Stop()
	workerThree := startWorker(t, ctx, running.Server.Client(), Activities{FixtureDir: fixture, Ledger: ledger, PauseAfterSubmitFile: pause})
	restored := query(t, ctx, running.Server.Client(), id.WorkflowID())
	if restored.Phase != domain.PhaseReview || restored.ArtifactDigest != status.ArtifactDigest || restored.SourceDigest != id.SourceDigest {
		t.Fatalf("status after server restart = %#v, before = %#v", restored, status)
	}
	record := domain.ApprovalRecord{
		ApprovalID: "appr-restart", RunID: id.RunID, ProposalRevision: restored.ProposalRevision,
		ArtifactDigest: restored.ArtifactDigest, TargetPlanRef: id.TargetPlanRef, Decision: domain.DecisionApproved,
	}
	if err := ledger.PutApproval(record); err != nil {
		t.Fatal(err)
	}
	if err := running.Server.Client().SignalWorkflow(ctx, id.WorkflowID(), "", SignalReview, domain.ReviewMessage{
		ApprovalID: record.ApprovalID, RunID: record.RunID, ProposalRevision: record.ProposalRevision,
		ArtifactDigest: record.ArtifactDigest, TargetPlanRef: record.TargetPlanRef,
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 15*time.Second, func() bool { return fileExists(pause + ".ready") })
	plans, err := ledger.PlanCount()
	if err != nil {
		t.Fatal(err)
	}
	if plans != 1 {
		t.Fatalf("plans before acknowledgement = %d", plans)
	}
	workerThree.Stop()
	if err := os.Remove(pause); err != nil {
		t.Fatal(err)
	}
	workerFour := startWorker(t, ctx, running.Server.Client(), Activities{FixtureDir: fixture, Ledger: ledger})
	defer workerFour.Stop()
	var result domain.Result
	if err := running.Server.Client().GetWorkflow(ctx, id.WorkflowID(), "").Get(ctx, &result); err != nil {
		t.Fatal(err)
	}
	plans, err = ledger.PlanCount()
	if err != nil {
		t.Fatal(err)
	}
	if plans != 1 || result.PlanID == "" || result.Outcome != domain.OutcomeHandoffRecorded || result.ArtifactDigest != status.ArtifactDigest {
		t.Fatalf("result = %#v plans = %d", result, plans)
	}
	replayHistory(t, ctx, running.Server.Client(), id.WorkflowID())
	logFootprint(t, db, startup, temporalRSS())
	_ = run
}

func startWorker(t *testing.T, ctx context.Context, c client.Client, acts Activities) worker.Worker {
	t.Helper()
	work := RegisterWorker(c, acts)
	if err := work.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {})
	_ = ctx
	return work
}

func waitPhase(t *testing.T, ctx context.Context, c client.Client, workflowID, phase string) domain.RunStatus {
	t.Helper()
	var status domain.RunStatus
	waitFor(t, 20*time.Second, func() bool {
		got, err := queryStatus(ctx, c, workflowID)
		if err != nil {
			return false
		}
		status = got
		return got.Phase == phase
	})
	if status.Phase != phase {
		t.Fatalf("phase = %s, want %s (%#v)", status.Phase, phase, status)
	}
	return status
}

func query(t *testing.T, ctx context.Context, c client.Client, workflowID string) domain.RunStatus {
	t.Helper()
	status, err := queryStatus(ctx, c, workflowID)
	if err != nil {
		t.Fatal(err)
	}
	return status
}

func queryStatus(ctx context.Context, c client.Client, workflowID string) (domain.RunStatus, error) {
	value, err := c.QueryWorkflow(ctx, workflowID, "", QueryStatus)
	if err != nil {
		return domain.RunStatus{}, err
	}
	var status domain.RunStatus
	if err := value.Get(&status); err != nil {
		return domain.RunStatus{}, err
	}
	return status, nil
}

func replayHistory(t *testing.T, ctx context.Context, c client.Client, workflowID string) {
	t.Helper()
	iter := c.GetWorkflowHistory(ctx, workflowID, "", false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	var events []*historypb.HistoryEvent
	for iter.HasNext() {
		event, err := iter.Next()
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	history := &historypb.History{Events: events}
	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflowWithOptions(PackagingWorkflow, sdkworkflow.RegisterOptions{Name: WorkflowName})
	if err := replayer.ReplayWorkflowHistory(nil, history); err != nil {
		t.Fatalf("replay: %v", err)
	}
	raw := history.String()
	if strings.Contains(raw, "kind: fixture") || strings.Contains(raw, "BEGIN PRIVATE KEY") {
		t.Fatal("workflow history contains fixture body or credential material")
	}
}

func waitFor(t *testing.T, budget time.Duration, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", budget)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func temporalCLI(t *testing.T) string {
	t.Helper()
	if path := os.Getenv("CONDUIT_TEMPORAL_CLI"); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	t.Skip("CONDUIT_TEMPORAL_CLI is unset; the dev-server recovery test needs the pinned CLI")
	return ""
}

func checkCLIVersion(t *testing.T, path string) {
	t.Helper()
	out, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	for _, needle := range []string{
		"temporal version " + packagingworkflow.CLIVersion,
		"Server " + packagingworkflow.ServerVersion,
		"UI " + packagingworkflow.UIVersion,
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("cli version %q does not contain %q", text, needle)
		}
	}
}

func assertLoopback(t *testing.T, hostport string) {
	t.Helper()
	host, _, err := splitHost(hostport)
	if err != nil || (host != "127.0.0.1" && host != "::1") {
		t.Fatalf("%s is not loopback", hostport)
	}
}

func splitHost(hostport string) (string, string, error) {
	host, port, err := hostportSplit(hostport)
	return host, port, err
}

func logListeners(t *testing.T, running *devserver.Running) {
	t.Helper()
	frontendPort := portOf(running.Frontend)
	uiPort := portOf(running.UI)
	seenFrontend, seenUI := false, false
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		body, err := os.ReadFile(table)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(body), "\n")[1:] {
			fields := strings.Fields(line)
			if len(fields) < 4 || fields[3] != "0A" { // 0A is TCP LISTEN
				continue
			}
			ip, port, ok := parseProcAddr(fields[1])
			if !ok || (port != frontendPort && port != uiPort) {
				continue
			}
			if !ip.IsLoopback() {
				t.Fatalf("%s listens on %s", fields[1], ip)
			}
			if port == frontendPort {
				seenFrontend = true
			}
			if port == uiPort {
				seenUI = true
			}
			t.Logf("listener %s %s:%s", table, ip, port)
		}
	}
	if !seenFrontend || !seenUI {
		t.Fatalf("did not observe loopback listeners for frontend %s and ui %s", running.Frontend, running.UI)
	}
}

func parseProcAddr(value string) (net.IP, string, bool) {
	ipHex, portHex, ok := strings.Cut(value, ":")
	if !ok {
		return nil, "", false
	}
	portNum, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return nil, "", false
	}
	raw, err := hex.DecodeString(ipHex)
	if err != nil {
		return nil, "", false
	}
	// /proc/net/tcp stores IPv4 little-endian.
	if len(raw) == 4 {
		return net.IPv4(raw[3], raw[2], raw[1], raw[0]), strconv.FormatUint(portNum, 10), true
	}
	if len(raw) == 16 {
		reordered := make(net.IP, 16)
		for word := 0; word < 4; word++ {
			for b := 0; b < 4; b++ {
				reordered[word*4+b] = raw[word*4+(3-b)]
			}
		}
		return reordered, strconv.FormatUint(portNum, 10), true
	}
	return nil, "", false
}

func logFootprint(t *testing.T, db string, startup time.Duration, rssKB int) {
	t.Helper()
	info, err := os.Stat(db)
	if err != nil {
		t.Fatal(err)
	}
	wal := sizeIfExists(db + "-wal")
	shm := sizeIfExists(db + "-shm")
	t.Logf("MEASURE startup=%s rss_kb=%d db=%d wal=%d shm=%d", startup, rssKB, info.Size(), wal, shm)
}

func temporalRSS() int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		cmdline, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil || !bytes.Contains(cmdline, []byte("start-dev")) {
			continue
		}
		status, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "status"))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, "VmRSS:") {
				fields := strings.Fields(line)
				kb, _ := strconv.Atoi(fields[1])
				return kb
			}
		}
	}
	return 0
}

func sizeIfExists(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func portOf(hostport string) string {
	_, port, _ := hostportSplit(hostport)
	return port
}

func hostportSplit(hostport string) (string, string, error) {
	// net.SplitHostPort without importing net at the call's verbosity.
	colon := strings.LastIndex(hostport, ":")
	if colon < 0 {
		return "", "", strconv.ErrSyntax
	}
	return hostport[:colon], hostport[colon+1:], nil
}
