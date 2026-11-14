package domain

import (
	"strings"
	"testing"
)

func TestStatusCarriesTheRunTemplateAndTools(t *testing.T) {
	state := State{
		Phase: "assess", TargetPlanRef: "env-dev/sample-app",
		TemplateVersion: "fixture-1", ToolVersions: "lint=fixture-1",
	}
	status := state.Status()
	if status.TemplateVersion != "fixture-1" || status.ToolVersions != "lint=fixture-1" {
		t.Fatalf("status = %#v", status)
	}
}

func sampleIdentity() RunIdentity {
	return RunIdentity{
		RunID:           "sample-app-1",
		SourceDigest:    "sha256:abc",
		TemplateVersion: "fixture-1",
		ToolVersions:    "lint=fixture-1,render=fixture-1",
		TargetPlanRef:   "env-dev/sample-app",
	}
}

func accepted(binding Binding) (ReviewMessage, ApprovalRecord) {
	message := ReviewMessage{
		ApprovalID: "appr-1", RunID: binding.RunID, ProposalRevision: binding.ProposalRevision,
		ArtifactDigest: binding.ArtifactDigest, TargetPlanRef: binding.TargetPlanRef,
	}
	record := ApprovalRecord{
		ApprovalID: message.ApprovalID, RunID: message.RunID, ProposalRevision: message.ProposalRevision,
		ArtifactDigest: message.ArtifactDigest, TargetPlanRef: message.TargetPlanRef, Decision: DecisionApproved,
	}
	return message, record
}

func TestCanonicalToolsIsIndependentOfMapOrder(t *testing.T) {
	left, err := CanonicalTools(map[string]string{"render": "fixture-1", "lint": "fixture-1"})
	if err != nil {
		t.Fatal(err)
	}
	right, err := CanonicalTools(map[string]string{"lint": "fixture-1", "render": "fixture-1"})
	if err != nil {
		t.Fatal(err)
	}
	if left != right || left != "lint=fixture-1,render=fixture-1" {
		t.Fatalf("canonical tools = %q and %q", left, right)
	}
}

func TestRejectSensitiveDoesNotEchoTheValue(t *testing.T) {
	err := RejectSensitive("-----BEGIN PRIVATE KEY-----\nMIISECRET\n")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if err.Error() == "" || strings.Contains(err.Error(), "MIISECRET") {
		t.Fatalf("error = %q", err)
	}
}

func TestClassifyReviewRejectsSignalThatGovernanceDoesNotHold(t *testing.T) {
	binding := Binding{RunID: "sample-app-1", ProposalRevision: "rev-1", ArtifactDigest: "sha256:aaa", TargetPlanRef: "env-dev/sample-app"}
	message, _ := accepted(binding)
	record := ApprovalRecord{ApprovalID: message.ApprovalID, Decision: DecisionAbsent}
	if got := ClassifyReview(nil, binding, message, record); got != ReviewRejected {
		t.Fatalf("disposition = %s", got)
	}
}

func TestClassifyReviewRejectsStaleArtifactAndDuplicate(t *testing.T) {
	binding := Binding{RunID: "sample-app-1", ProposalRevision: "rev-1", ArtifactDigest: "sha256:aaa", TargetPlanRef: "env-dev/sample-app"}
	message, record := accepted(binding)
	if got := ClassifyReview(nil, binding, message, record); got != ReviewAccepted {
		t.Fatalf("disposition = %s", got)
	}
	message.ArtifactDigest = "sha256:bbb"
	if got := ClassifyReview(nil, binding, message, record); got != ReviewStale {
		t.Fatalf("changed message digest disposition = %s", got)
	}
	record.ArtifactDigest = "sha256:ccc"
	message.ArtifactDigest = "sha256:ccc"
	if got := ClassifyReview(nil, binding, message, record); got != ReviewStale {
		t.Fatalf("changed record digest disposition = %s, want stale against the current proposal", got)
	}
	message, record = accepted(binding)
	if got := ClassifyReview([]string{message.ApprovalID}, binding, message, record); got != ReviewDuplicate {
		t.Fatalf("duplicate disposition = %s", got)
	}
}

func TestApplyProposalDropsInheritedValidation(t *testing.T) {
	state := State{
		Validation: Validation{Outcome: OutcomePass, ArtifactDigest: "sha256:aaa", ProposalRevision: "rev-1"},
		Approval:   ApprovalRecord{Decision: DecisionApproved, ArtifactDigest: "sha256:aaa", ProposalRevision: "rev-1"},
	}
	next := ApplyProposal(state, Receipt{
		RunID: "sample-app-1", ProposalRevision: "rev-2", Outcome: OutcomePass,
		Artifact: ArtifactRef{Digest: "sha256:bbb"},
	})
	if next.Validation.Outcome != "" || next.Approval.Decision != "" {
		t.Fatalf("inherited validation or approval survived a new proposal: %#v", next)
	}
	if next.Proposal.Artifact.Digest != "sha256:bbb" {
		t.Fatalf("proposal = %#v", next.Proposal)
	}
	kept := ApplyProposal(state, Receipt{
		ProposalRevision: "rev-1", Artifact: ArtifactRef{Digest: "sha256:aaa"}, Outcome: OutcomePass,
	})
	if kept.Validation.Outcome != OutcomePass {
		t.Fatal("matching proposal cleared its own validation")
	}
}

func TestHandoffPermittedRequiresMatchingPassAndApproval(t *testing.T) {
	identity := sampleIdentity()
	state := State{
		TargetPlanRef: identity.TargetPlanRef,
		Proposal: Receipt{
			RunID: identity.RunID, ProposalRevision: "rev-1", TargetPlanRef: identity.TargetPlanRef,
			Artifact: ArtifactRef{Digest: "sha256:aaa"}, Outcome: OutcomePass,
		},
		Validation: Validation{Outcome: OutcomePass, ArtifactDigest: "sha256:aaa", ProposalRevision: "rev-1"},
		Approval: ApprovalRecord{
			Decision: DecisionApproved, RunID: identity.RunID, ProposalRevision: "rev-1",
			ArtifactDigest: "sha256:aaa", TargetPlanRef: identity.TargetPlanRef,
		},
	}
	if err := HandoffPermitted(state, identity.TargetPlanRef); err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []Outcome{OutcomeFailRequired, OutcomeUnknownSecurity, OutcomeUnsupported, OutcomeTimedOut} {
		blocked := state
		blocked.Validation.Outcome = outcome
		if err := HandoffPermitted(blocked, identity.TargetPlanRef); err == nil {
			t.Fatalf("%s was permitted to hand off", outcome)
		}
	}
	stale := state
	stale.Approval.TargetPlanRef = "env-other/sample-app"
	if err := HandoffPermitted(stale, identity.TargetPlanRef); err == nil {
		t.Fatal("stale target was permitted")
	}
}

func TestValidationFailureDoesNotPass(t *testing.T) {
	for _, outcome := range []Outcome{OutcomeFailRequired, OutcomeUnknownSecurity, OutcomeUnsupported, ""} {
		if !(Validation{Outcome: outcome}).BlocksHandoff() {
			t.Fatalf("%q did not block", outcome)
		}
	}
	if (Validation{Outcome: OutcomePass}).BlocksHandoff() {
		t.Fatal("pass blocked")
	}
}
