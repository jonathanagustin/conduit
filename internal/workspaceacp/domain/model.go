// Package domain owns the workspace ACP governance invariants, risk tiers,
// session scoping, and correlation identifiers. It imports the Go standard
// library only.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"
)

// CONDUIT-INVENTED: workspace.GovernedACPBridge
// Displaced-by: a published or authorized sanitized workspace coding-agent ACP transport, governed permission mapping, and console activity projection contract.
const (
	// ProtocolVersion is the pinned ACP protocol version checked during initialize.
	ProtocolVersion = 1

	// ValueTier marks Hub-native workspace ACP records.
	ValueTier = "CONDUIT_NATIVE"
)

var (
	ErrUnauthorized      = errors.New("unauthorized")
	ErrForbidden         = errors.New("forbidden")
	ErrNotFound          = errors.New("workspace or session not found")
	ErrInvalid           = errors.New("invalid workspace ACP request")
	ErrConflict          = errors.New("conflicting workspace ACP state")
	ErrDisabled          = errors.New("workspace ACP bridge is not configured")
	ErrProtocolVersion   = errors.New("unsupported ACP protocol version")
	ErrAuditUnavailable  = errors.New("workspace ACP audit log is unavailable")
	ErrPermissionPending = errors.New("governed permission is still pending approval")
	ErrPermissionDenied  = errors.New("governed permission was denied")
	ErrSelfApproval      = errors.New("high-risk workspace change requires a separate human approver")
)

// Caller is the resolved operator identity from OIDC or an explicit dev header.
type Caller struct {
	Username       string
	Teams          []string
	Groups         []string
	ServiceAccount bool
	OnBehalfOf     *Caller
}

// WorkspaceSnapshot is the immutable workspace fact set supplied across the port.
type WorkspaceSnapshot struct {
	ID               string
	Owner            string
	EnvironmentID    string
	ProposalRevision string
	SourceDigest     string
	Status           string
}

// Scope binds an ACP session to exactly one human user and one workspace branch.
type Scope struct {
	Owner         string `json:"owner"`
	WorkspaceID   string `json:"workspaceId"`
	EnvironmentID string `json:"environmentId"`
	Branch        string `json:"branch"`
}

var safeBranchPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,128}$`)

// DefaultBranch returns the canonical workspace branch for an owner and workspace.
func DefaultBranch(owner, workspaceID string) string {
	return "workspace/" + strings.TrimSpace(owner) + "/" + strings.TrimSpace(workspaceID)
}

// ResolveBranch validates or derives the workspace branch bound to one owner and workspace.
// A caller cannot bind a branch outside their own workspace/<owner>/<workspaceID> namespace.
func ResolveBranch(owner, workspaceID, requested string) (string, error) {
	owner = strings.TrimSpace(owner)
	workspaceID = strings.TrimSpace(workspaceID)
	if owner == "" || workspaceID == "" {
		return "", ErrInvalid
	}
	canonical := DefaultBranch(owner, workspaceID)
	trimmed := strings.TrimSpace(requested)
	if trimmed == "" {
		return canonical, nil
	}
	if !safeBranchPattern.MatchString(trimmed) ||
		strings.Contains(trimmed, "..") ||
		strings.HasPrefix(trimmed, "/") ||
		strings.HasSuffix(trimmed, "/") ||
		strings.Contains(trimmed, "//") {
		return "", ErrInvalid
	}
	allowedPrefix := canonical + "/"
	if trimmed != canonical && !strings.HasPrefix(trimmed, allowedPrefix) {
		return "", ErrForbidden
	}
	return trimmed, nil
}

// RiskTier classifies an ACP permission request into a governed Hub action.
type RiskTier string

const (
	RiskLowConfirm        RiskTier = "low_confirm"
	RiskHighChangeRequest RiskTier = "high_change_request"
)

// StricterRisk returns the stricter of two risk classifications so a bridge or
// agent label can never downgrade the Hub's own risk evaluation.
func StricterRisk(a, b RiskTier) RiskTier {
	if a == RiskHighChangeRequest || b == RiskHighChangeRequest {
		return RiskHighChangeRequest
	}
	return RiskLowConfirm
}

// ActionKind names the audited category of an editor coding-agent operation.
type ActionKind string

const (
	ActionToolCall  ActionKind = "tool_call"
	ActionFileWrite ActionKind = "file_write"
	ActionCommand   ActionKind = "command"
)

// PermissionStatus tracks the lifecycle of a governed ACP permission gate.
type PermissionStatus string

const (
	StatusPendingConfirm       PermissionStatus = "PENDING_CONFIRM"
	StatusPendingChangeRequest PermissionStatus = "PENDING_CHANGE_REQUEST"
	StatusApproved             PermissionStatus = "APPROVED"
	StatusDenied               PermissionStatus = "DENIED"
	StatusCancelled            PermissionStatus = "CANCELLED"
)

// ApprovalOption is one sanitized ACP permission choice.
type ApprovalOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// PermissionGate is the Hub's governed record for one ACP permission request.
type PermissionGate struct {
	RequestID       string           `json:"requestId"`
	BridgeRequestID string           `json:"-"`
	SessionID       string           `json:"sessionId"`
	ThreadID        string           `json:"threadId"`
	RunID           string           `json:"runId"`
	ToolCallID      string           `json:"toolCallId"`
	WorkspaceID     string           `json:"workspaceId"`
	Owner           string           `json:"owner"`
	Branch          string           `json:"branch"`
	ActionKind      ActionKind       `json:"kind"`
	Title           string           `json:"title"`
	Path            string           `json:"path,omitempty"`
	Command         string           `json:"command,omitempty"`
	RiskTier        RiskTier         `json:"riskTier"`
	Status          PermissionStatus `json:"status"`
	ChangeRequestID string           `json:"changeRequestId,omitempty"`
	Options         []ApprovalOption `json:"options"`
	CreatedAt       time.Time        `json:"createdAt"`
	UpdatedAt       time.Time        `json:"updatedAt"`
}

// Session is an active ACP session scoped to one user and one workspace branch.
type Session struct {
	SessionID     string    `json:"sessionId"`
	ThreadID      string    `json:"threadId"`
	WorkspaceID   string    `json:"workspaceId"`
	EnvironmentID string    `json:"environmentId"`
	Owner         string    `json:"owner"`
	Branch        string    `json:"branch"`
	ActiveRunID   string    `json:"activeRunId,omitempty"`
	ConfigOptions []any     `json:"configOptions,omitempty"`
	ValueTier     string    `json:"valueTier"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// AuditEntry carries mandatory session, run, and thread correlation identifiers.
type AuditEntry struct {
	Subject         string
	Action          string
	Resource        string
	Decision        string
	Reason          string
	WorkspaceID     string
	Branch          string
	SessionID       string
	RunID           string
	ThreadID        string
	ToolCallID      string
	Kind            string
	Path            string
	Command         string
	RiskTier        string
	ChangeRequestID string
	Digest          string
	At              time.Time
}

// SanitizeOptions strips or downgrades standing allow_always options so every
// governed approval applies to a single operation.
func SanitizeOptions(options []ApprovalOption) []ApprovalOption {
	hasAllowOnce := false
	for _, opt := range options {
		if opt.Kind == "allow_once" {
			hasAllowOnce = true
			break
		}
	}
	out := make([]ApprovalOption, 0, len(options))
	for _, opt := range options {
		if opt.Kind == "allow_always" {
			if hasAllowOnce {
				continue
			}
			out = append(out, ApprovalOption{
				OptionID: opt.OptionID,
				Name:     strings.ReplaceAll(strings.ReplaceAll(opt.Name, "Always", "Once"), "always", "once"),
				Kind:     "allow_once",
			})
			continue
		}
		out = append(out, opt)
	}
	return out
}

var (
	highRiskPathPattern = regexp.MustCompile(`(?i)(?:^|/)(?:\.\.(?:/|$)|\.github(?:/|$)|deploy(?:/|$)|\.env(?:\.|$)|secrets?(?:/|\.|$)|credentials?(?:/|\.|$))`)
	highRiskCmdPattern  = regexp.MustCompile(`(?i)\b(?:git\s+push|gh\s+(?:pr|release|repo)|deploy|publish|helm\b|kubectl\b|terraform\b|curl\b|wget\b|ssh\b|scp\b|rsync\b|nc\b|ncat\b|sudo\b|rm\s+-[a-z]*r[a-z]*f|docker\s+push|oras\s+push)\b`)
)

// ClassifyPermission evaluates the governed risk tier for an ACP permission request.
// Low-risk in-workspace reads, edits, and local test commands require owner Confirm;
// path escapes, deployment/workflow/credential edits, and network or mutating infra
// commands open a Change Request requiring separate human approval.
func ClassifyPermission(kind, name, title, path, command, rawArgs string) RiskTier {
	lk := strings.ToLower(strings.TrimSpace(kind))
	ln := strings.ToLower(strings.TrimSpace(name))
	if lk == "deploy" || lk == "publish" || lk == "external" || ln == "deploy" || ln == "publish" {
		return RiskHighChangeRequest
	}
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath != "" && (strings.HasPrefix(trimmedPath, "/") || highRiskPathPattern.MatchString(trimmedPath)) {
		return RiskHighChangeRequest
	}
	combined := strings.Join([]string{title, command, trimmedPath, rawArgs}, "\n")
	if highRiskCmdPattern.MatchString(combined) || highRiskPathPattern.MatchString(combined) {
		return RiskHighChangeRequest
	}
	return RiskLowConfirm
}

// DigestBytes computes a sha256:<hex> digest for audit and state correlation.
func DigestBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
