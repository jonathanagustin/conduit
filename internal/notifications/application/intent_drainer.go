package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

type IntentDrainerConfig struct {
	LeaseDuration   time.Duration
	MaximumAttempts uint32
	BaseRetryDelay  time.Duration
	PollInterval    time.Duration
	OnError         func(error)
}

// IntentDrainer transfers immutable source intents into event/inbox storage.
// It runs independently of transport configuration: IN_APP admission also
// needs the handoff. It never reads current preferences or sends a message.
type IntentDrainer struct {
	queue       ports.IntentQueue
	recorder    ports.EventRecorder
	eligibility ports.RecipientEligibility
	clock       ports.Clock
	config      IntentDrainerConfig
}

func NewIntentDrainer(queue ports.IntentQueue, recorder ports.EventRecorder, eligibility ports.RecipientEligibility, clock ports.Clock, config IntentDrainerConfig) (*IntentDrainer, error) {
	if queue == nil || recorder == nil || eligibility == nil || clock == nil {
		return nil, errors.New("notification intent queue, event recorder, current recipient eligibility and clock are required")
	}
	if config.LeaseDuration <= 0 || config.MaximumAttempts == 0 || config.BaseRetryDelay <= 0 || config.PollInterval <= 0 {
		return nil, errors.New("notification intent lease, attempts, retry delay and poll interval must be positive")
	}
	return &IntentDrainer{queue: queue, recorder: recorder, eligibility: eligibility, clock: clock, config: config}, nil
}

// TransferOne claims one due intent. A crash after RecordEvent and before the
// acknowledgment repeats that same immutable handoff; RecordEvent must retain
// its first committed event and audience, including an empty audience.
func (drainer *IntentDrainer) TransferOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	now := drainer.clock.Now().UTC()
	lease, found, err := drainer.queue.LeaseIntent(ctx, now, drainer.config.LeaseDuration)
	if err != nil || !found {
		return found, err
	}
	if err := lease.Intent.Validate(); err != nil {
		return true, drainer.retainFailure(ctx, lease, fmt.Sprintf("invalid intent: %v", err), now, true)
	}
	entries := make([]domain.InboxEntry, 0, len(lease.Intent.Candidates))
	decisions := map[domain.Owner]bool{}
	for _, candidate := range lease.Intent.Candidates {
		allowed, checked := decisions[candidate.Recipient.Owner]
		if !checked {
			allowed, err = drainer.eligibility.Check(ctx, candidate.Recipient.Owner, lease.Intent.Event.ResourceRID)
			if err != nil {
				return true, drainer.retry(ctx, lease, fmt.Sprintf("authorize recipient: %v", err), now)
			}
			decisions[candidate.Recipient.Owner] = allowed
		}
		if allowed {
			entries = append(entries, domain.InboxEntry{Event: lease.Intent.Event, Recipient: candidate.Recipient, PreferenceID: candidate.PreferenceID})
		}
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	if err := drainer.recorder.RecordEvent(ctx, lease.Intent.Event, entries); err != nil {
		return true, drainer.retry(ctx, lease, fmt.Sprintf("record event: %v", err), now)
	}
	return true, drainer.queue.CompleteIntent(ctx, lease, drainer.clock.Now().UTC())
}

func (drainer *IntentDrainer) retry(ctx context.Context, lease ports.IntentLease, reason string, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err // Leave the lease to expire; a canceled attempt cannot acknowledge work.
	}
	exhausted := lease.Attempt >= drainer.config.MaximumAttempts
	delay := drainer.config.BaseRetryDelay
	for count := uint32(1); count < lease.Attempt && delay < time.Hour; count++ {
		delay *= 2
	}
	if delay > time.Hour {
		delay = time.Hour
	}
	return drainer.retainFailure(ctx, lease, reason, now.Add(delay), exhausted)
}

func (drainer *IntentDrainer) retainFailure(ctx context.Context, lease ports.IntentLease, reason string, retryAt time.Time, exhausted bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := drainer.queue.FailIntent(ctx, lease, reason, retryAt, exhausted); err != nil {
		return err
	}
	if drainer.config.OnError != nil {
		// Report only after the outcome is durable. Return nil so a retained
		// failure does not prevent admission of the other due intents.
		// Bound and quote the diagnostic, and never include the event body,
		// consent candidates or contact payloads.
		eventID := lease.Intent.Event.ID
		if len(eventID) > 128 {
			eventID = eventID[:125] + "..."
		}
		if len(reason) > 768 {
			reason = reason[:765] + "..."
		}
		drainer.config.OnError(fmt.Errorf("notification intent retained: event_id=%q attempt=%d/%d exhausted=%t reason=%q", eventID, lease.Attempt, drainer.config.MaximumAttempts, exhausted, reason))
	}
	return nil
}

func (drainer *IntentDrainer) Drain(ctx context.Context) error {
	for {
		found, err := drainer.TransferOne(ctx)
		if err != nil || !found {
			return err
		}
	}
}

func (drainer *IntentDrainer) Run(ctx context.Context) {
	ticker := time.NewTicker(drainer.config.PollInterval)
	defer ticker.Stop()
	for {
		if err := drainer.Drain(ctx); err != nil && drainer.config.OnError != nil {
			drainer.config.OnError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
