package pipeline

import (
	"context"
	"errors"
	"os"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/fixtures"
)

const (
	assessTimeout   = 30 * time.Second
	proposeTimeout  = 60 * time.Second
	validateTimeout = 60 * time.Second
	approvalTimeout = 15 * time.Second
	handoffTimeout  = 30 * time.Second
	heartbeatEvery  = 5 * time.Second
)

// PlanSubmitter proposes one Plan. Implementations must not approve, lease,
// or execute it.
type PlanSubmitter interface {
	Submit(context.Context, domain.HandoffRequest) (domain.PlanReceipt, error)
}

// Activities are the only place packaging reads the fixture, talks to the
// approval read model, or writes the Plan journal. The workflow calls them by
// the names in names.go.
type Activities struct {
	FixtureDir           string
	Ledger               *adapters.Ledger
	Plans                PlanSubmitter
	HoldFile             string
	PauseAfterSubmitFile string
	// Optional replacements for tests. Nil uses the pinned fixture.
	AssessFn   func(context.Context, domain.RunIdentity, string) (domain.Receipt, error)
	ProposeFn  func(context.Context, domain.RunIdentity, string) (domain.Receipt, error)
	ValidateFn func(context.Context, domain.RunIdentity, string, domain.Receipt) (domain.Validation, error)
}

type handoffInput struct {
	RunID            string `json:"runId"`
	ProposalRevision string `json:"proposalRevision"`
	ArtifactDigest   string `json:"artifactDigest"`
	TargetPlanRef    string `json:"targetPlanRef"`
	EnvironmentID    string `json:"environmentId,omitempty"`
	Generation       string `json:"generation,omitempty"`
	Product          string `json:"product,omitempty"`
	Version          string `json:"version,omitempty"`
	Namespace        string `json:"namespace,omitempty"`
	EntityName       string `json:"entityName,omitempty"`
	ChartRepository  string `json:"chartRepository,omitempty"`
	ImageRepository  string `json:"imageRepository,omitempty"`
}

func (a Activities) Assess(ctx context.Context, id domain.RunIdentity) (domain.Receipt, error) {
	activity.RecordHeartbeat(ctx, "assess")
	if err := id.Validate(); err != nil {
		return domain.Receipt{}, badInput(err)
	}
	if err := waitUntilGone(ctx, a.HoldFile); err != nil {
		return domain.Receipt{}, err
	}
	key := domain.StepKey(id.RunID, "assess", id.TemplateVersion, id.SourceDigest, "1")
	if existing, ok, err := a.load(ctx, key); err != nil || ok {
		return existing, err
	}
	fn := a.AssessFn
	if fn == nil {
		fn = fixtures.Suite{}.Assess
	}
	receipt, err := fn(ctx, id, a.FixtureDir)
	if err != nil {
		return domain.Receipt{}, classify(err)
	}
	if receipt.SourceDigest != id.SourceDigest {
		return domain.Receipt{}, badInput(errors.New("assessed source digest does not match the run identity"))
	}
	receipt.Step = "assess"
	return a.store(ctx, key, receipt)
}

func (a Activities) Propose(ctx context.Context, id domain.RunIdentity) (domain.Receipt, error) {
	activity.RecordHeartbeat(ctx, "propose")
	if err := id.Validate(); err != nil {
		return domain.Receipt{}, badInput(err)
	}
	key := domain.StepKey(id.RunID, "propose", id.TemplateVersion, id.SourceDigest, "1")
	if existing, ok, err := a.load(ctx, key); err != nil || ok {
		return existing, err
	}
	fn := a.ProposeFn
	if fn == nil {
		fn = fixtures.Suite{}.Propose
	}
	receipt, err := fn(ctx, id, a.FixtureDir)
	if err != nil {
		return domain.Receipt{}, classify(err)
	}
	if receipt.SourceDigest != id.SourceDigest || receipt.Artifact.Digest == "" || receipt.ProposalRevision == "" {
		return domain.Receipt{}, badInput(errors.New("proposal receipt is not bound to the source"))
	}
	receipt.Step = "propose"
	receipt.TargetPlanRef = id.TargetPlanRef
	return a.store(ctx, key, receipt)
}

func (a Activities) Validate(ctx context.Context, id domain.RunIdentity, proposal domain.Receipt) (domain.Validation, error) {
	activity.RecordHeartbeat(ctx, "validate")
	if proposal.Artifact.Digest == "" || proposal.ProposalRevision == "" {
		return domain.Validation{}, badInput(errors.New("validation requires a proposal identity"))
	}
	fn := a.ValidateFn
	if fn == nil {
		fn = fixtures.Suite{}.Validate
	}
	validation, err := fn(ctx, id, a.FixtureDir, proposal)
	if err != nil {
		return domain.Validation{}, classify(err)
	}
	if validation.ArtifactDigest != proposal.Artifact.Digest || validation.ProposalRevision != proposal.ProposalRevision {
		validation.Outcome = domain.OutcomeFailRequired
		validation.Reason = "validation is not bound to this proposal"
	}
	validation.Reason = domain.TrimReason(validation.Reason)
	return validation, nil
}

func (a Activities) ReadApproval(ctx context.Context, message domain.ReviewMessage) (domain.ApprovalRecord, error) {
	activity.RecordHeartbeat(ctx, "approval")
	if message.ApprovalID == "" {
		return domain.ApprovalRecord{}, badInput(errors.New("review message has no approval id"))
	}
	record, ok, err := a.Ledger.ReadApproval(ctx, message.ApprovalID)
	if err != nil {
		return domain.ApprovalRecord{}, err
	}
	if !ok {
		return domain.ApprovalRecord{ApprovalID: message.ApprovalID, Decision: domain.DecisionAbsent}, nil
	}
	return record, nil
}

func (a Activities) Handoff(ctx context.Context, in handoffInput) (domain.PlanReceipt, error) {
	activity.RecordHeartbeat(ctx, "reconcile")
	if a.Plans == nil {
		return domain.PlanReceipt{}, badInput(errors.New("packaging worker has no plan submitter"))
	}
	if ctx.Err() != nil {
		return domain.PlanReceipt{}, ctx.Err()
	}
	activity.RecordHeartbeat(ctx, "submit")
	receipt, err := a.Plans.Submit(ctx, domain.HandoffRequest{
		RunID: in.RunID, ProposalRevision: in.ProposalRevision, ArtifactDigest: in.ArtifactDigest,
		TargetPlanRef: in.TargetPlanRef, EnvironmentID: in.EnvironmentID, Generation: in.Generation,
		Product: in.Product, Version: in.Version, Namespace: in.Namespace, EntityName: in.EntityName,
		ChartRepository: in.ChartRepository, ImageRepository: in.ImageRepository,
	})
	if errors.Is(err, adapters.ErrDenied) || errors.Is(err, domain.ErrIdentity) {
		return domain.PlanReceipt{}, temporal.NewNonRetryableApplicationError(err.Error(), "ExecutionControl", err)
	}
	if err != nil {
		// The Plan write is durable. A failure after it is an uncertain
		// acknowledgement: the next attempt reconciles instead of submitting.
		return domain.PlanReceipt{}, temporal.NewApplicationError(err.Error(), "Transient", err)
	}
	if err := waitUntilGone(ctx, a.PauseAfterSubmitFile); err != nil {
		return domain.PlanReceipt{}, err
	}
	return receipt, nil
}

func (a Activities) load(ctx context.Context, key string) (domain.Receipt, bool, error) {
	receipt, ok, err := a.Ledger.LoadReceipt(ctx, key)
	if err != nil || !ok {
		return receipt, ok, err
	}
	if err := receipt.Bound(); err != nil {
		return domain.Receipt{}, false, badInput(err)
	}
	return receipt, true, nil
}

func (a Activities) store(ctx context.Context, key string, receipt domain.Receipt) (domain.Receipt, error) {
	receipt.Reason = domain.TrimReason(receipt.Reason)
	if err := receipt.Bound(); err != nil {
		return domain.Receipt{}, badInput(err)
	}
	stored, err := a.Ledger.StoreReceipt(ctx, key, receipt)
	if err != nil {
		return domain.Receipt{}, err
	}
	return stored, nil
}

func waitUntilGone(ctx context.Context, path string) error {
	if path == "" {
		return nil
	}
	// The marker is how a test sees that the activity is inside this wait.
	// It is not a workflow result and it is not a credential.
	_ = os.WriteFile(path+".ready", []byte("1"), 0o600)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		activity.RecordHeartbeat(ctx, path)
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func badInput(err error) error {
	return temporal.NewNonRetryableApplicationError(err.Error(), "BadInput", err)
}

func classify(err error) error {
	if errors.Is(err, domain.ErrSensitive) || errors.Is(err, domain.ErrBound) || errors.Is(err, domain.ErrIdentity) {
		return badInput(err)
	}
	return temporal.NewApplicationError(err.Error(), "Transient", err)
}
