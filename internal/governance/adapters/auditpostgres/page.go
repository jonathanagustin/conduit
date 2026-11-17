package auditpostgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/governance/domain"
)

// PageQuery bounds one read of the chained log. Since is inclusive and Until
// exclusive; a zero time leaves that side open. AfterSequence is a strict seek
// on the chain sequence, which is both the hash-chain order and the primary
// key, so a page costs an index range rather than a sort of the whole log.
type PageQuery struct {
	Resource      string
	Since, Until  time.Time
	AfterSequence uint64
	Limit         int
}

type PageRow struct {
	Sequence uint64
	Event    domain.AuthorizationAuditEvent
}

// createdAtExpression reads the event time out of the stored payload. The
// table has no writable time mirror: the immutable extractor and both time
// indexes derive it from the digest-covered bytes. Keep this exact expression
// aligned with migration 0002. PostgreSQL compares at microsecond resolution.
const createdAtExpression = "conduit_authorization_audit_created_at(payload)"

func (query PageQuery) predicate(seek bool) (string, []any) {
	var clauses []string
	var args []any
	add := func(clause string, value any) {
		args = append(args, value)
		clauses = append(clauses, clause+" $"+strconv.Itoa(len(args)))
	}
	if query.Resource != "" {
		add("resource_rid =", query.Resource)
	}
	if seek && query.AfterSequence > 0 {
		add("sequence >", int64(query.AfterSequence))
	}
	if !query.Since.IsZero() {
		add(createdAtExpression+" >=", query.Since.UTC())
	}
	if !query.Until.IsZero() {
		add(createdAtExpression+" <", query.Until.UTC())
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// PageAuthorizationAudit returns at most Limit events after AfterSequence, in
// chain order. The limit is part of the SQL statement: the Hub never holds
// more than one page of the log, whatever the log's size.
func (store *Store) PageAuthorizationAudit(ctx context.Context, query PageQuery) ([]PageRow, error) {
	if query.Limit < 1 {
		return nil, errors.New("authorization audit page limit must be positive")
	}
	where, args := query.predicate(true)
	args = append(args, query.Limit)
	statement := "SELECT " + columns + " FROM conduit_authorization_audit_events" + where +
		" ORDER BY sequence LIMIT $" + strconv.Itoa(len(args))
	rows, err := store.transaction.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("page authorization audit events: %w", err)
	}
	defer rows.Close()
	page := make([]PageRow, 0, query.Limit)
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		event, err := decodeRecord(record)
		if err != nil {
			return nil, err
		}
		page = append(page, PageRow{Sequence: record.Sequence, Event: event})
	}
	return page, rows.Err()
}

// CountAuthorizationAudit counts the events a query matches, ignoring its seek
// and limit.
func (store *Store) CountAuthorizationAudit(ctx context.Context, query PageQuery) (int, error) {
	where, args := query.predicate(false)
	var total int
	if err := store.transaction.QueryRowContext(ctx, "SELECT COUNT(*) FROM conduit_authorization_audit_events"+where, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count authorization audit events: %w", err)
	}
	return total, nil
}

// HasAuthorizationAuditEvent reports whether an event ID is already chained.
func (store *Store) HasAuthorizationAuditEvent(ctx context.Context, id string) (bool, error) {
	var found bool
	err := store.transaction.QueryRowContext(ctx, "SELECT TRUE FROM conduit_authorization_audit_events WHERE event_id = $1", id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return found, err
}
