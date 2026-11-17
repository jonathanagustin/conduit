package controlplaneadapter

import (
	"errors"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// ConfigureNotificationIntents preserves the optional source-transaction
// capability through this repository decorator, just as Transaction does.
// The underlying root still owns the SQL transaction and its intent ports.
func (repository *Repository) ConfigureNotificationIntents(factory controlplane.TransactionAnnouncementFactory) error {
	if repository == nil || repository.Repository == nil {
		return errors.New("a wrapped metadata repository is required for notification intents")
	}
	host, ok := repository.Repository.(interface {
		ConfigureNotificationIntents(controlplane.TransactionAnnouncementFactory) error
	})
	if !ok {
		return errors.New("wrapped metadata repository does not support transactional notification intents")
	}
	return host.ConfigureNotificationIntents(factory)
}
