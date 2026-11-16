// Package ports defines the narrow inward-facing interfaces for Hub-governed
// workspace ACP sessions, bridge execution, change governance, and auditing.
package ports

import (
	"context"

	"github.com/jonathanagustin/conduit/internal/workspaceacp/domain"
)

// Workspaces reads workspace ownership/visibility and commits governed file edits.
type Workspaces interface {
	Observe(ctx context.Context, workspaceID string) (domain.WorkspaceSnapshot, error)
	CanInspect(ctx context.Context, caller domain.Caller, workspaceID string, visible func(string) bool) (domain.WorkspaceSnapshot, error)
	SnapshotFiles(ctx context.Context, caller domain.Caller, workspaceID string) (map[string][]byte, error)
	ApplyEdit(ctx context.Context, caller domain.Caller, workspaceID, path string, content []byte) (string, error)
}

// ChangeTicket reports the status of a high-risk governed Change Request.
type ChangeTicket struct {
	ChangeRequestID string
	Status          string
	ApprovedByOther bool
}

// ChangeGovernance opens and inspects mandatory-human-review Change Requests.
type ChangeGovernance interface {
	OpenHighRiskChange(ctx context.Context, caller domain.Caller, workspace domain.WorkspaceSnapshot, gate domain.PermissionGate) (ChangeTicket, error)
	CheckChange(ctx context.Context, changeRequestID, author string) (ChangeTicket, error)
}

// Auditor persists an authorization audit record with session, run, and thread IDs.
type Auditor interface {
	Record(ctx context.Context, entry domain.AuditEntry) error
}

// BridgePermission carries a permission request emitted by the ACP bridge.
type BridgePermission struct {
	RequestID string                  `json:"requestId"`
	ThreadID  string                  `json:"threadId"`
	RunID     string                  `json:"runId"`
	ToolCall  string                  `json:"toolCallId"`
	Title     string                  `json:"title"`
	Kind      string                  `json:"kind,omitempty"`
	RiskTier  string                  `json:"riskTier,omitempty"`
	Path      string                  `json:"path,omitempty"`
	Command   string                  `json:"command,omitempty"`
	Options   []domain.ApprovalOption `json:"options"`
}

// BridgeActivity carries one normalized tool call, file write, command, or message event.
type BridgeActivity struct {
	SessionID  string `json:"sessionId"`
	ThreadID   string `json:"threadId"`
	RunID      string `json:"runId"`
	ToolCallID string `json:"toolCallId"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Path       string `json:"path,omitempty"`
	Digest     string `json:"digest,omitempty"`
	Content    string `json:"content,omitempty"`
	Command    string `json:"command,omitempty"`
	RiskTier   string `json:"riskTier,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
	ArgsText   string `json:"argsText,omitempty"`
	ResultText string `json:"resultText,omitempty"`
	Text       string `json:"text,omitempty"`
}

// BridgeEvent is one frame emitted by the ACP bridge during session/prompt.
type BridgeEvent struct {
	Type       string            `json:"type"`
	Snapshot   map[string]any    `json:"snapshot,omitempty"`
	Permission *BridgePermission `json:"request,omitempty"`
	Activity   *BridgeActivity   `json:"activity,omitempty"`
	RawFrame   map[string]any    `json:"-"`
}

// Bridge executes ACP JSON-RPC methods for one scoped (user, workspace, branch) instance.
type Bridge interface {
	Initialize(ctx context.Context, protocolVersion int) (map[string]any, error)
	NewSession(ctx context.Context, threadID string) (sessionID string, configOptions []any, err error)
	SetConfigOption(ctx context.Context, sessionID, configID string, value any) ([]any, error)
	Prompt(ctx context.Context, rpcID any, sessionID, runID string, prompt []map[string]any, onEvent func(BridgeEvent) error) error
	SettlePermission(ctx context.Context, requestID, threadID, runID, optionID string, cancelled bool) error
	CancelSession(ctx context.Context, sessionID string) error
	CloseSession(ctx context.Context, sessionID string) error
	Close() error
}

// BridgePool provisions or reuses a confined ACP bridge per (owner, workspaceID, branch).
type BridgePool interface {
	Acquire(ctx context.Context, scope domain.Scope, files map[string][]byte) (Bridge, error)
}
