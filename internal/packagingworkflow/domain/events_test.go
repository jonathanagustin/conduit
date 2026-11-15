package domain

import (
	"encoding/json"
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

func TestStatusJournalIsVersionEnabledAndSurvivesProposal(t *testing.T) {
	legacy := &State{}
	legacy.EnableEvents()
	legacy.Block(OutcomeCancelled, "run cancelled")
	legacy.NoteStatus(time.Unix(10, 0))
	if len(legacy.Events) != 0 {
		t.Fatal("legacy journal gained a terminal status")
	}
	state := &State{Phase: PhaseReview}
	state.EnableStatusEvents()
	state.NoteStatus(time.Unix(10, 0))
	if len(state.Events) != 0 {
		t.Fatal("nonterminal snapshot was invented as an event")
	}
	state.Block(OutcomeCancelled, "run cancelled")
	state.NoteStatus(time.Unix(11, 0))
	if len(state.Events) != 1 || state.Events[0].Step != PhaseBlocked || state.Events[0].Outcome != string(OutcomeCancelled) {
		t.Fatal("native terminal status not recorded")
	}
	next := ApplyProposal(*state, Receipt{})
	next.Block(OutcomeFailRequired, "missing evidence")
	next.NoteStatus(time.Unix(12, 0))
	if len(next.Events) != 2 || next.Events[1].Sequence != 2 {
		t.Fatal("proposal replacement dropped version-enabled status journal")
	}
	body, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	var restored State
	if err := json.Unmarshal(body, &restored); err != nil {
		t.Fatal(err)
	}
	restored.NoteStatus(time.Unix(13, 0))
	if len(restored.Events) != 2 {
		t.Fatal("serialized state enabled version behavior without its workflow marker")
	}
	restored.EnableStatusEvents()
	restored.NoteStatus(time.Unix(13, 0))
	if len(restored.Events) != 3 || restored.Events[2].Sequence != 3 {
		t.Fatal("version marker did not restore native journal after carry")
	}
}

func TestRollingJournalRetainsTerminalPositionsAndDetachedCarry(t *testing.T) {
	state := State{Phase: PhaseReview}
	state.EnableRollingEvents()
	for i := 0; i < MaxStepEvents; i++ {
		state.Note(time.Unix(int64(i+1), 0), StepReview, EventWaiting, "waiting", "")
	}
	carried := append([]StepEvent(nil), state.Events...)
	next := ApplyProposal(state, Receipt{})
	next.Note(time.Unix(100, 0), StepReview, EventWaiting, "waiting", "")
	next.Block(OutcomeCancelled, "run cancelled")
	next.NoteStatus(time.Unix(101, 0))
	if len(next.Events) != MaxStepEvents || next.Events[0].Sequence != 3 || next.Events[MaxStepEvents-1].Sequence != MaxStepEvents+2 || next.Events[MaxStepEvents-1].Outcome != string(OutcomeCancelled) {
		t.Fatalf("full journal lost terminal position: %+v", next.Events)
	}
	for i, event := range state.Events {
		if event != carried[i] {
			t.Fatal("rotating proposal mutated carried snapshot")
		}
	}
	body, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	var restarted State
	if err := json.Unmarshal(body, &restarted); err != nil {
		t.Fatal(err)
	}
	restarted.NoteStatus(time.Unix(102, 0))
	if restarted.Events[MaxStepEvents-1].Sequence != MaxStepEvents+2 {
		t.Fatal("caller JSON enabled rolling behavior")
	}
	restarted.EnableRollingEvents()
	restarted.NoteStatus(time.Unix(103, 0))
	if len(restarted.Events) != MaxStepEvents || restarted.Events[0].Sequence != 4 || restarted.Events[MaxStepEvents-1].Sequence != MaxStepEvents+3 {
		t.Fatal("carry restart reused positions")
	}
}
func TestLegacyFullJournalAndExhaustedPositionCannotRotate(t *testing.T) {
	legacy := State{Phase: PhaseReview}
	legacy.EnableStatusEvents()
	for i := 0; i < MaxStepEvents; i++ {
		legacy.Note(time.Unix(int64(i+1), 0), StepReview, EventWaiting, "waiting", "")
	}
	legacy.Block(OutcomeCancelled, "cancelled")
	legacy.NoteStatus(time.Unix(100, 0))
	if legacy.Events[0].Sequence != 1 || legacy.Events[MaxStepEvents-1].Step != StepReview || legacy.CanRecordEvent() {
		t.Fatal("legacy full-history query changed")
	}
	// No wrap or malformed continuation is allowed even for an invalid carry.
	for _, last := range []int{0, -1, int(^uint(0) >> 1)} {
		state := State{Events: []StepEvent{{Sequence: last}}}
		state.EnableRollingEvents()
		state.Note(time.Unix(10, 0), StepReview, EventWaiting, "", "")
		if len(state.Events) != 1 {
			t.Fatal("invalid/exhausted position wrapped")
		}
	}
}
