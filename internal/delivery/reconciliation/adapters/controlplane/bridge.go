// Package controlplaneadapter is the temporary anti-corruption layer between
// Conduit's reconciliation context and the legacy control-plane facade. No
// reconciliation domain or application package imports the facade.
package controlplaneadapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/domain"
	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/ports"
)

const (
	planActionReconcile = "reconcile"
	planActionUninstall = "uninstall"
)

type InputStore interface {
	UpsertDesired(context.Context, domain.DesiredSnapshot) error
	UpsertObservation(context.Context, domain.Observation) error
	CurrentDesired(context.Context, domain.Key) (domain.DesiredSnapshot, bool, error)
}

// Repository observes only the two legacy persistence operations that define
// reconciliation inputs. Embedding preserves the rest of the compatibility
// surface while extraction continues.
type Repository struct {
	controlplane.Repository
	inputs InputStore
}

var _ controlplane.Repository = (*Repository)(nil)
var _ controlplane.TransactionalRepository = (*Repository)(nil)

func NewRepository(repository controlplane.Repository, inputs InputStore) (*Repository, error) {
	if repository == nil || inputs == nil {
		return nil, errors.New("control-plane repository and reconciliation input store are required")
	}
	return &Repository{Repository: repository, inputs: inputs}, nil
}

// Transaction preserves the production repository's atomic governance
// capability. The callback receives the underlying transactional view because
// reconciliation notifications are repaired from committed plans/reports by
// Sync; publishing them before the outer commit would be incorrect.
func (repository *Repository) Transaction(
	ctx context.Context, operation func(controlplane.Repository) error,
) error {
	transactional, ok := repository.Repository.(controlplane.TransactionalRepository)
	if !ok {
		return errors.New("wrapped control-plane repository is not transactional")
	}
	return transactional.Transaction(ctx, operation)
}

func (repository *Repository) CreatePlan(
	ctx context.Context, record controlplane.PlanRecord,
) (controlplane.PlanRecord, error) {
	if _, err := desiredSnapshots(record); err != nil {
		return controlplane.PlanRecord{}, err
	}
	created, err := repository.Repository.CreatePlan(ctx, record)
	if err != nil {
		return controlplane.PlanRecord{}, err
	}
	if err := repository.ingestPlan(ctx, created); err != nil {
		return controlplane.PlanRecord{}, fmt.Errorf("persist plan reconciliation inputs: %w", err)
	}
	return created, nil
}

func (repository *Repository) PutEnvironmentReport(
	ctx context.Context, report controlplane.EnvironmentReport,
) (controlplane.EnvironmentReport, error) {
	persisted, err := repository.Repository.PutEnvironmentReport(ctx, report)
	if err != nil {
		return controlplane.EnvironmentReport{}, err
	}
	if err := repository.ingestReport(ctx, persisted); err != nil {
		return controlplane.EnvironmentReport{}, fmt.Errorf("persist report reconciliation inputs: %w", err)
	}
	return persisted, nil
}

// Sync is the durable repair boundary for the small cross-context dual-write
// window. It derives notifications only from committed plans and reports and is
// safe to run at startup and periodically on every Hub replica.
func (repository *Repository) Sync(ctx context.Context) error {
	plans, err := repository.Repository.ListPlans(ctx, "")
	if err != nil {
		return fmt.Errorf("list plans for reconciliation sync: %w", err)
	}
	sort.Slice(plans, func(left, right int) bool {
		if plans[left].CreatedAt.Equal(plans[right].CreatedAt) {
			return plans[left].ID < plans[right].ID
		}
		return plans[left].CreatedAt.Before(plans[right].CreatedAt)
	})
	for _, record := range plans {
		if err := repository.ingestPlan(ctx, record); err != nil {
			return fmt.Errorf("sync plan %s: %w", record.ID, err)
		}
	}
	reports, err := repository.Repository.ListEnvironmentReports(ctx)
	if err != nil {
		return fmt.Errorf("list reports for reconciliation sync: %w", err)
	}
	sort.Slice(reports, func(left, right int) bool {
		if reports[left].ObservedAt.Equal(reports[right].ObservedAt) {
			return reports[left].Environment < reports[right].Environment
		}
		return reports[left].ObservedAt.Before(reports[right].ObservedAt)
	})
	for _, report := range reports {
		if err := repository.ingestReport(ctx, report); err != nil {
			return fmt.Errorf("sync report for %s: %w", report.Environment, err)
		}
	}
	return nil
}

func (repository *Repository) ingestPlan(ctx context.Context, record controlplane.PlanRecord) error {
	snapshots, err := desiredSnapshots(record)
	if err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		if err := repository.inputs.UpsertDesired(ctx, snapshot); err != nil {
			return err
		}
	}
	return nil
}

func (repository *Repository) ingestReport(ctx context.Context, report controlplane.EnvironmentReport) error {
	if strings.TrimSpace(report.PlanID) == "" {
		// Reports without an exact Plan identity remain visible through the
		// compatibility API but cannot assert a desired generation.
		return nil
	}
	record, err := repository.Repository.GetPlan(ctx, report.PlanID)
	if errors.Is(err, controlplane.ErrNotFound) {
		// Imported/relayed evidence may refer to a Plan this Hub does not own. It
		// must not be relabeled as a local generation.
		return nil
	}
	if err != nil {
		return err
	}
	if report.Environment != record.Plan.Environment {
		return errors.New("reported Environment differs from its exact Plan")
	}
	steps := make(map[string]controlplane.PlanStep, len(record.Plan.Steps))
	for _, step := range record.Plan.Steps {
		steps[step.InstallationID] = step
	}
	for _, entity := range report.Entities {
		step, found := steps[entity.InstallationID]
		if !found || step.Action == controlplane.PlanActionUnmanage {
			continue
		}
		observedAt := entity.ObservedAt
		if observedAt.IsZero() {
			observedAt = report.ObservedAt
		}
		observation := domain.Observation{
			Key:        domain.Key{Environment: report.Environment, Installation: entity.InstallationID},
			Generation: record.Plan.Generation, TargetDigest: targetDigest(targetProjection{
				Product: entity.Product, Version: entity.Version, ReleaseDigest: entity.ReleaseDigest,
				Namespace: entity.Namespace, ConfigDigest: entity.ConfigDigest, Lifecycle: entity.Lifecycle,
			}),
			Complete:   reportedComplete(step, entity),
			ObservedAt: observedAt.UTC(),
		}
		if err := repository.inputs.UpsertObservation(ctx, observation); err != nil {
			return err
		}
	}
	return nil
}

// Planner turns a proposal into the ordinary reviewed Plan lifecycle. It does
// not bypass approval, policy, or execution. The latest committed desired state
// is loaded at dispatch time so an unchanged installation never replays an old
// full-environment payload after another installation changes.
type Planner struct {
	repository controlplane.Repository
	inputs     InputStore
}

var _ ports.Planner = (*Planner)(nil)

func NewPlanner(repository controlplane.Repository, inputs InputStore) (*Planner, error) {
	if repository == nil || inputs == nil {
		return nil, errors.New("control-plane repository and desired reader are required")
	}
	return &Planner{repository: repository, inputs: inputs}, nil
}

func (planner *Planner) Propose(ctx context.Context, proposal domain.Proposal) (domain.PlanReceipt, error) {
	if err := planner.requireCurrent(ctx, proposal); err != nil {
		return domain.PlanReceipt{}, err
	}
	var recordedState controlplane.DesiredState
	if err := json.Unmarshal(proposal.Desired.Payload, &recordedState); err != nil {
		return domain.PlanReceipt{}, fmt.Errorf("decode proposed desired state: %w", err)
	}
	if recordedState.Environment.ID != proposal.Key.Environment ||
		recordedState.Environment.Generation != proposal.Desired.Generation {
		return domain.PlanReceipt{}, errors.New("proposal payload identity differs from its desired snapshot")
	}
	state, err := planner.latestDesiredState(ctx, proposal.Key.Environment)
	if err != nil {
		return domain.PlanReceipt{}, err
	}
	if err := planner.requireCurrent(ctx, proposal); err != nil {
		return domain.PlanReceipt{}, err
	}
	record, err := controlplane.NewLifecycle(planner.repository).CreatePlan(ctx, state)
	if err != nil {
		return domain.PlanReceipt{}, fmt.Errorf("create ordinary reconciliation plan: %w", err)
	}
	// CreatePlan is a get-or-create, and a Plan's ID is a pure hash of its
	// desired-state digest and its content. So when a rollout has already failed
	// or been cancelled, the next cycle builds the same bytes, resolves to the
	// same ID, and gets the dead record back -- with its terminal status intact.
	//
	// Reading that status is the whole point. Without this, the step digests
	// still match, so a receipt pointing at the failed Plan was returned as a
	// successful dispatch, CompleteProposal recorded it, and the loop repeated
	// forever: an endless stream of successes naming a Plan that failed. Nothing
	// escalated, because dispatch never reported an error and FailProposal was
	// never reached.
	//
	// This returns an ordinary error rather than ErrProposalSuperseded, which
	// would exhaust the proposal immediately. Two reasons. The snapshot has not
	// been superseded, and saying so would trade one false report for another.
	// And the retry path is the honest one here: it escalates by exhaustion
	// after MaximumAttempts, on the backoff the worker already applies, which is
	// what the loop is supposed to do when it cannot converge.
	//
	// Minting a fresh Plan instead is deliberately not done here. The ID is
	// content-derived, so a new record would need that derivation changed, and
	// the determinism it provides is relied on elsewhere. Reporting the failure
	// is the fix; choosing a re-plan policy is a separate decision.
	if record.Status == controlplane.PlanFailed || record.Status == controlplane.PlanCancelled {
		return domain.PlanReceipt{}, fmt.Errorf(
			"ordinary Plan %s for this desired state is %s", record.ID, record.Status,
		)
	}
	matched := false
	for _, step := range record.Plan.Steps {
		if step.InstallationID != proposal.Key.Installation {
			continue
		}
		digest, actionable, err := targetDigestForStep(step)
		if err != nil {
			return domain.PlanReceipt{}, err
		}
		if actionable && digest == proposal.Desired.TargetDigest {
			matched = true
			break
		}
	}
	if !matched {
		if err := planner.requireCurrent(ctx, proposal); err != nil {
			return domain.PlanReceipt{}, err
		}
		return domain.PlanReceipt{}, errors.New("ordinary Plan did not contain the proposed installation target")
	}
	if err := planner.requireCurrent(ctx, proposal); err != nil {
		return domain.PlanReceipt{}, err
	}
	return domain.PlanReceipt{ProposalID: proposal.ID, PlanID: record.ID, CreatedAt: record.CreatedAt.UTC()}, nil
}

func (planner *Planner) requireCurrent(ctx context.Context, proposal domain.Proposal) error {
	current, found, err := planner.inputs.CurrentDesired(ctx, proposal.Key)
	if err != nil {
		return err
	}
	if !found || current.Generation != proposal.Desired.Generation ||
		current.TargetDigest != proposal.Desired.TargetDigest || current.PayloadDigest != proposal.Desired.PayloadDigest {
		return fmt.Errorf("%w: desired snapshot changed", ports.ErrProposalSuperseded)
	}
	return nil
}

func (planner *Planner) latestDesiredState(ctx context.Context, environment string) (controlplane.DesiredState, error) {
	plans, err := planner.repository.ListPlans(ctx, environment)
	if err != nil {
		return controlplane.DesiredState{}, err
	}
	sort.Slice(plans, func(left, right int) bool {
		if plans[left].CreatedAt.Equal(plans[right].CreatedAt) {
			return plans[left].ID > plans[right].ID
		}
		return plans[left].CreatedAt.After(plans[right].CreatedAt)
	})
	for _, record := range plans {
		if record.DesiredState != nil {
			return *record.DesiredState, nil
		}
	}
	return controlplane.DesiredState{}, errors.New("no committed desired state exists for proposal environment")
}

type targetProjection struct {
	Product       string `json:"product"`
	Version       string `json:"version"`
	ReleaseDigest string `json:"releaseDigest"`
	Namespace     string `json:"namespace"`
	ConfigDigest  string `json:"configDigest"`
	Lifecycle     string `json:"lifecycle"`
}

func desiredSnapshots(record controlplane.PlanRecord) ([]domain.DesiredSnapshot, error) {
	if record.DesiredState == nil {
		return nil, nil
	}
	if record.Plan.Environment != record.DesiredState.Environment.ID ||
		record.Plan.Generation != record.DesiredState.Environment.Generation {
		return nil, errors.New("Plan and desired-state identities differ")
	}
	payload, err := json.Marshal(record.DesiredState)
	if err != nil {
		return nil, fmt.Errorf("encode desired-state reconciliation snapshot: %w", err)
	}
	payloadSum := sha256.Sum256(payload)
	payloadDigest := "sha256:" + hex.EncodeToString(payloadSum[:])
	seen := map[string]struct{}{}
	snapshots := make([]domain.DesiredSnapshot, 0, len(record.Plan.Steps))
	for _, step := range record.Plan.Steps {
		if _, duplicate := seen[step.InstallationID]; duplicate {
			return nil, fmt.Errorf("Plan contains duplicate installation %s", step.InstallationID)
		}
		seen[step.InstallationID] = struct{}{}
		digest, actionable, err := targetDigestForStep(step)
		if err != nil {
			return nil, err
		}
		if !actionable {
			continue
		}
		snapshots = append(snapshots, domain.DesiredSnapshot{
			Key:        domain.Key{Environment: record.Plan.Environment, Installation: step.InstallationID},
			Generation: record.Plan.Generation, TargetDigest: digest, PayloadDigest: payloadDigest,
			Payload: append(json.RawMessage(nil), payload...), RecordedAt: record.CreatedAt.UTC(),
		})
	}
	return snapshots, nil
}

func targetDigestForStep(step controlplane.PlanStep) (string, bool, error) {
	lifecycle := ""
	switch step.Action {
	case planActionReconcile:
		lifecycle = controlplane.LifecycleInstalled
	case planActionUninstall:
		lifecycle = controlplane.LifecycleUninstalled
	case controlplane.PlanActionUnmanage:
		return "", false, nil
	default:
		return "", false, fmt.Errorf("unsupported Plan action %q for installation %s", step.Action, step.InstallationID)
	}
	return targetDigest(targetProjection{
		Product: step.Product, Version: step.Version, ReleaseDigest: step.ReleaseDigest,
		Namespace: step.Namespace, ConfigDigest: step.ConfigDigest, Lifecycle: lifecycle,
	}), true, nil
}

func targetDigest(target targetProjection) string {
	encoded, err := json.Marshal(target)
	if err != nil {
		panic(err) // fixed, non-recursive value struct cannot fail JSON encoding
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func reportedComplete(step controlplane.PlanStep, entity controlplane.ReportedEntity) bool {
	if entity.Product != step.Product || entity.Version != step.Version || entity.ReleaseDigest != step.ReleaseDigest ||
		entity.Namespace != step.Namespace || entity.ConfigDigest != step.ConfigDigest || entity.ConfigStatus != controlplane.ConfigApplied {
		return false
	}
	if step.Action == planActionUninstall {
		return entity.Lifecycle == controlplane.LifecycleUninstalled && entity.Health == controlplane.HealthUnknown &&
			entity.RolloutStatus == controlplane.RolloutUpToDate
	}
	if step.Action != planActionReconcile || entity.Lifecycle != controlplane.LifecycleInstalled {
		return false
	}
	if step.HelmExecution.RolloutStrategy == controlplane.RolloutApplyChangesNoWait {
		return entity.Health == controlplane.HealthUnknown && entity.RolloutStatus == controlplane.RolloutInGracePeriod
	}
	return entity.Health == controlplane.HealthHealthy && entity.RolloutStatus == controlplane.RolloutUpToDate
}
