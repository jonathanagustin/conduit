package controlplane

import (
	"database/sql"
	"errors"
)

// TransactionAnnouncementFactory constructs intent recorders owned by this
// exact SQL transaction. They may enqueue private source intent, never commit
// an independent inbox transaction or submit an external notification.
type TransactionAnnouncementFactory func(*sql.Tx) (Announcements, error)

func (repository *PostgresRepository) ConfigureNotificationIntents(factory TransactionAnnouncementFactory) error {
	if factory == nil {
		return errors.New("a transactional notification factory is required")
	}
	repository.notificationFactory.Store(&factory)
	return nil
}

func (repository *PostgresRepository) TransactionAnnouncements() (Announcements, bool) {
	return Announcements{}, repository.notificationFactory.Load() != nil
}

func (view *authorizationAuditView) TransactionAnnouncements() (Announcements, bool) {
	return view.notificationIntents, view.hasNotificationIntents
}

func (*MemoryRepository) TransactionAnnouncements() (Announcements, bool) {
	return Announcements{}, false
}

func (*FileRepository) TransactionAnnouncements() (Announcements, bool) {
	return Announcements{}, false
}

func (announcements Announcements) complete() bool {
	return announcements.ChangeRequestOpenings != nil && announcements.GrantChanges != nil &&
		announcements.PlanFailures != nil && announcements.PromotionFailures != nil && announcements.RecallOpenings != nil
}

func needsNotificationTransaction(repository Repository) bool {
	if repository == nil || repository.InTransaction() {
		return false
	}
	_, configured := repository.TransactionAnnouncements()
	return configured
}
