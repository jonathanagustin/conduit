package controlplane

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/execution"
	"sort"
	"strings"
	"time"
)

type PlanStatus string

const (
	PlanAwaitingApproval PlanStatus = "AWAITING_APPROVAL"
	PlanApproved         PlanStatus = "APPROVED"
	PlanLeased           PlanStatus = "LEASED"
	PlanRunning          PlanStatus = "RUNNING"
	PlanSucceeded        PlanStatus = "SUCCEEDED"
	PlanFailed           PlanStatus = "FAILED"
	PlanCancelled        PlanStatus = "CANCELLED"
	// PlanRecorded is a reviewed desired generation for an Environment this Hub
	// does not drive. It carries no executable work and cannot be approved or
	// leased, but remains the authoritative topology/settings record.
	PlanRecorded PlanStatus = "RECORDED"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("conflict")
	ErrNoPlanAvailable   = errors.New("no approved plan available")
	ErrInvalidTransition = errors.New("invalid plan transition")
	ErrLeaseRejected     = errors.New("lease rejected")
	ErrPlanCancelled     = errors.New("plan cancelled")
)

type Approval struct {
	Subject   string    `json:"subject"`
	Rationale string    `json:"rationale"`
	CreatedAt time.Time `json:"createdAt"`
}

type PlanCancellation struct {
	Subject   string    `json:"subject"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
}

type PlanRecord struct {
	ID                 string                   `json:"id"`
	DesiredStateDigest string                   `json:"desiredStateDigest"`
	DesiredState       *DesiredState            `json:"desiredState,omitempty"`
	ChangeRequestID    string                   `json:"changeRequestId,omitempty"`
	Plan               Plan                     `json:"plan"`
	Status             PlanStatus               `json:"status"`
	RequiredApprovals  int                      `json:"requiredApprovals"`
	Approvals          []Approval               `json:"approvals,omitempty"`
	Lease              *PlanLease               `json:"lease,omitempty"`
	Failure            string                   `json:"failure,omitempty"`
	FailedInstallation string                   `json:"failedInstallationId,omitempty"`
	Execution          *PlanExecution           `json:"execution,omitempty"`
	Cancellation       *PlanCancellation        `json:"cancellation,omitempty"`
	Command            *EntityCommand           `json:"command,omitempty"`
	Optimizer          *OptimizerPlanProvenance `json:"optimizer,omitempty"`
	CreatedAt          time.Time                `json:"createdAt"`
	UpdatedAt          time.Time                `json:"updatedAt"`
	Revision           uint64                   `json:"revision"`
}

// PlanExecution is the bounded, attributable task timeline reported by the
// Agent. Provenance (reviewed 2026-08-02):
// https://www.palantir.com/docs/apollo/core/plans-and-constraints/
type PlanExecution struct {
	Attempt    int                 `json:"attempt"`
	AgentID    string              `json:"agentId"`
	StartedAt  time.Time           `json:"startedAt"`
	FinishedAt time.Time           `json:"finishedAt"`
	Tasks      []PlanExecutionTask `json:"tasks"`
}

type PlanExecutionTask struct {
	Sequence       int                  `json:"sequence"`
	Phase          string               `json:"phase"`
	Status         string               `json:"status"`
	InstallationID string               `json:"installationId,omitempty"`
	Namespace      string               `json:"namespace,omitempty"`
	StartedAt      time.Time            `json:"startedAt"`
	FinishedAt     time.Time            `json:"finishedAt"`
	Failure        string               `json:"failure,omitempty"`
	AgentOutput    string               `json:"agentOutput,omitempty"`
	Resources      []ResourceDiagnostic `json:"resources,omitempty"`
}

type ResourceDiagnostic struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
	Reason     string `json:"reason,omitempty"`
	Message    string `json:"message,omitempty"`
}

const (
	ExecutionPhaseValidate       = "VALIDATE"
	ExecutionPhaseLease          = "LEASE"
	ExecutionPhaseLegacy         = "LEGACY"
	ExecutionPhaseSecretMutation = "SECRET_MUTATION"
	ExecutionPhaseApply          = "APPLY"
	ExecutionPhaseObserve        = "OBSERVE"
	ExecutionPhaseRollback       = "ROLLBACK"

	ExecutionTaskSucceeded      = "SUCCEEDED"
	ExecutionTaskFailed         = "FAILED"
	ExecutionTaskRolledBack     = "ROLLED_BACK"
	ExecutionTaskRollbackFailed = "ROLLBACK_FAILED"
)

type EntityCommand struct {
	Type           string    `json:"type"`
	InstallationID string    `json:"installationId"`
	ResourceKind   string    `json:"resourceKind,omitempty"`
	ResourceName   string    `json:"resourceName,omitempty"`
	TargetVersion  string    `json:"targetVersion,omitempty"`
	IssuedBy       string    `json:"issuedBy"`
	Rationale      string    `json:"rationale"`
	IssuedAt       time.Time `json:"issuedAt"`
}

const (
	CommandInstall                 = "INSTALL"
	CommandUninstall               = "UNINSTALL"
	CommandEnforceConfig           = "ENFORCE_CONFIG"
	CommandEnforceConfigAndVersion = "ENFORCE_CONFIG_AND_VERSION"
	CommandRestartResource         = "RESTART_RESOURCE"
)

type ReportedEntity struct {
	InstallationID string               `json:"installationId"`
	Product        string               `json:"product"`
	Version        string               `json:"version"`
	ReleaseDigest  string               `json:"releaseDigest"`
	Namespace      string               `json:"namespace"`
	ConfigDigest   string               `json:"configDigest"`
	Health         string               `json:"health"`
	RolloutStatus  string               `json:"rolloutStatus"`
	ConfigStatus   string               `json:"configStatus"`
	Lifecycle      string               `json:"lifecycle"`
	ObservedAt     time.Time            `json:"observedAt"`
	HealthDetails  *EntityHealthDetails `json:"healthDetails,omitempty"`
}

const (
	HealthHealthy       = "HEALTHY"
	HealthUnhealthy     = "UNHEALTHY"
	HealthIndeterminate = "INDETERMINATE"
	HealthUnknown       = "UNKNOWN"
	HealthRepairing     = "REPAIRING"
	HealthWarning       = "WARNING"
	HealthError         = "ERROR"
	HealthTerminal      = "TERMINAL"

	RolloutUpToDate      = "UP_TO_DATE"
	RolloutInGracePeriod = "IN_GRACE_PERIOD"
	RolloutBlocked       = "BLOCKED"
	RolloutRecalled      = "RECALLED"
	RolloutOutOfDate     = "OUT_OF_DATE"

	ConfigApplied       = "APPLIED"
	ConfigDrifted       = "DRIFTED"
	ConfigUnavailable   = "UNAVAILABLE"
	ConfigIndeterminate = "INDETERMINATE"

	LifecycleInstalled   = "INSTALLED"
	LifecycleUninstalled = "UNINSTALLED"
	LifecycleUnknown     = "UNKNOWN"
)

type EnvironmentReport struct {
	Environment string           `json:"environment"`
	AgentID     string           `json:"agentId"`
	PlanID      string           `json:"planId,omitempty"`
	ObservedAt  time.Time        `json:"observedAt"`
	Entities    []ReportedEntity `json:"entities"`
	Secrets     []ReportedSecret `json:"secrets,omitempty"`
	Revision    uint64           `json:"revision"`
}

type ReportedSecret struct {
	Assignment           string   `json:"assignment"`
	Namespace            string   `json:"namespace"`
	Revision             int      `json:"revision"`
	ProviderRef          string   `json:"providerRef"`
	KubernetesSecretName string   `json:"kubernetesSecretName"`
	Keys                 []string `json:"keys"`
}

type Repository interface {
	ModuleRepository
	ArtifactRepository
	ProductPublicationRepository
	BundleRepository
	GovernanceRepository
	EnvironmentProfileRepository
	CatalogRetirementRepository
	LabelDefinitionRepository
	CreatePlan(context.Context, PlanRecord) (PlanRecord, error)
	GetPlan(context.Context, string) (PlanRecord, error)
	ListPlans(context.Context, string) ([]PlanRecord, error)
	UpdatePlan(context.Context, string, func(*PlanRecord) error) (PlanRecord, error)
	PutEnvironmentReport(context.Context, EnvironmentReport) (EnvironmentReport, error)
	GetEnvironmentReport(context.Context, string) (EnvironmentReport, error)
	ListEnvironmentReports(context.Context) ([]EnvironmentReport, error)
	CreateSuppression(context.Context, SuppressionRecord) (SuppressionRecord, error)
	GetSuppression(context.Context, string) (SuppressionRecord, error)
	ListSuppressions(context.Context, string) ([]SuppressionRecord, error)
	UpdateSuppression(context.Context, string, func(*SuppressionRecord) error) (SuppressionRecord, error)
	CreatePromotionUnlessStagePaused(context.Context, PromotionRecord) (PromotionRecord, error)
	GetPromotion(context.Context, string) (PromotionRecord, error)
	ListPromotions(context.Context, string) ([]PromotionRecord, error)
	GetPromotionStageControl(context.Context, string, string, string) (PromotionStageControl, error)
	UpdatePromotionStageControl(context.Context, string, string, string, func(*PromotionStageControl) error) (PromotionStageControl, error)
}

type Lifecycle struct {
	repository Repository
	now        func() time.Time
	newToken   func() (string, error)
}

// OptimizerPlanProvenance is a non-authoritative evidence link. The ordinary
// Plan lifecycle remains the authority; optimizer-originated plans additionally
// require explicit approval and last-safe-point revalidation receipts.
type OptimizerPlanProvenance struct {
	ProposalID                  string `json:"proposalId"`
	SnapshotID                  string `json:"snapshotId"`
	SolverRequestID             string `json:"solverRequestId"`
	SolverReceiptID             string `json:"solverReceiptId"`
	DocumentDigest              string `json:"documentDigest"`
	ApprovalRevalidationID      string `json:"approvalRevalidationId,omitempty"`
	ApprovalAcquisitionID       string `json:"approvalAcquisitionId,omitempty"`
	LastSafePointRevalidationID string `json:"lastSafePointRevalidationId,omitempty"`
	LastSafePointAcquisitionID  string `json:"lastSafePointAcquisitionId,omitempty"`
}

type optimizerLeasePermit struct {
	planID         string
	proposalID     string
	revalidationID string
	acquisitionID  string
	evaluatedAt    time.Time
}

func NewLifecycle(repository Repository) *Lifecycle {
	return &Lifecycle{repository: repository, now: time.Now, newToken: randomToken}
}

func (lifecycle *Lifecycle) CreatePlan(ctx context.Context, state DesiredState) (PlanRecord, error) {
	return lifecycle.createPlan(ctx, state, nil)
}

func (lifecycle *Lifecycle) createPlan(ctx context.Context, state DesiredState, optimizer *OptimizerPlanProvenance) (PlanRecord, error) {
	if err := validateDesiredStateLabelDefinitions(ctx, lifecycle.repository, state); err != nil {
		return PlanRecord{}, err
	}
	if federation := resolveFederation(state.Environment); !federation.IssuesPlans() {
		if optimizer != nil {
			return PlanRecord{}, fmt.Errorf("%w: optimizer proposal targets a non-executing Environment", ErrLeaseRejected)
		}
		return lifecycle.recordNonExecutingState(ctx, state, federation)
	}
	report, reportErr := lifecycle.repository.GetEnvironmentReport(ctx, state.Environment.ID)
	if reportErr != nil && !errors.Is(reportErr, ErrNotFound) {
		return PlanRecord{}, reportErr
	}
	if err := lifecycle.validateNamespaceTransition(ctx, state, report); err != nil {
		return PlanRecord{}, err
	}
	plan, err := BuildPlanWithObserved(state, report, lifecycle.now().UTC())
	if err != nil {
		return PlanRecord{}, err
	}
	previousPlans, err := lifecycle.repository.ListPlans(ctx, state.Environment.ID)
	if err != nil {
		return PlanRecord{}, err
	}
	if err := validateOwnershipHandoffs(state, report, previousPlans); err != nil {
		return PlanRecord{}, err
	}
	plan = appendOrdinaryUninstalls(plan, report, previousPlans, state.OwnershipHandoffs)
	if err := validateHandoffSecretMutations(plan); err != nil {
		return PlanRecord{}, err
	}
	if err := validatePlanExecutionTaskBudget(plan); err != nil {
		return PlanRecord{}, err
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return PlanRecord{}, fmt.Errorf("encode desired state: %w", err)
	}
	stateDigest := sha256.Sum256(stateJSON)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return PlanRecord{}, fmt.Errorf("encode plan: %w", err)
	}
	identity := sha256.Sum256(append(stateDigest[:], planJSON...))
	now := lifecycle.now().UTC()
	record := PlanRecord{
		ID:                 "pln_" + hex.EncodeToString(identity[:16]),
		DesiredStateDigest: "sha256:" + hex.EncodeToString(stateDigest[:]),
		DesiredState:       pointerTo(cloneJSON(state)),
		Plan:               plan,
		Status:             PlanAwaitingApproval,
		RequiredApprovals:  1,
		CreatedAt:          now,
		UpdatedAt:          now,
		Revision:           1,
	}
	if optimizer != nil {
		provenance := *optimizer
		record.Optimizer = &provenance
	}
	created, err := lifecycle.repository.CreatePlan(ctx, record)
	if err != nil {
		return PlanRecord{}, err
	}
	if err := lifecycle.supersedePendingPlans(ctx, created, previousPlans); err != nil {
		return PlanRecord{}, err
	}
	return lifecycle.repository.GetPlan(ctx, created.ID)
}

func (lifecycle *Lifecycle) recordNonExecutingState(
	ctx context.Context, state DesiredState, federation EnvironmentFederation,
) (PlanRecord, error) {
	if errs := Validate(state); len(errs) > 0 {
		return PlanRecord{}, fmt.Errorf("desired state has %d validation error(s)", len(errs))
	}
	previous, err := lifecycle.repository.ListPlans(ctx, state.Environment.ID)
	if err != nil {
		return PlanRecord{}, err
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return PlanRecord{}, fmt.Errorf("encode desired state: %w", err)
	}
	stateDigest := sha256.Sum256(stateJSON)
	identity := sha256.Sum256(append(stateDigest[:], []byte("\x00RECORDED")...))
	now := lifecycle.now().UTC()
	record := PlanRecord{
		ID:                 "pln_" + hex.EncodeToString(identity[:16]),
		DesiredStateDigest: "sha256:" + hex.EncodeToString(stateDigest[:]),
		DesiredState:       pointerTo(cloneJSON(state)),
		Plan:               Plan{Environment: state.Environment.ID, Generation: state.Environment.Generation, Steps: []PlanStep{}},
		Status:             PlanRecorded,
		CreatedAt:          now,
		UpdatedAt:          now,
		Revision:           1,
	}
	created, err := lifecycle.repository.CreatePlan(ctx, record)
	if err != nil {
		return PlanRecord{}, err
	}
	for _, candidate := range previous {
		if candidate.Status != PlanAwaitingApproval && candidate.Status != PlanApproved &&
			candidate.Status != PlanLeased && candidate.Status != PlanRunning {
			continue
		}
		_, err := lifecycle.repository.UpdatePlan(ctx, candidate.ID, func(current *PlanRecord) error {
			if current.Status != PlanAwaitingApproval && current.Status != PlanApproved &&
				current.Status != PlanLeased && current.Status != PlanRunning {
				return nil
			}
			current.Status = PlanCancelled
			current.Cancellation = &PlanCancellation{
				Subject:   "orchestration-engine",
				Reason:    "connection setting changed to " + federation.ConnectionSetting + " in " + created.ID,
				CreatedAt: now,
			}
			return nil
		})
		if err != nil {
			return PlanRecord{}, fmt.Errorf("cancel Plan %s after topology change: %w", candidate.ID, err)
		}
	}
	return lifecycle.repository.GetPlan(ctx, created.ID)
}

func appendOrdinaryUninstalls(plan Plan, report EnvironmentReport, previous []PlanRecord, handoffs []OwnershipHandoff) Plan {
	desired := make(map[string]struct{}, len(plan.Steps))
	for _, step := range plan.Steps {
		desired[step.InstallationID] = struct{}{}
	}
	observed := make(map[string]ReportedEntity, len(report.Entities))
	for _, entity := range report.Entities {
		observed[entity.InstallationID] = entity
	}
	sort.Slice(previous, func(i, j int) bool {
		if previous[i].CreatedAt.Equal(previous[j].CreatedAt) {
			return previous[i].ID > previous[j].ID
		}
		return previous[i].CreatedAt.After(previous[j].CreatedAt)
	})
	chosen := make(map[string]struct{})
	handoffByInstallation := make(map[string]OwnershipHandoff, len(handoffs))
	for _, handoff := range handoffs {
		handoffByInstallation[handoff.InstallationID] = handoff
	}
	// Search newest-to-oldest so a superseded removal cannot erase the last
	// executable metadata for a still-reported installation. Within each Plan,
	// reverse dependency order removes dependents before dependencies.
	for _, record := range previous {
		if record.Command != nil || record.DesiredState == nil {
			continue
		}
		for index := len(record.Plan.Steps) - 1; index >= 0; index-- {
			prior := record.Plan.Steps[index]
			if _, alreadyChosen := chosen[prior.InstallationID]; alreadyChosen {
				continue
			}
			if _, stillDesired := desired[prior.InstallationID]; stillDesired {
				continue
			}
			if prior.Action == PlanActionUnmanage && record.Status == PlanSucceeded {
				chosen[prior.InstallationID] = struct{}{}
				continue
			}
			current, exists := observed[prior.InstallationID]
			if !exists || current.Lifecycle == LifecycleUninstalled {
				continue
			}
			step := cloneJSON(prior)
			step.Position = len(plan.Steps) + 1
			step.Action = planActionUninstall
			if _, handoff := handoffByInstallation[prior.InstallationID]; handoff {
				step.Action = PlanActionUnmanage
			}
			step.DependsOn = nil
			step.Version = current.Version
			step.ReleaseDigest = current.ReleaseDigest
			step.Namespace = current.Namespace
			step.ConfigDigest = current.ConfigDigest
			step.RampedRollout = nil
			step.RecallRollOff = false
			step.RecallIDs = nil
			plan.Steps = append(plan.Steps, step)
			chosen[prior.InstallationID] = struct{}{}
		}
	}
	return plan
}

func validateOwnershipHandoffs(state DesiredState, report EnvironmentReport, previous []PlanRecord) error {
	if len(state.OwnershipHandoffs) == 0 {
		return nil
	}
	observed := make(map[string]ReportedEntity, len(report.Entities))
	for _, entity := range report.Entities {
		observed[entity.InstallationID] = entity
	}
	known := make(map[string]struct{})
	for _, record := range previous {
		for _, step := range record.Plan.Steps {
			known[step.InstallationID] = struct{}{}
		}
	}
	for _, handoff := range state.OwnershipHandoffs {
		if _, exists := known[handoff.InstallationID]; !exists {
			return fmt.Errorf("%w: ownership handoff Entity %s was never managed", ErrNotFound, handoff.InstallationID)
		}
		entity, exists := observed[handoff.InstallationID]
		if !exists || entity.Lifecycle != LifecycleInstalled {
			return fmt.Errorf("%w: ownership handoff Entity %s must be exactly reported as installed", ErrConflict, handoff.InstallationID)
		}
	}
	return nil
}

func validateHandoffSecretMutations(plan Plan) error {
	protected := map[string]struct{}{}
	for _, step := range plan.Steps {
		if step.Action != PlanActionUnmanage {
			continue
		}
		for _, access := range step.SecretAccess {
			protected[access.Assignment+"\x00"+step.Namespace] = struct{}{}
		}
	}
	for _, mutation := range plan.SecretMutations {
		if mutation.Action != SecretDelete || mutation.Previous == nil {
			continue
		}
		if _, ownedByHandoff := protected[mutation.Previous.Assignment+"\x00"+mutation.Namespace]; ownedByHandoff {
			return fmt.Errorf("%w: ownership handoff cannot delete Entity secret assignment %s", ErrConflict, mutation.Previous.Assignment)
		}
	}
	return nil
}

func (lifecycle *Lifecycle) supersedePendingPlans(ctx context.Context, current PlanRecord, older []PlanRecord) error {
	for _, candidate := range older {
		if candidate.ID == current.ID || candidate.Command != nil ||
			(candidate.Status != PlanAwaitingApproval && candidate.Status != PlanApproved) {
			continue
		}
		_, err := lifecycle.repository.UpdatePlan(ctx, candidate.ID, func(record *PlanRecord) error {
			if record.Status != PlanAwaitingApproval && record.Status != PlanApproved {
				return nil
			}
			now := lifecycle.now().UTC()
			record.Status = PlanCancelled
			record.Cancellation = &PlanCancellation{
				Subject: "orchestration-engine", Reason: "superseded by " + current.ID, CreatedAt: now,
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("supersede Plan %s: %w", candidate.ID, err)
		}
	}
	return nil
}

func pointerTo[T any](value T) *T { return &value }

func (lifecycle *Lifecycle) ApprovePlan(ctx context.Context, planID, subject, rationale string) (PlanRecord, error) {
	subject = strings.TrimSpace(subject)
	rationale = strings.TrimSpace(rationale)
	if subject == "" || rationale == "" {
		return PlanRecord{}, fmt.Errorf("%w: subject and rationale are required", ErrInvalidTransition)
	}
	return lifecycle.updatePlan(ctx, planID, func(record *PlanRecord) error {
		if record.Optimizer != nil {
			return fmt.Errorf("%w: optimizer Plan requires governed proposal approval", ErrInvalidTransition)
		}
		if record.Status != PlanAwaitingApproval && record.Status != PlanApproved {
			return fmt.Errorf("%w: cannot approve plan in %s", ErrInvalidTransition, record.Status)
		}
		for _, approval := range record.Approvals {
			if approval.Subject == subject {
				return fmt.Errorf("%w: subject has already approved this plan", ErrConflict)
			}
		}
		record.Approvals = append(record.Approvals, Approval{Subject: subject, Rationale: rationale, CreatedAt: lifecycle.now().UTC()})
		if len(record.Approvals) >= record.RequiredApprovals {
			record.Status = PlanApproved
		}
		return nil
	})
}

func (lifecycle *Lifecycle) LeaseNextPlan(
	ctx context.Context, environment, agentID string, duration time.Duration,
) (PlanRecord, error) {
	return lifecycle.leaseNextPlan(ctx, environment, agentID, duration, nil)
}

func (lifecycle *Lifecycle) leaseNextPlan(
	ctx context.Context, environment, agentID string, duration time.Duration, permit *optimizerLeasePermit,
) (PlanRecord, error) {
	if strings.TrimSpace(agentID) == "" || duration <= 0 || duration > 15*time.Minute {
		return PlanRecord{}, fmt.Errorf("%w: agent id and a lease duration up to 15 minutes are required", ErrLeaseRejected)
	}
	plans, err := lifecycle.repository.ListPlans(ctx, environment)
	if err != nil {
		return PlanRecord{}, err
	}
	// An Environment that switched away from agents-connected may still hold
	// approved Plans from before the switch. Refuse to hand them out rather than
	// letting an Agent drain stale work the Hub no longer owns.
	if federation := resolveFederationFromPlans(environment, plans); !federation.AcceptsAgentConnections() {
		return PlanRecord{}, fmt.Errorf(
			"%w: connection setting %s does not accept Agent connections on this Hub",
			ErrLeaseRejected, federation.ConnectionSetting)
	}
	sort.Slice(plans, func(i, j int) bool {
		left, right := planPriority(plans[i]), planPriority(plans[j])
		if left != right {
			return left < right
		}
		if plans[i].CreatedAt.Equal(plans[j].CreatedAt) {
			return plans[i].ID < plans[j].ID
		}
		return plans[i].CreatedAt.Before(plans[j].CreatedAt)
	})
	now := lifecycle.now().UTC()
	for _, candidate := range plans {
		if permit == nil && candidate.Optimizer != nil {
			continue
		}
		if permit != nil && (candidate.ID != permit.planID || candidate.Optimizer == nil || candidate.Optimizer.ProposalID != permit.proposalID) {
			continue
		}
		if candidate.Status != PlanApproved &&
			!((candidate.Status == PlanLeased || candidate.Status == PlanRunning) && execution.Expired(candidate.Lease, now)) {
			continue
		}
		if budgetErr := validatePlanExecutionTaskBudget(candidate.Plan); budgetErr != nil {
			return PlanRecord{}, fmt.Errorf("%w: Plan %s: %v", ErrLeaseRejected, candidate.ID, budgetErr)
		}
		if planLeaseBlocked(candidate, plans, now) {
			continue
		}
		// Read once here and re-evaluated inside the lease write below, because the
		// repository lock is held across that update and cannot be read again.
		var suppressions []SuppressionRecord
		if candidate.Command == nil {
			records, suppressionErr := lifecycle.repository.ListSuppressions(ctx, candidate.Plan.Environment)
			if suppressionErr != nil {
				return PlanRecord{}, suppressionErr
			}
			suppressions = records
			if activeSuppressionForPlan(suppressions, candidate.Plan, now) != nil {
				continue
			}
			if !planWithinMaintenanceWindow(candidate.Plan, now) {
				continue
			}
		}
		report, reportErr := lifecycle.repository.GetEnvironmentReport(ctx, candidate.Plan.Environment)
		if reportErr != nil && !errors.Is(reportErr, ErrNotFound) {
			return PlanRecord{}, reportErr
		}
		if candidate.Command == nil && !rampedRolloutReady(candidate.Plan, report, now) {
			continue
		}
		token, tokenErr := lifecycle.newToken()
		if tokenErr != nil {
			return PlanRecord{}, tokenErr
		}
		leased, updateErr := lifecycle.updatePlan(ctx, candidate.ID, func(record *PlanRecord) error {
			currentTime := lifecycle.now().UTC()
			if permit == nil && record.Optimizer != nil {
				return ErrConflict
			}
			if permit != nil && (record.Optimizer == nil || record.ID != permit.planID || record.Optimizer.ProposalID != permit.proposalID || record.Optimizer.ApprovalRevalidationID == "" || record.Optimizer.ApprovalAcquisitionID == "" || permit.revalidationID == "" || permit.acquisitionID == "" || !optimizerEvidenceFreshAt(permit.evaluatedAt, currentTime)) {
				return ErrConflict
			}
			// Re-evaluated here, not just in the scan above, because a suppression
			// window can open between the two and the scan's clock is already stale
			// by the time this write runs. Maintenance and ramped rollout are
			// rechecked for the same reason; suppression is the operator's stop
			// control and has more claim to it than either.
			if record.Command == nil && activeSuppressionForPlan(suppressions, record.Plan, currentTime) != nil {
				return ErrConflict
			}
			if record.Command == nil && !planWithinMaintenanceWindow(record.Plan, currentTime) {
				return ErrConflict
			}
			if record.Command == nil && !rampedRolloutReady(record.Plan, report, currentTime) {
				return ErrConflict
			}
			eligible := record.Status == PlanApproved ||
				((record.Status == PlanLeased || record.Status == PlanRunning) && execution.Expired(record.Lease, currentTime))
			if !eligible {
				return ErrConflict
			}
			attempt := execution.NextAttempt(record.Lease)
			record.Status = PlanLeased
			record.Failure = ""
			record.FailedInstallation = ""
			record.Execution = nil
			record.Lease = &PlanLease{
				AgentID: agentID, Token: token, Attempt: attempt,
				IssuedAt: currentTime, ExpiresAt: currentTime.Add(duration),
			}
			if permit != nil {
				record.Optimizer.LastSafePointRevalidationID = permit.revalidationID
				record.Optimizer.LastSafePointAcquisitionID = permit.acquisitionID
			}
			return nil
		})
		if updateErr == nil {
			return leased, nil
		}
		if !errors.Is(updateErr, ErrConflict) {
			return PlanRecord{}, updateErr
		}
	}
	return PlanRecord{}, ErrNoPlanAvailable
}

func planPriority(record PlanRecord) int {
	if record.Command != nil {
		return 0
	}
	for _, step := range record.Plan.Steps {
		if step.RecallRollOff {
			return 1
		}
	}
	return 2
}

func planLeaseBlocked(candidate PlanRecord, plans []PlanRecord, now time.Time) bool {
	for _, other := range plans {
		if other.ID == candidate.ID || !plansOverlap(candidate, other) {
			continue
		}
		otherExecuting := (other.Status == PlanLeased || other.Status == PlanRunning) && execution.Live(other.Lease, now)
		if candidate.Command != nil {
			if otherExecuting {
				return true
			}
			continue
		}
		if other.Command != nil && (other.Status == PlanApproved || otherExecuting) {
			return true
		}
	}
	return false
}

func rampedRolloutReady(plan Plan, report EnvironmentReport, now time.Time) bool {
	observed := make(map[string]ReportedEntity, len(report.Entities))
	for _, entity := range report.Entities {
		observed[entity.InstallationID] = entity
	}
	for _, step := range plan.Steps {
		if step.RampedRollout == nil {
			continue
		}
		current, installed := observed[step.InstallationID]
		// Ramping applies only to upgrades. Installs, config-only reconciles,
		// downgrades/recall roll-offs, and rollback paths bypass it.
		if !installed || current.Version == step.Version || !versionPattern.MatchString(current.Version) ||
			compareVersions(current.Version, step.Version) >= 0 {
			continue
		}
		if now.Before(step.RampedRollout.NotBefore) {
			return false
		}
	}
	return true
}

func (lifecycle *Lifecycle) StartPlan(ctx context.Context, planID, leaseToken string) (PlanRecord, error) {
	return lifecycle.transitionWithLease(ctx, planID, leaseToken, func(record *PlanRecord) error {
		if record.Status == PlanCancelled {
			return ErrPlanCancelled
		}
		if record.Status != PlanLeased {
			return fmt.Errorf("%w: cannot start plan in %s", ErrInvalidTransition, record.Status)
		}
		record.Status = PlanRunning
		return nil
	})
}

func (lifecycle *Lifecycle) RenewLease(
	ctx context.Context, planID, leaseToken string, duration time.Duration,
) (PlanRecord, error) {
	if duration <= 0 || duration > 15*time.Minute {
		return PlanRecord{}, fmt.Errorf("%w: lease duration must be at most 15 minutes", ErrLeaseRejected)
	}
	return lifecycle.transitionWithLease(ctx, planID, leaseToken, func(record *PlanRecord) error {
		if record.Status == PlanCancelled {
			return ErrPlanCancelled
		}
		if record.Status != PlanLeased && record.Status != PlanRunning {
			return fmt.Errorf("%w: cannot renew plan in %s", ErrInvalidTransition, record.Status)
		}
		record.Lease.ExpiresAt = lifecycle.now().UTC().Add(duration)
		return nil
	})
}

func (lifecycle *Lifecycle) FinishPlan(
	ctx context.Context, planID, leaseToken string, succeeded bool, failure, failedInstallationID string,
	execution *PlanExecution, report *EnvironmentReport,
) (PlanRecord, error) {
	existing, err := lifecycle.repository.GetPlan(ctx, planID)
	if err != nil {
		return PlanRecord{}, err
	}
	if report == nil {
		return PlanRecord{}, fmt.Errorf("%w: reported state is required", ErrInvalidTransition)
	}
	if execution == nil {
		now := lifecycle.now().UTC()
		status := ExecutionTaskSucceeded
		if !succeeded {
			status = ExecutionTaskFailed
		}
		execution = &PlanExecution{StartedAt: now, FinishedAt: now, Tasks: []PlanExecutionTask{{
			Sequence: 1, Phase: ExecutionPhaseLegacy, Status: status, StartedAt: now, FinishedAt: now,
			InstallationID: strings.TrimSpace(failedInstallationID), Failure: strings.TrimSpace(failure),
		}}}
	}
	normalizedExecution, err := normalizePlanExecution(existing.Plan, *execution, succeeded)
	if err != nil {
		return PlanRecord{}, err
	}
	if !succeeded && strings.TrimSpace(failedInstallationID) != "" {
		found := false
		for _, step := range existing.Plan.Steps {
			if step.InstallationID == failedInstallationID {
				found = true
				break
			}
		}
		if !found {
			return PlanRecord{}, fmt.Errorf("%w: failed installation is not in the Plan", ErrInvalidTransition)
		}
	}
	if err := validateEnvironmentReport(existing.Plan, *report, succeeded); err != nil {
		return PlanRecord{}, err
	}
	record, err := lifecycle.transitionWithLease(ctx, planID, leaseToken, func(record *PlanRecord) error {
		if record.Status == PlanCancelled {
			return ErrPlanCancelled
		}
		if record.Status != PlanRunning {
			return fmt.Errorf("%w: cannot finish plan in %s", ErrInvalidTransition, record.Status)
		}
		if succeeded {
			record.Status = PlanSucceeded
			record.Failure = ""
		} else {
			if strings.TrimSpace(failure) == "" {
				return fmt.Errorf("%w: failed plans require a failure reason", ErrInvalidTransition)
			}
			record.Status = PlanFailed
			record.Failure = strings.TrimSpace(failure)
			record.FailedInstallation = strings.TrimSpace(failedInstallationID)
		}
		normalizedExecution.AgentID = record.Lease.AgentID
		normalizedExecution.Attempt = record.Lease.Attempt
		record.Execution = &normalizedExecution
		return nil
	})
	if err != nil {
		return PlanRecord{}, err
	}
	if report != nil && (succeeded || len(report.Entities) > 0) {
		report.Environment = record.Plan.Environment
		report.AgentID = record.Lease.AgentID
		report.PlanID = record.ID
		report.ObservedAt = lifecycle.now().UTC()
		for index := range report.Entities {
			if report.Entities[index].ObservedAt.IsZero() {
				report.Entities[index].ObservedAt = report.ObservedAt
			}
		}
		if record.Command != nil {
			if previous, previousErr := lifecycle.repository.GetEnvironmentReport(ctx, record.Plan.Environment); previousErr == nil {
				mergeCommandReport(report, previous, record.Plan)
			} else if !errors.Is(previousErr, ErrNotFound) {
				return PlanRecord{}, fmt.Errorf("load reported state for command merge: %w", previousErr)
			}
		}
		if _, err := lifecycle.repository.PutEnvironmentReport(ctx, *report); err != nil {
			return PlanRecord{}, fmt.Errorf("persist reported state: %w", err)
		}
	}
	if !succeeded {
		if err := lifecycle.createPlanFailureSuppression(
			ctx, record.Plan, record.ID, record.FailedInstallation, record.Failure,
		); err != nil {
			return PlanRecord{}, fmt.Errorf("create automatic Plan-failure suppression: %w", err)
		}
	}
	return record, nil
}

func mergeCommandReport(report *EnvironmentReport, previous EnvironmentReport, plan Plan) {
	targets := make(map[string]struct{}, len(plan.Steps))
	for _, step := range plan.Steps {
		targets[step.InstallationID] = struct{}{}
	}
	mergedEntities := append([]ReportedEntity(nil), report.Entities...)
	for _, entity := range previous.Entities {
		if _, replaced := targets[entity.InstallationID]; !replaced {
			mergedEntities = append(mergedEntities, entity)
		}
	}
	sort.Slice(mergedEntities, func(i, j int) bool { return mergedEntities[i].InstallationID < mergedEntities[j].InstallationID })
	report.Entities = mergedEntities

	mutated := make(map[string]struct{}, len(plan.SecretMutations))
	deleted := make(map[string]struct{}, len(plan.SecretMutations))
	for _, mutation := range plan.SecretMutations {
		access := mutation.Current
		if access == nil {
			access = mutation.Previous
		}
		if access == nil {
			continue
		}
		key := access.Assignment + "\x00" + mutation.Namespace
		mutated[key] = struct{}{}
		if mutation.Action == SecretDelete {
			deleted[key] = struct{}{}
		}
	}
	mergedSecrets := append([]ReportedSecret(nil), report.Secrets...)
	for _, secret := range previous.Secrets {
		key := secret.Assignment + "\x00" + secret.Namespace
		if _, changed := mutated[key]; !changed {
			mergedSecrets = append(mergedSecrets, secret)
		}
	}
	filtered := mergedSecrets[:0]
	for _, secret := range mergedSecrets {
		if _, remove := deleted[secret.Assignment+"\x00"+secret.Namespace]; !remove {
			filtered = append(filtered, secret)
		}
	}
	report.Secrets = filtered
}

func (lifecycle *Lifecycle) CancelPlan(
	ctx context.Context, planID, subject, reason string,
) (PlanRecord, error) {
	subject = strings.TrimSpace(subject)
	reason = strings.TrimSpace(reason)
	if subject == "" || reason == "" {
		return PlanRecord{}, fmt.Errorf("%w: cancellation subject and reason are required", ErrInvalidTransition)
	}
	return lifecycle.updatePlan(ctx, planID, func(record *PlanRecord) error {
		switch record.Status {
		case PlanAwaitingApproval, PlanApproved, PlanLeased, PlanRunning:
		default:
			return fmt.Errorf("%w: cannot cancel plan in %s", ErrInvalidTransition, record.Status)
		}
		record.Status = PlanCancelled
		record.Cancellation = &PlanCancellation{Subject: subject, Reason: reason, CreatedAt: lifecycle.now().UTC()}
		return nil
	})
}

func (lifecycle *Lifecycle) UndoUnmanage(
	ctx context.Context, planID, subject, reason string,
) (PlanRecord, error) {
	if strings.TrimSpace(subject) == "" || strings.TrimSpace(reason) == "" {
		return PlanRecord{}, ErrInvalidTransition
	}
	return lifecycle.updatePlan(ctx, planID, func(record *PlanRecord) error {
		if record.Lease != nil || (record.Status != PlanAwaitingApproval && record.Status != PlanApproved) {
			return fmt.Errorf("%w: ownership handoff can only be undone before lease", ErrInvalidTransition)
		}
		containsHandoff := false
		for _, step := range record.Plan.Steps {
			if step.Action == PlanActionUnmanage {
				containsHandoff = true
				break
			}
		}
		if !containsHandoff {
			return ErrInvalidTransition
		}
		record.Status = PlanCancelled
		record.Cancellation = &PlanCancellation{
			Subject: strings.TrimSpace(subject), Reason: strings.TrimSpace(reason), CreatedAt: lifecycle.now().UTC(),
		}
		return nil
	})
}

func validateEnvironmentReport(plan Plan, report EnvironmentReport, succeeded bool) error {
	expected := make(map[string]PlanStep, len(plan.Steps))
	for _, step := range plan.Steps {
		expected[step.InstallationID] = step
	}
	seen := make(map[string]struct{}, len(report.Entities))
	for _, entity := range report.Entities {
		if _, duplicate := seen[entity.InstallationID]; duplicate {
			return fmt.Errorf("%w: duplicate reported installation %s", ErrInvalidTransition, entity.InstallationID)
		}
		seen[entity.InstallationID] = struct{}{}
		step, exists := expected[entity.InstallationID]
		if !exists {
			return fmt.Errorf("%w: unplanned reported installation %s", ErrInvalidTransition, entity.InstallationID)
		}
		if entity.Product != step.Product || entity.Version != step.Version || entity.ReleaseDigest != step.ReleaseDigest || entity.Namespace != step.Namespace ||
			entity.ConfigDigest != step.ConfigDigest {
			return fmt.Errorf("%w: reported installation %s does not match the leased plan", ErrInvalidTransition, entity.InstallationID)
		}
		if !contains([]string{HealthHealthy, HealthUnhealthy, HealthIndeterminate, HealthUnknown, HealthRepairing, HealthWarning, HealthError, HealthTerminal}, entity.Health) {
			return fmt.Errorf("%w: reported installation %s has invalid health %q", ErrInvalidTransition, entity.InstallationID, entity.Health)
		}
		if !contains([]string{RolloutUpToDate, RolloutInGracePeriod, RolloutBlocked, RolloutRecalled, RolloutOutOfDate}, entity.RolloutStatus) {
			return fmt.Errorf("%w: reported installation %s has invalid rollout status %q", ErrInvalidTransition, entity.InstallationID, entity.RolloutStatus)
		}
		if !contains([]string{ConfigApplied, ConfigDrifted, ConfigUnavailable, ConfigIndeterminate}, entity.ConfigStatus) {
			return fmt.Errorf("%w: reported installation %s has invalid config status %q", ErrInvalidTransition, entity.InstallationID, entity.ConfigStatus)
		}
		if !contains([]string{LifecycleInstalled, LifecycleUninstalled, LifecycleUnknown}, entity.Lifecycle) {
			return fmt.Errorf("%w: reported installation %s has invalid lifecycle %q", ErrInvalidTransition, entity.InstallationID, entity.Lifecycle)
		}
		acceptableSuccess := entity.Health == HealthHealthy && entity.RolloutStatus == RolloutUpToDate
		if step.HelmExecution.RolloutStrategy == RolloutApplyChangesNoWait {
			acceptableSuccess = entity.Health == HealthUnknown && entity.RolloutStatus == RolloutInGracePeriod
		}
		expectedLifecycle := LifecycleInstalled
		if step.Action == planActionUninstall {
			expectedLifecycle = LifecycleUninstalled
			acceptableSuccess = entity.Health == HealthUnknown && entity.RolloutStatus == RolloutUpToDate
		}
		if succeeded && (!acceptableSuccess || entity.ConfigStatus != ConfigApplied || entity.Lifecycle != expectedLifecycle) {
			return fmt.Errorf("%w: successful plan reported %s with health=%s rollout=%s config=%s lifecycle=%s",
				ErrInvalidTransition, entity.InstallationID, entity.Health, entity.RolloutStatus, entity.ConfigStatus, entity.Lifecycle)
		}
	}
	if succeeded && len(seen) != len(expected) {
		return fmt.Errorf("%w: successful plan must report every planned installation", ErrInvalidTransition)
	}
	expectedSecrets := make(map[string]struct {
		Namespace string
		Access    SecretAccess
	})
	for _, mutation := range plan.SecretMutations {
		if mutation.Action != SecretDelete && mutation.Current != nil {
			expectedSecrets[mutation.Current.Assignment+"\x00"+mutation.Namespace] = struct {
				Namespace string
				Access    SecretAccess
			}{Namespace: mutation.Namespace, Access: *mutation.Current}
		}
	}
	seenSecrets := make(map[string]struct{}, len(report.Secrets))
	for _, secret := range report.Secrets {
		key := secret.Assignment + "\x00" + secret.Namespace
		if _, duplicate := seenSecrets[key]; duplicate {
			return fmt.Errorf("%w: duplicate reported secret %s in %s", ErrInvalidTransition, secret.Assignment, secret.Namespace)
		}
		seenSecrets[key] = struct{}{}
		expected, exists := expectedSecrets[key]
		if !exists || secret.Namespace != expected.Namespace || secret.Revision != expected.Access.Revision ||
			secret.ProviderRef != expected.Access.ProviderRef || secret.KubernetesSecretName != expected.Access.KubernetesSecretName ||
			!equalStrings(secret.Keys, expected.Access.Keys) {
			return fmt.Errorf("%w: reported secret %s does not match the leased plan", ErrInvalidTransition, secret.Assignment)
		}
	}
	if succeeded && len(seenSecrets) != len(expectedSecrets) {
		return fmt.Errorf("%w: successful plan must report every desired secret", ErrInvalidTransition)
	}
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (lifecycle *Lifecycle) transitionWithLease(
	ctx context.Context, planID, token string, transition func(*PlanRecord) error,
) (PlanRecord, error) {
	return lifecycle.updatePlan(ctx, planID, func(record *PlanRecord) error {
		if !execution.HeldByToken(record.Lease, token) {
			return ErrLeaseRejected
		}
		if !execution.Live(record.Lease, lifecycle.now().UTC()) {
			return fmt.Errorf("%w: lease expired", ErrLeaseRejected)
		}
		return transition(record)
	})
}

func (lifecycle *Lifecycle) updatePlan(
	ctx context.Context, planID string, update func(*PlanRecord) error,
) (PlanRecord, error) {
	return lifecycle.repository.UpdatePlan(ctx, planID, func(record *PlanRecord) error {
		if err := update(record); err != nil {
			return err
		}
		record.UpdatedAt = lifecycle.now().UTC()
		return nil
	})
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate lease token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
