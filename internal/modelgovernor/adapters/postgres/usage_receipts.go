package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
)

func (store *Store) BeginUsage(ctx context.Context, receipt modelgovernor.UsageReceipt) (modelgovernor.Totals, error) {
	if err := receipt.Validate(); err != nil {
		return modelgovernor.Totals{}, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return modelgovernor.Totals{}, err
	}
	defer tx.Rollback()
	// Serializes competing begins for this owner/day. Completions make the
	// aggregate and settled receipt visible together in one transaction.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, receipt.Day+"\n"+receipt.Owner); err != nil {
		return modelgovernor.Totals{}, err
	}
	var owner, day string
	var payload []byte
	err = tx.QueryRowContext(ctx, `SELECT owner, day::text, payload FROM conduit_model_usage_receipts WHERE receipt_id = $1`, receipt.ID).Scan(&owner, &day, &payload)
	switch {
	case err == nil:
		if owner != receipt.Owner || day != receipt.Day || payload != nil {
			return modelgovernor.Totals{}, modelgovernor.ErrUsageReceiptConflict
		}
	case errors.Is(err, sql.ErrNoRows):
		var pending bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM conduit_model_usage_receipts WHERE owner = $1 AND day = $2::date AND payload IS NULL)`, receipt.Owner, receipt.Day).Scan(&pending); err != nil {
			return modelgovernor.Totals{}, err
		}
		if pending {
			return modelgovernor.Totals{}, modelgovernor.ErrUsagePending
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO conduit_model_usage_receipts (receipt_id, owner, day) VALUES ($1, $2, $3::date)`, receipt.ID, receipt.Owner, receipt.Day); err != nil {
			return modelgovernor.Totals{}, err
		}
	default:
		return modelgovernor.Totals{}, err
	}
	totals, err := readOwnerDay(ctx, tx, receipt.Owner, receipt.Day)
	if err != nil {
		return modelgovernor.Totals{}, err
	}
	if err := tx.Commit(); err != nil {
		return modelgovernor.Totals{}, err
	}
	return totals, nil
}

func (store *Store) CancelUsage(ctx context.Context, receiptID string) error {
	result, err := store.db.ExecContext(ctx, `DELETE FROM conduit_model_usage_receipts WHERE receipt_id = $1 AND payload IS NULL`, receiptID)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected > 0 {
		return nil
	}
	var exists bool
	if err := store.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM conduit_model_usage_receipts WHERE receipt_id = $1)`, receiptID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return modelgovernor.ErrUsageReceiptConflict
	}
	return nil
}

func (store *Store) completeUsage(ctx context.Context, record modelgovernor.UsageRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner, day string
	var prior []byte
	err = tx.QueryRowContext(ctx, `SELECT owner, day::text, payload FROM conduit_model_usage_receipts WHERE receipt_id = $1 FOR UPDATE`, record.ReceiptID).Scan(&owner, &day, &prior)
	if errors.Is(err, sql.ErrNoRows) {
		return modelgovernor.ErrUsageReceiptConflict
	}
	if err != nil {
		return err
	}
	if owner != record.Owner || day != record.Day {
		return modelgovernor.ErrUsageReceiptConflict
	}
	if prior != nil {
		if bytes.Equal(prior, payload) {
			return nil
		}
		return modelgovernor.ErrUsageReceiptConflict
	}
	if err := recordUsage(ctx, tx, record); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE conduit_model_usage_receipts SET payload = $1 WHERE receipt_id = $2`, payload, record.ReceiptID); err != nil {
		return err
	}
	return tx.Commit()
}
