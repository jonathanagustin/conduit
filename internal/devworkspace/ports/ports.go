// Package ports is the developer workspace boundary.
package ports

import (
	"context"
	"time"

	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

// Catalog stores workspace records.
type Catalog interface {
	List(ctx context.Context) ([]domain.Workspace, error)
	Get(ctx context.Context, id string) (domain.Workspace, error)
	Save(ctx context.Context, workspace domain.Workspace) error
	Delete(ctx context.Context, id string) error
}

// Files is the authorized tree. Implementations confine every path.
type Files interface {
	List(ctx context.Context, id, prefix, cursor string, limit int) (domain.TreePage, error)
	Read(ctx context.Context, id, path, revision string) (domain.FileView, error)
	Write(ctx context.Context, id, path string, content []byte) (domain.FileView, error)
	Snapshot(ctx context.Context, id string) (map[string][]byte, error)
	Diff(ctx context.Context, id string) (domain.Diff, error)
	Copy(ctx context.Context, fromID, toID string) error
}

// Runtime starts and stops a local preview process.
type Runtime interface {
	Start(ctx context.Context, workspace domain.Workspace) (domain.Runtime, error)
	Stop(ctx context.Context, workspaceID string) (domain.Runtime, error)
	Status(ctx context.Context, workspaceID string) (domain.Runtime, error)
	Logs(ctx context.Context, workspaceID, cursor string, limit int) (domain.LogPage, error)
	Clear(ctx context.Context, workspaceID string) error
}

// Provisioner creates a least-privilege pod. A missing cluster returns
// domain.ErrNoCluster so a recorded adapter can answer instead.
type Provisioner interface {
	Create(ctx context.Context, workspace domain.Workspace) error
	Delete(ctx context.Context, workspaceID string) error
}

// ChangeSubmitter opens a change request through the existing governance
// command. Workspace submission does not add a proposal-submit command.
// This port is the only seam: a later adapter can replace the call without
// changing workspace actions. Submit must not approve, deploy, or publish.
type ChangeSubmitter interface {
	Submit(ctx context.Context, caller domain.Caller, draft domain.Submission) (domain.Receipt, error)
}

// Auditor records an authorization decision on the existing hash chain.
type Auditor interface {
	Record(ctx context.Context, subject, action, resource, decision, reason string) error
}

// BranchPreserver commits dirty /repo changes to a preservation commit and pushes
// the workspace branch before idle compute is reclaimed, or marks the workspace
// volume preserved for the retention window when push fails.
type BranchPreserver interface {
	PreserveOnIdle(ctx context.Context, workspace domain.Workspace, now time.Time) (domain.Workspace, error)
	ExpirePreservedVolumes(ctx context.Context, now time.Time, audit VolumeExpiryAudit) ([]domain.Workspace, error)
}

// VolumeExpiryAudit durably records expiry intent and the observed removal result.
// Implementations must not re-enter the workspace store from these callbacks.
type VolumeExpiryAudit interface {
	BeforeExpire(ctx context.Context, workspace domain.Workspace) error
	AfterExpire(ctx context.Context, workspace domain.Workspace, removalErr error) error
}
