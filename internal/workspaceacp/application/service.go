// Package application implements Hub-governed ACP session lifecycle, single-user
// and single-branch confinement, governed permission mapping (Confirm vs Change
// Request), mandatory audit correlation, and console AG-UI mirroring.
package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/workspaceacp/agui"
	"github.com/jonathanagustin/conduit/internal/workspaceacp/domain"
	"github.com/jonathanagustin/conduit/internal/workspaceacp/ports"
)

type sessionRecord struct {
	session domain.Session
	bridge  ports.Bridge
}

type permissionRecord struct {
	gate   domain.PermissionGate
	bridge ports.Bridge
}

// Dependencies supplies the ports required by the workspace ACP service.
type Dependencies struct {
	Workspaces ports.Workspaces
	Bridges    ports.BridgePool
	Governance ports.ChangeGovernance
	Auditor    ports.Auditor
	Mirror     *agui.Mirror
	Now        func() time.Time
}

// Service governs ACP sessions between workspace editors and coding agents.
type Service struct {
	workspaces ports.Workspaces
	bridges    ports.BridgePool
	governance ports.ChangeGovernance
	auditor    ports.Auditor
	mirror     *agui.Mirror
	now        func() time.Time

	mu          sync.Mutex
	sessions    map[string]*sessionRecord
	permissions map[string]*permissionRecord
}

// New constructs a workspace ACP Service.
func New(deps Dependencies) *Service {
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	mirror := deps.Mirror
	if mirror == nil {
		mirror = agui.NewMirror(deps.Auditor, now)
	}
	return &Service{
		workspaces:  deps.Workspaces,
		bridges:     deps.Bridges,
		governance:  deps.Governance,
		auditor:     deps.Auditor,
		mirror:      mirror,
		now:         now,
		sessions:    map[string]*sessionRecord{},
		permissions: map[string]*permissionRecord{},
	}
}

// Mirror returns the AG-UI console mirror managed by this service.
func (s *Service) Mirror() *agui.Mirror { return s.mirror }

// Initialize validates the pinned ACP protocol version for an owned workspace and branch.
func (s *Service) Initialize(ctx context.Context, caller domain.Caller, workspaceID, requestedBranch string, protocolVersion int) (map[string]any, error) {
	scope, _, err := s.authorizeOwnerScope(ctx, caller, workspaceID, requestedBranch)
	if err != nil {
		return nil, err
	}
	if protocolVersion != domain.ProtocolVersion {
		_ = s.audit(ctx, domain.AuditEntry{
			Subject:     caller.Username,
			Action:      "workspace.acp.initialize",
			Resource:    "workspace/" + workspaceID,
			Decision:    "DENY",
			Reason:      fmt.Sprintf("unsupported ACP protocolVersion %d", protocolVersion),
			WorkspaceID: workspaceID,
			Branch:      scope.Branch,
		})
		return nil, domain.ErrProtocolVersion
	}
	if s.bridges == nil {
		return nil, domain.ErrDisabled
	}
	files, err := s.workspaces.SnapshotFiles(ctx, caller, workspaceID)
	if err != nil {
		return nil, err
	}
	bridge, err := s.bridges.Acquire(ctx, scope, files)
	if err != nil {
		return nil, err
	}
	initResult, err := bridge.Initialize(ctx, domain.ProtocolVersion)
	if err != nil {
		return nil, err
	}
	initResult["protocolVersion"] = domain.ProtocolVersion
	initResult["_meta"] = map[string]any{
		"workspaceId": scope.WorkspaceID,
		"owner":       scope.Owner,
		"branch":      scope.Branch,
		"valueTier":   domain.ValueTier,
	}
	return initResult, nil
}

// NewSession opens a new ACP session bound to one operator and one workspace branch.
func (s *Service) NewSession(ctx context.Context, caller domain.Caller, workspaceID, requestedBranch, requestedThreadID string) (domain.Session, error) {
	scope, _, err := s.authorizeOwnerScope(ctx, caller, workspaceID, requestedBranch)
	if err != nil {
		return domain.Session{}, err
	}
	if s.bridges == nil {
		return domain.Session{}, domain.ErrDisabled
	}
	files, err := s.workspaces.SnapshotFiles(ctx, caller, workspaceID)
	if err != nil {
		return domain.Session{}, err
	}
	bridge, err := s.bridges.Acquire(ctx, scope, files)
	if err != nil {
		return domain.Session{}, err
	}

	threadID := strings.TrimSpace(requestedThreadID)
	if threadID == "" || !strings.HasPrefix(threadID, "wsp-acp-") {
		threadID = "wsp-acp-" + randomHex(8)
	}
	sessionID, configOptions, err := bridge.NewSession(ctx, threadID)
	if err != nil {
		return domain.Session{}, err
	}
	now := s.now()
	session := domain.Session{
		SessionID:     sessionID,
		ThreadID:      threadID,
		WorkspaceID:   scope.WorkspaceID,
		EnvironmentID: scope.EnvironmentID,
		Owner:         scope.Owner,
		Branch:        scope.Branch,
		ConfigOptions: configOptions,
		ValueTier:     domain.ValueTier,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.audit(ctx, domain.AuditEntry{
		Subject:     caller.Username,
		Action:      "workspace.acp.session_opened",
		Resource:    "workspace/" + scope.WorkspaceID,
		Decision:    "ALLOW",
		Reason:      "ACP session bound to user and workspace branch",
		WorkspaceID: scope.WorkspaceID,
		Branch:      scope.Branch,
		SessionID:   session.SessionID,
		RunID:       "session-init",
		ThreadID:    session.ThreadID,
	}); err != nil {
		_ = bridge.CloseSession(ctx, sessionID)
		return domain.Session{}, err
	}

	s.mu.Lock()
	s.sessions[session.SessionID] = &sessionRecord{session: session, bridge: bridge}
	s.mu.Unlock()

	s.mirror.RegisterThread(scope, session.SessionID, session.ThreadID)
	return session, nil
}

// SetConfigOption updates a session configuration option for the bound owner and branch.
func (s *Service) SetConfigOption(ctx context.Context, caller domain.Caller, workspaceID, requestedBranch, sessionID, configID string, value any) ([]any, error) {
	rec, err := s.requireOwnedSession(ctx, caller, workspaceID, requestedBranch, sessionID)
	if err != nil {
		return nil, err
	}
	updated, err := rec.bridge.SetConfigOption(ctx, sessionID, configID, value)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	rec.session.ConfigOptions = updated
	rec.session.UpdatedAt = s.now()
	s.mu.Unlock()
	return updated, nil
}

// CancelSession cancels any in-flight run and pending permissions for the bound session.
func (s *Service) CancelSession(ctx context.Context, caller domain.Caller, workspaceID, requestedBranch, sessionID string) error {
	rec, err := s.requireOwnedSession(ctx, caller, workspaceID, requestedBranch, sessionID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	activeRun := rec.session.ActiveRunID
	for _, perm := range s.permissions {
		if perm.gate.SessionID == sessionID && (perm.gate.Status == domain.StatusPendingConfirm || perm.gate.Status == domain.StatusPendingChangeRequest) {
			perm.gate.Status = domain.StatusCancelled
			perm.gate.UpdatedAt = s.now()
		}
	}
	s.mu.Unlock()
	if activeRun != "" {
		_ = s.audit(ctx, domain.AuditEntry{
			Subject:     caller.Username,
			Action:      "workspace.acp.session_cancelled",
			Resource:    "workspace/" + workspaceID,
			Decision:    "ALLOW",
			Reason:      "operator cancelled ACP run",
			WorkspaceID: workspaceID,
			Branch:      rec.session.Branch,
			SessionID:   rec.session.SessionID,
			RunID:       activeRun,
			ThreadID:    rec.session.ThreadID,
		})
	}
	return rec.bridge.CancelSession(ctx, sessionID)
}

// CloseSession closes an owned ACP session.
func (s *Service) CloseSession(ctx context.Context, caller domain.Caller, workspaceID, requestedBranch, sessionID string) error {
	rec, err := s.requireOwnedSession(ctx, caller, workspaceID, requestedBranch, sessionID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()
	if err := s.audit(ctx, domain.AuditEntry{
		Subject:     caller.Username,
		Action:      "workspace.acp.session_closed",
		Resource:    "workspace/" + workspaceID,
		Decision:    "ALLOW",
		Reason:      "ACP session closed",
		WorkspaceID: workspaceID,
		Branch:      rec.session.Branch,
		SessionID:   rec.session.SessionID,
		RunID:       "session-close",
		ThreadID:    rec.session.ThreadID,
	}); err != nil {
		return err
	}
	return rec.bridge.CloseSession(ctx, sessionID)
}

// Prompt streams one governed ACP turn, auditing every tool call, file write,
// command, and permission gate with sessionId, runId, and threadId, and
// mirroring activity onto the console AG-UI stream.
func (s *Service) Prompt(
	ctx context.Context,
	caller domain.Caller,
	workspaceID, requestedBranch, sessionID, requestedRunID string,
	rpcID any,
	prompt []map[string]any,
	writeSSE func(map[string]any) error,
) error {
	rec, err := s.requireOwnedSession(ctx, caller, workspaceID, requestedBranch, sessionID)
	if err != nil {
		return err
	}
	if len(prompt) == 0 {
		return domain.ErrInvalid
	}
	runID := strings.TrimSpace(requestedRunID)
	if runID == "" {
		runID = "run-" + randomHex(8)
	}

	s.mu.Lock()
	if rec.session.ActiveRunID != "" {
		s.mu.Unlock()
		return domain.ErrConflict
	}
	rec.session.ActiveRunID = runID
	rec.session.UpdatedAt = s.now()
	threadID := rec.session.ThreadID
	branch := rec.session.Branch
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		if rec.session.ActiveRunID == runID {
			rec.session.ActiveRunID = ""
		}
		s.mu.Unlock()
	}()

	workspace, err := s.workspaces.Observe(ctx, workspaceID)
	if err != nil {
		return err
	}

	userPromptText := extractPromptText(prompt)
	if err := s.mirror.StartRun(ctx, threadID, runID, userPromptText); err != nil {
		return err
	}

	var assistantText strings.Builder
	cancelledOutcome := false

	promptErr := rec.bridge.Prompt(ctx, rpcID, sessionID, runID, prompt, func(ev ports.BridgeEvent) error {
		switch ev.Type {
		case "permission_request":
			if ev.Permission == nil {
				return nil
			}
			return s.handlePermissionRequest(ctx, caller, workspace, rec, runID, *ev.Permission, writeSSE)

		case "activity":
			if ev.Activity == nil {
				return nil
			}
			return s.handleActivity(ctx, caller, workspaceID, branch, sessionID, threadID, runID, *ev.Activity, &assistantText, writeSSE)

		case "rpc_response":
			if resp, ok := ev.RawFrame["response"].(map[string]any); ok {
				if res, ok := resp["result"].(map[string]any); ok {
					if sr, _ := res["stopReason"].(string); sr == "cancelled" {
						cancelledOutcome = true
					}
				}
			}
			if assistantText.Len() > 0 {
				if err := s.mirror.RecordAssistantText(ctx, threadID, runID, assistantText.String()); err != nil {
					return err
				}
				assistantText.Reset()
			}
			if err := s.mirror.FinishRun(ctx, threadID, runID, cancelledOutcome); err != nil {
				return err
			}
			return writeSSE(ev.RawFrame)

		case "rpc_error":
			msg := "ACP turn failed"
			if resp, ok := ev.RawFrame["response"].(map[string]any); ok {
				if errObj, ok := resp["error"].(map[string]any); ok {
					if m, _ := errObj["message"].(string); m != "" {
						msg = m
					}
				}
			}
			s.mirror.FailRun(ctx, threadID, runID, "ACP_RUN_FAILED", msg)
			return writeSSE(ev.RawFrame)

		default:
			return writeSSE(ev.RawFrame)
		}
	})

	if promptErr != nil {
		s.mirror.FailRun(ctx, threadID, runID, "ACP_RUN_FAILED", promptErr.Error())
		return promptErr
	}
	return nil
}

func (s *Service) handlePermissionRequest(
	ctx context.Context,
	caller domain.Caller,
	workspace domain.WorkspaceSnapshot,
	rec *sessionRecord,
	runID string,
	perm ports.BridgePermission,
	writeSSE func(map[string]any) error,
) error {
	hubRisk := domain.ClassifyPermission(perm.Kind, "", perm.Title, perm.Path, perm.Command, "")
	bridgeRisk := domain.RiskLowConfirm
	if perm.RiskTier == string(domain.RiskHighChangeRequest) {
		bridgeRisk = domain.RiskHighChangeRequest
	}
	risk := domain.StricterRisk(hubRisk, bridgeRisk)
	options := domain.SanitizeOptions(perm.Options)

	kind := domain.ActionToolCall
	switch perm.Kind {
	case string(domain.ActionFileWrite), "edit", "write":
		kind = domain.ActionFileWrite
	case string(domain.ActionCommand), "execute", "terminal":
		kind = domain.ActionCommand
	}

	now := s.now()
	gate := domain.PermissionGate{
		RequestID:       "perm-" + randomHex(8),
		BridgeRequestID: perm.RequestID,
		SessionID:       rec.session.SessionID,
		ThreadID:        rec.session.ThreadID,
		RunID:           runID,
		ToolCallID:      perm.ToolCall,
		WorkspaceID:     workspace.ID,
		Owner:           workspace.Owner,
		Branch:          rec.session.Branch,
		ActionKind:      kind,
		Title:           perm.Title,
		Path:            perm.Path,
		Command:         perm.Command,
		RiskTier:        risk,
		Status:          domain.StatusPendingConfirm,
		Options:         options,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if risk == domain.RiskHighChangeRequest {
		if s.governance == nil {
			_ = rec.bridge.SettlePermission(ctx, perm.RequestID, perm.ThreadID, runID, "", true)
			return domain.ErrDisabled
		}
		ticket, err := s.governance.OpenHighRiskChange(ctx, caller, workspace, gate)
		if err != nil {
			_ = rec.bridge.SettlePermission(ctx, perm.RequestID, perm.ThreadID, runID, "", true)
			return err
		}
		gate.Status = domain.StatusPendingChangeRequest
		gate.ChangeRequestID = ticket.ChangeRequestID
	}

	if err := s.audit(ctx, domain.AuditEntry{
		Subject:         caller.Username,
		Action:          "workspace.acp.permission_requested",
		Resource:        "workspace/" + workspace.ID,
		Decision:        "ALLOW",
		Reason:          "ACP permission gated on governed decision: " + gate.Title,
		WorkspaceID:     workspace.ID,
		Branch:          rec.session.Branch,
		SessionID:       gate.SessionID,
		RunID:           gate.RunID,
		ThreadID:        gate.ThreadID,
		ToolCallID:      gate.ToolCallID,
		Kind:            string(gate.ActionKind),
		Path:            gate.Path,
		Command:         gate.Command,
		RiskTier:        string(gate.RiskTier),
		ChangeRequestID: gate.ChangeRequestID,
	}); err != nil {
		_ = rec.bridge.SettlePermission(ctx, perm.RequestID, perm.ThreadID, runID, "", true)
		return err
	}

	s.mu.Lock()
	s.permissions[gate.RequestID] = &permissionRecord{gate: gate, bridge: rec.bridge}
	s.mu.Unlock()

	if err := s.mirror.RecordPermission(ctx, gate); err != nil {
		return err
	}
	return writeSSE(map[string]any{
		"type":    "permission_request",
		"request": gate,
	})
}

func (s *Service) handleActivity(
	ctx context.Context,
	caller domain.Caller,
	workspaceID, branch, sessionID, threadID, runID string,
	act ports.BridgeActivity,
	assistantText *strings.Builder,
	writeSSE func(map[string]any) error,
) error {
	switch act.Kind {
	case "message":
		assistantText.WriteString(act.Text)
		return writeSSE(map[string]any{"type": "activity", "activity": act})

	case "permission":
		return nil

	case string(domain.ActionFileWrite):
		digest := act.Digest
		if act.Status == "completed" && act.Path != "" && act.Name == "fs/write_text_file" {
			writtenDigest, err := s.workspaces.ApplyEdit(ctx, caller, workspaceID, act.Path, []byte(act.Content))
			if err != nil {
				return err
			}
			digest = writtenDigest
		}
		if err := s.audit(ctx, domain.AuditEntry{
			Subject:     caller.Username,
			Action:      "workspace.acp.file_write",
			Resource:    "workspace/" + workspaceID,
			Decision:    "ALLOW",
			Reason:      fmt.Sprintf("ACP file_write %s (%s)", act.Path, act.Status),
			WorkspaceID: workspaceID,
			Branch:      branch,
			SessionID:   sessionID,
			RunID:       runID,
			ThreadID:    threadID,
			ToolCallID:  act.ToolCallID,
			Kind:        string(domain.ActionFileWrite),
			Path:        act.Path,
			Digest:      digest,
		}); err != nil {
			return err
		}
		if act.Status == "pending" || act.Status == "in_progress" {
			if err := s.mirror.RecordToolStart(ctx, threadID, runID, act.ToolCallID, act.Name, string(domain.ActionFileWrite), act.Path, "", act.ArgsText); err != nil {
				return err
			}
		} else {
			if err := s.mirror.RecordToolResult(ctx, threadID, runID, act.ToolCallID, act.Name, string(domain.ActionFileWrite), act.Status, act.Path, "", digest, act.ResultText); err != nil {
				return err
			}
		}
		sanitized := act
		sanitized.Content = ""
		sanitized.Digest = digest
		return writeSSE(map[string]any{"type": "activity", "activity": sanitized})

	case string(domain.ActionCommand):
		if err := s.audit(ctx, domain.AuditEntry{
			Subject:     caller.Username,
			Action:      "workspace.acp.command",
			Resource:    "workspace/" + workspaceID,
			Decision:    "ALLOW",
			Reason:      fmt.Sprintf("ACP command %s (%s)", act.Command, act.Status),
			WorkspaceID: workspaceID,
			Branch:      branch,
			SessionID:   sessionID,
			RunID:       runID,
			ThreadID:    threadID,
			ToolCallID:  act.ToolCallID,
			Kind:        string(domain.ActionCommand),
			Command:     act.Command,
			Digest:      domain.DigestBytes([]byte(act.Command + "\x00" + act.ResultText)),
		}); err != nil {
			return err
		}
		if act.Status == "pending" || act.Status == "in_progress" {
			if err := s.mirror.RecordToolStart(ctx, threadID, runID, act.ToolCallID, act.Name, string(domain.ActionCommand), "", act.Command, act.ArgsText); err != nil {
				return err
			}
		} else {
			if err := s.mirror.RecordToolResult(ctx, threadID, runID, act.ToolCallID, act.Name, string(domain.ActionCommand), act.Status, "", act.Command, "", act.ResultText); err != nil {
				return err
			}
		}
		return writeSSE(map[string]any{"type": "activity", "activity": act})

	default:
		if err := s.audit(ctx, domain.AuditEntry{
			Subject:     caller.Username,
			Action:      "workspace.acp.tool_call",
			Resource:    "workspace/" + workspaceID,
			Decision:    "ALLOW",
			Reason:      fmt.Sprintf("ACP tool_call %s (%s)", act.Name, act.Status),
			WorkspaceID: workspaceID,
			Branch:      branch,
			SessionID:   sessionID,
			RunID:       runID,
			ThreadID:    threadID,
			ToolCallID:  act.ToolCallID,
			Kind:        string(domain.ActionToolCall),
			Path:        act.Path,
			Digest:      act.Digest,
		}); err != nil {
			return err
		}
		if act.Status == "pending" || act.Status == "in_progress" {
			if err := s.mirror.RecordToolStart(ctx, threadID, runID, act.ToolCallID, act.Name, string(domain.ActionToolCall), act.Path, "", act.ArgsText); err != nil {
				return err
			}
		} else {
			if err := s.mirror.RecordToolResult(ctx, threadID, runID, act.ToolCallID, act.Name, string(domain.ActionToolCall), act.Status, act.Path, "", act.Digest, act.ResultText); err != nil {
				return err
			}
		}
		return writeSSE(map[string]any{"type": "activity", "activity": act})
	}
}

// DecidePermission settles a pending ACP permission gate. Low-risk actions admit
// on explicit owner Confirm; high-risk actions remain blocked until their Change
// Request has been approved by a separate human reviewer.
func (s *Service) DecidePermission(
	ctx context.Context,
	caller domain.Caller,
	workspaceID, requestedBranch, requestID, sessionID, runID, decision, optionID string,
) (domain.PermissionGate, error) {
	if _, _, err := s.authorizeOwnerScope(ctx, caller, workspaceID, requestedBranch); err != nil {
		return domain.PermissionGate{}, err
	}

	s.mu.Lock()
	record, ok := s.permissions[requestID]
	if !ok {
		s.mu.Unlock()
		return domain.PermissionGate{}, domain.ErrNotFound
	}
	gate := record.gate
	bridge := record.bridge
	s.mu.Unlock()

	if gate.WorkspaceID != workspaceID || gate.Owner != caller.Username {
		_ = s.audit(ctx, domain.AuditEntry{
			Subject:     caller.Username,
			Action:      "workspace.acp.permission_decided",
			Resource:    "workspace/" + workspaceID,
			Decision:    "DENY",
			Reason:      "cross-workspace or cross-owner permission settlement refused",
			WorkspaceID: workspaceID,
			Branch:      gate.Branch,
			SessionID:   gate.SessionID,
			RunID:       gate.RunID,
			ThreadID:    gate.ThreadID,
		})
		return domain.PermissionGate{}, domain.ErrForbidden
	}
	if requestedBranch != "" && requestedBranch != gate.Branch {
		return domain.PermissionGate{}, domain.ErrForbidden
	}
	if (sessionID != "" && sessionID != gate.SessionID) || (runID != "" && runID != gate.RunID) {
		return domain.PermissionGate{}, domain.ErrConflict
	}
	if gate.Status != domain.StatusPendingConfirm && gate.Status != domain.StatusPendingChangeRequest {
		return domain.PermissionGate{}, domain.ErrConflict
	}

	normalizedDecision := strings.ToLower(strings.TrimSpace(decision))
	if normalizedDecision == "deny" || normalizedDecision == "reject" || normalizedDecision == "cancel" {
		rejectOption := findOptionByKind(gate.Options, "reject")
		var settleErr error
		if rejectOption != "" {
			settleErr = bridge.SettlePermission(ctx, gate.BridgeRequestID, gate.ThreadID, gate.RunID, rejectOption, false)
		} else {
			settleErr = bridge.SettlePermission(ctx, gate.BridgeRequestID, gate.ThreadID, gate.RunID, "", true)
		}
		if settleErr != nil {
			return domain.PermissionGate{}, settleErr
		}
		gate.Status = domain.StatusDenied
		gate.UpdatedAt = s.now()
		s.mu.Lock()
		record.gate = gate
		s.mu.Unlock()
		if err := s.audit(ctx, domain.AuditEntry{
			Subject:         caller.Username,
			Action:          "workspace.acp.permission_decided",
			Resource:        "workspace/" + workspaceID,
			Decision:        "DENY",
			Reason:          "operator denied ACP permission",
			WorkspaceID:     workspaceID,
			Branch:          gate.Branch,
			SessionID:       gate.SessionID,
			RunID:           gate.RunID,
			ThreadID:        gate.ThreadID,
			ToolCallID:      gate.ToolCallID,
			Kind:            string(gate.ActionKind),
			RiskTier:        string(gate.RiskTier),
			ChangeRequestID: gate.ChangeRequestID,
		}); err != nil {
			return domain.PermissionGate{}, err
		}
		_ = s.mirror.RecordPermission(ctx, gate)
		return gate, nil
	}

	if normalizedDecision != "confirm" && normalizedDecision != "approve" {
		return domain.PermissionGate{}, domain.ErrInvalid
	}

	if gate.RiskTier == domain.RiskHighChangeRequest {
		if s.governance == nil || gate.ChangeRequestID == "" {
			return domain.PermissionGate{}, domain.ErrDisabled
		}
		ticket, err := s.governance.CheckChange(ctx, gate.ChangeRequestID, gate.Owner)
		if err != nil {
			return domain.PermissionGate{}, err
		}
		switch strings.ToUpper(ticket.Status) {
		case "REJECTED", "CANCELLED":
			rejectOpt := findOptionByKind(gate.Options, "reject")
			if rejectOpt != "" {
				_ = bridge.SettlePermission(ctx, gate.BridgeRequestID, gate.ThreadID, gate.RunID, rejectOpt, false)
			} else {
				_ = bridge.SettlePermission(ctx, gate.BridgeRequestID, gate.ThreadID, gate.RunID, "", true)
			}
			gate.Status = domain.StatusDenied
			gate.UpdatedAt = s.now()
			s.mu.Lock()
			record.gate = gate
			s.mu.Unlock()
			_ = s.audit(ctx, domain.AuditEntry{
				Subject:         caller.Username,
				Action:          "workspace.acp.permission_decided",
				Resource:        "workspace/" + workspaceID,
				Decision:        "DENY",
				Reason:          "high-risk Change Request was rejected or cancelled",
				WorkspaceID:     workspaceID,
				Branch:          gate.Branch,
				SessionID:       gate.SessionID,
				RunID:           gate.RunID,
				ThreadID:        gate.ThreadID,
				ToolCallID:      gate.ToolCallID,
				Kind:            string(gate.ActionKind),
				RiskTier:        string(gate.RiskTier),
				ChangeRequestID: gate.ChangeRequestID,
			})
			_ = s.mirror.RecordPermission(ctx, gate)
			return gate, domain.ErrPermissionDenied

		case "APPROVED":
			if !ticket.ApprovedByOther {
				_ = s.audit(ctx, domain.AuditEntry{
					Subject:         caller.Username,
					Action:          "workspace.acp.permission_decided",
					Resource:        "workspace/" + workspaceID,
					Decision:        "DENY",
					Reason:          "high-risk Change Request lacks a separate human approver",
					WorkspaceID:     workspaceID,
					Branch:          gate.Branch,
					SessionID:       gate.SessionID,
					RunID:           gate.RunID,
					ThreadID:        gate.ThreadID,
					ToolCallID:      gate.ToolCallID,
					Kind:            string(gate.ActionKind),
					RiskTier:        string(gate.RiskTier),
					ChangeRequestID: gate.ChangeRequestID,
				})
				return gate, domain.ErrSelfApproval
			}

		default:
			_ = s.audit(ctx, domain.AuditEntry{
				Subject:         caller.Username,
				Action:          "workspace.acp.permission_decided",
				Resource:        "workspace/" + workspaceID,
				Decision:        "DENY",
				Reason:          "high-risk Change Request is still pending approval",
				WorkspaceID:     workspaceID,
				Branch:          gate.Branch,
				SessionID:       gate.SessionID,
				RunID:           gate.RunID,
				ThreadID:        gate.ThreadID,
				ToolCallID:      gate.ToolCallID,
				Kind:            string(gate.ActionKind),
				RiskTier:        string(gate.RiskTier),
				ChangeRequestID: gate.ChangeRequestID,
			})
			return gate, domain.ErrPermissionPending
		}
	}

	chosenOption := strings.TrimSpace(optionID)
	if chosenOption == "" {
		chosenOption = findOptionByKind(gate.Options, "allow_once")
	}
	validAllow := false
	for _, opt := range gate.Options {
		if opt.OptionID == chosenOption && opt.Kind == "allow_once" {
			validAllow = true
			break
		}
	}
	if !validAllow {
		return domain.PermissionGate{}, domain.ErrInvalid
	}

	if err := s.audit(ctx, domain.AuditEntry{
		Subject:         caller.Username,
		Action:          "workspace.acp.permission_decided",
		Resource:        "workspace/" + workspaceID,
		Decision:        "ALLOW",
		Reason:          "governed ACP permission settled",
		WorkspaceID:     workspaceID,
		Branch:          gate.Branch,
		SessionID:       gate.SessionID,
		RunID:           gate.RunID,
		ThreadID:        gate.ThreadID,
		ToolCallID:      gate.ToolCallID,
		Kind:            string(gate.ActionKind),
		RiskTier:        string(gate.RiskTier),
		ChangeRequestID: gate.ChangeRequestID,
	}); err != nil {
		return domain.PermissionGate{}, err
	}

	if err := bridge.SettlePermission(ctx, gate.BridgeRequestID, gate.ThreadID, gate.RunID, chosenOption, false); err != nil {
		return domain.PermissionGate{}, err
	}

	gate.Status = domain.StatusApproved
	gate.UpdatedAt = s.now()
	s.mu.Lock()
	record.gate = gate
	s.mu.Unlock()
	_ = s.mirror.RecordPermission(ctx, gate)
	return gate, nil
}

// ListSessions lists active ACP sessions for an authorized workspace owner or environment reader.
func (s *Service) ListSessions(ctx context.Context, caller domain.Caller, workspaceID string, visible func(string) bool) ([]domain.Session, error) {
	if strings.TrimSpace(caller.Username) == "" {
		return nil, domain.ErrUnauthorized
	}
	if _, err := s.workspaces.CanInspect(ctx, caller, workspaceID, visible); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []domain.Session{}
	for _, rec := range s.sessions {
		if rec.session.WorkspaceID == workspaceID {
			out = append(out, rec.session)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })
	return out, nil
}

// StreamWorkspaceRun validates caller visibility for workspaceID and streams the
// mirrored console AG-UI exchange for threadID.
func (s *Service) StreamWorkspaceRun(
	ctx context.Context,
	caller domain.Caller,
	workspaceID, threadID, runID string,
	snapshotOnly bool,
	visible func(string) bool,
	writeFrame func([]byte) error,
) error {
	if strings.TrimSpace(caller.Username) == "" {
		return domain.ErrUnauthorized
	}
	if _, err := s.workspaces.CanInspect(ctx, caller, workspaceID, visible); err != nil {
		return err
	}
	scope, _, ok := s.mirror.ThreadScope(threadID)
	if !ok || scope.WorkspaceID != workspaceID {
		return domain.ErrNotFound
	}
	return s.mirror.Stream(ctx, caller.Username, threadID, runID, snapshotOnly, writeFrame)
}

// StreamThreadRun resolves the workspace for threadID and streams the mirrored
// console AG-UI exchange when the caller owns or can read that workspace's environment.
func (s *Service) StreamThreadRun(
	ctx context.Context,
	caller domain.Caller,
	threadID, runID string,
	snapshotOnly bool,
	visible func(string) bool,
	writeFrame func([]byte) error,
) error {
	if strings.TrimSpace(caller.Username) == "" {
		return domain.ErrUnauthorized
	}
	scope, _, ok := s.mirror.ThreadScope(threadID)
	if !ok {
		return domain.ErrNotFound
	}
	if _, err := s.workspaces.CanInspect(ctx, caller, scope.WorkspaceID, visible); err != nil {
		return err
	}
	return s.mirror.Stream(ctx, caller.Username, threadID, runID, snapshotOnly, writeFrame)
}

func (s *Service) authorizeOwnerScope(ctx context.Context, caller domain.Caller, workspaceID, requestedBranch string) (domain.Scope, domain.WorkspaceSnapshot, error) {
	if strings.TrimSpace(caller.Username) == "" {
		return domain.Scope{}, domain.WorkspaceSnapshot{}, domain.ErrUnauthorized
	}
	if caller.ServiceAccount {
		_ = s.audit(ctx, domain.AuditEntry{
			Subject:     caller.Username,
			Action:      "workspace.acp.authorize",
			Resource:    "workspace/" + workspaceID,
			Decision:    "DENY",
			Reason:      "service accounts cannot open interactive workspace ACP sessions",
			WorkspaceID: workspaceID,
		})
		return domain.Scope{}, domain.WorkspaceSnapshot{}, domain.ErrForbidden
	}
	workspace, err := s.workspaces.Observe(ctx, workspaceID)
	if err != nil {
		return domain.Scope{}, domain.WorkspaceSnapshot{}, err
	}
	if workspace.Owner != caller.Username {
		_ = s.audit(ctx, domain.AuditEntry{
			Subject:     caller.Username,
			Action:      "workspace.acp.authorize",
			Resource:    "workspace/" + workspaceID,
			Decision:    "DENY",
			Reason:      "caller is not the workspace owner",
			WorkspaceID: workspaceID,
		})
		return domain.Scope{}, domain.WorkspaceSnapshot{}, domain.ErrForbidden
	}
	branch, err := domain.ResolveBranch(workspace.Owner, workspace.ID, requestedBranch)
	if err != nil {
		_ = s.audit(ctx, domain.AuditEntry{
			Subject:     caller.Username,
			Action:      "workspace.acp.authorize",
			Resource:    "workspace/" + workspaceID,
			Decision:    "DENY",
			Reason:      "workspace branch outside owner scope refused",
			WorkspaceID: workspaceID,
			Branch:      requestedBranch,
		})
		return domain.Scope{}, domain.WorkspaceSnapshot{}, err
	}
	return domain.Scope{
		Owner:         workspace.Owner,
		WorkspaceID:   workspace.ID,
		EnvironmentID: workspace.EnvironmentID,
		Branch:        branch,
	}, workspace, nil
}

func (s *Service) requireOwnedSession(ctx context.Context, caller domain.Caller, workspaceID, requestedBranch, sessionID string) (*sessionRecord, error) {
	scope, _, err := s.authorizeOwnerScope(ctx, caller, workspaceID, requestedBranch)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	rec, ok := s.sessions[sessionID]
	s.mu.Unlock()
	if !ok {
		return nil, domain.ErrNotFound
	}
	if rec.session.WorkspaceID != scope.WorkspaceID || rec.session.Owner != scope.Owner {
		_ = s.audit(ctx, domain.AuditEntry{
			Subject:     caller.Username,
			Action:      "workspace.acp.session_access",
			Resource:    "workspace/" + workspaceID,
			Decision:    "DENY",
			Reason:      "ACP session belongs to another workspace or owner",
			WorkspaceID: workspaceID,
			Branch:      scope.Branch,
			SessionID:   sessionID,
			RunID:       "access-denied",
			ThreadID:    rec.session.ThreadID,
		})
		return nil, domain.ErrForbidden
	}
	if strings.TrimSpace(requestedBranch) != "" && rec.session.Branch != scope.Branch {
		_ = s.audit(ctx, domain.AuditEntry{
			Subject:     caller.Username,
			Action:      "workspace.acp.session_access",
			Resource:    "workspace/" + workspaceID,
			Decision:    "DENY",
			Reason:      "ACP session is bound to another workspace branch",
			WorkspaceID: workspaceID,
			Branch:      scope.Branch,
			SessionID:   sessionID,
			RunID:       "branch-mismatch",
			ThreadID:    rec.session.ThreadID,
		})
		return nil, domain.ErrForbidden
	}
	return rec, nil
}

func (s *Service) audit(ctx context.Context, entry domain.AuditEntry) error {
	if s.auditor == nil {
		return domain.ErrAuditUnavailable
	}
	if entry.At.IsZero() {
		entry.At = s.now()
	}
	if entry.SessionID == "" {
		entry.SessionID = "unbound"
	}
	if entry.RunID == "" {
		entry.RunID = "unbound"
	}
	if entry.ThreadID == "" {
		entry.ThreadID = "unbound"
	}
	if err := s.auditor.Record(ctx, entry); err != nil {
		if errors.Is(err, domain.ErrAuditUnavailable) {
			return err
		}
		return domain.ErrAuditUnavailable
	}
	return nil
}

func findOptionByKind(options []domain.ApprovalOption, prefix string) string {
	for _, opt := range options {
		if opt.Kind == prefix || strings.HasPrefix(opt.Kind, prefix) {
			return opt.OptionID
		}
	}
	return ""
}

func extractPromptText(prompt []map[string]any) string {
	var parts []string
	for _, item := range prompt {
		if item["type"] == "text" {
			if txt, ok := item["text"].(string); ok && txt != "" {
				parts = append(parts, txt)
			}
		}
	}
	raw := strings.Join(parts, "\n")
	if len(raw) > 2048 {
		raw = raw[:2048]
	}
	return raw
}

func randomHex(byteCount int) string {
	buf := make([]byte, byteCount)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

var _ = json.Marshal
