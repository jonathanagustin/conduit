package agui

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"

	protocol "github.com/jonathanagustin/conduit/api/agui"
	"github.com/jonathanagustin/conduit/internal/workspaceacp/domain"
)

type publicationAuditor func(context.Context, domain.AuditEntry) error

func (audit publicationAuditor) Record(ctx context.Context, entry domain.AuditEntry) error {
	return audit(ctx, entry)
}

func publicationRun(t *testing.T, m *Mirror, thread string) (*threadEntry, *activeRun) {
	t.Helper()
	m.RegisterThread(domain.Scope{Owner: "alice", WorkspaceID: "workspace"}, "session", thread)
	if err := m.StartRun(context.Background(), thread, "run", "test"); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.threads[thread], m.threads[thread].active
}

func assertPublicationSequence(t *testing.T, m *Mirror, entry *threadEntry, run *activeRun) {
	t.Helper()
	if err := m.FinishRun(context.Background(), entry.threadID, run.runID, false); err != nil {
		t.Fatal(err)
	}
	sequence := protocol.NewSequence(entry.threadID, run.runID)
	for _, frame := range run.frames {
		if err := sequence.Accept(frame); err != nil {
			t.Fatalf("invalid published frame %s: %v", frame, err)
		}
	}
	if !sequence.Complete() {
		t.Fatal("published run is incomplete")
	}
}

func TestPublicationSerializesValidationAuditAndCommit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		var blocked atomic.Bool
		m := NewMirror(publicationAuditor(func(_ context.Context, entry domain.AuditEntry) error {
			if entry.Action == "workspace.acp.agui.STATE_DELTA" && blocked.CompareAndSwap(false, true) {
				close(entered)
				<-release
			}
			return nil
		}), nil)
		entry, run := publicationRun(t, m, "thread")
		first, second := make(chan error, 1), make(chan error, 1)
		go func() {
			first <- m.emit(context.Background(), entry, run, map[string]any{"type": "STATE_DELTA", "delta": []any{}}, true)
		}()
		<-entered
		go func() {
			second <- m.emit(context.Background(), entry, run, map[string]any{"type": "TEXT_MESSAGE_START", "messageId": "answer", "role": "assistant"}, true)
		}()
		synctest.Wait()
		var secondFinished bool
		select {
		case err := <-second:
			secondFinished = true
			t.Errorf("second publisher passed an uncommitted audited event: %v", err)
		default:
		}
		close(release)
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		if !secondFinished {
			if err := <-second; err != nil {
				t.Fatal(err)
			}
		}
		for _, event := range []map[string]any{
			{"type": "TEXT_MESSAGE_CONTENT", "messageId": "answer", "delta": "ready"},
			{"type": "TEXT_MESSAGE_END", "messageId": "answer"},
		} {
			if err := m.emit(context.Background(), entry, run, event, true); err != nil {
				t.Fatal(err)
			}
		}
		assertPublicationSequence(t, m, entry, run)
	})
}

func TestPublicationStateBaselineDoesNotRegressAcrossQueuedUpdates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		var blocked atomic.Bool
		m := NewMirror(publicationAuditor(func(_ context.Context, entry domain.AuditEntry) error {
			if entry.Action == "workspace.acp.agui.STATE_DELTA" && blocked.CompareAndSwap(false, true) {
				close(entered)
				<-release
			}
			return nil
		}), nil)
		entry, run := publicationRun(t, m, "thread")
		first, second := make(chan error, 1), make(chan error, 1)
		gate := domain.PermissionGate{ThreadID: "thread", RunID: "run", Status: domain.StatusApproved}
		one, two := gate, gate
		one.RequestID, two.RequestID = "first", "second"
		go func() { first <- m.RecordPermission(context.Background(), one) }()
		<-entered
		go func() { second <- m.RecordPermission(context.Background(), two) }()
		synctest.Wait()
		close(release)
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		if err := <-second; err != nil {
			t.Fatal(err)
		}
		var permissions []any
		for _, frame := range run.frames {
			var event map[string]any
			if err := json.Unmarshal(frame, &event); err != nil {
				t.Fatal(err)
			}
			if event["type"] == "STATE_SNAPSHOT" {
				permissions, _ = event["snapshot"].(map[string]any)["permissions"].([]any)
			}
			if event["type"] == "STATE_DELTA" {
				for _, raw := range event["delta"].([]any) {
					patch := raw.(map[string]any)
					if patch["path"] == "/permissions" {
						permissions = patch["value"].([]any)
					}
				}
			}
		}
		if len(permissions) != 2 {
			t.Fatalf("published permission state regressed to %v", permissions)
		}
		var baseline []any
		if err := json.Unmarshal(run.state["permissions"], &baseline); err != nil {
			t.Fatal(err)
		}
		if len(baseline) != 2 {
			t.Fatalf("state baseline regressed to %v", baseline)
		}
		assertPublicationSequence(t, m, entry, run)
	})
}

func TestPublicationWaitingIsCancellableAndOtherRunsProceed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered, release := make(chan struct{}), make(chan struct{})
		var blocked atomic.Bool
		var m *Mirror
		m = NewMirror(publicationAuditor(func(_ context.Context, entry domain.AuditEntry) error {
			// This reentrant projection read must stay outside the global mutex.
			if _, _, ok := m.ThreadScope(entry.ThreadID); !ok {
				return domain.ErrNotFound
			}
			if entry.ThreadID == "first" && entry.Action == "workspace.acp.agui.TEXT_MESSAGE_START" && blocked.CompareAndSwap(false, true) {
				close(entered)
				<-release
			}
			return nil
		}), nil)
		_, first := publicationRun(t, m, "first")
		entry, second := publicationRun(t, m, "second")
		busy := make(chan error, 1)
		go func() { busy <- m.RecordAssistantText(context.Background(), "first", "run", "busy") }()
		<-entered
		ctx, cancel := context.WithCancel(context.Background())
		waiting, independent := make(chan error, 1), make(chan error, 1)
		go func() { waiting <- m.RecordAssistantText(ctx, "first", "run", "cancelled waiter") }()
		go func() { independent <- m.RecordAssistantText(context.Background(), "second", "run", "independent") }()
		synctest.Wait()
		cancel()
		if err := <-waiting; !errors.Is(err, context.Canceled) {
			t.Errorf("waiting publisher: %v", err)
		}
		var otherFinished bool
		select {
		case err := <-independent:
			otherFinished = true
			if err != nil {
				t.Error(err)
			}
		default:
			t.Error("one run's blocked audit stopped an independent run")
		}
		close(release)
		if err := <-busy; err != nil {
			t.Fatal(err)
		}
		if !otherFinished {
			if err := <-independent; err != nil {
				t.Fatal(err)
			}
		}
		assertPublicationSequence(t, m, entry, second)
		if first.closed {
			t.Fatal("independent run completion closed the first run")
		}
	})
}

func TestPublicationAuditFailureDoesNotAdvanceStateOrOrdering(t *testing.T) {
	var fail atomic.Bool
	m := NewMirror(publicationAuditor(func(_ context.Context, _ domain.AuditEntry) error {
		if fail.Load() {
			return errors.New("audit unavailable")
		}
		return nil
	}), nil)
	entry, run := publicationRun(t, m, "thread")
	before := len(run.frames)
	fail.Store(true)
	if err := m.RecordPermission(context.Background(), domain.PermissionGate{ThreadID: "thread", RunID: "run", RequestID: "permission"}); !errors.Is(err, domain.ErrAuditUnavailable) {
		t.Fatalf("failed state audit: %v", err)
	}
	if len(run.frames) != before || string(run.state["permissions"]) != "[]" {
		t.Fatal("failed audit advanced publication/state baseline")
	}
	if err := m.emit(context.Background(), entry, run, map[string]any{"type": "TEXT_MESSAGE_START", "messageId": "retry", "role": "assistant"}, true); !errors.Is(err, domain.ErrAuditUnavailable) {
		t.Fatalf("failed text audit: %v", err)
	}
	fail.Store(false)
	for _, event := range []map[string]any{
		{"type": "TEXT_MESSAGE_START", "messageId": "retry", "role": "assistant"},
		{"type": "TEXT_MESSAGE_CONTENT", "messageId": "retry", "delta": "audited"},
		{"type": "TEXT_MESSAGE_END", "messageId": "retry"},
	} {
		if err := m.emit(context.Background(), entry, run, event, true); err != nil {
			t.Fatal(err)
		}
	}
	assertPublicationSequence(t, m, entry, run)
}
