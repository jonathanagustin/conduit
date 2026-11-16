// Package agui mirrors workspace coding-agent ACP activity onto the Hub's
// validated AG-UI 1.0 console event stream. Every emitted frame is checked
// against api/agui.Sequence and audited before publication.
package agui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/api/agui"
	"github.com/jonathanagustin/conduit/internal/workspaceacp/domain"
	"github.com/jonathanagustin/conduit/internal/workspaceacp/ports"
)

// FileEditView is one governed file write projected into the console AG-UI state.
type FileEditView struct {
	Path       string `json:"path"`
	Digest     string `json:"digest"`
	RunID      string `json:"runId"`
	ToolCallID string `json:"toolCallId"`
	UpdatedAt  string `json:"updatedAt"`
}

// ToolView is one tool call or terminal command projected into the console AG-UI state.
type ToolView struct {
	ToolCallID string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Path       string `json:"path,omitempty"`
	Command    string `json:"command,omitempty"`
	Digest     string `json:"digest,omitempty"`
}

type activeRun struct {
	runID         string
	sequence      *agui.Sequence
	frames        [][]byte
	bodyEvents    []map[string]any
	subscribers   map[chan []byte]struct{}
	done          chan struct{}
	closed        bool
	startedTools  map[string]bool
	answeredTools map[string]bool
	state         map[string]json.RawMessage
	msgSeq        int
}

type threadEntry struct {
	scope         domain.Scope
	sessionID     string
	threadID      string
	status        string
	edits         []FileEditView
	tools         []ToolView
	permissions   []domain.PermissionGate
	messages      []map[string]any
	lastRunEvents []map[string]any
	active        *activeRun
}

// Mirror maintains per-thread AG-UI projections and live SSE fan-out for
// workspace ACP sessions.
type Mirror struct {
	mu      sync.Mutex
	threads map[string]*threadEntry
	auditor ports.Auditor
	now     func() time.Time
}

// NewMirror constructs an AG-UI mirror backed by the Hub authorization auditor.
func NewMirror(auditor ports.Auditor, now func() time.Time) *Mirror {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Mirror{
		threads: map[string]*threadEntry{},
		auditor: auditor,
		now:     now,
	}
}

// RegisterThread initializes the mirrored console thread for an ACP session.
func (m *Mirror) RegisterThread(scope domain.Scope, sessionID, threadID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.threads[threadID]; exists {
		return
	}
	m.threads[threadID] = &threadEntry{
		scope:       scope,
		sessionID:   sessionID,
		threadID:    threadID,
		status:      "idle",
		edits:       []FileEditView{},
		tools:       []ToolView{},
		permissions: []domain.PermissionGate{},
		messages:    []map[string]any{},
	}
}

// ThreadScope returns the bound Scope and SessionID for a mirrored thread, if known.
func (m *Mirror) ThreadScope(threadID string) (domain.Scope, string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.threads[threadID]
	if !ok {
		return domain.Scope{}, "", false
	}
	return entry.scope, entry.sessionID, true
}

// StartRun opens a validated AG-UI run for an active ACP prompt turn.
func (m *Mirror) StartRun(ctx context.Context, threadID, runID, userPrompt string) error {
	m.mu.Lock()
	entry, ok := m.threads[threadID]
	if !ok {
		m.mu.Unlock()
		return domain.ErrNotFound
	}
	run := &activeRun{
		runID:         runID,
		sequence:      agui.NewSequence(threadID, runID),
		subscribers:   map[chan []byte]struct{}{},
		done:          make(chan struct{}),
		startedTools:  map[string]bool{},
		answeredTools: map[string]bool{},
	}
	entry.status = "running"
	if strings.TrimSpace(userPrompt) != "" {
		entry.messages = append(entry.messages, map[string]any{
			"id":      fmt.Sprintf("user:%s:%d", runID, len(entry.messages)+1),
			"role":    "user",
			"content": userPrompt,
		})
	}
	entry.active = run
	snap := snapshotPayloadLocked(entry)
	m.mu.Unlock()

	if err := m.emit(ctx, entry, run, map[string]any{
		"type":     "RUN_STARTED",
		"threadId": threadID,
		"runId":    runID,
	}, false); err != nil {
		return err
	}
	return m.emitState(ctx, entry, run, snap)
}

// RecordToolStart mirrors the start of an editor tool call, file write, or command.
func (m *Mirror) RecordToolStart(ctx context.Context, threadID, runID, toolCallID, toolName, kind, path, command, argsJSON string) error {
	m.mu.Lock()
	entry, ok := m.threads[threadID]
	if !ok || entry.active == nil || entry.active.runID != runID {
		m.mu.Unlock()
		return nil
	}
	run := entry.active
	if run.startedTools[toolCallID] {
		m.mu.Unlock()
		return nil
	}
	run.startedTools[toolCallID] = true
	run.msgSeq++
	parentID := fmt.Sprintf("msg:%s:tc:%d", runID, run.msgSeq)
	if argsJSON == "" {
		rawArgs, _ := json.Marshal(map[string]string{"kind": kind, "path": path, "command": command})
		argsJSON = string(rawArgs)
	}
	if len(argsJSON) > 4096 {
		argsJSON = argsJSON[:4096]
	}
	upsertToolLocked(entry, ToolView{
		ToolCallID: toolCallID,
		Name:       toolName,
		Kind:       kind,
		Status:     "running",
		Path:       path,
		Command:    command,
	})
	entry.messages = append(entry.messages, map[string]any{
		"id":   parentID,
		"role": "assistant",
		"toolCalls": []any{
			map[string]any{
				"id":   toolCallID,
				"type": "function",
				"function": map[string]any{
					"name":      toolName,
					"arguments": argsJSON,
				},
			},
		},
	})
	snap := snapshotPayloadLocked(entry)
	m.mu.Unlock()

	for _, ev := range []map[string]any{
		{"type": "TEXT_MESSAGE_START", "messageId": parentID, "role": "assistant"},
		{"type": "TOOL_CALL_START", "toolCallId": toolCallID, "toolCallName": toolName, "parentMessageId": parentID},
		{"type": "TOOL_CALL_ARGS", "toolCallId": toolCallID, "delta": argsJSON},
		{"type": "TOOL_CALL_END", "toolCallId": toolCallID},
		{"type": "TEXT_MESSAGE_END", "messageId": parentID},
	} {
		if err := m.emit(ctx, entry, run, ev, true); err != nil {
			return err
		}
	}
	return m.emitState(ctx, entry, run, snap)
}

// RecordToolResult mirrors completion or failure of a tool call, file write, or command.
func (m *Mirror) RecordToolResult(ctx context.Context, threadID, runID, toolCallID, toolName, kind, status, path, command, digest, resultText string) error {
	if err := m.RecordToolStart(ctx, threadID, runID, toolCallID, toolName, kind, path, command, ""); err != nil {
		return err
	}
	m.mu.Lock()
	entry, ok := m.threads[threadID]
	if !ok || entry.active == nil || entry.active.runID != runID {
		m.mu.Unlock()
		return nil
	}
	run := entry.active
	alreadyAnswered := run.answeredTools[toolCallID]
	run.answeredTools[toolCallID] = true

	upsertToolLocked(entry, ToolView{
		ToolCallID: toolCallID,
		Name:       toolName,
		Kind:       kind,
		Status:     status,
		Path:       path,
		Command:    command,
		Digest:     digest,
	})
	if kind == string(domain.ActionFileWrite) && status == "completed" && path != "" {
		entry.edits = append(entry.edits, FileEditView{
			Path:       path,
			Digest:     digest,
			RunID:      runID,
			ToolCallID: toolCallID,
			UpdatedAt:  m.now().UTC().Format(time.RFC3339Nano),
		})
	}
	resultPayload, _ := json.Marshal(map[string]any{
		"status": status,
		"kind":   kind,
		"path":   path,
		"digest": digest,
		"output": truncateText(resultText, 1024),
	})
	resultMsgID := fmt.Sprintf("res:%s:%s", runID, toolCallID)
	if !alreadyAnswered {
		entry.messages = append(entry.messages, map[string]any{
			"id":         resultMsgID,
			"role":       "tool",
			"toolCallId": toolCallID,
			"content":    string(resultPayload),
		})
	}
	snap := snapshotPayloadLocked(entry)
	m.mu.Unlock()

	if !alreadyAnswered {
		if err := m.emit(ctx, entry, run, map[string]any{
			"type":       "TOOL_CALL_RESULT",
			"toolCallId": toolCallID,
			"messageId":  resultMsgID,
			"content":    string(resultPayload),
		}, true); err != nil {
			return err
		}
	}
	return m.emitState(ctx, entry, run, snap)
}

// RecordPermission updates the governed permission gate projection on the AG-UI stream.
func (m *Mirror) RecordPermission(ctx context.Context, gate domain.PermissionGate) error {
	m.mu.Lock()
	entry, ok := m.threads[threadIDOrEmpty(gate)]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	replaced := false
	for idx, existing := range entry.permissions {
		if existing.RequestID == gate.RequestID {
			entry.permissions[idx] = gate
			replaced = true
			break
		}
	}
	if !replaced {
		entry.permissions = append(entry.permissions, gate)
	}
	run := entry.active
	if run == nil || run.runID != gate.RunID {
		m.mu.Unlock()
		return nil
	}
	snap := snapshotPayloadLocked(entry)
	m.mu.Unlock()
	return m.emitState(ctx, entry, run, snap)
}

// RecordAssistantText streams one assistant message segment onto the AG-UI run.
func (m *Mirror) RecordAssistantText(ctx context.Context, threadID, runID, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	m.mu.Lock()
	entry, ok := m.threads[threadID]
	if !ok || entry.active == nil || entry.active.runID != runID {
		m.mu.Unlock()
		return nil
	}
	run := entry.active
	run.msgSeq++
	msgID := fmt.Sprintf("msg:%s:txt:%d", runID, run.msgSeq)
	entry.messages = append(entry.messages, map[string]any{
		"id":      msgID,
		"role":    "assistant",
		"content": text,
	})
	m.mu.Unlock()

	for _, ev := range []map[string]any{
		{"type": "TEXT_MESSAGE_START", "messageId": msgID, "role": "assistant"},
		{"type": "TEXT_MESSAGE_CONTENT", "messageId": msgID, "delta": text},
		{"type": "TEXT_MESSAGE_END", "messageId": msgID},
	} {
		if err := m.emit(ctx, entry, run, ev, true); err != nil {
			return err
		}
	}
	return nil
}

// FinishRun closes the active AG-UI run with RUN_FINISHED (success or cancelled).
func (m *Mirror) FinishRun(ctx context.Context, threadID, runID string, cancelled bool) error {
	m.mu.Lock()
	entry, ok := m.threads[threadID]
	if !ok || entry.active == nil || entry.active.runID != runID {
		m.mu.Unlock()
		return nil
	}
	run := entry.active
	if cancelled {
		entry.status = "cancelled"
	} else {
		entry.status = "ready"
	}
	snap := snapshotPayloadLocked(entry)
	msgs := cloneSlice(entry.messages)
	m.mu.Unlock()

	if err := m.emitState(ctx, entry, run, snap); err != nil {
		return err
	}
	if err := m.emit(ctx, entry, run, map[string]any{
		"type":     "MESSAGES_SNAPSHOT",
		"messages": msgs,
	}, true); err != nil {
		return err
	}
	outcomeType := "success"
	if cancelled {
		outcomeType = "cancelled"
	}
	err := m.emit(ctx, entry, run, map[string]any{
		"type":     "RUN_FINISHED",
		"threadId": threadID,
		"runId":    runID,
		"outcome":  map[string]any{"type": outcomeType},
	}, false)
	m.closeActive(entry, run)
	return err
}

// FailRun terminates the active AG-UI run with RUN_ERROR.
func (m *Mirror) FailRun(ctx context.Context, threadID, runID, code, message string) {
	m.mu.Lock()
	entry, ok := m.threads[threadID]
	if !ok || entry.active == nil || entry.active.runID != runID {
		m.mu.Unlock()
		return
	}
	run := entry.active
	entry.status = "failed"
	m.mu.Unlock()

	if code == "" {
		code = "ACP_RUN_FAILED"
	}
	if message == "" {
		message = "Workspace coding-agent run failed."
	}
	_ = m.emit(ctx, entry, run, map[string]any{
		"type":    "RUN_ERROR",
		"code":    code,
		"message": message,
	}, false)
	m.closeActive(entry, run)
}

func (m *Mirror) closeActive(entry *threadEntry, run *activeRun) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !run.closed {
		run.closed = true
		close(run.done)
		for ch := range run.subscribers {
			close(ch)
			delete(run.subscribers, ch)
		}
	}
	entry.lastRunEvents = append([]map[string]any(nil), run.bodyEvents...)
	if entry.active == run {
		entry.active = nil
	}
}

// Stream writes a complete, Sequence-validated AG-UI exchange for threadID and
// requestedRunID to writeFrame. If a turn is currently active and snapshotOnly
// is false, it streams live events until the turn finishes; otherwise it replays
// the latest mirrored state and tool events for the thread.
func (m *Mirror) Stream(ctx context.Context, actor, threadID, requestedRunID string, snapshotOnly bool, writeFrame func([]byte) error) error {
	m.mu.Lock()
	entry, ok := m.threads[threadID]
	if !ok {
		m.mu.Unlock()
		return domain.ErrNotFound
	}
	scope := entry.scope
	sessionID := entry.sessionID
	active := entry.active
	if !snapshotOnly && active != nil && !active.closed {
		existingFrames := make([][]byte, len(active.frames))
		for i, f := range active.frames {
			existingFrames[i] = append([]byte(nil), f...)
		}
		ch := make(chan []byte, 64)
		active.subscribers[ch] = struct{}{}
		m.mu.Unlock()

		defer func() {
			m.mu.Lock()
			delete(active.subscribers, ch)
			m.mu.Unlock()
		}()

		seq := agui.NewSequence(threadID, requestedRunID)
		forward := func(frame []byte) error {
			adapted, err := adaptRunID(frame, requestedRunID)
			if err != nil {
				return err
			}
			next, err := seq.Next(adapted)
			if err != nil {
				return err
			}
			if err := writeFrame(adapted); err != nil {
				return err
			}
			seq = next
			return nil
		}
		for _, frame := range existingFrames {
			if err := forward(frame); err != nil {
				return err
			}
		}
		if seq.Complete() {
			return nil
		}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case frame, open := <-ch:
				if !open {
					return nil
				}
				if err := forward(frame); err != nil {
					return err
				}
				if seq.Complete() {
					return nil
				}
			}
		}
	}

	snap := snapshotPayloadLocked(entry)
	msgs := cloneSlice(entry.messages)
	recordedBody := append([]map[string]any(nil), entry.lastRunEvents...)
	m.mu.Unlock()

	seq := agui.NewSequence(threadID, requestedRunID)
	sendOne := func(ev map[string]any) error {
		raw, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		next, err := seq.Next(raw)
		if err != nil {
			return err
		}
		if m.auditor != nil {
			if err := m.auditor.Record(ctx, domain.AuditEntry{
				Subject:     actor,
				Action:      "workspace.acp.agui." + fmt.Sprint(ev["type"]),
				Resource:    "workspace/" + scope.WorkspaceID,
				Decision:    "ALLOW",
				Reason:      "mirrored workspace ACP console event",
				WorkspaceID: scope.WorkspaceID,
				Branch:      scope.Branch,
				SessionID:   sessionID,
				RunID:       requestedRunID,
				ThreadID:    threadID,
				Digest:      domain.DigestBytes(raw),
				At:          m.now(),
			}); err != nil {
				return domain.ErrAuditUnavailable
			}
		}
		if err := writeFrame(raw); err != nil {
			return err
		}
		seq = next
		return nil
	}

	if err := sendOne(map[string]any{
		"type":     "RUN_STARTED",
		"threadId": threadID,
		"runId":    requestedRunID,
	}); err != nil {
		return err
	}
	if !snapshotOnly && len(recordedBody) > 0 {
		for _, ev := range recordedBody {
			if err := sendOne(ev); err != nil {
				return err
			}
		}
	} else {
		if err := sendOne(map[string]any{
			"type":     "STATE_SNAPSHOT",
			"snapshot": snap,
		}); err != nil {
			return err
		}
		if err := sendOne(map[string]any{
			"type":     "MESSAGES_SNAPSHOT",
			"messages": msgs,
		}); err != nil {
			return err
		}
	}
	return sendOne(map[string]any{
		"type":     "RUN_FINISHED",
		"threadId": threadID,
		"runId":    requestedRunID,
		"outcome":  map[string]any{"type": "success"},
	})
}

func (m *Mirror) emitState(ctx context.Context, entry *threadEntry, run *activeRun, value map[string]any) error {
	m.mu.Lock()
	next := map[string]json.RawMessage{}
	for key, child := range value {
		raw, err := json.Marshal(child)
		if err != nil {
			m.mu.Unlock()
			return err
		}
		next[key] = raw
	}
	prev := run.state
	m.mu.Unlock()

	if prev == nil {
		if err := m.emit(ctx, entry, run, map[string]any{"type": "STATE_SNAPSHOT", "snapshot": value}, true); err != nil {
			return err
		}
	} else {
		keys := make([]string, 0, len(next))
		for k := range next {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		patch := []any{}
		for _, k := range keys {
			if bytes.Equal(prev[k], next[k]) {
				continue
			}
			op := "replace"
			if _, exists := prev[k]; !exists {
				op = "add"
			}
			escaped := strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
			var parsed any
			_ = json.Unmarshal(next[k], &parsed)
			patch = append(patch, map[string]any{"op": op, "path": "/" + escaped, "value": parsed})
		}
		if len(patch) > 0 {
			if err := m.emit(ctx, entry, run, map[string]any{"type": "STATE_DELTA", "delta": patch}, true); err != nil {
				return err
			}
		}
	}
	m.mu.Lock()
	run.state = next
	m.mu.Unlock()
	return nil
}

func (m *Mirror) emit(ctx context.Context, entry *threadEntry, run *activeRun, event map[string]any, recordInBody bool) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if run.closed {
		m.mu.Unlock()
		return nil
	}
	nextSeq, err := run.sequence.Next(raw)
	scope := entry.scope
	sessionID := entry.sessionID
	threadID := entry.threadID
	m.mu.Unlock()
	if err != nil {
		return err
	}

	if m.auditor != nil {
		if err := m.auditor.Record(ctx, domain.AuditEntry{
			Subject:     scope.Owner,
			Action:      "workspace.acp.agui." + fmt.Sprint(event["type"]),
			Resource:    "workspace/" + scope.WorkspaceID,
			Decision:    "ALLOW",
			Reason:      "mirrored workspace ACP console event",
			WorkspaceID: scope.WorkspaceID,
			Branch:      scope.Branch,
			SessionID:   sessionID,
			RunID:       run.runID,
			ThreadID:    threadID,
			Digest:      domain.DigestBytes(raw),
			At:          m.now(),
		}); err != nil {
			return domain.ErrAuditUnavailable
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if run.closed {
		return nil
	}
	run.sequence = nextSeq
	copied := append([]byte(nil), raw...)
	run.frames = append(run.frames, copied)
	if recordInBody {
		var cloned map[string]any
		_ = json.Unmarshal(copied, &cloned)
		run.bodyEvents = append(run.bodyEvents, cloned)
	}
	for ch := range run.subscribers {
		select {
		case ch <- copied:
		default:
		}
	}
	return nil
}

func snapshotPayloadLocked(entry *threadEntry) map[string]any {
	return map[string]any{
		"session": map[string]any{
			"sessionId":     entry.sessionID,
			"threadId":      entry.threadID,
			"workspaceId":   entry.scope.WorkspaceID,
			"environmentId": entry.scope.EnvironmentID,
			"owner":         entry.scope.Owner,
			"branch":        entry.scope.Branch,
			"status":        entry.status,
			"valueTier":     domain.ValueTier,
		},
		"edits":       append([]FileEditView{}, entry.edits...),
		"toolCalls":   append([]ToolView{}, entry.tools...),
		"permissions": append([]domain.PermissionGate{}, entry.permissions...),
	}
}

func upsertToolLocked(entry *threadEntry, next ToolView) {
	for i, existing := range entry.tools {
		if existing.ToolCallID == next.ToolCallID {
			if next.Path == "" {
				next.Path = existing.Path
			}
			if next.Command == "" {
				next.Command = existing.Command
			}
			if next.Digest == "" {
				next.Digest = existing.Digest
			}
			entry.tools[i] = next
			return
		}
	}
	entry.tools = append(entry.tools, next)
}

func adaptRunID(frame []byte, requestedRunID string) ([]byte, error) {
	var parsed map[string]any
	if err := json.Unmarshal(frame, &parsed); err != nil {
		return nil, err
	}
	kind, _ := parsed["type"].(string)
	if (kind == "RUN_STARTED" || kind == "RUN_FINISHED") && requestedRunID != "" {
		parsed["runId"] = requestedRunID
		return json.Marshal(parsed)
	}
	return frame, nil
}

func cloneSlice(items []map[string]any) []map[string]any {
	raw, _ := json.Marshal(items)
	var out []map[string]any
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []map[string]any{}
	}
	return out
}

func truncateText(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func threadIDOrEmpty(gate domain.PermissionGate) string {
	return gate.ThreadID
}
