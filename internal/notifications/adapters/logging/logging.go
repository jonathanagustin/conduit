// Package logging delivers a rendered notification to the Hub's structured log.
//
// This is not a delivery channel in the sense the readiness document means: a
// log line is still something an operator has to be looking at, which is the
// gap. It exists because a deployment that already ships its logs somebody
// watches can turn a notification into a real signal with no new endpoint and
// no credential, and because it makes the dispatcher's own behaviour visible
// while a webhook endpoint is being set up.
//
// It is off unless configured, like every transport here. Nothing logs a
// notification until an operator asks for it.
package logging

import (
	"context"
	"log/slog"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// Transport writes one log record per delivery.
type Transport struct {
	logger *slog.Logger
}

var _ ports.Transport = (*Transport)(nil)

// New accepts a nil logger and uses the default, because a transport that
// refused to be constructed without one would be harder to enable than the
// endpoint it stands in for.
func New(logger *slog.Logger) *Transport {
	if logger == nil {
		logger = slog.Default()
	}
	return &Transport{logger: logger}
}

func (transport *Transport) Name() string { return "log" }

// Deliver never fails, and that is a claim worth stating: it means an entry
// delivered this way is marked delivered on the first attempt and never
// retried. A logger that dropped records would make that a lie, which is why
// the summary and the recipient are attributes rather than an interpolated
// message -- a handler that samples or truncates does so visibly.
func (transport *Transport) Deliver(_ context.Context, message domain.Message) error {
	transport.logger.Info("notification delivered",
		"event", message.EventID,
		"type", string(message.Type),
		"resource", message.ResourceRID,
		"environment", message.Environment,
		"ownerKind", string(message.Recipient.Owner.Kind),
		"owner", message.Recipient.Owner.ID,
		"channel", string(message.Recipient.Channel),
		"subject", message.Subject,
		"occurredAt", message.OccurredAt,
	)
	return nil
}
