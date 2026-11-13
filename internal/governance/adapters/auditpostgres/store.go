package auditpostgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"github.com/jonathanagustin/conduit/internal/governance/domain"
)

type Store struct {
	transaction *sql.Tx
	savepoints  atomic.Uint64
}

func New(transaction *sql.Tx) *Store { return &Store{transaction: transaction} }

const columns = "sequence, previous_digest, event_id, resource_rid, origin, payload, digest"

type scanner interface{ Scan(...any) error }

func scanRecord(row scanner) (domain.AuditChainRecord, error) {
	var record domain.AuditChainRecord
	err := row.Scan(&record.Sequence, &record.PreviousDigest, &record.EventID, &record.ResourceRID, &record.Origin, &record.Payload, &record.Digest)
	return record, err
}

func decodeRecord(record domain.AuditChainRecord) (domain.AuthorizationAuditEvent, error) {
	if record.Sequence == 0 {
		return domain.AuthorizationAuditEvent{}, errors.New("invalid authorization audit sequence")
	}
	predecessor := domain.AuditCheckpoint{Version: domain.AuditChainVersion, Sequence: record.Sequence - 1, Digest: record.PreviousDigest}
	if _, err := domain.VerifyAuditChainRecord(predecessor, record); err != nil {
		return domain.AuthorizationAuditEvent{}, err
	}
	var event domain.AuthorizationAuditEvent
	if err := json.Unmarshal(record.Payload, &event); err != nil {
		return domain.AuthorizationAuditEvent{}, errors.New("decode authorization audit event")
	}
	return event, nil
}

func (store *Store) AppendAuthorizationAudit(ctx context.Context, event domain.AuthorizationAuditEvent) (domain.AuthorizationAuditEvent, error) {
	return store.append(ctx, event, domain.AuditOriginLive)
}

func (store *Store) append(ctx context.Context, event domain.AuthorizationAuditEvent, origin string) (domain.AuthorizationAuditEvent, error) {
	if err := domain.ValidateAuditCategoryEvent(event); err != nil {
		return domain.AuthorizationAuditEvent{}, err
	}
	if event.ID == "" {
		return domain.AuthorizationAuditEvent{}, errors.New("authorization audit event ID is required")
	}
	var previous domain.AuditCheckpoint
	previous.Version = domain.AuditChainVersion
	if err := store.transaction.QueryRowContext(ctx, "SELECT sequence, digest FROM conduit_authorization_audit_head WHERE singleton = TRUE FOR UPDATE").Scan(&previous.Sequence, &previous.Digest); err != nil {
		return domain.AuthorizationAuditEvent{}, fmt.Errorf("lock authorization audit head: %w", err)
	}
	if err := previous.Validate(); err != nil {
		return domain.AuthorizationAuditEvent{}, err
	}
	// A head accidentally reset behind its rows must not fork later appends.
	var lastSequence uint64
	if err := store.transaction.QueryRowContext(ctx, "SELECT COALESCE(MAX(sequence), 0) FROM conduit_authorization_audit_events").Scan(&lastSequence); err != nil {
		return domain.AuthorizationAuditEvent{}, err
	}
	if lastSequence != previous.Sequence {
		return domain.AuthorizationAuditEvent{}, errors.New("authorization audit head does not match its rows")
	}
	if previous.Sequence > 0 {
		last, err := scanRecord(store.transaction.QueryRowContext(ctx, "SELECT "+columns+" FROM conduit_authorization_audit_events WHERE sequence = $1", previous.Sequence))
		if err != nil {
			return domain.AuthorizationAuditEvent{}, err
		}
		if last.Digest != previous.Digest {
			return domain.AuthorizationAuditEvent{}, errors.New("authorization audit head digest does not match its last row")
		}
		if _, err := decodeRecord(last); err != nil {
			return domain.AuthorizationAuditEvent{}, err
		}
	}
	existing, err := scanRecord(store.transaction.QueryRowContext(ctx, "SELECT "+columns+" FROM conduit_authorization_audit_events WHERE event_id = $1", event.ID))
	if err == nil {
		return decodeRecord(existing)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.AuthorizationAuditEvent{}, fmt.Errorf("read existing authorization audit event: %w", err)
	}
	record, err := domain.NewAuditChainRecord(previous, event, origin)
	if err != nil {
		return domain.AuthorizationAuditEvent{}, err
	}
	if _, err := store.transaction.ExecContext(ctx, `INSERT INTO conduit_authorization_audit_events (`+columns+`) VALUES ($1,$2,$3,$4,$5,$6,$7)`, record.Sequence, record.PreviousDigest, record.EventID, record.ResourceRID, record.Origin, record.Payload, record.Digest); err != nil {
		return domain.AuthorizationAuditEvent{}, fmt.Errorf("append authorization audit event: %w", err)
	}
	if _, err := store.transaction.ExecContext(ctx, "UPDATE conduit_authorization_audit_head SET sequence = $1, digest = $2 WHERE singleton = TRUE", record.Sequence, record.Digest); err != nil {
		return domain.AuthorizationAuditEvent{}, fmt.Errorf("advance authorization audit head: %w", err)
	}
	return decodeRecord(record)
}

// ImportLegacy orders an unordered legacy map deterministically. It preserves
// an already-chained event on duplicate IDs and never asserts historical order.
func (store *Store) ImportLegacy(ctx context.Context, events []domain.AuthorizationAuditEvent) error {
	sort.Slice(events, func(i, j int) bool {
		if events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].ID < events[j].ID
		}
		return events[i].CreatedAt.Before(events[j].CreatedAt)
	})
	for _, event := range events {
		if _, err := store.append(ctx, event, domain.AuditOriginLegacy); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) ListAuthorizationAudit(ctx context.Context, resource string) ([]domain.AuthorizationAuditEvent, error) {
	query := "SELECT " + columns + " FROM conduit_authorization_audit_events"
	var args []any
	if resource != "" {
		query += " WHERE resource_rid = $1"
		args = append(args, resource)
	}
	query += " ORDER BY sequence"
	rows, err := store.transaction.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list authorization audit events: %w", err)
	}
	defer rows.Close()
	events := []domain.AuthorizationAuditEvent{}
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		event, err := decodeRecord(record)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].ID < events[j].ID
		}
		return events[i].CreatedAt.Before(events[j].CreatedAt)
	})
	return events, nil
}

// Savepoint keeps a caught nested transaction failure from leaking an audit
// append into an outer metadata transaction that subsequently succeeds.
func (store *Store) Savepoint(ctx context.Context, operation func() error) error {
	if operation == nil {
		return errors.New("audit savepoint operation is required")
	}
	name := fmt.Sprintf("conduit_audit_scope_%d", store.savepoints.Add(1))
	if _, err := store.transaction.ExecContext(ctx, "SAVEPOINT "+name); err != nil {
		return err
	}
	released := false
	rollback := func() error {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, err := store.transaction.ExecContext(cleanup, "ROLLBACK TO SAVEPOINT "+name+"; RELEASE SAVEPOINT "+name)
		return err
	}
	defer func() {
		if !released {
			_ = rollback()
		}
	}()
	if err := operation(); err != nil {
		cleanupErr := rollback()
		released = true
		return errors.Join(err, cleanupErr)
	}
	if _, err := store.transaction.ExecContext(ctx, "RELEASE SAVEPOINT "+name); err != nil {
		return err
	}
	released = true
	return nil
}
