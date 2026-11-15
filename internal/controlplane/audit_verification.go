package controlplane

import (
	"context"
	"database/sql"

	"github.com/jonathanagustin/conduit/internal/governance/adapters/auditpostgres"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

// VerifyAuthorizationAudit checks stored chain links in one read-only snapshot.
// No external checkpoint is asserted; this proves internal consistency only.
func (repository *PostgresRepository) VerifyAuthorizationAudit(ctx context.Context) (governance.AuditCheckpoint, error) {
	transaction, err := repository.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return governance.AuditCheckpoint{}, err
	}
	defer transaction.Rollback()
	checkpoint, err := auditpostgres.New(transaction).Verify(ctx, nil)
	if err != nil {
		return governance.AuditCheckpoint{}, err
	}
	if err = transaction.Commit(); err != nil {
		return governance.AuditCheckpoint{}, err
	}
	return checkpoint, nil
}
