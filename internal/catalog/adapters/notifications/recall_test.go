package notificationsadapter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	apolloapplication "github.com/jonathanagustin/conduit/internal/apollo/application"
	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/domain"
)

func at() time.Time { return time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC) }

func announcer(t *testing.T, emitter *recordingEmitter) *RecallOpenings {
	t.Helper()
	openings, err := NewRecallOpenings(emitter, at)
	if err != nil {
		t.Fatal(err)
	}
	return openings
}

func boundedRecall(product, from, to string) catalog.Recall {
	return catalog.Recall{
		ID: "rec-bounded", Product: product, StartingVersion: from, EndingVersionExclusive: to,
		Reason: "chart renders an empty ingress",
	}
}

// The join. A preference is matched to an event by resource identifier, so an
// announcement that names the Environment differently from the surface an
// operator configured the preference on reaches nobody -- and reaches them
// silently, because a fan-out over no matching preference is indistinguishable
// from a fan-out over none configured.
func TestTheAnnouncementNamesTheEnvironmentTheApolloSurfaceNamesIt(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	if err := announcer(t, emitter).RecallsInForce(
		context.Background(), "tenant-a", []catalog.Recall{boundedRecall("g:p", "2.0.0", "2.3.0")},
	); err != nil {
		t.Fatal(err)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("one recall in force produced %d events", len(emitter.events))
	}
	event := emitter.events[0]
	if event.ResourceRID != apolloapplication.EnvironmentRID("tenant-a") {
		t.Errorf("the event names an Environment nobody could have subscribed to: %q", event.ResourceRID)
	}
	if event.Environment != "tenant-a" {
		t.Errorf("the event does not carry the Environment: %q", event.Environment)
	}
	if err := event.Validate(); err != nil {
		t.Errorf("the announcement produced an event the domain refuses: %v", err)
	}
	if !event.OccurredAt.Equal(at()) {
		t.Errorf("the event moved in time: %s", event.OccurredAt)
	}
	// Nobody wrote this particular recall down as an act attributable to them:
	// a DesiredState carries no author per recall, and an empty Actor is what
	// the domain reserves for exactly that.
	if event.Actor != "" {
		t.Errorf("the recall was attributed to %q, whom nothing names", event.Actor)
	}
}

// The two recall types Apollo publishes carry different descriptions, and the
// difference is the one this routes on: "Future releases of this Product will
// be recalled" is true of an unbounded recall and false of a bounded one.
func TestAnUnboundedRecallIsTypedAsTheOpenEndedOne(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	open := catalog.Recall{ID: "rec-open", Product: "g:p", StartingVersion: "2.0.0", Reason: "data loss on upgrade"}
	if err := announcer(t, emitter).RecallsInForce(
		context.Background(), "tenant-a", []catalog.Recall{open, boundedRecall("g:q", "1.0.0", "1.2.0")},
	); err != nil {
		t.Fatal(err)
	}
	if emitter.events[0].Type != notifications.EventProductOpenEndedRecall {
		t.Errorf("a recall with no upper bound is typed %q, which is not the type whose description is "+
			"\"Future releases of this Product will be recalled\"", emitter.events[0].Type)
	}
	if emitter.events[1].Type != notifications.EventProductRecalls {
		t.Errorf("a recall that stops at 1.2.0 is typed %q, which claims future Releases are recalled",
			emitter.events[1].Type)
	}
	for _, event := range emitter.events {
		if _, observed := event.Type.ObservedScope(); !observed {
			t.Errorf("type %q is not one a captured settings page offers, so nobody could have ticked it", event.Type)
		}
	}
}

// The summary is the whole of what a reader gets: the resource identifier names
// an Environment, so the Product and the range it withdrew have to be in the
// sentence or the reader is told something was recalled and not what.
func TestTheSummarySaysWhatWasWithdrawnAndWhy(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	single := catalog.Recall{
		ID: "rec-single", Product: "g:p", StartingVersion: "2.1.0", SingleVersion: true,
		Reason: "leaks credentials into logs", Source: catalog.RecallSourceSecurity, AssessmentRunID: "run-7",
	}
	excluded := catalog.Recall{
		ID: "rec-excluded", Product: "g:q", StartingVersion: "1.0.0",
		ExcludedVersions: []string{"1.4.2"}, Reason: "bad migration",
	}
	if err := announcer(t, emitter).RecallsInForce(
		context.Background(), "tenant-a", []catalog.Recall{single, excluded, boundedRecall("g:r", "3.0.0", "3.1.0")},
	); err != nil {
		t.Fatal(err)
	}
	for index, needed := range [][]string{
		{"g:p", "2.1.0", "leaks credentials into logs", "run-7"},
		{"g:q", "1.0.0", "1.4.2", "bad migration"},
		{"g:r", "3.0.0", "3.1.0", "chart renders an empty ingress"},
	} {
		summary := emitter.events[index].Summary
		for _, fragment := range needed {
			if !strings.Contains(summary, fragment) {
				t.Errorf("summary %d omits %q, so a reader has to open the console: %q", index, fragment, summary)
			}
		}
	}
	// An unbounded recall has to say so, because "recalled from 3.0.0" reads as
	// a range that ends somewhere.
	emitter.events = nil
	if err := announcer(t, emitter).RecallsInForce(context.Background(), "tenant-a",
		[]catalog.Recall{{ID: "rec-open", Product: "g:p", StartingVersion: "2.0.0", Reason: "data loss"}},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(emitter.events[0].Summary, "not yet published") {
		t.Errorf("an open-ended recall does not say future Releases are covered: %q", emitter.events[0].Summary)
	}
}

// Recalls are a field of a DesiredState and are submitted whole every time, so
// the same recall arrives again with every Plan. The identity is what makes the
// second arrival the same fact rather than a second inbox row, and the store
// deduplicates on it.
func TestARecallRestatedByALaterSubmissionCarriesOneIdentity(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	openings := announcer(t, emitter)
	recall := boundedRecall("g:p", "2.0.0", "2.3.0")
	if err := openings.RecallsInForce(context.Background(), "tenant-a", []catalog.Recall{recall}); err != nil {
		t.Fatal(err)
	}
	// A later submission that corrects the wording is not a second recall.
	restated := recall
	restated.Reason = "chart renders an empty ingress; see INC-42"
	if err := openings.RecallsInForce(context.Background(), "tenant-a", []catalog.Recall{restated}); err != nil {
		t.Fatal(err)
	}
	if emitter.events[0].ID != emitter.events[1].ID {
		t.Errorf("one recall restated carries two identities: %q and %q", emitter.events[0].ID, emitter.events[1].ID)
	}
	// The same recall id in another Environment is another withdrawal, and an
	// operator subscribed to that Environment has not been told about it.
	if err := openings.RecallsInForce(context.Background(), "tenant-b", []catalog.Recall{recall}); err != nil {
		t.Fatal(err)
	}
	if emitter.events[2].ID == emitter.events[0].ID {
		t.Error("a recall in two Environments collapsed into one event")
	}
	// A different recall in the same Environment is a different fact.
	other := recall
	other.ID = "rec-other"
	if err := openings.RecallsInForce(context.Background(), "tenant-a", []catalog.Recall{other}); err != nil {
		t.Fatal(err)
	}
	if emitter.events[3].ID == emitter.events[0].ID {
		t.Error("two different recalls collapsed into one event")
	}
}

// A reverted recall withdraws nothing, so announcing one would tell an operator
// that software they can install is unavailable.
func TestARevertedRecallIsNotAnnounced(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	reverted := boundedRecall("g:p", "2.0.0", "2.3.0")
	reverted.Reverted = true
	if err := announcer(t, emitter).RecallsInForce(
		context.Background(), "tenant-a", []catalog.Recall{reverted},
	); err != nil {
		t.Fatal(err)
	}
	if len(emitter.events) != 0 {
		t.Errorf("a reverted recall was announced: %#v", emitter.events)
	}
}

// One malformed recall must not silence the rest of the submission. Silence is
// the failure this whole capability exists to remove, and a set that stopped at
// the first bad entry would reintroduce it for everything behind it.
func TestOneUnannounceableRecallDoesNotSilenceTheRest(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	anonymous := boundedRecall("g:p", "2.0.0", "2.3.0")
	anonymous.ID = " "
	good := boundedRecall("g:q", "1.0.0", "1.2.0")
	good.ID = "rec-good"
	err := announcer(t, emitter).RecallsInForce(
		context.Background(), "tenant-a", []catalog.Recall{anonymous, good})
	if err == nil {
		t.Fatal("a recall with no id was announced under an identity nothing can deduplicate")
	}
	if len(emitter.events) != 1 || !strings.Contains(emitter.events[0].Summary, "g:q") {
		t.Errorf("the announceable recall was dropped with the broken one: %#v", emitter.events)
	}
}

func TestAnAnnouncementForAnUnnamedEnvironmentIsRefused(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	if err := announcer(t, emitter).RecallsInForce(
		context.Background(), "  ", []catalog.Recall{boundedRecall("g:p", "2.0.0", "2.3.0")},
	); err == nil {
		t.Fatal("recalls in force for no Environment produced events about nothing")
	}
	if len(emitter.events) != 0 {
		t.Errorf("a refused announcement still emitted: %#v", emitter.events)
	}
}

// An emitter failure has to reach the caller. The control plane drops it on
// purpose -- the DesiredState is already committed -- and the composition root
// logs it, so an adapter that swallowed it here would make that impossible.
func TestAnEmitterFailureReachesTheCaller(t *testing.T) {
	t.Parallel()
	failure := errors.New("notification store unreachable")
	emitter := &recordingEmitter{err: failure}
	err := announcer(t, emitter).RecallsInForce(
		context.Background(), "tenant-a", []catalog.Recall{boundedRecall("g:p", "2.0.0", "2.3.0")})
	if !errors.Is(err, failure) {
		t.Fatalf("the caller was told the announcement succeeded: %v", err)
	}
}

// A reason is free text an operator wrote through an API, so it is bounded --
// and bounded on a rune boundary, because the one place it has to be legible is
// an inbox row.
func TestALongReasonIsBoundedWithoutSplittingARune(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	long := boundedRecall("g:p", "2.0.0", "2.3.0")
	long.Reason = strings.Repeat("é", 4000)
	if err := announcer(t, emitter).RecallsInForce(
		context.Background(), "tenant-a", []catalog.Recall{long}); err != nil {
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

func TestAnAnnouncerWithoutAnEmitterOrAClockIsRefused(t *testing.T) {
	t.Parallel()
	if _, err := NewRecallOpenings(nil, at); err == nil {
		t.Error("an announcer with nowhere to emit was constructed")
	}
	if _, err := NewRecallOpenings(&recordingEmitter{}, nil); err == nil {
		t.Error("an announcer with no clock was constructed")
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
