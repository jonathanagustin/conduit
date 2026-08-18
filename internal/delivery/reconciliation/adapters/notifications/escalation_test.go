package notificationsadapter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	apolloapplication "github.com/jonathanagustin/conduit/internal/apollo/application"
	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/domain"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/domain"
)

func proposal() domain.Proposal {
	return domain.Proposal{
		ID:             "rcp_abc",
		Key:            domain.Key{Environment: "tenant-a", Installation: "mi-catalog"},
		InputDigest:    "sha256:" + strings.Repeat("a", 64),
		IdempotencyKey: "rci_abc",
	}
}

// The join. A preference is matched to an event by resource identifier, so an
// escalation that names the Environment differently from the surface an
// operator configured the preference on reaches nobody -- and reaches them
// silently, because a fan-out over no matching preference is indistinguishable
// from a fan-out over none configured.
func TestTheEscalationNamesTheEnvironmentTheApolloSurfaceNamesIt(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	escalations, err := NewEscalations(emitter)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	if err := escalations.ProposalExhausted(
		context.Background(), proposal(), "registry unavailable", 10, at,
	); err != nil {
		t.Fatal(err)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("an exhausted proposal produced %d events", len(emitter.events))
	}
	event := emitter.events[0]
	if event.ResourceRID != apolloapplication.EnvironmentRID("tenant-a") {
		t.Errorf("the event names an Environment nobody could have subscribed to: %q", event.ResourceRID)
	}
	if err := event.Validate(); err != nil {
		t.Errorf("the escalation produced an event the domain refuses: %v", err)
	}
	if event.Type != notifications.EventPlanFailures {
		t.Errorf("the escalation is typed %q, which is not what a subscriber to Plan failures ticked", event.Type)
	}
	// The installation is not in the identifier, so it has to be in the sentence
	// or the reader is told an Environment failed and not which part of it.
	if !strings.Contains(event.Summary, "mi-catalog") || !strings.Contains(event.Summary, "registry unavailable") {
		t.Errorf("the summary does not say what failed or why: %q", event.Summary)
	}
	if event.Actor != "" {
		t.Errorf("an event nobody caused was attributed to %q", event.Actor)
	}
	if !event.OccurredAt.Equal(at) {
		t.Errorf("the event moved in time: %s", event.OccurredAt)
	}
}

// The same proposal escalated twice is one fact. The store deduplicates on the
// event identity, so a redelivery after a crash must carry the identity it
// carried the first time -- and the attempt count must not be part of it, or a
// retried escalation would arrive as a second inbox row saying the same thing.
func TestTheSameProposalEscalatedTwiceCarriesOneIdentity(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	escalations, err := NewEscalations(emitter)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	for attempt := uint32(9); attempt <= 10; attempt++ {
		if err := escalations.ProposalExhausted(
			context.Background(), proposal(), "registry unavailable", attempt, at,
		); err != nil {
			t.Fatal(err)
		}
	}
	if emitter.events[0].ID != emitter.events[1].ID {
		t.Errorf("two escalations of one proposal carry two identities: %q and %q",
			emitter.events[0].ID, emitter.events[1].ID)
	}
	// A different proposal is a different fact.
	other := proposal()
	other.ID = "rcp_def"
	if err := escalations.ProposalExhausted(context.Background(), other, "registry unavailable", 10, at); err != nil {
		t.Fatal(err)
	}
	if emitter.events[2].ID == emitter.events[0].ID {
		t.Error("two different proposals collapsed into one event")
	}
}

// A planner refusal can be a whole rendered validation. It is bounded on a rune
// boundary because the one place this text has to be legible is an inbox, and
// half a rune renders there as a replacement character.
func TestALongPlannerRefusalIsBoundedWithoutSplittingARune(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	escalations, err := NewEscalations(emitter)
	if err != nil {
		t.Fatal(err)
	}
	reason := strings.Repeat("é", 4000)
	if err := escalations.ProposalExhausted(
		context.Background(), proposal(), reason, 10, time.Now().UTC(),
	); err != nil {
		t.Fatal(err)
	}
	summary := emitter.events[0].Summary
	if len(summary) > 1024 {
		t.Errorf("the summary is %d bytes; an inbox row is not a log", len(summary))
	}
	if strings.ContainsRune(summary, '�') {
		t.Error("the summary was truncated mid-rune")
	}
}

func TestAnEscalationForAnUnnamedInstallationIsRefused(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	escalations, err := NewEscalations(emitter)
	if err != nil {
		t.Fatal(err)
	}
	broken := proposal()
	broken.Key.Environment = ""
	if err := escalations.ProposalExhausted(
		context.Background(), broken, "registry unavailable", 10, time.Now().UTC(),
	); err == nil {
		t.Fatal("a proposal with no Environment produced an event about nothing")
	}
	if len(emitter.events) != 0 {
		t.Errorf("a refused escalation still emitted: %#v", emitter.events)
	}
}

func TestAnEmitterFailureReachesTheWorker(t *testing.T) {
	t.Parallel()
	failure := errors.New("notification store unreachable")
	escalations, err := NewEscalations(&recordingEmitter{err: failure})
	if err != nil {
		t.Fatal(err)
	}
	if err := escalations.ProposalExhausted(
		context.Background(), proposal(), "registry unavailable", 10, time.Now().UTC(),
	); !errors.Is(err, failure) {
		t.Fatalf("the worker was told the escalation succeeded: %v", err)
	}
}

type recordingEmitter struct {
	events []notifications.Event
	err    error
}

func (emitter *recordingEmitter) Emit(_ context.Context, event notifications.Event) error {
	if emitter.err != nil {
		return emitter.err
	}
	emitter.events = append(emitter.events, event)
	return nil
}
