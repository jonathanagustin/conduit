package composition

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	assistant "github.com/jonathanagustin/conduit/internal/assistant/application"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	ontology "github.com/jonathanagustin/conduit/internal/ontology/domain"
	packaging "github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

const assistantBindingVersion = "conduit.assistant-action/v1"

// assistantActionBinding records executable intent without retaining credentials.
// CONDUIT-INVENTED: assistant.BoundPackagingActions
// Displaced-by: a published or authorized sanitized assistant action execution contract covering run binding, approval, command receipts and recovery.
type assistantActionBinding struct {
	Version      string                   `json:"version"`
	Tier         string                   `json:"tier"`
	Action       string                   `json:"action"`
	RunID        string                   `json:"runId"`
	Environment  string                   `json:"environment"`
	Installation string                   `json:"installation"`
	Product      string                   `json:"product"`
	Binding      packaging.ControlBinding `json:"binding"`
}

type assistantActions struct {
	repository controlplane.Repository
	options    controlplane.HandlerOptions
	hub        Options
}

func withAssistantActions(repository controlplane.Repository, options controlplane.HandlerOptions, hub Options) controlplane.HandlerOptions {
	previous := options.AfterChangeApproved
	actions := assistantActions{repository: repository, options: options, hub: hub}
	options.AfterChangeApproved = func(ctx context.Context, request controlplane.ChangeRequest) (controlplane.ChangeRequest, error) {
		if request.TargetKind != controlplane.AssistantActionTarget {
			if previous != nil {
				return previous(ctx, request)
			}
			return request, nil
		}
		return actions.execute(ctx, request)
	}
	return options
}

func (actions assistantActions) target(ctx context.Context, runID, environment, installation, product string) (domain.RunStatus, error) {
	if actions.hub.PackagingStatus == nil || actions.hub.PackagingControls == nil || !actions.hub.PackagingControls.Configured() {
		return domain.RunStatus{}, fmt.Errorf("packaging worker is not configured")
	}
	run, err := actions.hub.PackagingStatus.Read(ctx, runID)
	if err != nil {
		return domain.RunStatus{}, err
	}
	if run.RunID != runID || run.TargetPlanRef != environment+"/"+installation || domain.EnvironmentOf(run.TargetPlanRef) != environment {
		return domain.RunStatus{}, fmt.Errorf("packaging run does not name the requested installation")
	}
	fleet, err := controlplane.BuildFleetProjection(ctx, actions.repository, controlplane.FleetQuery{Environment: environment}, time.Now())
	if err != nil {
		return domain.RunStatus{}, err
	}
	for _, row := range fleet.Installations {
		if row.Environment == environment && row.InstallationID == installation && row.Product == product && row.DesiredVersion != "" {
			return run, nil
		}
	}
	return domain.RunStatus{}, fmt.Errorf("installation and Product are not in current desired fleet state")
}

// A proposal is not a standing credential. Re-resolve server-owned membership
// rather than replaying Groups/Teams copied from a token at proposal time.
// Group-only and delegated authors are refused for unattended dispatch; they
// can use the existing directly authenticated packaging controls instead.
func (actions assistantActions) author(ctx context.Context, original controlplane.Principal, environment string) (controlplane.Principal, error) {
	if original.ServiceAccount || original.OnBehalfOf != nil {
		return controlplane.Principal{}, controlplane.ErrForbidden
	}
	service := controlplane.NewGovernanceService(actions.repository)
	current, err := service.ResolvePrincipal(ctx, controlplane.IdentityClaims{Username: original.Username})
	if err != nil {
		return current, err
	}
	if !service.AuthorizeResourceOperation(ctx, current, actions.options, "environment/"+environment, controlplane.OperationOperate) ||
		!service.AuthorizeResourceOperation(ctx, current, actions.options, "environment/"+environment, controlplane.OperationProposeChange) {
		return current, controlplane.ErrForbidden
	}
	return current, nil
}

func (actions assistantActions) open(ctx context.Context, service *controlplane.GovernanceService, proposal assistant.Proposal) (assistant.OpenedChange, error) {
	principal, err := operatorFrom(ctx)
	if err != nil || principal.Username != proposal.Author {
		return assistant.OpenedChange{}, controlplane.ErrForbidden
	}
	if proposal.ObjectType != "PackagingRun" && proposal.ObjectType != "ChangeRequest" {
		return assistant.OpenedChange{}, controlplane.ErrInvalidTransition
	}
	if proposal.ActionID != "cancel_packaging_run" && proposal.ActionID != "rerun_packaging_run" {
		return assistant.OpenedChange{}, controlplane.ErrInvalidTransition
	}
	if !service.AuthorizeResourceOperation(ctx, principal, actions.options, "environment/"+proposal.Environment, controlplane.OperationProposeChange) {
		return assistant.OpenedChange{}, controlplane.ErrForbidden
	}
	currentAuthor, err := actions.author(ctx, principal, proposal.Environment)
	if err != nil {
		return assistant.OpenedChange{}, err
	}
	run, err := actions.target(ctx, proposal.TargetEntityID, proposal.Environment, proposal.InstallationID, proposal.ProductID)
	if err != nil {
		return assistant.OpenedChange{}, err
	}
	facts := assistantPackagingFacts(run)
	facts.Roles = currentAuthor.Teams
	if decision := ontology.Decide(proposal.ActionID, facts); !decision.Known || !decision.Allowed {
		return assistant.OpenedChange{}, fmt.Errorf("action rule refused proposal: %s", decision.Reason)
	}
	bound := assistantActionBinding{Version: assistantBindingVersion, Tier: "CONDUIT_NATIVE", Action: proposal.ActionID, RunID: run.RunID, Environment: proposal.Environment, Installation: proposal.InstallationID, Product: proposal.ProductID,
		Binding: packaging.ControlBinding{SourceDigest: run.SourceDigest, ProposalRevision: run.ProposalRevision, TargetPlanRef: run.TargetPlanRef, TemplateVersion: run.TemplateVersion, ToolVersions: run.ToolVersions},
	}
	// Identity includes every reviewed coordinate and the author. A same-bound
	// re-proposal is idempotent; a changed source is a different review.
	identity, _ := json.Marshal(struct {
		Author string
		Bound  assistantActionBinding
	}{principal.Username, bound})
	bound.Binding.DecisionID = "assistant-" + assistantDigest(identity)
	if !bound.Binding.Complete(bound.Action == "rerun_packaging_run") {
		return assistant.OpenedChange{}, fmt.Errorf("packaging run has no complete execution binding")
	}
	request, err := service.ProposeHumanChange(ctx, controlplane.AssistantActionTarget, bound.Binding.DecisionID, bound.Environment, []controlplane.EntityChange{{
		ID: "assistant-packaging-action", Type: controlplane.ChangeTypeUpdateEntity, InstallationID: bound.Installation,
		Request:             map[string]any{"assistantAction": bound},
		Diff:                map[string]controlplane.ChangeDiffEntry{"action": {After: bound.Action}, "run": {After: bound.RunID}, "sourceDigest": {After: bound.Binding.SourceDigest}, "proposalRevision": {After: bound.Binding.ProposalRevision}},
		ReferencedResources: map[string]controlplane.ReferencedResource{"environment": {RID: "environment/" + bound.Environment, Name: bound.Environment}, "installation": {RID: "installation/" + bound.Environment + "/" + bound.Installation, Name: bound.Installation}, "product": {RID: "product/" + bound.Product, Name: bound.Product}},
	}}, principal, "Proposed "+bound.Action+" on "+bound.RunID, proposal.Reason)
	return assistant.OpenedChange{ID: request.ID, Status: request.Status}, err
}

func assistantDigest(raw []byte) string { return fmt.Sprintf("%x", sha256.Sum256(raw))[:32] }

func assistantPackagingFacts(run domain.RunStatus) ontology.ActionFacts {
	phase := strings.ToUpper(run.Phase)
	switch run.Phase {
	case domain.PhaseAssess, domain.PhasePropose, domain.PhaseValidate, domain.PhaseHandoff:
		phase = "RUNNING"
	case domain.PhaseReview:
		phase = "STAGED"
	}
	return ontology.ActionFacts{RunKnown: run.RunID != "", RunPhase: phase}
}

func (actions assistantActions) execute(ctx context.Context, snapshot controlplane.ChangeRequest) (controlplane.ChangeRequest, error) {
	current, err := actions.repository.GetChangeRequest(ctx, snapshot.ID)
	if err != nil {
		return current, err
	}
	if current.TargetKind != controlplane.AssistantActionTarget || current.Status != controlplane.ChangeApproved || !current.RequirementsSatisfied || current.Revision != snapshot.Revision || len(current.Changes) != 1 {
		return current, controlplane.ErrConflict
	}
	for _, label := range current.Labels {
		if label == "Do not merge" {
			return current, controlplane.ErrConflict
		}
	}
	if current.Execution != nil && current.Execution.Status == controlplane.ExecutionSucceeded {
		return current, nil
	}
	var bound assistantActionBinding
	raw, err := json.Marshal(current.Changes[0].Request["assistantAction"])
	if err != nil || json.Unmarshal(raw, &bound) != nil || bound.Version != assistantBindingVersion || bound.Tier != "CONDUIT_NATIVE" || bound.Binding.DecisionID != current.TargetID || bound.Environment != current.Environment || bound.Installation != current.Changes[0].InstallationID {
		return current, controlplane.ErrConflict
	}
	identity := bound
	identity.Binding.DecisionID = ""
	identityBytes, _ := json.Marshal(struct {
		Author string
		Bound  assistantActionBinding
	}{current.Author.Username, identity})
	if bound.Binding.DecisionID != "assistant-"+assistantDigest(identityBytes) {
		return current, controlplane.ErrConflict
	}
	if bound.Action != "cancel_packaging_run" && bound.Action != "rerun_packaging_run" {
		return current, controlplane.ErrInvalidTransition
	}
	// A durable RUNNING claim is deliberately visible after a process death.
	// A repeat approval by a recorded reviewer resumes the SAME decision and
	// deterministic successor ID; it cannot approve a different source.
	current, err = actions.repository.UpdateChangeRequest(ctx, current.ID, func(record *controlplane.ChangeRequest) error {
		if record.Status != controlplane.ChangeApproved || record.Revision != snapshot.Revision {
			return controlplane.ErrConflict
		}
		if record.Execution != nil && record.Execution.Status == controlplane.ExecutionSucceeded {
			return controlplane.ErrConflict
		}
		if record.Execution == nil {
			record.Execution = &controlplane.ChangeExecution{StartedAt: time.Now().UTC()}
		}
		record.Execution.Status = controlplane.ExecutionRunning
		record.UpdatedAt = time.Now().UTC()
		return nil
	})
	if err != nil {
		return current, err
	}
	run, dispatchErr := actions.target(ctx, bound.RunID, bound.Environment, bound.Installation, bound.Product)
	if dispatchErr == nil && (!bound.Binding.Matches(run) || bound.Binding.TemplateVersion != run.TemplateVersion || bound.Binding.ToolVersions != run.ToolVersions) {
		dispatchErr = controlplane.ErrConflict
	}
	if dispatchErr == nil {
		author, authorErr := actions.author(ctx, current.Author, bound.Environment)
		dispatchErr = authorErr
		if dispatchErr == nil {
			facts := assistantPackagingFacts(run)
			facts.Roles = author.Teams
			decision := ontology.Decide(bound.Action, facts)
			// An acknowledged cancel may already be observed terminal while a
			// receipt is being recovered; no new termination is being authorized.
			if (!decision.Known || !decision.Allowed) && !(bound.Action == "cancel_packaging_run" && run.Outcome == string(domain.OutcomeCancelled)) {
				dispatchErr = fmt.Errorf("action rule refused dispatch: %s", decision.Reason)
			}
		}
	}
	var result packaging.ControlResult
	if dispatchErr == nil {
		service := packaging.ControlService{Reader: actions.hub.PackagingStatus, Worker: actions.hub.PackagingControls, Decisions: packagingDecisions{repository: actions.repository}, Permit: func(ctx context.Context, environment string) (string, error) {
			author, err := actions.author(ctx, current.Author, environment)
			return author.Username, err
		}}
		action := packaging.ControlCancel
		if bound.Action == "rerun_packaging_run" {
			action = packaging.ControlRerun
		}
		result, dispatchErr = service.Execute(ctx, bound.RunID, action, bound.Binding, true)
	}
	// A disconnected caller cannot erase an accepted command's receipt. A
	// bounded independent context records the acknowledgement when possible;
	// failed persistence leaves RUNNING for explicit same-decision recovery.
	receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var canonical *controlplane.ChangeRequest
	updated, persistErr := actions.repository.UpdateChangeRequest(receiptCtx, current.ID, func(record *controlplane.ChangeRequest) error {
		if !sameAssistantAction(*record, current, bound) {
			return controlplane.ErrConflict
		}
		// A retry can acknowledge the same command while this attempt is still
		// waiting. Its saved success is authoritative, including for this caller;
		// neither a late error nor a second acknowledgement may replace it.
		if record.Execution != nil && record.Execution.Status == controlplane.ExecutionSucceeded {
			canonical = record
			return controlplane.ErrConflict // No write or revision increment.
		}
		// An acknowledgement describes an effect of this exact bound command,
		// even if another resume has since claimed or failed it. A failed stale
		// attempt, however, must not overwrite a newer attempt's receipt.
		if dispatchErr != nil && record.Revision != current.Revision {
			return controlplane.ErrConflict
		}
		item := controlplane.ChangeExecutionItem{ChangeID: record.Changes[0].ID, ResourceRID: "packaging-run/" + bound.RunID, Status: controlplane.ExecutionSucceeded, ResultID: result.RunID, CommandOutcome: result.Outcome, ObservedState: result.Phase}
		if dispatchErr != nil {
			item.Status, item.Error = controlplane.ExecutionFailed, dispatchErr.Error()
		}
		record.Execution.Status = item.Status
		record.Execution.Items = []controlplane.ChangeExecutionItem{item}
		record.Execution.CompletedAt = time.Now().UTC()
		record.UpdatedAt = time.Now().UTC()
		return nil
	})
	if canonical != nil {
		return *canonical, nil
	}
	if persistErr != nil {
		// A storage conflict can occur after a different attempt has committed
		// success. Return that receipt instead of stale RUNNING plus an error.
		if latest, readErr := actions.repository.GetChangeRequest(receiptCtx, current.ID); readErr == nil && sameAssistantAction(latest, current, bound) {
			if latest.Execution != nil && latest.Execution.Status == controlplane.ExecutionSucceeded {
				return latest, nil
			}
			return latest, errors.Join(dispatchErr, persistErr)
		}
		return current, errors.Join(dispatchErr, persistErr)
	}
	return updated, dispatchErr
}

func sameAssistantAction(record, claimed controlplane.ChangeRequest, bound assistantActionBinding) bool {
	if record.Status != controlplane.ChangeApproved || !record.RequirementsSatisfied || record.Execution == nil || record.TargetKind != controlplane.AssistantActionTarget || record.TargetID != claimed.TargetID || record.Environment != claimed.Environment || record.Author.Username != claimed.Author.Username || len(record.Changes) != 1 || record.Changes[0].ID != claimed.Changes[0].ID || record.Changes[0].InstallationID != bound.Installation {
		return false
	}
	for _, label := range record.Labels {
		if label == "Do not merge" {
			return false
		}
	}
	var actual assistantActionBinding
	raw, err := json.Marshal(record.Changes[0].Request["assistantAction"])
	return err == nil && json.Unmarshal(raw, &actual) == nil && actual == bound
}

type assistantReceiptReader struct{ repository controlplane.Repository }

func (reader assistantReceiptReader) ReadExecution(ctx context.Context, id string) (*assistant.ExecutionReceipt, error) {
	request, err := reader.repository.GetChangeRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	if request.Execution == nil {
		return nil, nil
	}
	receipt := &assistant.ExecutionReceipt{State: request.Execution.Status}
	if len(request.Execution.Items) > 0 {
		item := request.Execution.Items[0]
		receipt.Outcome, receipt.RunID, receipt.ObservedPhase, receipt.Error = item.CommandOutcome, item.ResultID, item.ObservedState, item.Error
	}
	return receipt, nil
}
