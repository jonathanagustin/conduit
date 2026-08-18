package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

func deliveredAt() time.Time { return time.Date(2026, time.August, 18, 9, 0, 0, 0, time.UTC) }

func deliverableEvent() domain.Event {
	return domain.Event{
		ID:          "nev-1",
		Type:        domain.EventPlanFailures,
		ResourceRID: "ri.apollo.main.environment.d41d8cd98f00",
		Environment: "tenant-a",
		Summary:     "Reconciliation of installation mi-catalog in Environment tenant-a gave up",
		OccurredAt:  deliveredAt(),
	}
}

func entry(channel domain.Channel) domain.InboxEntry {
	return domain.InboxEntry{
		Recipient: domain.Recipient{
			Owner:   domain.Owner{Kind: domain.PrincipalUser, ID: "alice"},
			Channel: channel,
		},
		Event:        deliverableEvent(),
		PreferenceID: "pref-1",
	}
}

// The in-app inbox entry is its own delivery. Handing one to a transport would
// send it somewhere its owner never chose, and the whole queue depends on this
// being the rule rather than a convention.
func TestAnInAppEntryIsDeliveredByBeingInTheInbox(t *testing.T) {
	t.Parallel()
	if domain.NeedsTransport(entry(domain.ChannelInApp)) {
		t.Error("an IN_APP entry was queued for a transport it should never reach")
	}
	if !domain.NeedsTransport(entry(domain.ChannelEmail)) {
		t.Error("an EMAIL entry was not queued for a transport, which is the gap this closes")
	}
}

func TestARenderedMessageCarriesEverythingNeededToActOnIt(t *testing.T) {
	t.Parallel()
	message, err := domain.Render(entry(domain.ChannelEmail))
	if err != nil {
		t.Fatal(err)
	}
	if message.EventID != "nev-1" || message.Recipient.Owner.ID != "alice" {
		t.Fatalf("the message lost the delivery's identity: %#v", message)
	}
	if !strings.Contains(message.Subject, string(domain.EventPlanFailures)) {
		t.Errorf("the subject does not name the type a subscriber filtered on: %q", message.Subject)
	}
	if !strings.Contains(message.Subject, "tenant-a") {
		t.Errorf("the subject does not name the environment: %q", message.Subject)
	}
	for _, needed := range []string{
		"gave up", "nev-1", string(domain.EventPlanFailures),
		"ri.apollo.main.environment.d41d8cd98f00", "tenant-a", "2026-08-18T09:00:00Z",
	} {
		if !strings.Contains(message.Body, needed) {
			t.Errorf("the body omits %q, so a reader has to open the console: %q", needed, message.Body)
		}
	}
	if strings.Contains(message.Body, "Actor:") {
		t.Errorf("an event nobody caused reported an actor: %q", message.Body)
	}
}

// An empty Actor is reserved for events the system raised on its own, so the
// line has to be absent rather than empty -- and present when a person did it.
func TestAnActorAppearsOnlyWhenAPersonCausedTheEvent(t *testing.T) {
	t.Parallel()
	caused := entry(domain.ChannelEmail)
	caused.Event.Actor = "alice@example.com"
	message, err := domain.Render(caused)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message.Body, "Actor: alice@example.com") {
		t.Errorf("the body does not say who caused it: %q", message.Body)
	}
}

// An event with no environment must not render "in " with nothing after it.
func TestASubjectWithoutAnEnvironmentDoesNotTrailAPreposition(t *testing.T) {
	t.Parallel()
	global := entry(domain.ChannelEmail)
	global.Event.Environment = ""
	message, err := domain.Render(global)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(message.Subject, " in ") {
		t.Errorf("the subject named an environment the event does not have: %q", message.Subject)
	}
}

func TestRenderRefusesWhatCannotBePhrased(t *testing.T) {
	t.Parallel()
	for name, broken := range map[string]func(domain.InboxEntry) domain.InboxEntry{
		"an event with no summary": func(candidate domain.InboxEntry) domain.InboxEntry {
			candidate.Event.Summary = ""
			return candidate
		},
		"an event with no id": func(candidate domain.InboxEntry) domain.InboxEntry {
			candidate.Event.ID = ""
			return candidate
		},
		"a channel no inbox is kept on": func(candidate domain.InboxEntry) domain.InboxEntry {
			candidate.Recipient.Channel = "CARRIER_PIGEON"
			return candidate
		},
		"no recipient at all": func(candidate domain.InboxEntry) domain.InboxEntry {
			candidate.Recipient.Owner.ID = "  "
			return candidate
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := domain.Render(broken(entry(domain.ChannelEmail))); err == nil {
				t.Errorf("%s rendered into a message somebody would be sent", name)
			}
		})
	}
}
