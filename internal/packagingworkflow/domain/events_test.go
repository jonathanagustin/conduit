package domain

import (
	"strings"
	"testing"
	"time"
)

func TestEnvironmentOfReadsTheTargetReference(t *testing.T) {
	if got := EnvironmentOf("env-dev/sample-app"); got != "env-dev" {
		t.Fatalf("environment = %q", got)
	}
	for _, target := range []string{"", "env-dev", "env dev/sample", "/sample"} {
		if got := EnvironmentOf(target); got != "" {
			t.Fatalf("EnvironmentOf(%q) = %q", target, got)
		}
	}
}

func TestNoteRedactsSecretsAndDoesNotInventAPass(t *testing.T) {
	state := &State{TargetPlanRef: "env-dev/sample-app"}
	state.Note(time.Now(), StepAssess, string(OutcomePass), "should not record", "")
	if len(state.Events) != 0 {
		t.Fatal("events recorded before the journal was enabled")
	}
	state.EnableEvents()
	state.Assessment.SourceDigest = "sha256:source"
	state.Note(time.Now(), StepAssess, string(OutcomePass), "assessed", "")
	state.Note(time.Now(), StepValidate, string(OutcomeFailRequired), "leak BEGIN PRIVATE KEY material", "")
	if len(state.Events) != 2 {
		t.Fatalf("events = %#v", state.Events)
	}
	if state.Events[0].Sequence != 1 || state.Events[0].Step != StepAssess || state.Events[0].Outcome != string(OutcomePass) {
		t.Fatalf("first = %#v", state.Events[0])
	}
	if state.Events[1].Step != StepValidate || state.Events[1].Outcome != string(OutcomeFailRequired) || state.Events[1].Reason != "redacted" {
		t.Fatalf("second = %#v", state.Events[1])
	}
	encoded := state.Events[1].Reason + state.Events[1].Step
	if strings.Contains(encoded, "BEGIN PRIVATE KEY") || strings.Contains(encoded, "leak") {
		t.Fatalf("secret stored: %#v", state.Events[1])
	}
	for i := 0; i < MaxStepEvents; i++ {
		state.Note(time.Now(), StepReview, EventWaiting, "waiting", "")
	}
	if len(state.Events) != MaxStepEvents {
		t.Fatalf("len = %d", len(state.Events))
	}
}

func TestApplyProposalKeepsTheJournal(t *testing.T) {
	state := &State{}
	state.EnableEvents()
	state.Note(time.Unix(10, 0), StepAssess, string(OutcomePass), "ok", "")
	next := ApplyProposal(*state, Receipt{ProposalRevision: "rev-1", Artifact: ArtifactRef{Digest: "sha256:abc"}})
	if !next.EventsEnabled() || len(next.Events) != 1 || next.Events[0].Step != StepAssess {
		t.Fatalf("proposal dropped the journal: %#v", next.Events)
	}
}
