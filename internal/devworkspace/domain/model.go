// Package domain is the developer workspace: an authorized file tree, a local
// preview runtime, and a governed exit through a change request.
//
// CONDUIT-INVENTED: devworkspace.WorkspaceRecord
// Displaced-by: a published workspace object schema with its action and pod contract
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

const (
	// ValueTier marks a workspace coordinate this Hub composed.
	ValueTier = "CONDUIT_NATIVE"
	// OriginLocalRuntime is the only origin a preview log may claim.
	OriginLocalRuntime = "local-runtime"

	StatusStopped      = "stopped"
	StatusStarting     = "starting"
	StatusInitializing = "initializing"
	StatusRunning      = "running"
	StatusStopping     = "stopping"
	StatusIdle         = "idle"
	StatusFailed       = "failed"

	DefaultRetentionWindow = 7 * 24 * time.Hour

	MaxTreePage  = 50
	MaxFileBytes = 256 * 1024
	MaxLogBytes  = 64 * 1024
	MaxLogLine   = 1024

	SignatureUnsigned = "unsigned"
	SignatureFailed   = "failed"
	SignatureVerified = "verified"
)

var (
	ErrNotFound        = errors.New("workspace not found")
	ErrForbidden       = errors.New("forbidden")
	ErrUnauthorized    = errors.New("unauthorized")
	ErrInvalid         = errors.New("invalid workspace request")
	ErrBranchNotPushed = errors.New("branch not pushed; volume preservation recorded")
	ErrConflict        = errors.New("conflict")
	ErrDisabled        = errors.New("workspace support is not configured")
	ErrNoCluster       = errors.New("no cluster")
	ErrStale           = errors.New("stale revision")
	ErrSelfApproval    = errors.New("self approval")
	ErrPath            = errors.New("path is outside the workspace")
	ErrMissing         = errors.New("file not found")
)

// Workspace is one authorized working copy. ApprovalID is empty unless a
// separate reviewer recorded an approval through governance.
type Workspace struct {
	ID                string    `json:"id"`
	ProfileID         string    `json:"profileId"`
	EnvironmentID     string    `json:"environmentId"`
	Owner             string    `json:"owner"`
	Title             string    `json:"title"`
	Status            string    `json:"status"`
	UpstreamRepo      string    `json:"upstreamRepo,omitempty"`
	RepoURL           string    `json:"repoUrl,omitempty"`
	Branch            string    `json:"branch,omitempty"`
	BaseCommit        string    `json:"baseCommit,omitempty"`
	HeadCommit        string    `json:"headCommit,omitempty"`
	SourceDigest      string    `json:"sourceDigest"`
	ProposalRevision  string    `json:"proposalRevision"`
	ArtifactDigest    string    `json:"artifactDigest,omitempty"`
	ParentID          string    `json:"parentId,omitempty"`
	TemplateVersion   string    `json:"templateVersion,omitempty"`
	ToolVersions      string    `json:"toolVersions,omitempty"`
	ValidationOutcome string    `json:"validationOutcome,omitempty"`
	ApprovalID        string    `json:"approvalId,omitempty"`
	SignatureStatus   string    `json:"signatureStatus"`
	VolumePreserved   bool      `json:"volumePreserved,omitempty"`
	PreservedUntil    time.Time `json:"preservedUntil,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
	LastUsed          time.Time `json:"lastUsed"`
	ValueTier         string    `json:"valueTier"`
}

// Caller is the operator the HTTP adapter already authenticated.
type Caller struct {
	Username       string
	Teams          []string
	Groups         []string
	ServiceAccount bool
	OnBehalfOf     *Caller
}

// TreeEntry is one file or directory directly under a prefix.
type TreeEntry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Digest string `json:"digest,omitempty"`
	Size   int64  `json:"size"`
}

// TreePage is one page of a directory listing.
type TreePage struct {
	Entries    []TreeEntry `json:"entries"`
	NextCursor string      `json:"nextCursor,omitempty"`
	ValueTier  string      `json:"valueTier"`
}

// FileView is a bounded read. Content is omitted for binary and oversized files.
type FileView struct {
	Path         string `json:"path"`
	Revision     string `json:"revision"`
	Digest       string `json:"digest"`
	Content      string `json:"content,omitempty"`
	Binary       bool   `json:"binary"`
	TooLarge     bool   `json:"tooLarge"`
	Redacted     bool   `json:"redacted"`
	DownloadPath string `json:"downloadPath,omitempty"`
	ValueTier    string `json:"valueTier"`
}

// Hunk is one display block of a generated diff. It is untrusted text.
type Hunk struct {
	Header string   `json:"header"`
	Lines  []string `json:"lines"`
}

// DiffFile is one path in a proposal diff.
type DiffFile struct {
	Path   string `json:"path"`
	Change string `json:"change"`
	Hunks  []Hunk `json:"hunks"`
}

// Diff is the proposal already stored for a workspace. Current is false when
// the requested revision is not the workspace's proposal revision.
type Diff struct {
	WorkspaceID      string     `json:"workspaceId"`
	SourceDigest     string     `json:"sourceDigest"`
	ProposalRevision string     `json:"proposalRevision"`
	ArtifactDigest   string     `json:"artifactDigest"`
	Current          bool       `json:"current"`
	Files            []DiffFile `json:"files"`
	ValueTier        string     `json:"valueTier"`
}

// Runtime is a local preview process. It is not a Plan and not a cluster lease.
type Runtime struct {
	ID               string `json:"id"`
	WorkspaceID      string `json:"workspaceId"`
	Owner            string `json:"owner"`
	Status           string `json:"status"`
	ProposalRevision string `json:"proposalRevision,omitempty"`
	ArtifactDigest   string `json:"artifactDigest,omitempty"`
	Failure          string `json:"failure,omitempty"`
	Origin           string `json:"origin"`
}

// LogPage is a bounded page of local preview output.
type LogPage struct {
	Lines      []string `json:"lines"`
	NextCursor string   `json:"nextCursor,omitempty"`
	Origin     string   `json:"origin"`
	Status     string   `json:"status"`
	ValueTier  string   `json:"valueTier"`
}

// Download is an authorized snapshot. SignatureValid is true only when the
// stored status is verified; unsigned and failed stay false.
type Download struct {
	WorkspaceID     string     `json:"workspaceId"`
	SignatureStatus string     `json:"signatureStatus"`
	SignatureValid  bool       `json:"signatureValid"`
	Files           []FileView `json:"files"`
	ValueTier       string     `json:"valueTier"`
}

// Submission is the draft a workspace may hand to the change-request port.
type Submission struct {
	WorkspaceID   string
	EnvironmentID string
	Revision      string
	SourceDigest  string
	Title         string
	Description   string
}

// Receipt is the stored change request. The workspace does not approve it.
type Receipt struct {
	ChangeRequestID string
	Status          string
}

// Digest is the content identity shown next to a file.
func Digest(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// SignatureValid reports whether a stored status may be treated as verified.
func SignatureValid(status string) bool {
	return status == SignatureVerified
}

// Touch stamps the clock the service was given.
func Touch(ws Workspace, now time.Time) Workspace {
	if ws.ValueTier == "" {
		ws.ValueTier = ValueTier
	}
	if ws.SignatureStatus == "" {
		ws.SignatureStatus = SignatureUnsigned
	}
	if ws.Status == "" {
		ws.Status = StatusStopped
	}
	ws.UpdatedAt = now
	if ws.CreatedAt.IsZero() {
		ws.CreatedAt = now
	}
	if ws.LastUsed.IsZero() {
		ws.LastUsed = now
	}
	return ws
}
