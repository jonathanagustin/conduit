package composition

import (
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	notificationpostgres "github.com/jonathanagustin/conduit/internal/notifications/adapters/postgres"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/application"
	notificationports "github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// ConfigureNotificationIntents binds durable source intent to the metadata
// transaction. It must run before any source worker starts. A nonpersistent
// deployment keeps its existing optional post-commit observers.
func ConfigureNotificationIntents(repository controlplane.Repository, store notificationports.Store, logger *slog.Logger) (bool, error) {
	if store == nil {
		return false, nil
	}
	if _, durable := store.(notificationports.IntentQueue); !durable {
		return false, nil
	}
	if logger == nil {
		return false, errors.New("a notification intent logger is required")
	}
	host, ok := repository.(interface {
		ConfigureNotificationIntents(controlplane.TransactionAnnouncementFactory) error
	})
	if !ok || repository.InTransaction() {
		return false, errors.New("durable notification intents require a root PostgreSQL metadata repository")
	}
	err := host.ConfigureNotificationIntents(func(transaction *sql.Tx) (controlplane.Announcements, error) {
		recorder, err := notificationpostgres.NewIntentRecorder(transaction)
		if err != nil {
			return controlplane.Announcements{}, err
		}
		return mappedAnnouncements(recorder, logger, true)
	})
	return err == nil, err
}

func DefaultIntentDrainerConfig() notifications.IntentDrainerConfig {
	return notifications.IntentDrainerConfig{
		LeaseDuration: time.Minute, MaximumAttempts: 10,
		BaseRetryDelay: time.Second, PollInterval: time.Second,
	}
}

// NewNotificationIntentDrainer is independent of external transport settings.
// A PostgreSQL-backed, IN_APP-only Hub still has committed work to transfer.
func NewNotificationIntentDrainer(store notificationports.Store, eligibility notificationports.RecipientEligibility, clock notificationports.Clock, config notifications.IntentDrainerConfig) (*notifications.IntentDrainer, error) {
	if store == nil {
		return nil, nil
	}
	queue, ok := store.(notificationports.IntentQueue)
	if !ok {
		return nil, errors.New("notification intent admission requires a durable intent queue")
	}
	return notifications.NewIntentDrainer(queue, store, eligibility, clock, config)
}
