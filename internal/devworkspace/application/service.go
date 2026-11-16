// Package application is the developer workspace service.
package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"github.com/jonathanagustin/conduit/internal/devworkspace/ports"
)

const defaultIdle = 30 * time.Minute

// Service is the workspace application. It does not approve a change request,
// create a Plan, or publish an artifact.
type Service struct {
	records ports.Catalog
	files   ports.Files
	runtime ports.Runtime
	pods    ports.Provisioner
	submit  ports.ChangeSubmitter
	audit   ports.Auditor
	now     func() time.Time
	idle    time.Duration
	podsOn  bool
}

// Dependencies are the ports a deployment supplies. A nil runtime or pod
// provisioner fails closed on that action.
type Dependencies struct {
	Records     ports.Catalog
	Files       ports.Files
	Runtime     ports.Runtime
	Pods        ports.Provisioner
	Submitter   ports.ChangeSubmitter
	Auditor     ports.Auditor
	Now         func() time.Time
	Idle        time.Duration
	PodsEnabled bool
}

// New returns a service. Records and files are required.
func New(deps Dependencies) *Service {
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	idle := deps.Idle
	if idle <= 0 {
		idle = defaultIdle
	}
	return &Service{
		records: deps.Records, files: deps.Files, runtime: deps.Runtime, pods: deps.Pods,
		submit: deps.Submitter, audit: deps.Auditor, now: now, idle: idle, podsOn: deps.PodsEnabled,
	}
}

// List returns the workspaces the caller may see and reaps idle pods first.
func (service *Service) List(ctx context.Context, caller domain.Caller, visible func(string) bool) ([]domain.Workspace, error) {
	if err := service.requireCaller(caller); err != nil {
		return nil, err
	}
	if _, err := service.Reap(ctx, service.now()); err != nil {
		return nil, err
	}
	rows, err := service.records.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Workspace, 0, len(rows))
	for _, row := range rows {
		if !service.canSee(caller, row, visible) {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// Get returns one workspace the caller may see.
func (service *Service) Get(ctx context.Context, caller domain.Caller, id string, visible func(string) bool) (domain.Workspace, error) {
	if err := service.requireCaller(caller); err != nil {
		return domain.Workspace{}, err
	}
	row, err := service.records.Get(ctx, id)
	if err != nil {
		return domain.Workspace{}, err
	}
	if !service.canSee(caller, row, visible) {
		return domain.Workspace{}, domain.ErrNotFound
	}
	return row, nil
}

// Create stores a workspace. A pod is created only when that port is enabled.
func (service *Service) Create(ctx context.Context, caller domain.Caller, profile, environment, title string) (domain.Workspace, error) {
	if err := service.requireCaller(caller); err != nil {
		return domain.Workspace{}, err
	}
	if strings.TrimSpace(profile) == "" || strings.TrimSpace(environment) == "" {
		return domain.Workspace{}, domain.ErrInvalid
	}
	now := service.now()
	workspace := domain.Touch(domain.Workspace{
		ID: newID(), ProfileID: profile, EnvironmentID: environment, Owner: caller.Username,
		Title: strings.TrimSpace(title), Status: domain.StatusStopped, ProposalRevision: "rev-1",
	}, now)
	if workspace.Title == "" {
		workspace.Title = workspace.ID
	}
	if err := service.records.Save(ctx, workspace); err != nil {
		return domain.Workspace{}, err
	}
	if err := service.record(ctx, caller, "workspace.created", workspace.ID, "ALLOW", "workspace created"); err != nil {
		return domain.Workspace{}, err
	}
	if service.podsOn {
		if service.pods == nil {
			return domain.Workspace{}, domain.ErrDisabled
		}
		if err := service.pods.Create(ctx, workspace); err != nil && !errors.Is(err, domain.ErrNoCluster) {
			return domain.Workspace{}, err
		}
	}
	return workspace, nil
}

// StopRuntime stops the local preview. It does not approve a Plan.
func (service *Service) StopRuntime(ctx context.Context, caller domain.Caller, id string) (domain.Runtime, error) {
	workspace, err := service.owned(ctx, caller, id)
	if err != nil {
		return domain.Runtime{}, err
	}
	if service.runtime == nil {
		return domain.Runtime{}, domain.ErrDisabled
	}
	runtime, err := service.runtime.Stop(ctx, workspace.ID)
	if err != nil {
		return domain.Runtime{}, err
	}
	workspace.Status = domain.StatusStopped
	workspace.UpdatedAt = service.now()
	if err := service.records.Save(ctx, workspace); err != nil {
		return domain.Runtime{}, err
	}
	if err := service.record(ctx, caller, "workspace.runtime_stopped", workspace.ID, "ALLOW", "local runtime stopped"); err != nil {
		return domain.Runtime{}, err
	}
	runtime.Origin = domain.OriginLocalRuntime
	return runtime, nil
}

// Delete removes a stopped workspace. A running runtime must be stopped first.
func (service *Service) Delete(ctx context.Context, caller domain.Caller, id, confirmation string) error {
	workspace, err := service.owned(ctx, caller, id)
	if err != nil {
		return err
	}
	if confirmation != workspace.ID {
		return domain.ErrInvalid
	}
	if workspace.Status == domain.StatusRunning || workspace.Status == domain.StatusStarting {
		return domain.ErrConflict
	}
	if service.pods != nil {
		if err := service.pods.Delete(ctx, workspace.ID); err != nil && !errors.Is(err, domain.ErrNoCluster) && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
	}
	if err := service.records.Delete(ctx, workspace.ID); err != nil {
		return err
	}
	return service.record(ctx, caller, "workspace.deleted", workspace.ID, "ALLOW", "workspace deleted")
}

// Submit opens a change request. It refuses an approval flag and never calls
// an approve method.
func (service *Service) Submit(ctx context.Context, caller domain.Caller, id, title, description string, approve bool) (domain.Receipt, error) {
	workspace, err := service.owned(ctx, caller, id)
	if err != nil {
		return domain.Receipt{}, err
	}
	if approve {
		if err := service.record(ctx, caller, "workspace.submitted", workspace.ID, "DENY", "caller cannot approve this submission"); err != nil {
			return domain.Receipt{}, err
		}
		return domain.Receipt{}, domain.ErrSelfApproval
	}
	if service.submit == nil {
		return domain.Receipt{}, domain.ErrDisabled
	}
	receipt, err := service.submit.Submit(ctx, caller, domain.Submission{
		WorkspaceID: workspace.ID, EnvironmentID: workspace.EnvironmentID,
		Revision: workspace.ProposalRevision, SourceDigest: workspace.SourceDigest,
		Title: title, Description: description,
	})
	if err != nil {
		return domain.Receipt{}, err
	}
	if strings.EqualFold(receipt.Status, "APPROVED") {
		return domain.Receipt{}, domain.ErrSelfApproval
	}
	if err := service.record(ctx, caller, "workspace.submitted", workspace.ID, "ALLOW", "change request opened"); err != nil {
		return domain.Receipt{}, err
	}
	return receipt, nil
}

// Tree lists one directory page.
func (service *Service) Tree(ctx context.Context, caller domain.Caller, id, prefix, cursor string, limit int, visible func(string) bool) (domain.TreePage, error) {
	if _, err := service.Get(ctx, caller, id, visible); err != nil {
		return domain.TreePage{}, err
	}
	if _, err := domain.Confine(prefix); err != nil && prefix != "" {
		return domain.TreePage{}, err
	}
	return service.files.List(ctx, id, prefix, cursor, limit)
}

// File reads one authorized file.
func (service *Service) File(ctx context.Context, caller domain.Caller, id, path, revision string, visible func(string) bool) (domain.FileView, error) {
	if _, err := service.Get(ctx, caller, id, visible); err != nil {
		return domain.FileView{}, err
	}
	return service.files.Read(ctx, id, path, revision)
}

// Diff returns the stored proposal diff. It does not record an approval.
func (service *Service) Diff(ctx context.Context, caller domain.Caller, id, revision string, visible func(string) bool) (domain.Diff, error) {
	workspace, err := service.Get(ctx, caller, id, visible)
	if err != nil {
		return domain.Diff{}, err
	}
	if revision != "" && revision != workspace.ProposalRevision {
		return domain.Diff{}, domain.ErrStale
	}
	diff, err := service.files.Diff(ctx, id)
	if err != nil {
		return domain.Diff{}, err
	}
	diff.WorkspaceID = workspace.ID
	diff.SourceDigest = workspace.SourceDigest
	diff.ProposalRevision = workspace.ProposalRevision
	diff.ArtifactDigest = workspace.ArtifactDigest
	diff.Current = true
	diff.ValueTier = domain.ValueTier
	if diff.Files == nil {
		diff.Files = []domain.DiffFile{}
	}
	return diff, nil
}

// StartRuntime starts one local preview. A second start returns the same runtime.
func (service *Service) StartRuntime(ctx context.Context, caller domain.Caller, id string) (domain.Runtime, error) {
	workspace, err := service.owned(ctx, caller, id)
	if err != nil {
		return domain.Runtime{}, err
	}
	if service.runtime == nil {
		return domain.Runtime{}, domain.ErrDisabled
	}
	current, err := service.runtime.Status(ctx, workspace.ID)
	if err == nil && (current.Status == domain.StatusRunning || current.Status == domain.StatusStarting) {
		return current, nil
	}
	workspace.Status = domain.StatusStarting
	workspace.UpdatedAt = service.now()
	if err := service.records.Save(ctx, workspace); err != nil {
		return domain.Runtime{}, err
	}
	runtime, err := service.runtime.Start(ctx, workspace)
	if err != nil {
		workspace.Status = domain.StatusFailed
		_ = service.records.Save(ctx, workspace)
		_ = service.record(ctx, caller, "workspace.runtime_started", workspace.ID, "DENY", "local runtime failed to start")
		return domain.Runtime{}, err
	}
	workspace.Status = runtime.Status
	workspace.LastUsed = service.now()
	if err := service.records.Save(ctx, workspace); err != nil {
		return domain.Runtime{}, err
	}
	if err := service.record(ctx, caller, "workspace.runtime_started", workspace.ID, "ALLOW", "local runtime started"); err != nil {
		return domain.Runtime{}, err
	}
	runtime.Origin = domain.OriginLocalRuntime
	runtime.ProposalRevision = workspace.ProposalRevision
	runtime.ArtifactDigest = workspace.ArtifactDigest
	return runtime, nil
}

// RuntimeStatus is the local preview status.
func (service *Service) RuntimeStatus(ctx context.Context, caller domain.Caller, id string, visible func(string) bool) (domain.Runtime, error) {
	workspace, err := service.Get(ctx, caller, id, visible)
	if err != nil {
		return domain.Runtime{}, err
	}
	if service.runtime == nil {
		return domain.Runtime{WorkspaceID: workspace.ID, Status: domain.StatusStopped, Origin: domain.OriginLocalRuntime}, nil
	}
	runtime, err := service.runtime.Status(ctx, workspace.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Runtime{WorkspaceID: workspace.ID, Status: workspace.Status, Origin: domain.OriginLocalRuntime}, nil
	}
	if err != nil {
		return domain.Runtime{}, err
	}
	runtime.Origin = domain.OriginLocalRuntime
	return runtime, nil
}

// Logs returns bounded local output. inspect is the environment read grant.
func (service *Service) Logs(ctx context.Context, caller domain.Caller, id, cursor string, limit int, inspect bool) (domain.LogPage, error) {
	workspace, err := service.records.Get(ctx, id)
	if err != nil {
		return domain.LogPage{}, err
	}
	if caller.Username != workspace.Owner && !inspect {
		return domain.LogPage{}, domain.ErrForbidden
	}
	if service.runtime == nil {
		return domain.LogPage{Lines: []string{}, Origin: domain.OriginLocalRuntime, Status: domain.StatusStopped, ValueTier: domain.ValueTier}, nil
	}
	page, err := service.runtime.Logs(ctx, id, cursor, limit)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.LogPage{Lines: []string{}, Origin: domain.OriginLocalRuntime, Status: workspace.Status, ValueTier: domain.ValueTier}, nil
	}
	if err != nil {
		return domain.LogPage{}, err
	}
	page.Origin = domain.OriginLocalRuntime
	page.ValueTier = domain.ValueTier
	return page, nil
}

// ClearLogs drops the retained local buffer.
func (service *Service) ClearLogs(ctx context.Context, caller domain.Caller, id string) error {
	if _, err := service.owned(ctx, caller, id); err != nil {
		return err
	}
	if service.runtime == nil {
		return domain.ErrDisabled
	}
	return service.runtime.Clear(ctx, id)
}

// Fork copies a workspace and records the parent. The original is not saved again.
func (service *Service) Fork(ctx context.Context, caller domain.Caller, id, title string) (domain.Workspace, error) {
	parent, err := service.Get(ctx, caller, id, func(string) bool { return true })
	if err != nil {
		return domain.Workspace{}, err
	}
	if caller.Username != parent.Owner {
		return domain.Workspace{}, domain.ErrForbidden
	}
	now := service.now()
	child := parent
	child.ID = newID()
	child.ParentID = parent.ID
	child.Owner = caller.Username
	child.ApprovalID = ""
	child.ValidationOutcome = ""
	child.Status = domain.StatusStopped
	if strings.TrimSpace(title) != "" {
		child.Title = strings.TrimSpace(title)
	}
	child.CreatedAt = now
	child.UpdatedAt = now
	child.LastUsed = now
	if err := service.files.Copy(ctx, parent.ID, child.ID); err != nil {
		return domain.Workspace{}, err
	}
	if err := service.records.Save(ctx, child); err != nil {
		return domain.Workspace{}, err
	}
	if err := service.record(ctx, caller, "workspace.forked", child.ID, "ALLOW", "forked from "+parent.ID); err != nil {
		return domain.Workspace{}, err
	}
	return child, nil
}

// Records lists every workspace for a server-side catalog projection.
func (service *Service) Records(ctx context.Context) ([]domain.Workspace, error) {
	return service.records.List(ctx)
}

// Observe reads one workspace without an authorization decision. Callers that
// already authenticated use it to load action facts.
func (service *Service) Observe(ctx context.Context, id string) (domain.Workspace, error) {
	return service.records.Get(ctx, id)
}

// Upgrade starts a new review when the inputs changed and drops prior
// validation and approval. It does not create a Plan.
func (service *Service) Upgrade(ctx context.Context, caller domain.Caller, id, sourceDigest, templateVersion, toolVersions string) (domain.Workspace, error) {
	workspace, err := service.owned(ctx, caller, id)
	if err != nil {
		return domain.Workspace{}, err
	}
	if strings.TrimSpace(sourceDigest) == "" {
		return domain.Workspace{}, domain.ErrInvalid
	}
	if sourceDigest == workspace.SourceDigest && templateVersion == workspace.TemplateVersion && toolVersions == workspace.ToolVersions {
		return workspace, nil
	}
	workspace.SourceDigest = sourceDigest
	workspace.TemplateVersion = templateVersion
	workspace.ToolVersions = toolVersions
	workspace.ProposalRevision = "rev-" + shortHash(sourceDigest+"\x00"+templateVersion+"\x00"+toolVersions)
	workspace.ValidationOutcome = ""
	workspace.ApprovalID = ""
	workspace.ArtifactDigest = ""
	workspace.UpdatedAt = service.now()
	if err := service.records.Save(ctx, workspace); err != nil {
		return domain.Workspace{}, err
	}
	if err := service.record(ctx, caller, "workspace.upgrade_staged", workspace.ID, "ALLOW", "review refreshed"); err != nil {
		return domain.Workspace{}, err
	}
	return workspace, nil
}

// Download returns an authorized snapshot. SignatureValid follows the stored status.
func (service *Service) Download(ctx context.Context, caller domain.Caller, id string, visible func(string) bool) (domain.Download, error) {
	workspace, err := service.Get(ctx, caller, id, visible)
	if err != nil {
		return domain.Download{}, err
	}
	page, err := service.files.List(ctx, id, ".", "", domain.MaxTreePage)
	if err != nil {
		return domain.Download{}, err
	}
	files := []domain.FileView{}
	for _, entry := range page.Entries {
		if entry.Kind != "file" {
			continue
		}
		view, err := service.files.Read(ctx, id, entry.Path, "")
		if err != nil {
			return domain.Download{}, err
		}
		files = append(files, view)
	}
	status := workspace.SignatureStatus
	if status == "" {
		status = domain.SignatureUnsigned
	}
	return domain.Download{
		WorkspaceID: workspace.ID, SignatureStatus: status,
		SignatureValid: domain.SignatureValid(status), Files: files, ValueTier: domain.ValueTier,
	}, nil
}

// Reap stops idle compute, retaining the working copy and catalog for retry if
// any cleanup, audit, or persistence step fails. It never deletes workspace files.
func (service *Service) Reap(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := service.records.List(ctx)
	if err != nil {
		return nil, err
	}
	preserver, _ := service.records.(ports.BranchPreserver)
	if preserver == nil {
		preserver, _ = service.files.(ports.BranchPreserver)
	}
	reaped := []string{}
	for _, row := range rows {
		if row.Status != domain.StatusRunning && row.Status != domain.StatusStarting && row.Status != domain.StatusInitializing && row.Status != domain.StatusStopping {
			continue
		}
		if row.LastUsed.IsZero() || now.Sub(row.LastUsed) < service.idle {
			continue
		}
		if err := ctx.Err(); err != nil {
			return reaped, err
		}
		actor := domain.Caller{Username: "system:workspace-reaper"}
		reason := "idle timeout; owner=" + row.Owner + "; workspace files retained"
		if err := service.record(ctx, actor, "workspace.reap_requested", row.ID, "ALLOW", reason); err != nil {
			return reaped, err
		}
		fail := func(err error) ([]string, error) {
			auditErr := service.record(ctx, actor, "workspace.compute_reclaimed", row.ID, "DENY", reason+"; cleanup incomplete")
			return reaped, errors.Join(err, auditErr)
		}
		if service.runtime == nil || (service.podsOn && service.pods == nil) {
			return fail(domain.ErrDisabled)
		}
		if preserver != nil {
			preservedRow, preserveErr := preserver.PreserveOnIdle(ctx, row, now)
			row = preservedRow
			if preserveErr != nil {
				reason = reason + "; branch push failed; volume preserved until " + row.PreservedUntil.Format(time.RFC3339)
			} else if row.HeadCommit != "" {
				reason = reason + "; branch=" + row.Branch + "; head=" + row.HeadCommit
			}
		}
		if service.runtime != nil {
			runtime, err := service.runtime.Stop(ctx, row.ID)
			if err != nil && !errors.Is(err, domain.ErrNotFound) {
				return fail(err)
			}
			if err == nil && runtime.Status != domain.StatusStopped {
				return fail(domain.ErrConflict)
			}
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if service.pods != nil {
			if err := service.pods.Delete(ctx, row.ID); err != nil && !errors.Is(err, domain.ErrNotFound) {
				return fail(err)
			}
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		// This receipt describes completed compute cleanup, not a branch push or
		// volume deletion. Persist stopped only after that receipt is durable.
		if err := service.record(ctx, actor, "workspace.compute_reclaimed", row.ID, "ALLOW", reason); err != nil {
			return reaped, err
		}
		row.Status = domain.StatusStopped
		row.UpdatedAt = now
		if err := service.records.Save(ctx, row); err != nil {
			return reaped, err
		}
		reaped = append(reaped, row.ID)
	}
	if preserver != nil {
		expired, err := preserver.ExpirePreservedVolumes(ctx, now)
		if err != nil {
			return reaped, err
		}
		actor := domain.Caller{Username: "system:workspace-reaper"}
		for _, exp := range expired {
			_ = service.record(ctx, actor, "workspace.preserved_volume_expired", exp.ID, "ALLOW", "retention window elapsed; owner="+exp.Owner)
		}
	}
	return reaped, nil
}

func (service *Service) owned(ctx context.Context, caller domain.Caller, id string) (domain.Workspace, error) {
	if err := service.requireCaller(caller); err != nil {
		return domain.Workspace{}, err
	}
	workspace, err := service.records.Get(ctx, id)
	if err != nil {
		return domain.Workspace{}, err
	}
	if caller.Username != workspace.Owner {
		return domain.Workspace{}, domain.ErrForbidden
	}
	return workspace, nil
}

func (service *Service) canSee(caller domain.Caller, workspace domain.Workspace, visible func(string) bool) bool {
	if caller.Username == workspace.Owner {
		return true
	}
	return visible != nil && visible(workspace.EnvironmentID)
}

func (service *Service) requireCaller(caller domain.Caller) error {
	if strings.TrimSpace(caller.Username) == "" {
		return domain.ErrUnauthorized
	}
	return nil
}

func (service *Service) record(ctx context.Context, caller domain.Caller, action, resource, decision, reason string) error {
	if service.audit == nil {
		return errors.New("audit is not configured")
	}
	return service.audit.Record(ctx, caller.Username, action, "workspace/"+resource, decision, reason)
}

func newID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return "wsp_" + hex.EncodeToString(buf)
}

func shortHash(value string) string {
	return domain.Digest([]byte(value))[7:19]
}
