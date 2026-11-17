package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

type nativeDeliveryCapture struct{ messages []domain.Message }

func (*nativeDeliveryCapture) Name() string { return "native-capture" }
func (capture *nativeDeliveryCapture) Deliver(_ context.Context, message domain.Message) error {
	capture.messages = append(capture.messages, message)
	return nil
}

func TestNativeChannelsFanOutPersistAndRouteThroughAnotherPool(t *testing.T) {
	ctx := t.Context()
	first := newStore(t, ctx)
	resetStore(t, ctx, first)
	resetContacts(t, ctx, first)
	contacts, err := application.NewChannelContacts(first, fixedAt{})
	if err != nil {
		t.Fatal(err)
	}
	var selectedTeamContact string
	for _, contact := range []domain.Contact{
		{Owner: alice(), Kind: domain.ContactEmail, Address: "alice@example.test"},
		{Owner: alice(), Kind: domain.ContactSlack, Address: "C-alice"},
		{Owner: platform(), Kind: domain.ContactSlack, Address: "C-other-owner"},
	} {
		saved, err := contacts.Save(ctx, contact)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Owner == platform() {
			selectedTeamContact = saved.ID
		}
	}
	for index, channel := range []domain.Channel{domain.ChannelEmail, domain.ChannelSlack, domain.ChannelInApp} {
		identifiers := []string{"native-email", "native-slack", "native-inapp"}
		if err := first.SavePreference(ctx, preference(identifiers[index], alice(), channel)); err != nil {
			t.Fatal(err)
		}
	}
	teamPreference := preference("native-team", platform(), domain.ChannelSlack)
	teamPreference.ContactID = selectedTeamContact
	if err := first.SavePreference(ctx, teamPreference); err != nil {
		t.Fatal(err)
	}
	emitter, err := application.NewEmitter(first, allowRecipients{})
	if err != nil {
		t.Fatal(err)
	}
	if err := emitter.Emit(ctx, planFailed("native-event")); err != nil {
		t.Fatal(err)
	}
	second := newStore(t, ctx)
	resolver, err := application.NewNativeDestinations(second, second, fixedAt{})
	if err != nil {
		t.Fatal(err)
	}
	email, slack := &nativeDeliveryCapture{}, &nativeDeliveryCapture{}
	router, err := application.NewChannelTransport(map[domain.Channel]ports.Transport{domain.ChannelEmail: email, domain.ChannelSlack: slack})
	if err != nil {
		t.Fatal(err)
	}
	config := application.DispatcherConfig{Eligibility: allowRecipients{}, LeaseDuration: 30 * time.Second, MaximumAttempts: 3, BaseRetryDelay: 2 * time.Second, PollInterval: time.Second, Destinations: resolver}
	worker, err := application.NewDispatcher(second, router, fixedAt{}, config)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if len(email.messages) != 1 || email.messages[0].Destination.Address != "alice@example.test" {
		t.Fatal("email did not reach its owner's selected contact")
	}
	if len(slack.messages) != 2 {
		t.Fatal("Slack did not deliver both selected owner contacts")
	}
	destinations := map[string]string{}
	for _, message := range slack.messages {
		destinations[message.Recipient.Owner.ID] = message.Destination.Address
	}
	if destinations[alice().ID] != "C-alice" || destinations[platform().ID] != "C-other-owner" {
		t.Fatal("Slack deliveries crossed contact owners")
	}
	stats, err := second.Deliveries(ctx)
	if err != nil || stats.Pending != 0 || stats.Exhausted != 0 {
		t.Fatalf("native delivery state=%#v, %v", stats, err)
	}
	entries, err := second.Inbox(ctx, domain.Recipient{Owner: alice(), Channel: domain.ChannelSlack}, 10)
	if err != nil || len(entries) != 1 {
		t.Fatalf("Slack inbox was not retained: %#v, %v", entries, err)
	}
	if err := worker.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if len(email.messages) != 1 || len(slack.messages) != 2 {
		t.Fatal("completed native deliveries were sent again")
	}
}
