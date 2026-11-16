// Package memorybridge provides a deterministic pure-Go ACP bridge pool so
// Hub unit and composition tests exercise the full workspace ACP contract
// without requiring a Node subprocess in Go-only CI lanes.
package memorybridge

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/jonathanagustin/conduit/internal/workspaceacp/domain"
	"github.com/jonathanagustin/conduit/internal/workspaceacp/ports"
)

type pendingDecision struct {
	optionID  string
	cancelled bool
}

type sessionState struct {
	threadID      string
	configOptions []any
	cancelMu      sync.Mutex
	activeCancel  context.CancelFunc
}

// Pool provisions deterministic in-memory ACP bridges keyed by (owner, workspace, branch).
type Pool struct {
	mu      sync.Mutex
	bridges map[string]*Bridge
}

// NewPool creates an in-memory ACP BridgePool.
func NewPool() *Pool {
	return &Pool{bridges: map[string]*Bridge{}}
}

// Acquire returns the scoped in-memory Bridge for (owner, workspaceID, branch).
func (p *Pool) Acquire(_ context.Context, scope domain.Scope, files map[string][]byte) (ports.Bridge, error) {
	key := scope.Owner + "\x00" + scope.WorkspaceID + "\x00" + scope.Branch
	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.bridges[key]; ok {
		existing.syncFiles(files)
		return existing, nil
	}
	b := &Bridge{
		scope:    scope,
		files:    map[string][]byte{},
		sessions: map[string]*sessionState{},
		pending:  map[string]chan pendingDecision{},
	}
	b.syncFiles(files)
	p.bridges[key] = b
	return b, nil
}

// Bridge is a deterministic in-memory implementation of ports.Bridge.
type Bridge struct {
	scope domain.Scope

	mu       sync.Mutex
	files    map[string][]byte
	sessions map[string]*sessionState
	pending  map[string]chan pendingDecision
	seq      atomic.Int64
}

func (b *Bridge) syncFiles(files map[string][]byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for k, v := range files {
		if _, exists := b.files[k]; !exists {
			b.files[k] = append([]byte(nil), v...)
		}
	}
}

// Initialize checks the pinned ACP protocol version.
func (b *Bridge) Initialize(_ context.Context, protocolVersion int) (map[string]any, error) {
	if protocolVersion != domain.ProtocolVersion {
		return nil, domain.ErrProtocolVersion
	}
	return map[string]any{
		"protocolVersion":   domain.ProtocolVersion,
		"agentCapabilities": map[string]any{"loadSession": false},
		"agentInfo":         map[string]any{"name": "conduit-memory-acp", "version": "1.0.0"},
		"authMethods":       []any{},
	}, nil
}

// NewSession creates an isolated ACP session within this bridge's scope.
func (b *Bridge) NewSession(_ context.Context, threadID string) (string, []any, error) {
	id := fmt.Sprintf("acp-sess-%d", b.seq.Add(1))
	defaults := []any{
		map[string]any{
			"id":           "mode",
			"name":         "Approval Mode",
			"category":     "mode",
			"type":         "select",
			"currentValue": "workspace-write",
			"options": []any{
				map[string]any{"value": "read-only", "name": "Read Only"},
				map[string]any{"value": "workspace-write", "name": "Workspace Write"},
			},
		},
	}
	b.mu.Lock()
	b.sessions[id] = &sessionState{
		threadID:      threadID,
		configOptions: defaults,
	}
	b.mu.Unlock()
	return id, defaults, nil
}

// SetConfigOption updates a session configuration option, refusing agent-full-access.
func (b *Bridge) SetConfigOption(_ context.Context, sessionID, configID string, value any) ([]any, error) {
	if str, ok := value.(string); ok && str == "agent-full-access" {
		return nil, domain.ErrForbidden
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	sess, ok := b.sessions[sessionID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	updated := make([]any, 0, len(sess.configOptions))
	for _, raw := range sess.configOptions {
		item, ok := raw.(map[string]any)
		if !ok {
			updated = append(updated, raw)
			continue
		}
		cloned := map[string]any{}
		for k, v := range item {
			cloned[k] = v
		}
		if cloned["id"] == configID {
			cloned["currentValue"] = value
		}
		updated = append(updated, cloned)
	}
	sess.configOptions = updated
	return updated, nil
}

// Prompt runs a scripted ACP turn that gates every tool call, file write, and
// command on an explicit governed permission settlement.
func (b *Bridge) Prompt(
	ctx context.Context,
	rpcID any,
	sessionID, runID string,
	prompt []map[string]any,
	onEvent func(ports.BridgeEvent) error,
) error {
	b.mu.Lock()
	sess, ok := b.sessions[sessionID]
	b.mu.Unlock()
	if !ok {
		return domain.ErrNotFound
	}

	turnCtx, cancelTurn := context.WithCancel(ctx)
	sess.cancelMu.Lock()
	sess.activeCancel = cancelTurn
	sess.cancelMu.Unlock()
	defer func() {
		cancelTurn()
		sess.cancelMu.Lock()
		sess.activeCancel = nil
		sess.cancelMu.Unlock()
	}()

	text := firstText(prompt)

	if strings.Contains(strings.ToLower(text), "please cancel") {
		<-turnCtx.Done()
		return onEvent(rpcStopResponse(rpcID, "cancelled"))
	}

	if rest, ok := cutPrefixCaseInsensitive(text, "edit "); ok {
		targetPath, content, hasColon := strings.Cut(rest, ":")
		targetPath = strings.TrimSpace(targetPath)
		content = strings.TrimPrefix(content, " ")
		if !hasColon || targetPath == "" {
			return domain.ErrInvalid
		}
		callID := fmt.Sprintf("call-%d", b.seq.Add(1))
		reqID := fmt.Sprintf("%s::%s::%s", sess.threadID, runID, callID)
		if err := onEvent(ports.BridgeEvent{
			Type: "activity",
			Activity: &ports.BridgeActivity{
				SessionID:  sessionID,
				ThreadID:   sess.threadID,
				RunID:      runID,
				ToolCallID: callID,
				Kind:       string(domain.ActionFileWrite),
				Name:       "fs/write_text_file",
				Status:     "pending",
				Path:       targetPath,
				ArgsText:   fmt.Sprintf(`{"path":%q}`, targetPath),
			},
		}); err != nil {
			return err
		}
		approved, cancelled, err := b.awaitPermission(turnCtx, onEvent, ports.BridgePermission{
			RequestID: reqID,
			ThreadID:  sess.threadID,
			RunID:     runID,
			ToolCall:  callID,
			Title:     "Write " + targetPath,
			Kind:      string(domain.ActionFileWrite),
			RiskTier:  string(domain.ClassifyPermission(string(domain.ActionFileWrite), "fs/write_text_file", "Write "+targetPath, targetPath, "", "")),
			Path:      targetPath,
			Options: []domain.ApprovalOption{
				{OptionID: "allow", Name: "Allow once", Kind: "allow_once"},
				{OptionID: "always", Name: "Always allow", Kind: "allow_always"},
				{OptionID: "reject", Name: "Reject", Kind: "reject_once"},
			},
		})
		if err != nil {
			return err
		}
		if cancelled {
			return onEvent(rpcStopResponse(rpcID, "cancelled"))
		}
		if !approved {
			if err := onEvent(ports.BridgeEvent{
				Type: "activity",
				Activity: &ports.BridgeActivity{
					SessionID:  sessionID,
					ThreadID:   sess.threadID,
					RunID:      runID,
					ToolCallID: callID,
					Kind:       string(domain.ActionFileWrite),
					Name:       "fs/write_text_file",
					Status:     "failed",
					Path:       targetPath,
					ResultText: "permission denied",
				},
			}); err != nil {
				return err
			}
			if err := onEvent(messageActivity(sessionID, sess.threadID, runID, "Edit to "+targetPath+" was declined.")); err != nil {
				return err
			}
			return onEvent(rpcStopResponse(rpcID, "end_turn"))
		}

		b.mu.Lock()
		b.files[targetPath] = []byte(content)
		b.mu.Unlock()
		digest := domain.DigestBytes([]byte(content))
		if err := onEvent(ports.BridgeEvent{
			Type: "activity",
			Activity: &ports.BridgeActivity{
				SessionID:  sessionID,
				ThreadID:   sess.threadID,
				RunID:      runID,
				ToolCallID: callID,
				Kind:       string(domain.ActionFileWrite),
				Name:       "fs/write_text_file",
				Status:     "completed",
				Path:       targetPath,
				Digest:     digest,
				Content:    content,
				ResultText: "wrote " + targetPath,
			},
		}); err != nil {
			return err
		}
		if err := onEvent(messageActivity(sessionID, sess.threadID, runID, "Updated "+targetPath+".")); err != nil {
			return err
		}
		return onEvent(rpcStopResponse(rpcID, "end_turn"))
	}

	if cmdText, ok := cutPrefixCaseInsensitive(text, "run command:"); ok {
		cmdText = strings.TrimSpace(cmdText)
		if cmdText == "" {
			return domain.ErrInvalid
		}
		callID := fmt.Sprintf("call-%d", b.seq.Add(1))
		reqID := fmt.Sprintf("%s::%s::%s", sess.threadID, runID, callID)
		if err := onEvent(ports.BridgeEvent{
			Type: "activity",
			Activity: &ports.BridgeActivity{
				SessionID:  sessionID,
				ThreadID:   sess.threadID,
				RunID:      runID,
				ToolCallID: callID,
				Kind:       string(domain.ActionCommand),
				Name:       "terminal/create",
				Status:     "pending",
				Command:    cmdText,
				ArgsText:   fmt.Sprintf(`{"command":%q}`, cmdText),
			},
		}); err != nil {
			return err
		}
		approved, cancelled, err := b.awaitPermission(turnCtx, onEvent, ports.BridgePermission{
			RequestID: reqID,
			ThreadID:  sess.threadID,
			RunID:     runID,
			ToolCall:  callID,
			Title:     "Run command: " + cmdText,
			Kind:      string(domain.ActionCommand),
			RiskTier:  string(domain.ClassifyPermission(string(domain.ActionCommand), "terminal/create", "Run command: "+cmdText, "", cmdText, "")),
			Command:   cmdText,
			Options: []domain.ApprovalOption{
				{OptionID: "allow", Name: "Allow once", Kind: "allow_once"},
				{OptionID: "reject", Name: "Reject", Kind: "reject_once"},
			},
		})
		if err != nil {
			return err
		}
		if cancelled {
			return onEvent(rpcStopResponse(rpcID, "cancelled"))
		}
		if !approved {
			if err := onEvent(ports.BridgeEvent{
				Type: "activity",
				Activity: &ports.BridgeActivity{
					SessionID:  sessionID,
					ThreadID:   sess.threadID,
					RunID:      runID,
					ToolCallID: callID,
					Kind:       string(domain.ActionCommand),
					Name:       "terminal/create",
					Status:     "failed",
					Command:    cmdText,
					ResultText: "permission denied",
				},
			}); err != nil {
				return err
			}
			if err := onEvent(messageActivity(sessionID, sess.threadID, runID, "Command "+cmdText+" was declined.")); err != nil {
				return err
			}
			return onEvent(rpcStopResponse(rpcID, "end_turn"))
		}

		if err := onEvent(ports.BridgeEvent{
			Type: "activity",
			Activity: &ports.BridgeActivity{
				SessionID:  sessionID,
				ThreadID:   sess.threadID,
				RunID:      runID,
				ToolCallID: callID,
				Kind:       string(domain.ActionCommand),
				Name:       "terminal/exit",
				Status:     "completed",
				Command:    cmdText,
				ResultText: "executed: " + cmdText,
			},
		}); err != nil {
			return err
		}
		if err := onEvent(messageActivity(sessionID, sess.threadID, runID, "Executed "+cmdText+".")); err != nil {
			return err
		}
		return onEvent(rpcStopResponse(rpcID, "end_turn"))
	}

	callID := fmt.Sprintf("call-%d", b.seq.Add(1))
	reqID := fmt.Sprintf("%s::%s::%s", sess.threadID, runID, callID)
	if err := onEvent(ports.BridgeEvent{
		Type: "activity",
		Activity: &ports.BridgeActivity{
			SessionID:  sessionID,
			ThreadID:   sess.threadID,
			RunID:      runID,
			ToolCallID: callID,
			Kind:       string(domain.ActionToolCall),
			Name:       "fs/read_text_file",
			Status:     "pending",
			Path:       "README.md",
			ArgsText:   `{"path":"README.md"}`,
		},
	}); err != nil {
		return err
	}
	approved, cancelled, err := b.awaitPermission(turnCtx, onEvent, ports.BridgePermission{
		RequestID: reqID,
		ThreadID:  sess.threadID,
		RunID:     runID,
		ToolCall:  callID,
		Title:     "Read README.md",
		Kind:      string(domain.ActionToolCall),
		RiskTier:  string(domain.RiskLowConfirm),
		Path:      "README.md",
		Options: []domain.ApprovalOption{
			{OptionID: "allow", Name: "Allow once", Kind: "allow_once"},
			{OptionID: "reject", Name: "Reject", Kind: "reject_once"},
		},
	})
	if err != nil {
		return err
	}
	if cancelled {
		return onEvent(rpcStopResponse(rpcID, "cancelled"))
	}
	if !approved {
		if err := onEvent(ports.BridgeEvent{
			Type: "activity",
			Activity: &ports.BridgeActivity{
				SessionID:  sessionID,
				ThreadID:   sess.threadID,
				RunID:      runID,
				ToolCallID: callID,
				Kind:       string(domain.ActionToolCall),
				Name:       "fs/read_text_file",
				Status:     "failed",
				Path:       "README.md",
				ResultText: "permission denied",
			},
		}); err != nil {
			return err
		}
		return onEvent(rpcStopResponse(rpcID, "end_turn"))
	}
	b.mu.Lock()
	readme := append([]byte(nil), b.files["README.md"]...)
	b.mu.Unlock()
	if err := onEvent(ports.BridgeEvent{
		Type: "activity",
		Activity: &ports.BridgeActivity{
			SessionID:  sessionID,
			ThreadID:   sess.threadID,
			RunID:      runID,
			ToolCallID: callID,
			Kind:       string(domain.ActionToolCall),
			Name:       "fs/read_text_file",
			Status:     "completed",
			Path:       "README.md",
			Digest:     domain.DigestBytes(readme),
			ResultText: string(readme),
		},
	}); err != nil {
		return err
	}
	if err := onEvent(messageActivity(sessionID, sess.threadID, runID, "Inspected README.md.")); err != nil {
		return err
	}
	return onEvent(rpcStopResponse(rpcID, "end_turn"))
}

func (b *Bridge) awaitPermission(
	ctx context.Context,
	onEvent func(ports.BridgeEvent) error,
	perm ports.BridgePermission,
) (approved bool, cancelled bool, err error) {
	ch := make(chan pendingDecision, 1)
	b.mu.Lock()
	b.pending[perm.RequestID] = ch
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.pending, perm.RequestID)
		b.mu.Unlock()
	}()
	if err := onEvent(ports.BridgeEvent{
		Type:       "permission_request",
		Permission: &perm,
	}); err != nil {
		return false, false, err
	}
	select {
	case <-ctx.Done():
		return false, true, nil
	case decision := <-ch:
		if decision.cancelled {
			return false, true, nil
		}
		return decision.optionID == "allow", false, nil
	}
}

// SettlePermission delivers a Hub-governed permission settlement to the active turn.
func (b *Bridge) SettlePermission(_ context.Context, requestID, _, _, optionID string, cancelled bool) error {
	b.mu.Lock()
	ch, ok := b.pending[requestID]
	b.mu.Unlock()
	if !ok {
		return domain.ErrNotFound
	}
	select {
	case ch <- pendingDecision{optionID: optionID, cancelled: cancelled}:
	default:
	}
	return nil
}

// CancelSession cancels any active turn on sessionID.
func (b *Bridge) CancelSession(_ context.Context, sessionID string) error {
	b.mu.Lock()
	sess, ok := b.sessions[sessionID]
	b.mu.Unlock()
	if !ok {
		return domain.ErrNotFound
	}
	sess.cancelMu.Lock()
	cancel := sess.activeCancel
	sess.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// CloseSession removes sessionID from the bridge.
func (b *Bridge) CloseSession(ctx context.Context, sessionID string) error {
	_ = b.CancelSession(ctx, sessionID)
	b.mu.Lock()
	delete(b.sessions, sessionID)
	b.mu.Unlock()
	return nil
}

// Close shuts down all sessions on the bridge.
func (b *Bridge) Close() error {
	b.mu.Lock()
	ids := make([]string, 0, len(b.sessions))
	for id := range b.sessions {
		ids = append(ids, id)
	}
	b.mu.Unlock()
	for _, id := range ids {
		_ = b.CloseSession(context.Background(), id)
	}
	return nil
}

func firstText(prompt []map[string]any) string {
	for _, block := range prompt {
		if block["type"] == "text" {
			if s, ok := block["text"].(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func cutPrefixCaseInsensitive(s, prefix string) (string, bool) {
	if len(s) < len(prefix) {
		return "", false
	}
	if strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return "", false
}

func messageActivity(sessionID, threadID, runID, text string) ports.BridgeEvent {
	return ports.BridgeEvent{
		Type: "activity",
		Activity: &ports.BridgeActivity{
			SessionID: sessionID,
			ThreadID:  threadID,
			RunID:     runID,
			Kind:      "message",
			Name:      "agent_message_chunk",
			Status:    "completed",
			Text:      text,
		},
	}
}

func rpcStopResponse(id any, stopReason string) ports.BridgeEvent {
	return ports.BridgeEvent{
		Type: "rpc_response",
		RawFrame: map[string]any{
			"type": "rpc_response",
			"response": map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"stopReason": stopReason,
				},
			},
		},
	}
}
