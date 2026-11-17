package notificationsadapter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	apolloapplication "github.com/jonathanagustin/conduit/internal/apollo/application"
	"github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/governance/ports"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/domain"
)

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

func at() time.Time { return time.Date(2026, time.November, 16, 9, 30, 0, 0, time.UTC) }

func announcer(t *testing.T, emitter *recordingEmitter) *Announcements {
	t.Helper()
	announcements, err := NewAnnouncements(emitter, func() time.Time { return at().Add(time.Hour) })
	if err != nil {
		t.Fatal(err)
	}
	return announcements
}

func opening() ports.ChangeRequestOpening {
	return ports.ChangeRequestOpening{
		ID: "crq_0123", Environment: "tenant-a", TargetKind: "PLAN", TargetID: "pln_abc",
		Title: "Deploy example-api 0.2.0", Author: "alice", Revision: 1, At: at(),
	}
}

// The join: the event has to name the Environment the way the Apollo surface
// names it, or it matches no preference and reaches nobody silently.
func TestAWaitingChangeRequestIsRaisedOnTheEnvironmentASubscriberNamed(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	if err := announcer(t, emitter).AwaitingApproval(context.Background(), opening()); err != nil {
		t.Fatal(err)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("one waiting change request produced %d events", len(emitter.events))
	}
	event := emitter.events[0]
	if event.Type != notifications.EventChangeRequestPendingApproval {
		t.Errorf("raised as %q", event.Type)
	}
	if event.ResourceRID != apolloapplication.EnvironmentRID("tenant-a") || event.Environment != "tenant-a" {
		t.Errorf("the event names %q / %q, which no preference joins on", event.ResourceRID, event.Environment)
	}
	if event.Actor != "alice" {
		t.Errorf("the author is not the actor: %q", event.Actor)
	}
	if !event.OccurredAt.Equal(at()) {
		t.Errorf("the event moved in time: %s", event.OccurredAt)
	}
	for _, want := range []string{"crq_0123", "Deploy example-api 0.2.0", "tenant-a", "waiting for approval"} {
		if !strings.Contains(event.Summary, want) {
			t.Errorf("the summary does not say %q: %q", want, event.Summary)
		}
	}
	if err := event.Validate(); err != nil {
		t.Errorf("the adapter produced an event the domain refuses: %v", err)
	}
}

// One wait is one event however often it is restated, and a second wait is a
// second event. The store deduplicates on the identity, so this is the whole
// of "an approver is told once per round".
func TestTheIdentityIsStableInTheRoundAndChangesWithIt(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	announcements := announcer(t, emitter)
	restated := opening()
	restated.At = at().Add(time.Minute)
	restated.Title = "retitled"
	reopened := opening()
	reopened.Revision = 4
	other := opening()
	other.ID = "crq_9999"
	for _, each := range []ports.ChangeRequestOpening{opening(), restated, reopened, other} {
		if err := announcements.AwaitingApproval(context.Background(), each); err != nil {
			t.Fatal(err)
		}
	}
	ids := emitter.events
	if ids[0].ID != ids[1].ID {
		t.Errorf("the same round restated got a second identity")
	}
	if ids[0].ID == ids[2].ID {
		t.Errorf("a reopened request shares the identity of its first round, so the store would drop it")
	}
	if ids[0].ID == ids[3].ID {
		t.Errorf("two change requests share an identity")
	}
}

func TestAChangeRequestWithNoEnvironmentIsReportedRatherThanAnnounced(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	nowhere := opening()
	nowhere.Environment = "  "
	err := announcer(t, emitter).AwaitingApproval(context.Background(), nowhere)
	if !errors.Is(err, ErrNoEnvironment) {
		t.Fatalf("a request with no Environment answered %v", err)
	}
	unnamed := opening()
	unnamed.ID = ""
	if err := announcer(t, emitter).AwaitingApproval(context.Background(), unnamed); err == nil {
		t.Error("a request with no id was announced")
	}
	if len(emitter.events) != 0 {
		t.Errorf("an event was raised on a resource nobody could have subscribed to: %#v", emitter.events)
	}
}

func TestAnEmitterFailureIsReturnedToTheCaller(t *testing.T) {
	t.Parallel()
	broken := errors.New("store unavailable")
	emitter := &recordingEmitter{err: broken}
	if err := announcer(t, emitter).AwaitingApproval(context.Background(), opening()); !errors.Is(err, broken) {
		t.Errorf("a failed announcement answered %v", err)
	}
	if err := announcer(t, emitter).GrantChanged(context.Background(), ports.GrantChange{After: grant(1, "environment:viewer")}); !errors.Is(err, broken) {
		t.Errorf("a failed grant announcement answered %v", err)
	}
}

func TestAnAnnouncerNeedsAnEmitterAndAClock(t *testing.T) {
	t.Parallel()
	if _, err := NewAnnouncements(nil, at); err == nil {
		t.Error("an announcer with no emitter was assembled")
	}
	if _, err := NewAnnouncements(&recordingEmitter{}, nil); err == nil {
		t.Error("an announcer with no clock was assembled")
	}
}

func grant(revision uint64, roles ...string) domain.ResourceGrant {
	return domain.ResourceGrant{
		ID: "grt_abc", ResourceRID: "environment/tenant-a", Team: "operators",
		Roles: roles, UpdatedAt: at(), Revision: revision,
	}
}

// A grant change is raised on the resource the grant names, as ROLE_CHANGE,
// saying what the grant was and what it became -- the before half is what tells
// a removal from an addition.
func TestAGrantChangeIsRaisedOnTheResourceItNames(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	before := grant(1, "environment:viewer")
	if err := announcer(t, emitter).GrantChanged(context.Background(), ports.GrantChange{
		Before: &before, After: grant(2, "environment:operator"), Actor: "alice",
	}); err != nil {
		t.Fatal(err)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("one grant change produced %d events", len(emitter.events))
	}
	event := emitter.events[0]
	if event.Type != notifications.EventRoleChange {
		t.Errorf("raised as %q", event.Type)
	}
	if event.ResourceRID != "environment/tenant-a" {
		t.Errorf("the event names %q, not the resource the grant is on", event.ResourceRID)
	}
	if event.Actor != "alice" || !event.OccurredAt.Equal(at()) {
		t.Errorf("actor %q at %s", event.Actor, event.OccurredAt)
	}
	for _, want := range []string{"operators", "environment:operator", "was: roles environment:viewer"} {
		if !strings.Contains(event.Summary, want) {
			t.Errorf("the summary does not say %q: %q", want, event.Summary)
		}
	}
	if err := event.Validate(); err != nil {
		t.Errorf("the adapter produced an event the domain refuses: %v", err)
	}
}

func TestANewGrantIsAChangeAndAnUnchangedOneIsNot(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter{}
	announcements := announcer(t, emitter)
	if err := announcements.GrantChanged(context.Background(), ports.GrantChange{After: grant(1, "environment:viewer")}); err != nil {
		t.Fatal(err)
	}
	if len(emitter.events) != 1 || !strings.Contains(emitter.events[0].Summary, "was: no grant") {
		t.Fatalf("a grant that did not exist before was not announced as new: %#v", emitter.events)
	}
	// Re-saved with the same authority in a different order: the revision
	// moved and who may do what did not.
	before := grant(1, "environment:viewer", "environment:operator")
	if err := announcements.GrantChanged(context.Background(), ports.GrantChange{
		Before: &before, After: grant(2, "environment:operator", "environment:viewer"),
	}); err != nil {
		t.Fatal(err)
	}
	if len(emitter.events) != 1 {
		t.Errorf("a grant re-saved unchanged was announced as a role change: %#v", emitter.events[1:])
	}
	// The same grant at two revisions is two changes.
	if err := announcements.GrantChanged(context.Background(), ports.GrantChange{
		Before: &before, After: grant(3, "environment:viewer"),
	}); err != nil {
		t.Fatal(err)
	}
	if len(emitter.events) != 2 || emitter.events[0].ID == emitter.events[1].ID {
		t.Errorf("two revisions of one grant share an identity: %#v", emitter.events)
	}
	if err := announcements.GrantChanged(context.Background(), ports.GrantChange{After: domain.ResourceGrant{ID: "grt_x"}}); err == nil {
		t.Error("a grant naming no resource was announced")
	}
}
