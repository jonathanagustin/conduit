package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

var ErrRunNotFound = errors.New("packaging run not found")

const (
	ControlCancel = "packaging-run.cancel"
	ControlRerun  = "packaging-run.rerun"
)

type RunReader interface {
	Read(context.Context, string) (domain.RunStatus, error)
}
type Controls interface {
	RunReader
	Configured() bool
	Cancel(context.Context, string) error
	Start(context.Context, domain.RunIdentity) (domain.RunStatus, error)
}

type DecisionRecord struct {
	Subject, Action, RunID, Verdict, Reason, DecisionID             string
	SourceDigest, ProposalRevision, TargetPlanRef, PlanID, NewRunID string
	At                                                              time.Time
}
type DecisionLog interface {
	Lookup(context.Context, string, string) (DecisionRecord, bool, error)
	Append(context.Context, DecisionRecord) error
}

// ControlBinding is the reviewed run identity, never a Plan approval.
type ControlBinding struct {
	DecisionID       string `json:"decisionId"`
	SourceDigest     string `json:"sourceDigest"`
	ProposalRevision string `json:"proposalRevision"`
	TargetPlanRef    string `json:"targetPlanRef"`
	TemplateVersion  string `json:"templateVersion"`
	ToolVersions     string `json:"toolVersions"`
}

func (body ControlBinding) Complete(rerun bool) bool {
	return strings.TrimSpace(body.DecisionID) != "" && !strings.Contains(body.DecisionID, "/") && len(body.DecisionID) <= domain.MaxIdentityBytes &&
		body.SourceDigest != "" && body.TargetPlanRef != "" && domain.EnvironmentOf(body.TargetPlanRef) != "" &&
		(!rerun || (body.TemplateVersion != "" && body.ToolVersions != "")) &&
		domain.RejectSensitive(body.DecisionID) == nil && domain.RejectSensitive(body.SourceDigest) == nil
}

func (body ControlBinding) Matches(status domain.RunStatus) bool {
	return status.SourceDigest == body.SourceDigest && status.ProposalRevision == body.ProposalRevision && status.TargetPlanRef == body.TargetPlanRef
}

type ControlFailure struct{ Kind, Reason, PlanID string }

func (failure ControlFailure) Error() string { return failure.Reason }

type ControlResult struct {
	domain.RunStatus
	PriorRunID string
	PlanID     string
	// Accepted acknowledges the command, not completion of cancellation.
	Accepted bool
	Outcome  string
}

// ControlService is shared by HTTP controls and reviewed assistant actions.
// Permit resolves the original credential carrier at the composition boundary;
// neither a model-supplied actor nor a role-name list is execution authority.
type ControlService struct {
	Reader    RunReader
	Worker    Controls
	Decisions DecisionLog
	Permit    func(context.Context, string) (string, error)
}

// Execute preserves the existing bound/audited worker command. Resume is only
// for an already-approved, durably claimed assistant operation: HTTP controls
// keep refusing duplicate decision IDs. Cancel requests can safely be repeated;
// reruns keep one deterministic workflow ID and re-observe it after a lost ack.
func (service ControlService) Execute(ctx context.Context, runID, action string, body ControlBinding, resume bool) (ControlResult, error) {
	fail := func(kind, reason, plan string) (ControlResult, error) {
		return ControlResult{}, ControlFailure{kind, reason, plan}
	}
	if service.Permit == nil {
		return fail("forbidden", "forbidden", "")
	}
	subject, err := service.Permit(ctx, domain.EnvironmentOf(body.TargetPlanRef))
	if err != nil || strings.TrimSpace(subject) == "" {
		return fail("forbidden", "forbidden", "")
	}
	if service.Decisions == nil {
		return fail("conflict", "packaging decision log is not configured", "")
	}
	newID := ""
	if action == ControlRerun {
		newID = ControlRunID(body.DecisionID, runID)
	}
	record := func(verdict, reason, plan string) error {
		return service.Decisions.Append(ctx, DecisionRecord{Subject: subject, Action: action, RunID: runID, Verdict: verdict, Reason: reason,
			DecisionID: body.DecisionID, SourceDigest: body.SourceDigest, ProposalRevision: body.ProposalRevision, TargetPlanRef: body.TargetPlanRef,
			PlanID: plan, NewRunID: newID, At: time.Now().UTC()})
	}
	refuse := func(kind, reason, plan string) (ControlResult, error) {
		if err := record("DENY", reason, plan); err != nil {
			return fail("unavailable", "packaging decision log is unavailable", "")
		}
		return fail(kind, reason, plan)
	}
	if (action != ControlCancel && action != ControlRerun) || !body.Complete(action == ControlRerun) {
		return refuse("conflict", "decision binding is incomplete", "")
	}
	prior, found, err := service.Decisions.Lookup(ctx, runID, body.DecisionID)
	if err != nil {
		return fail("unavailable", "packaging decision log is unavailable", "")
	}
	if found {
		if prior.Subject != subject {
			return refuse("forbidden", "decision belongs to another operator", "")
		}
		if !resume || prior.Verdict != "ALLOW" || prior.Action != action || prior.SourceDigest != body.SourceDigest ||
			prior.ProposalRevision != body.ProposalRevision || prior.TargetPlanRef != body.TargetPlanRef {
			return fail("conflict", "duplicate decision", "")
		}
	}
	if service.Worker == nil || !service.Worker.Configured() || service.Reader == nil {
		return refuse("conflict", "packaging worker is not configured", "")
	}
	status, err := service.Reader.Read(ctx, runID)
	if errors.Is(err, ErrRunNotFound) {
		return fail("missing", ErrRunNotFound.Error(), "")
	}
	if err != nil {
		return fail("unavailable", "packaging run status is unavailable", "")
	}
	if !body.Matches(status) {
		return refuse("conflict", "decision does not match the current run", status.PlanID)
	}
	if action == ControlCancel {
		if status.PlanID != "" || status.Phase == domain.PhaseComplete {
			return refuse("conflict", "packaging run already handed off a plan", status.PlanID)
		}
		if status.Outcome == string(domain.OutcomeCancelled) && resume {
			return ControlResult{RunStatus: status, Accepted: true, Outcome: "CANCELLATION_OBSERVED"}, nil
		}
		if status.Phase == domain.PhaseBlocked || status.Outcome == string(domain.OutcomeCancelled) {
			return refuse("conflict", "packaging run is already stopped", status.PlanID)
		}
		if !found {
			if err := record("ALLOW", "packaging run cancel authorized", status.PlanID); err != nil {
				return fail("unavailable", "packaging decision log is unavailable", "")
			}
		}
		if err := service.Worker.Cancel(ctx, runID); err != nil {
			if errors.Is(err, ErrRunNotFound) {
				return fail("missing", ErrRunNotFound.Error(), "")
			}
			return fail("unavailable", "packaging run cancel is unavailable", "")
		}
		updated, err := service.Reader.Read(ctx, runID)
		if err != nil {
			return fail("unavailable", "packaging run status is unavailable", "")
		}
		outcome := "CANCEL_REQUEST_ACCEPTED"
		if updated.Outcome == string(domain.OutcomeCancelled) {
			outcome = "CANCELLATION_OBSERVED"
		}
		return ControlResult{RunStatus: updated, PlanID: status.PlanID, Accepted: true, Outcome: outcome}, nil
	}
	identity := domain.RunIdentity{RunID: newID, SourceDigest: body.SourceDigest, TemplateVersion: body.TemplateVersion, ToolVersions: body.ToolVersions, TargetPlanRef: body.TargetPlanRef}
	if err := identity.Validate(); err != nil {
		return refuse("conflict", "rerun binding is not a packaging run identity", status.PlanID)
	}
	observeSuccessor := func() (domain.RunStatus, bool) {
		observed, err := service.Reader.Read(ctx, newID)
		return observed, err == nil && observed.RunID == newID && observed.SourceDigest == identity.SourceDigest &&
			observed.TargetPlanRef == identity.TargetPlanRef && observed.TemplateVersion == identity.TemplateVersion && observed.ToolVersions == identity.ToolVersions
	}
	if found && resume {
		if observed, ok := observeSuccessor(); ok {
			return ControlResult{RunStatus: observed, PriorRunID: runID, Accepted: true, Outcome: "RUN_OBSERVED"}, nil
		}
	}
	if !found {
		if err := record("ALLOW", "packaging run rerun authorized", status.PlanID); err != nil {
			return fail("unavailable", "packaging decision log is unavailable", "")
		}
	}
	started, err := service.Worker.Start(ctx, identity)
	if err != nil {
		if resume {
			if observed, ok := observeSuccessor(); ok {
				return ControlResult{RunStatus: observed, PriorRunID: runID, Accepted: true, Outcome: "RUN_OBSERVED"}, nil
			}
		}
		return fail("unavailable", "packaging run rerun is unavailable", "")
	}
	if started.RunID == "" {
		started.RunID = newID
	}
	started.ProposalRevision, started.SourceDigest, started.TargetPlanRef, started.Phase = "", identity.SourceDigest, identity.TargetPlanRef, domain.PhaseAssess
	return ControlResult{RunStatus: started, PriorRunID: runID, Accepted: true, Outcome: "RUN_STARTED"}, nil
}

func ControlRunID(decisionID, runID string) string {
	sum := sha256.Sum256([]byte(decisionID + "\x00" + runID))
	return "run_" + hex.EncodeToString(sum[:8])
}
