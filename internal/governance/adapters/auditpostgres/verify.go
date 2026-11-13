package auditpostgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/governance/domain"
)

// Verify walks the complete chain in one database snapshot. The caller must
// supply a repeatable-read or serializable transaction, preferably read-only.
// A nil checkpoint proves internal consistency, not independence from a DBA.
func (store *Store) Verify(ctx context.Context, expected *domain.AuditCheckpoint) (domain.AuditCheckpoint, error) {
	var isolation string
	if err := store.transaction.QueryRowContext(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
		return domain.AuditCheckpoint{}, err
	}
	if isolation != "repeatable read" && isolation != "serializable" {
		return domain.AuditCheckpoint{}, errors.New("authorization audit verification requires one repeatable database snapshot")
	}
	var trusted domain.AuditCheckpoint
	if expected != nil {
		trusted = *expected
		if err := trusted.Validate(); err != nil {
			return domain.AuditCheckpoint{}, err
		}
	}
	current := domain.EmptyAuditCheckpoint()
	matched := expected == nil || trusted.Sequence == 0
	rows, err := store.transaction.QueryContext(ctx, "SELECT "+columns+" FROM conduit_authorization_audit_events ORDER BY sequence")
	if err != nil {
		return domain.AuditCheckpoint{}, fmt.Errorf("read authorization audit chain: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return domain.AuditCheckpoint{}, err
		}
		next, err := domain.VerifyAuditChainRecord(current, record)
		if err != nil {
			return domain.AuditCheckpoint{}, err
		}
		current = next
		if expected != nil && current.Sequence == trusted.Sequence {
			if current.Digest != trusted.Digest {
				return domain.AuditCheckpoint{}, errors.New("authorization audit chain does not match the independent checkpoint")
			}
			matched = true
		}
	}
	if err := rows.Err(); err != nil {
		return domain.AuditCheckpoint{}, err
	}
	if err := rows.Close(); err != nil {
		return domain.AuditCheckpoint{}, err
	}
	if !matched {
		return domain.AuditCheckpoint{}, errors.New("authorization audit chain is shorter than the independent checkpoint")
	}
	head := domain.AuditCheckpoint{Version: domain.AuditChainVersion}
	if err := store.transaction.QueryRowContext(ctx, "SELECT sequence, digest FROM conduit_authorization_audit_head WHERE singleton = TRUE").Scan(&head.Sequence, &head.Digest); err != nil {
		return domain.AuditCheckpoint{}, err
	}
	if head != current {
		return domain.AuditCheckpoint{}, errors.New("authorization audit head does not match the verified chain")
	}
	return current, nil
}
