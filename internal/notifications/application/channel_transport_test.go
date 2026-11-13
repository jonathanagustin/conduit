package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

type channelCapture struct {
	messages []domain.Message
	failure  error
}

func (*channelCapture) Name() string { return "capture" }
func (capture *channelCapture) Deliver(_ context.Context, message domain.Message) error {
	capture.messages = append(capture.messages, message)
	return capture.failure
}

func TestNativeChannelTransportRoutesOnlyTheChosenProvider(t *testing.T) {
	email, slack := &channelCapture{}, &channelCapture{}
	transport, err := NewChannelTransport(map[domain.Channel]ports.Transport{domain.ChannelEmail: email, domain.ChannelSlack: slack})
	if err != nil {
		t.Fatal(err)
	}
	message := domain.Message{Recipient: domain.Recipient{Channel: domain.ChannelSlack}, Destination: domain.Destination{Kind: domain.ContactSlack, Address: "#private-channel"}}
	if err := transport.Deliver(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(email.messages) != 0 || len(slack.messages) != 1 {
		t.Fatalf("email deliveries=%d, Slack deliveries=%d", len(email.messages), len(slack.messages))
	}
	message.Recipient.Channel = domain.ChannelEmail
	if err := transport.Deliver(context.Background(), message); !errors.Is(err, ports.ErrPermanentDelivery) || strings.Contains(err.Error(), message.Destination.Address) {
		t.Fatalf("mismatched destination failure=%v", err)
	}
	if len(email.messages) != 0 || len(slack.messages) != 1 {
		t.Fatal("mismatched channel was sent")
	}
	message.Recipient.Channel = domain.ChannelInApp
	if err := transport.Deliver(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(email.messages) != 0 || len(slack.messages) != 1 {
		t.Fatal("in-app entry left the inbox")
	}
}

func TestNativeDeliveryMissingContactOrProviderIsNeverAcknowledged(t *testing.T) {
	transport, err := NewChannelTransport(map[domain.Channel]ports.Transport{domain.ChannelEmail: &channelCapture{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []domain.Message{
		{Recipient: domain.Recipient{Channel: domain.ChannelSlack}, Destination: domain.Destination{Kind: domain.ContactSlack, Address: "private"}},
		{Recipient: domain.Recipient{Channel: domain.ChannelEmail}},
	} {
		if err := transport.Deliver(context.Background(), message); !errors.Is(err, ports.ErrPermanentDelivery) {
			t.Fatalf("unconfigured delivery acknowledged: %v", err)
		}
	}
}
