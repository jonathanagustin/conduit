package pipeline

import (
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// Input is the workflow argument. Fixture bytes, credentials and generated
// files are deliberately absent: activities read those from the worker.
type Input struct {
	Identity                 domain.RunIdentity `json:"identity"`
	ApprovalWait             time.Duration      `json:"approvalWait"`
	ContinueAsNewAfterEvents int                `json:"continueAsNewAfterEvents"`
	CANCount                 int                `json:"canCount"`
	Carry                    *domain.State      `json:"carry,omitempty"`
}

// PackagingWorkflow is assessment, proposal, validation, a review wait, and
// one idempotent Plan handoff. A failed gate completes the run as blocked.
// It is not retried into a pass. Cancellation and continue-as-new are
// returned as Temporal errors so the server records them as such.
func PackagingWorkflow(ctx workflow.Context, in Input) (domain.Result, error) {
	// Version 2 records the step journal. A history recorded at version 1
	// replays without those timestamps, so an older run has an empty journal
	// rather than a rewritten command sequence.
	// Version 3 also journals terminal status transitions that have no activity
	// step (for example cancellation during the review wait).
	version := workflow.GetVersion(ctx, changeID, workflow.DefaultVersion, 3)
	if version < 1 {
		return domain.Result{}, temporal.NewNonRetryableApplicationError(
			"this worker cannot replay a packaging history from before the v1 marker",
			"UnsupportedWorkflowVersion", nil)
	}
	if err := in.Identity.Validate(); err != nil {
		return domain.Result{
			RunID: in.Identity.RunID, Phase: domain.PhaseBlocked,
			Outcome: domain.OutcomeFailRequired, Reason: domain.TrimReason(err.Error()),
		}, nil
	}
	state := &domain.State{
		Phase: domain.PhaseAssess, TargetPlanRef: in.Identity.TargetPlanRef,
		TemplateVersion: in.Identity.TemplateVersion, ToolVersions: in.Identity.ToolVersions,
		AttachmentSHA256: in.Identity.AttachmentSHA256,
	}
	if in.Carry != nil {
		copied := *in.Carry
		state = &copied
		if state.TemplateVersion == "" {
			state.TemplateVersion = in.Identity.TemplateVersion
		}
		if state.ToolVersions == "" {
			state.ToolVersions = in.Identity.ToolVersions
		}
	}
	if version >= 3 {
		state.EnableStatusEvents()
	} else if version >= 2 {
		state.EnableEvents()
	}
	if err := workflow.SetQueryHandler(ctx, QueryStatus, func() (domain.RunStatus, error) {
		return state.Status(), nil
	}); err != nil {
		return domain.Result{}, err
	}
	if err := workflow.SetQueryHandler(ctx, QueryEvents, func() ([]domain.StepEvent, error) {
		if len(state.Events) == 0 {
			return []domain.StepEvent{}, nil
		}
		return append([]domain.StepEvent(nil), state.Events...), nil
	}); err != nil {
		return domain.Result{}, err
	}
	if err := workflow.SetUpdateHandler(ctx, UpdateReview, func(ctx workflow.Context, message domain.ReviewMessage) (domain.ReviewDisposition, error) {
		return applyReview(ctx, state, message)
	}); err != nil {
		return domain.Result{}, err
	}

	for state.Phase != domain.PhaseBlocked && state.Phase != domain.PhaseComplete {
		var err error
		switch state.Phase {
		case domain.PhaseAssess:
			err = stepAssess(ctx, in, state)
		case domain.PhasePropose:
			err = stepPropose(ctx, in, state)
		case domain.PhaseValidate:
			err = stepValidate(ctx, in, state)
		case domain.PhaseReview:
			err = stepReview(ctx, in, state)
		case domain.PhaseHandoff:
			err = stepHandoff(ctx, in, state)
		default:
			return domain.Result{}, fmt.Errorf("unknown packaging phase %q", state.Phase)
		}
		if err != nil {
			// A cancellation is a terminal packaging-run outcome. Recording it
			// here does not cancel a Plan; the workflow returns the cancellation
			// so Temporal keeps that status, and the query shows it.
			if temporal.IsCanceledError(err) || ctx.Err() != nil {
				state.Block(domain.OutcomeCancelled, "run cancelled")
				state.NoteStatus(workflow.Now(ctx))
			}
			return domain.Result{}, err
		}
	}
	if state.Outcome == "" {
		state.Block(domain.OutcomeFailRequired, "run ended without an outcome")
		state.NoteStatus(workflow.Now(ctx))
	}
	return state.Result(in.CANCount), nil
}

func stepAssess(ctx workflow.Context, in Input, state *domain.State) error {
	var receipt domain.Receipt
	if err := workflow.ExecuteActivity(activityCtx(ctx, assessTimeout), ActAssess, in.Identity).Get(ctx, &receipt); err != nil {
		return noteActivity(ctx, state, domain.StepAssess, err)
	}
	if receipt.Outcome != domain.OutcomePass || receipt.SourceDigest != in.Identity.SourceDigest {
		state.Block(domain.OutcomeFailRequired, firstReason(receipt.Reason, "assessment did not match the run identity"))
		note(ctx, state, domain.StepAssess, string(state.Outcome), state.Reason, "")
		return nil
	}
	state.Assessment = receipt
	state.Phase = domain.PhasePropose
	note(ctx, state, domain.StepAssess, string(domain.OutcomePass), receipt.Reason, "")
	return checkpoint(ctx, in, state)
}

func stepPropose(ctx workflow.Context, in Input, state *domain.State) error {
	var receipt domain.Receipt
	if err := workflow.ExecuteActivity(activityCtx(ctx, proposeTimeout), ActPropose, in.Identity).Get(ctx, &receipt); err != nil {
		return noteActivity(ctx, state, domain.StepPropose, err)
	}
	if receipt.Outcome != domain.OutcomePass {
		state.Block(nonPass(receipt.Outcome), firstReason(receipt.Reason, "proposal failed"))
		note(ctx, state, domain.StepPropose, string(state.Outcome), state.Reason, "")
		return nil
	}
	*state = domain.ApplyProposal(*state, receipt)
	state.TargetPlanRef = in.Identity.TargetPlanRef
	state.Phase = domain.PhaseValidate
	note(ctx, state, domain.StepPropose, string(domain.OutcomePass), receipt.Reason, "")
	return checkpoint(ctx, in, state)
}

func stepValidate(ctx workflow.Context, in Input, state *domain.State) error {
	var validation domain.Validation
	if err := workflow.ExecuteActivity(activityCtx(ctx, validateTimeout), ActValidate, in.Identity, state.Proposal).Get(ctx, &validation); err != nil {
		return noteActivity(ctx, state, domain.StepValidate, err)
	}
	if validation.ArtifactDigest != state.Proposal.Artifact.Digest || validation.ProposalRevision != state.Proposal.ProposalRevision {
		state.Validation = domain.Validation{}
		state.Approval = domain.ApprovalRecord{}
		state.Block(domain.OutcomeFailRequired, "validation is not bound to this proposal")
		note(ctx, state, domain.StepValidate, string(state.Outcome), state.Reason, "")
		return nil
	}
	state.Validation = validation
	if validation.BlocksHandoff() {
		state.Block(nonPass(validation.Outcome), firstReason(validation.Reason, "validation blocked handoff"))
		note(ctx, state, domain.StepValidate, string(state.Outcome), state.Reason, "")
		return nil
	}
	state.Phase = domain.PhaseReview
	note(ctx, state, domain.StepValidate, string(domain.OutcomePass), validation.Reason, "")
	return checkpoint(ctx, in, state)
}

func stepReview(ctx workflow.Context, in Input, state *domain.State) error {
	wait := in.ApprovalWait
	if wait <= 0 {
		wait = defaultApprovalWait
	}
	note(ctx, state, domain.StepReview, domain.EventWaiting, "waiting for review", "")
	var timedOut bool
	workflow.Go(ctx, func(ctx workflow.Context) {
		channel := workflow.GetSignalChannel(ctx, SignalReview)
		for state.Phase == domain.PhaseReview && ctx.Err() == nil {
			var message domain.ReviewMessage
			if more := channel.Receive(ctx, &message); !more {
				return
			}
			if _, err := applyReview(ctx, state, message); err != nil {
				return
			}
			// A second message buffered in this task, or delivered on the next
			// turn, is classified before handoff. A duplicate must not start
			// another submission.
			_ = workflow.Sleep(ctx, time.Millisecond)
			for state.Phase == domain.PhaseReview || state.Phase == domain.PhaseHandoff {
				if !channel.ReceiveAsync(&message) {
					break
				}
				if _, err := applyReview(ctx, state, message); err != nil {
					return
				}
			}
			if state.Phase != domain.PhaseReview {
				return
			}
		}
	})
	workflow.Go(ctx, func(ctx workflow.Context) {
		if err := workflow.NewTimer(ctx, wait).Get(ctx, nil); err != nil {
			return
		}
		timedOut = true
	})
	if err := workflow.Await(ctx, func() bool {
		return state.Phase != domain.PhaseReview || timedOut
	}); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// An approval that becomes visible in the same decision as the wait
	// expiring does not hand off. A race is not a grant.
	if timedOut && state.Phase == domain.PhaseHandoff {
		state.Block(domain.OutcomeTimedOut, "approval arrived as the wait elapsed")
		note(ctx, state, domain.StepReview, string(state.Outcome), state.Reason, "")
		return nil
	}
	if state.Phase == domain.PhaseReview && timedOut {
		state.Block(domain.OutcomeTimedOut, "approval wait elapsed")
		note(ctx, state, domain.StepReview, string(state.Outcome), state.Reason, "")
	}
	return nil
}

func stepHandoff(ctx workflow.Context, in Input, state *domain.State) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := domain.HandoffPermitted(*state, in.Identity.TargetPlanRef); err != nil {
		state.Block(domain.OutcomeStaleApproval, err.Error())
		note(ctx, state, domain.StepHandoff, string(state.Outcome), state.Reason, "")
		return nil
	}
	var receipt domain.PlanReceipt
	err := workflow.ExecuteActivity(activityCtx(ctx, handoffTimeout), ActHandoff, handoffInput{
		RunID:            state.Proposal.RunID,
		ProposalRevision: state.Proposal.ProposalRevision,
		ArtifactDigest:   state.Proposal.Artifact.Digest,
		TargetPlanRef:    in.Identity.TargetPlanRef,
	}).Get(ctx, &receipt)
	if err != nil {
		return noteActivity(ctx, state, domain.StepHandoff, err)
	}
	if receipt.PlanID == "" {
		state.Block(domain.OutcomeFailRequired, "handoff returned no plan id")
		note(ctx, state, domain.StepHandoff, string(state.Outcome), state.Reason, "")
		return nil
	}
	state.Plan = receipt
	state.Phase = domain.PhaseComplete
	state.Outcome = domain.OutcomeHandoffRecorded
	state.Reason = "plan handoff recorded"
	note(ctx, state, domain.StepHandoff, string(state.Outcome), state.Reason, "")
	return nil
}

func applyReview(ctx workflow.Context, state *domain.State, message domain.ReviewMessage) (domain.ReviewDisposition, error) {
	if state.Phase == domain.PhaseHandoff {
		if message.ApprovalID != "" && containsID(state.SeenApprovalIDs, message.ApprovalID) {
			state.DuplicateReviews++
			state.LastReview = domain.ReviewDuplicate
			note(ctx, state, domain.StepApproval, string(domain.ReviewDuplicate), "duplicate review", message.ApprovalID)
			return domain.ReviewDuplicate, nil
		}
		note(ctx, state, domain.StepApproval, string(domain.ReviewRejected), "review arrived after handoff started", message.ApprovalID)
		return domain.ReviewRejected, nil
	}
	if state.Phase != domain.PhaseReview {
		return domain.ReviewRejected, nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if len(state.SeenApprovalIDs) >= domain.MaxSeenApprovals {
		state.Block(domain.OutcomeFailRequired, "too many review messages")
		state.NoteStatus(workflow.Now(ctx))
		return domain.ReviewRejected, nil
	}
	var record domain.ApprovalRecord
	if err := workflow.ExecuteActivity(activityCtx(ctx, approvalTimeout), ActReadApproval, message).Get(ctx, &record); err != nil {
		if temporal.IsCanceledError(err) || ctx.Err() != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", err
		}
		if noteErr := noteActivity(ctx, state, domain.StepApproval, err); noteErr != nil {
			return "", noteErr
		}
		return domain.ReviewRejected, nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	disposition := domain.ClassifyReview(state.SeenApprovalIDs, state.Binding(), message, record)
	state.LastReview = disposition
	switch disposition {
	case domain.ReviewDuplicate:
		state.DuplicateReviews++
	case domain.ReviewAccepted:
		state.SeenApprovalIDs = append(state.SeenApprovalIDs, message.ApprovalID)
		state.Approval = record
		state.Phase = domain.PhaseHandoff
	case domain.ReviewStale:
		state.Block(domain.OutcomeStaleApproval, "approval does not match this proposal")
	default:
		state.Block(domain.OutcomeRejected, "conduit has no matching approval")
	}
	outcome := string(disposition)
	if state.Outcome != "" && disposition != domain.ReviewAccepted && disposition != domain.ReviewDuplicate {
		outcome = string(state.Outcome)
	}
	note(ctx, state, domain.StepApproval, outcome, state.Reason, message.ApprovalID)
	return disposition, nil
}

func checkpoint(ctx workflow.Context, in Input, state *domain.State) error {
	if in.CANCount >= maxContinueAsNew {
		return nil
	}
	limit := in.ContinueAsNewAfterEvents
	if limit <= 0 {
		limit = defaultHistoryEvents
	}
	info := workflow.GetInfo(ctx)
	if info.GetCurrentHistoryLength() < limit && !info.GetContinueAsNewSuggested() {
		return nil
	}
	next := in
	next.CANCount++
	carried := *state
	next.Carry = &carried
	return workflow.NewContinueAsNewError(ctx, PackagingWorkflow, next)
}

func activityCtx(ctx workflow.Context, startToClose time.Duration) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: startToClose,
		HeartbeatTimeout:    heartbeatEvery,
		WaitForCancellation: true,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:        time.Second,
			BackoffCoefficient:     2,
			MaximumInterval:        10 * time.Second,
			MaximumAttempts:        3,
			NonRetryableErrorTypes: []string{"BadInput", "ExecutionControl", "UnsupportedWorkflowVersion"},
		},
	})
}

// noteActivity turns a finished activity failure into a blocked run. The
// error is returned only for cancellation, which the server has to record,
// and for continue-as-new, which is not an activity failure but shares the
// helper's call sites.
func note(ctx workflow.Context, state *domain.State, step, outcome, reason, approvalID string) {
	if !state.EventsEnabled() || len(state.Events) >= domain.MaxStepEvents {
		return
	}
	state.Note(workflow.Now(ctx), step, outcome, reason, approvalID)
}

func noteActivity(ctx workflow.Context, state *domain.State, step string, err error) error {
	if err == nil {
		return nil
	}
	if workflow.IsContinueAsNewError(err) {
		return err
	}
	if temporal.IsCanceledError(err) || ctx.Err() != nil {
		note(ctx, state, step, string(domain.OutcomeCancelled), "run cancelled", "")
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if temporal.IsTimeoutError(err) {
		state.Block(domain.OutcomeTimedOut, "activity timed out")
		note(ctx, state, step, string(state.Outcome), state.Reason, "")
		return nil
	}
	var app *temporal.ApplicationError
	if errors.As(err, &app) {
		state.Block(domain.OutcomeFailRequired, domain.TrimReason(app.Message()))
		note(ctx, state, step, string(state.Outcome), state.Reason, "")
		return nil
	}
	state.Block(domain.OutcomeFailRequired, domain.TrimReason(err.Error()))
	note(ctx, state, step, string(state.Outcome), state.Reason, "")
	return nil
}

func containsID(ids []string, id string) bool {
	for _, seen := range ids {
		if seen == id {
			return true
		}
	}
	return false
}

func nonPass(outcome domain.Outcome) domain.Outcome {
	if outcome == "" || outcome == domain.OutcomePass {
		return domain.OutcomeFailRequired
	}
	return outcome
}

func firstReason(reason, fallback string) string {
	if reason != "" {
		return reason
	}
	return fallback
}
