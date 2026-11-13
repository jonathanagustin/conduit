package application

import (
	"context"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// ChannelTransport routes native delivery to the configured channel adapter.
// Missing channels fail explicitly; no message is acknowledged by silently
// falling through to another provider or another contact kind.
type ChannelTransport struct {
	channels map[domain.Channel]ports.Transport
}

func NewChannelTransport(channels map[domain.Channel]ports.Transport) (*ChannelTransport, error) {
	if len(channels) == 0 {
		return nil, fmt.Errorf("at least one native notification channel is required")
	}
	copy := make(map[domain.Channel]ports.Transport, len(channels))
	for channel, transport := range channels {
		if (channel != domain.ChannelEmail && channel != domain.ChannelSlack) || transport == nil {
			return nil, fmt.Errorf("native notification channel requires a configured email or Slack transport")
		}
		copy[channel] = transport
	}
	return &ChannelTransport{channels: copy}, nil
}

func (*ChannelTransport) Name() string { return "native" }

func (transport *ChannelTransport) Deliver(ctx context.Context, message domain.Message) error {
	if message.Recipient.Channel == domain.ChannelInApp {
		return nil
	}
	provider := transport.channels[message.Recipient.Channel]
	if provider == nil {
		return fmt.Errorf("%w: no transport configured for notification channel %s", ports.ErrPermanentDelivery, message.Recipient.Channel)
	}
	if !message.Destination.Named() {
		return fmt.Errorf("%w: recipient has no contact for the selected notification channel", ports.ErrPermanentDelivery)
	}
	if string(message.Destination.Kind) != string(message.Recipient.Channel) {
		return fmt.Errorf("%w: notification contact does not match the selected channel", ports.ErrPermanentDelivery)
	}
	if err := provider.Deliver(ctx, message); err != nil {
		return fmt.Errorf("%s: %w", provider.Name(), err)
	}
	return nil
}
