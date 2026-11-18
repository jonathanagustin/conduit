package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	tw "go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/devserver"
	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

// This opt-in test runs a real loopback Temporal server and worker, not a test
// environment. The activity is deliberately synthetic and never calls a model.
func TestCancellationFenceSurvivesTemporalServerRestart(t *testing.T) {
	cli := os.Getenv("CONDUIT_TEMPORAL_CLI")
	if cli == "" {
		t.Skip("set CONDUIT_TEMPORAL_CLI to the pinned local Temporal CLI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	database := filepath.Join(t.TempDir(), "temporal.db")
	startServer := func() *devserver.Running {
		t.Helper()
		running, err := devserver.Start(ctx, devserver.Options{CLIPath: cli, DBPath: database})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := running.Server.Stop(); err != nil {
				t.Errorf("stop owned Temporal server: %v", err)
			}
		})
		return running
	}
	connect := func(address, identity string) (client.Client, func()) {
		t.Helper()
		c, err := client.DialContext(ctx, client.Options{HostPort: address, Namespace: "default", Identity: identity})
		if err != nil {
			t.Fatal(err)
		}
		var once sync.Once
		stop := func() { once.Do(c.Close) }
		t.Cleanup(stop)
		return c, stop
	}
	entered, cancellationSeen, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var enterOnce, cancelOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int32
	turn := func(ctx context.Context, input TurnActivityInput) (*application.TurnResult, error) {
		calls.Add(1)
		if input.Actor != "alice" || input.Session.ID != "ps_cancel_restart" {
			return nil, errors.New("unexpected synthetic activity identity")
		}
		enterOnce.Do(func() { close(entered) })
		// Keep heartbeating with the production activity timeout/options. SDK
		// throttling still controls when a cancellation reaches this activity.
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				cancelOnce.Do(func() { close(cancellationSeen) })
				<-release
				// Deliberately ignore the cancellation error and return a late plan.
				return &application.TurnResult{Plan: &domain.Plan{Text: "synthetic late plan"}}, nil
			case <-ticker.C:
				activity.RecordHeartbeat(ctx, "synthetic cancellation proof")
			}
		}
	}
	startWorker := func(c client.Client) func() {
		t.Helper()
		w := worker.New(c, "planning-cancellation-proof", worker.Options{Identity: "planning-cancellation-proof-worker"})
		w.RegisterWorkflowWithOptions(SessionWorkflow, tw.RegisterOptions{Name: WorkflowName})
		w.RegisterActivityWithOptions(turn, activity.RegisterOptions{Name: ActTurn})
		if err := w.Start(); err != nil {
			t.Fatal(err)
		}
		var once sync.Once
		stop := func() { once.Do(w.Stop) }
		t.Cleanup(stop)
		return stop
	}
	running := startServer()
	first, closeFirst := connect(running.Frontend, "planning-cancellation-proof-owner")
	peer, closePeer := connect(running.Frontend, "planning-cancellation-proof-peer")
	stopWorker := startWorker(first)
	alice := NewClientService(first, "planning-cancellation-proof", "alice", nil)
	bob := NewClientService(peer, "planning-cancellation-proof", "bob", nil)
	session, err := alice.CreateWithID(ctx, "ps_cancel_restart", "https://example.test/planning-cancellation", "main")
	if err != nil {
		t.Fatal(err)
	}
	before := domain.CloneSession(session)
	done := make(chan error, 1)
	go func() {
		runCtx := application.WithRunScope(ctx, application.RunScope{ThreadID: session.ID, RunID: "logical-cancelled"})
		_, err := alice.Turn(runCtx, session.ID, "synthetic request")
		done <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("synthetic activity did not start", ctx.Err())
	}
	if _, err := bob.CancelTurn(ctx, session.ID, "logical-cancelled"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("other owner: %v", err)
	}
	select {
	case <-cancellationSeen:
		t.Fatal("other owner cancelled the activity")
	default:
	}
	// A second independently connected owner service performs the cancellation.
	ownerPeer := NewClientService(peer, "planning-cancellation-proof", "alice", nil)
	receipt, err := ownerPeer.CancelTurn(ctx, session.ID, "logical-cancelled")
	if err != nil || receipt.State != "cancellation_requested" {
		t.Fatalf("requested receipt: %#v %v", receipt, err)
	}
	select {
	case <-cancellationSeen:
	case <-ctx.Done():
		t.Fatal("activity did not observe cancellation", ctx.Err())
	}
	unblock()
	select {
	case err := <-done:
		if !errors.Is(err, domain.ErrRunCancelled) {
			t.Fatalf("late turn result: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	read, err := ownerPeer.Get(ctx, session.ID)
	if err != nil || !reflect.DeepEqual(read, before) {
		t.Fatalf("cancelled result changed session: %#v %v", read, err)
	}
	stopWorker()
	closePeer()
	closeFirst()
	if err := running.Server.Stop(); err != nil {
		t.Fatal(err)
	}
	// New process, connections and worker, with only the same SQLite history.
	running = startServer()
	restarted, _ := connect(running.Frontend, "planning-cancellation-proof-restarted")
	startWorker(restarted)
	ownerAfterRestart := NewClientService(restarted, "planning-cancellation-proof", "alice", nil)
	receipt, err = ownerAfterRestart.CancelTurn(ctx, session.ID, "logical-cancelled")
	if err != nil || receipt.State != "cancelled" {
		t.Fatalf("receipt after restart: %#v %v", receipt, err)
	}
	runCtx := application.WithRunScope(ctx, application.RunScope{ThreadID: session.ID, RunID: "logical-cancelled"})
	if _, err := ownerAfterRestart.Turn(runCtx, session.ID, "must not execute again"); !errors.Is(err, domain.ErrRunCancelled) {
		t.Fatalf("reused id after restart: %v", err)
	}
	read, err = ownerAfterRestart.Get(ctx, session.ID)
	if err != nil || !reflect.DeepEqual(read, before) || calls.Load() != 1 {
		t.Fatalf("state after restart: %#v calls=%d error=%v", read, calls.Load(), err)
	}
	// Close only this isolated synthetic workflow after proving the logical-run
	// behavior. Whole-workflow cancellation is not the implementation of Stop.
	if err := restarted.CancelWorkflow(ctx, WorkflowID(session.ID), ""); err != nil {
		t.Fatal(err)
	}
	if err := restarted.GetWorkflow(ctx, WorkflowID(session.ID), "").Get(ctx, nil); !temporal.IsCanceledError(err) {
		t.Fatalf("close synthetic workflow: %v", err)
	}
	history := &historypb.History{}
	iterator := restarted.GetWorkflowHistory(ctx, WorkflowID(session.ID), "", false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	for iterator.HasNext() {
		event, err := iterator.Next()
		if err != nil {
			t.Fatal(err)
		}
		history.Events = append(history.Events, event)
	}
	assertCancellationHistory(t, history)
	replayCancellationHistory(t, history)
	if path := os.Getenv("CONDUIT_PLANNING_HISTORY_PATH"); path != "" {
		body, err := (protojson.MarshalOptions{Indent: "  "}).Marshal(history)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("owner-isolated exact-run cancellation survived server/worker restart; activity calls=%d history events=%d; no model/provider call", calls.Load(), len(history.Events))
}

func assertCancellationHistory(t *testing.T, history *historypb.History) {
	t.Helper()
	turns, failedTurns, activities := 0, 0, 0
	closed, versioned := false, false
	for _, event := range history.Events {
		if event.GetWorkflowExecutionCanceledEventAttributes() != nil {
			closed = true
		}
		if marker := event.GetMarkerRecordedEventAttributes(); marker != nil && marker.MarkerName == "Version" {
			var id string
			var version tw.Version
			decoder := converter.GetDefaultDataConverter()
			if err := decoder.FromPayloads(marker.Details["change-id"], &id); err != nil {
				t.Fatal(err)
			}
			if id == changeID {
				if err := decoder.FromPayloads(marker.Details["version"], &version); err != nil {
					t.Fatal(err)
				}
				if version != 1 {
					t.Fatalf("fixture is not planning workflow version 1: %d", version)
				}
				versioned = true
			}
		}
		if event.GetActivityTaskScheduledEventAttributes() != nil {
			activities++
		}
		if event.GetWorkflowTaskFailedEventAttributes() != nil {
			t.Fatal("captured workflow task failed")
		}
		if accepted := event.GetWorkflowExecutionUpdateAcceptedEventAttributes(); accepted != nil {
			input := accepted.GetAcceptedRequest().GetInput()
			if input.GetName() == UpdateTurn {
				var req TurnRequest
				if err := converter.GetDefaultDataConverter().FromPayloads(input.GetArgs(), &req); err != nil {
					t.Fatal(err)
				}
				if req.Actor != "alice" || req.RunID != "logical-cancelled" {
					t.Fatalf("unbound recorded turn: %#v", req)
				}
				turns++
			}
		}
		if completed := event.GetWorkflowExecutionUpdateCompletedEventAttributes(); completed != nil && completed.GetOutcome().GetFailure().GetApplicationFailureInfo().GetType() == ErrTypeRunCancelled {
			failedTurns++
		}
	}
	if turns != 2 || failedTurns != 2 || activities != 1 || !closed || !versioned {
		t.Fatalf("history lacks the active and reused-run refusals: turns=%d refused=%d activities=%d closed=%v versioned=%v", turns, failedTurns, activities, closed, versioned)
	}
}

func replayCancellationHistory(t *testing.T, history *historypb.History) {
	t.Helper()
	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflowWithOptions(SessionWorkflow, tw.RegisterOptions{Name: WorkflowName})
	if err := replayer.ReplayWorkflowHistory(nil, history); err != nil {
		t.Fatalf("cancellation history replay: %v", err)
	}
}
