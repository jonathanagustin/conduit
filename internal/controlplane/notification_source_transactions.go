package controlplane

import (
	"context"
	"fmt"
)

// notificationScope preserves the caller's policy trust, clocks and token
// source while rebinding all reads and writes to one repository transaction.
// Only that transaction's private intent ports may run before its commit.
func (service *GovernanceService) notificationScope(repository Repository) *GovernanceService {
	working := *service
	working.repository = repository
	lifecycle := *service.lifecycle
	lifecycle.repository = repository
	working.lifecycle = &lifecycle
	working.SetAnnouncements(Announcements{})
	return &working
}

func (service *GovernanceService) withNotificationTransaction(ctx context.Context, operation func(*GovernanceService) error) error {
	transactional, ok := service.repository.(TransactionalRepository)
	if !ok {
		return fmt.Errorf("%w: durable notification sources require a transactional repository", ErrInvalidTransition)
	}
	return transactional.Transaction(ctx, func(repository Repository) error {
		if err := operation(service.notificationScope(repository)); err != nil {
			return err
		}
		return ctx.Err()
	})
}
