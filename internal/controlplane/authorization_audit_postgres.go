package controlplane

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/jonathanagustin/conduit/internal/governance/adapters/auditpostgres"
)

// authorizationAuditView leaves metadata behavior in the existing repository
// while routing audit operations through the same SQL transaction. It avoids
// hydrating or serializing the full audit history on ordinary metadata writes.
type authorizationAuditView struct {
	Repository
	audit                  *auditpostgres.Store
	notificationIntents    Announcements
	hasNotificationIntents bool
}

var _ TransactionalRepository = (*authorizationAuditView)(nil)

func (view *authorizationAuditView) AppendAuthorizationAudit(ctx context.Context, event AuthorizationAuditEvent) (AuthorizationAuditEvent, error) {
	return view.audit.AppendAuthorizationAudit(ctx, event)
}

func (view *authorizationAuditView) ListAuthorizationAudit(ctx context.Context, resource string) ([]AuthorizationAuditEvent, error) {
	events, err := view.audit.ListAuthorizationAudit(ctx, resource)
	if err != nil {
		return nil, err
	}
	// An older writer may still leave records in the legacy snapshot during a
	// rollout. They remain readable until the next new writer imports them; the
	// chained table wins on an ID conflict and is never overwritten by a snapshot.
	legacy, err := view.Repository.ListAuthorizationAudit(ctx, resource)
	if err != nil {
		return nil, err
	}
	indexed := make(map[string]bool, len(events))
	for _, event := range events {
		indexed[event.ID] = true
	}
	for _, event := range legacy {
		if !indexed[event.ID] {
			events = append(events, event)
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].ID < events[j].ID
		}
		return events[i].CreatedAt.Before(events[j].CreatedAt)
	})
	return events, nil
}

func (view *authorizationAuditView) Transaction(ctx context.Context, operation func(Repository) error) error {
	if operation == nil {
		return fmt.Errorf("repository transaction operation is required")
	}
	transactional, ok := view.Repository.(TransactionalRepository)
	if !ok {
		return fmt.Errorf("metadata repository cannot create a nested transaction")
	}
	// The metadata copy must not apply before RELEASE SAVEPOINT succeeds.
	// Otherwise a canceled child can roll back SQL audit/intent while leaving
	// its source metadata in a parent that catches the error and commits.
	return transactional.Transaction(ctx, func(working Repository) error {
		return view.audit.Savepoint(ctx, func() error {
			return operation(&authorizationAuditView{
				Repository: working, audit: view.audit,
				notificationIntents: view.notificationIntents, hasNotificationIntents: view.hasNotificationIntents,
			})
		})
	})
}

func importLegacyAuthorizationAudit(ctx context.Context, transaction *sql.Tx, memory *MemoryRepository) error {
	events := make([]AuthorizationAuditEvent, 0, len(memory.authorizationAudit))
	for _, event := range memory.authorizationAudit {
		events = append(events, event)
	}
	if err := auditpostgres.New(transaction).ImportLegacy(ctx, events); err != nil {
		return fmt.Errorf("adopt legacy authorization audit: %w", err)
	}
	memory.authorizationAudit = nil
	return nil
}

func (repository *PostgresRepository) AppendAuthorizationAudit(ctx context.Context, event AuthorizationAuditEvent) (AuthorizationAuditEvent, error) {
	var result AuthorizationAuditEvent
	err := repository.withStateTransaction(ctx, true, func(_ *MemoryRepository, transaction *sql.Tx) error {
		var err error
		result, err = auditpostgres.New(transaction).AppendAuthorizationAudit(ctx, event)
		return err
	})
	return result, err
}

func (repository *PostgresRepository) ListAuthorizationAudit(ctx context.Context, resource string) ([]AuthorizationAuditEvent, error) {
	var result []AuthorizationAuditEvent
	err := repository.withStateTransaction(ctx, false, func(memory *MemoryRepository, transaction *sql.Tx) error {
		var err error
		result, err = (&authorizationAuditView{Repository: memory, audit: auditpostgres.New(transaction)}).ListAuthorizationAudit(ctx, resource)
		return err
	})
	return result, err
}
