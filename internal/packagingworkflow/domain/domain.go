// Package domain is the packaging-run decision model. It has no Temporal,
// HTTP, filesystem or control-plane dependency: those are adapters. The
// functions here are the rules a workflow must apply, so a test can fail them
// without starting a server.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	// MaxReasonBytes is the longest diagnostic copied into workflow state.
	// The source file, the generated manifest and any credential stay outside
	// history; a reason is a short redacted sentence.
	MaxReasonBytes = 240
	// MaxIdentityBytes bounds every identity field that is allowed into history.
	MaxIdentityBytes = 256
	// MaxSeenApprovals bounds the duplicate-detection list carried across
	// continue-as-new. A review wait does not accumulate an unbounded log.
	MaxSeenApprovals = 32

	OutcomePass            Outcome = "PASS"
	OutcomeFailRequired    Outcome = "FAIL_REQUIRED"
	OutcomeUnknownSecurity Outcome = "UNKNOWN_SECURITY"
	OutcomeUnsupported     Outcome = "UNSUPPORTED"
	OutcomeTimedOut        Outcome = "TIMED_OUT"
	OutcomeCancelled       Outcome = "CANCELLED"
	OutcomeStaleApproval   Outcome = "STALE_APPROVAL"
	OutcomeRejected        Outcome = "REJECTED"
	OutcomeHandoffRecorded Outcome = "HANDOFF_RECORDED"

	DecisionApproved = "approved"
	DecisionRejected = "rejected"
	DecisionAbsent   = "absent"

	ReviewAccepted  ReviewDisposition = "accepted"
	ReviewDuplicate ReviewDisposition = "duplicate"
	ReviewStale     ReviewDisposition = "stale"
	ReviewRejected  ReviewDisposition = "rejected"

	PhaseAssess   = "assess"
	PhasePropose  = "propose"
	PhaseValidate = "validate"
	PhaseReview   = "review"
	PhaseHandoff  = "handoff"
	PhaseBlocked  = "blocked"
	PhaseComplete = "complete"
)

var (
	ErrIdentity  = errors.New("run identity is incomplete")
	ErrSensitive = errors.New("credential material is not allowed in packaging workflow state")
	ErrBound     = errors.New("value exceeds the workflow history bound")
)

type Outcome string

type ReviewDisposition string

// RunIdentity is the workflow's correlation key. ProposalRevision is empty
// until a proposal receipt exists; every other field is known before start
// and is bound into the run. Digests name bytes that are not themselves
// stored in history.
type RunIdentity struct {
	RunID            string `json:"runId"`
	ProposalRevision string `json:"proposalRevision,omitempty"`
	SourceDigest     string `json:"sourceDigest"`
	TemplateVersion  string `json:"templateVersion"`
	ToolVersions     string `json:"toolVersions"`
	TargetPlanRef    string `json:"targetPlanRef"`
}

func (id RunIdentity) Validate() error {
	if id.RunID == "" || id.SourceDigest == "" || id.TemplateVersion == "" || id.ToolVersions == "" || id.TargetPlanRef == "" {
		return ErrIdentity
	}
	if strings.Contains(id.RunID, "/") || strings.Contains(id.RunID, " ") {
		return fmt.Errorf("%w: run id must be a single token", ErrIdentity)
	}
	for _, field := range []string{id.RunID, id.ProposalRevision, id.SourceDigest, id.TemplateVersion, id.ToolVersions, id.TargetPlanRef} {
		if len(field) > MaxIdentityBytes {
			return fmt.Errorf("%w: identity field is longer than %d bytes", ErrBound, MaxIdentityBytes)
		}
		if err := RejectSensitive(field); err != nil {
			return err
		}
	}
	return nil
}

func (id RunIdentity) WorkflowID() string {
	return "packaging-" + id.RunID
}

// CanonicalTools renders tool name/version pairs in sorted order so a workflow
// input does not depend on map iteration. The workflow stores the string, not
// the map.
func CanonicalTools(versions map[string]string) (string, error) {
	if len(versions) == 0 {
		return "", fmt.Errorf("%w: tool versions are required", ErrIdentity)
	}
	keys := make([]string, 0, len(versions))
	for name := range versions {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, name := range keys {
		version := versions[name]
		if name == "" || version == "" || strings.ContainsAny(name, ",= ") || strings.ContainsAny(version, ",= ") {
			return "", fmt.Errorf("%w: tool version %q=%q is not a single token", ErrIdentity, name, version)
		}
		parts = append(parts, name+"="+version)
	}
	return strings.Join(parts, ","), nil
}

// RejectSensitive refuses credential markers before they can be copied into
// workflow history or a receipt. The error does not echo the value.
func RejectSensitive(value string) error {
	if strings.Contains(value, "BEGIN PRIVATE KEY") || strings.Contains(value, "BEGIN CERTIFICATE") || strings.Contains(value, "TEMPORAL_API_KEY=") {
		return ErrSensitive
	}
	return nil
}

type ArtifactRef struct {
	Digest   string `json:"digest"`
	ByteSize int    `json:"byteSize"`
	Media    string `json:"media,omitempty"`
}

// Receipt is the bounded record of one step. It names digests and a short
// reason. It is not the artifact.
type Receipt struct {
	Step             string      `json:"step"`
	RunID            string      `json:"runId"`
	ProposalRevision string      `json:"proposalRevision,omitempty"`
	SourceDigest     string      `json:"sourceDigest,omitempty"`
	TargetPlanRef    string      `json:"targetPlanRef,omitempty"`
	Artifact         ArtifactRef `json:"artifact"`
	Outcome          Outcome     `json:"outcome"`
	Reason           string      `json:"reason,omitempty"`
	IdempotencyKey   string      `json:"idempotencyKey,omitempty"`
}

func (r Receipt) Bound() error {
	if len(r.Reason) > MaxReasonBytes {
		return fmt.Errorf("%w: reason", ErrBound)
	}
	if err := RejectSensitive(r.Reason); err != nil {
		return err
	}
	return nil
}

// StepKey is the idempotency key for one step of one proposal identity.
// A model retry that produces a different artifact digest is a different key,
// so it cannot load an earlier validation or approval.
func StepKey(runID, step, revision, digest, generation string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{runID, step, revision, digest, generation}, "\x00")))
	return hex.EncodeToString(sum[:])
}

type Validation struct {
	Outcome          Outcome `json:"outcome"`
	ArtifactDigest   string  `json:"artifactDigest"`
	ProposalRevision string  `json:"proposalRevision"`
	Reason           string  `json:"reason,omitempty"`
}

// BlocksHandoff is true for every outcome that must not proceed, including
// the ones that would be tempting to retry into a pass.
func (v Validation) BlocksHandoff() bool {
	return v.Outcome != OutcomePass
}

// ApprovalRecord is a copy of a Conduit governance decision. The packaging
// workflow reads it. It does not create it, and a Temporal message is not a
// substitute for it.
type ApprovalRecord struct {
	ApprovalID       string `json:"approvalId"`
	RunID            string `json:"runId"`
	ProposalRevision string `json:"proposalRevision"`
	ArtifactDigest   string `json:"artifactDigest"`
	TargetPlanRef    string `json:"targetPlanRef"`
	Decision         string `json:"decision"`
}

// ReviewMessage is the Signal or Update payload. It correlates an approval
// record. It does not grant one.
type ReviewMessage struct {
	ApprovalID       string `json:"approvalId"`
	RunID            string `json:"runId"`
	ProposalRevision string `json:"proposalRevision"`
	ArtifactDigest   string `json:"artifactDigest"`
	TargetPlanRef    string `json:"targetPlanRef"`
}

type Binding struct {
	RunID            string
	ProposalRevision string
	ArtifactDigest   string
	TargetPlanRef    string
}

// ClassifyReview decides what a review message means given the governance
// record and the proposal the run currently holds. Duplicate and stale
// messages never become a handoff.
func ClassifyReview(seen []string, current Binding, message ReviewMessage, record ApprovalRecord) ReviewDisposition {
	if message.ApprovalID == "" || record.ApprovalID == "" || message.ApprovalID != record.ApprovalID {
		return ReviewRejected
	}
	for _, id := range seen {
		if id == message.ApprovalID {
			return ReviewDuplicate
		}
	}
	if record.Decision != DecisionApproved {
		return ReviewRejected
	}
	matches := record.RunID == current.RunID &&
		record.ProposalRevision == current.ProposalRevision &&
		record.ArtifactDigest == current.ArtifactDigest &&
		record.TargetPlanRef == current.TargetPlanRef &&
		message.RunID == current.RunID &&
		message.ProposalRevision == current.ProposalRevision &&
		message.ArtifactDigest == current.ArtifactDigest &&
		message.TargetPlanRef == current.TargetPlanRef &&
		current.ProposalRevision != "" &&
		current.ArtifactDigest != ""
	if !matches {
		return ReviewStale
	}
	return ReviewAccepted
}

// ApplyProposal installs a proposal receipt and drops any validation or
// approval that named a different artifact or revision. A model retry that
// returns a new proposal must not inherit the previous pass.
func ApplyProposal(state State, proposal Receipt) State {
	if state.Validation.ArtifactDigest != proposal.Artifact.Digest || state.Validation.ProposalRevision != proposal.ProposalRevision {
		state.Validation = Validation{}
		state.Approval = ApprovalRecord{}
	}
	state.Proposal = proposal
	return state
}

// HandoffPermitted is the last deterministic check before the handoff
// activity. The activity still reconciles with the Plan journal and rechecks
// execution controls; this function is what keeps a mismatched receipt from
// being treated as permission.
func HandoffPermitted(state State, target string) error {
	proposal := state.Proposal
	if proposal.Artifact.Digest == "" || proposal.ProposalRevision == "" {
		return errors.New("proposal identity is incomplete")
	}
	if state.Validation.BlocksHandoff() ||
		state.Validation.ArtifactDigest != proposal.Artifact.Digest ||
		state.Validation.ProposalRevision != proposal.ProposalRevision {
		return errors.New("validation does not apply to this proposal")
	}
	if state.Approval.Decision != DecisionApproved ||
		state.Approval.RunID != proposal.RunID ||
		state.Approval.ProposalRevision != proposal.ProposalRevision ||
		state.Approval.ArtifactDigest != proposal.Artifact.Digest ||
		state.Approval.TargetPlanRef != target {
		return errors.New("conduit approval does not match this proposal")
	}
	return nil
}

// PlanReceipt is the delivery kernel's acknowledgement of one idempotent
// handoff. Reconciled is true when this attempt found the plan already
// recorded and did not submit again.
type PlanReceipt struct {
	PlanID     string `json:"planId"`
	Key        string `json:"key"`
	Reconciled bool   `json:"reconciled"`
}

func HandoffKey(runID, revision, digest, target string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{runID, revision, digest, target}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func PlanID(key string) string {
	return "plan_" + key[:16]
}

// State is the bounded carry across continue-as-new. It is the packaging
// run's progress, not Plan status and not an approval log of record.
type State struct {
	Phase            string            `json:"phase"`
	Assessment       Receipt           `json:"assessment"`
	Proposal         Receipt           `json:"proposal"`
	Validation       Validation        `json:"validation"`
	Approval         ApprovalRecord    `json:"approval"`
	SeenApprovalIDs  []string          `json:"seenApprovalIds,omitempty"`
	Plan             PlanReceipt       `json:"plan"`
	LastReview       ReviewDisposition `json:"lastReview,omitempty"`
	DuplicateReviews int               `json:"duplicateReviews,omitempty"`
	Reason           string            `json:"reason,omitempty"`
	Outcome          Outcome           `json:"outcome,omitempty"`
	TargetPlanRef    string            `json:"targetPlanRef,omitempty"`
}

func (s *State) Block(outcome Outcome, reason string) {
	s.Outcome = outcome
	s.Reason = TrimReason(reason)
	s.Phase = PhaseBlocked
}

func (s State) Binding() Binding {
	return Binding{
		RunID:            s.Proposal.RunID,
		ProposalRevision: s.Proposal.ProposalRevision,
		ArtifactDigest:   s.Proposal.Artifact.Digest,
		TargetPlanRef:    s.TargetPlanRef,
	}
}

func (s State) Result(canCount int) Result {
	return Result{
		RunID:              firstNonEmpty(s.Proposal.RunID, s.Assessment.RunID),
		Phase:              s.Phase,
		Outcome:            s.Outcome,
		Reason:             s.Reason,
		SourceDigest:       s.Assessment.SourceDigest,
		ArtifactDigest:     s.Proposal.Artifact.Digest,
		ProposalRevision:   s.Proposal.ProposalRevision,
		TargetPlanRef:      s.TargetPlanRef,
		PlanID:             s.Plan.PlanID,
		HandoffReconciled:  s.Plan.Reconciled,
		LastReview:         s.LastReview,
		DuplicateReviews:   s.DuplicateReviews,
		ContinueAsNewCount: canCount,
	}
}

func (s State) Status() RunStatus {
	status := RunStatus{
		Tier:              "CONDUIT_NATIVE",
		Configured:        true,
		Authority:         "packaging-run",
		PlanAuthority:     "conduit-plan",
		ApprovalAuthority: "conduit-governance",
		RunID:             firstNonEmpty(s.Proposal.RunID, s.Assessment.RunID),
		Phase:             s.Phase,
		Outcome:           string(s.Outcome),
		Reason:            s.Reason,
		SourceDigest:      s.Assessment.SourceDigest,
		ArtifactDigest:    s.Proposal.Artifact.Digest,
		ProposalRevision:  s.Proposal.ProposalRevision,
		TargetPlanRef:     s.TargetPlanRef,
		PlanID:            s.Plan.PlanID,
		HandoffReconciled: s.Plan.Reconciled,
		LastReview:        string(s.LastReview),
		DuplicateReviews:  s.DuplicateReviews,
	}
	return status
}

// RunStatus is the read model served by the Hub. Authority fields are fixed
// by the HTTP adapter so a reader cannot relabel the run as an approval or a
// Plan. Plan status is read from the existing Plan route using PlanID.
type RunStatus struct {
	Tier              string `json:"tier"`
	Configured        bool   `json:"configured"`
	Authority         string `json:"authority"`
	PlanAuthority     string `json:"planAuthority"`
	ApprovalAuthority string `json:"approvalAuthority"`
	RunID             string `json:"runId,omitempty"`
	Phase             string `json:"phase,omitempty"`
	Outcome           string `json:"outcome,omitempty"`
	Reason            string `json:"reason,omitempty"`
	SourceDigest      string `json:"sourceDigest,omitempty"`
	ArtifactDigest    string `json:"artifactDigest,omitempty"`
	ProposalRevision  string `json:"proposalRevision,omitempty"`
	TargetPlanRef     string `json:"targetPlanRef,omitempty"`
	PlanID            string `json:"planId,omitempty"`
	HandoffReconciled bool   `json:"handoffReconciled,omitempty"`
	LastReview        string `json:"lastReview,omitempty"`
	DuplicateReviews  int    `json:"duplicateReviews,omitempty"`
}

type Result struct {
	RunID              string            `json:"runId"`
	Phase              string            `json:"phase"`
	Outcome            Outcome           `json:"outcome"`
	Reason             string            `json:"reason,omitempty"`
	SourceDigest       string            `json:"sourceDigest,omitempty"`
	ArtifactDigest     string            `json:"artifactDigest,omitempty"`
	ProposalRevision   string            `json:"proposalRevision,omitempty"`
	TargetPlanRef      string            `json:"targetPlanRef,omitempty"`
	PlanID             string            `json:"planId,omitempty"`
	HandoffReconciled  bool              `json:"handoffReconciled,omitempty"`
	LastReview         ReviewDisposition `json:"lastReview,omitempty"`
	DuplicateReviews   int               `json:"duplicateReviews,omitempty"`
	ContinueAsNewCount int               `json:"continueAsNewCount,omitempty"`
}

func TrimReason(reason string) string {
	if len(reason) <= MaxReasonBytes {
		return reason
	}
	return reason[:MaxReasonBytes]
}

func DigestBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
