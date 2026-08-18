// Package postgres persists notification events, the preferences that decide
// who hears about them, and the per-recipient inbox entries they produce. It
// owns its schema independently of every other component in the same database.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

// component names this store's rows in the shared migration ledger. It is an
// identifier in a database, so it changes never.
const component = "notifications"

// migrations are applied in this order, once per database, and recorded.
//
// This one is written after the ledger existed, so it does not have to be
// idempotent the way the reconciliation store's first two do -- see
// docs/operations/schema-migrations.md, which scopes that requirement to the
// migrations that predate the ledger. The IF NOT EXISTS clauses below are
// therefore not load-bearing; they are kept because a half-applied migration is
// impossible here but a hand-created table during development is not.
//
// Three tables rather than one, because the three have different lifetimes. An
// event is immutable once raised. A preference is edited by whoever owns it. An
// inbox entry is created by the fan-out and then mutated by exactly one person
// marking it read -- which is why read state lives here and not on the event:
// two people told about the same failure read it independently.
var migrations = []pgmigrate.Migration{
	{
		Name: "0001-notifications-core",
		Statements: `
CREATE TABLE IF NOT EXISTS conduit_notification_events (
    id text PRIMARY KEY,
    type text NOT NULL,
    resource_rid text NOT NULL,
    environment text NOT NULL DEFAULT '',
    occurred_at timestamptz NOT NULL,
    document bytea NOT NULL
);
CREATE INDEX IF NOT EXISTS conduit_notification_events_resource
    ON conduit_notification_events (resource_rid, occurred_at DESC, id DESC);
CREATE TABLE IF NOT EXISTS conduit_notification_preferences (
    id text PRIMARY KEY,
    owner_kind text NOT NULL,
    owner_id text NOT NULL,
    resource_rid text NOT NULL,
    channel text NOT NULL,
    updated_at timestamptz NOT NULL,
    document bytea NOT NULL,
    UNIQUE (owner_kind, owner_id, resource_rid, channel)
);
CREATE INDEX IF NOT EXISTS conduit_notification_preferences_resource
    ON conduit_notification_preferences (resource_rid);
CREATE TABLE IF NOT EXISTS conduit_notification_inbox (
    event_id text NOT NULL REFERENCES conduit_notification_events (id) ON DELETE CASCADE,
    owner_kind text NOT NULL,
    owner_id text NOT NULL,
    channel text NOT NULL,
    preference_id text NOT NULL,
    occurred_at timestamptz NOT NULL,
    read_at timestamptz,
    PRIMARY KEY (event_id, owner_kind, owner_id, channel)
);
CREATE INDEX IF NOT EXISTS conduit_notification_inbox_recipient
    ON conduit_notification_inbox (owner_kind, owner_id, channel, occurred_at DESC, event_id DESC);`,
	},
	{
		// Delivery state, added to the inbox rather than to a table beside it.
		//
		// The columns are the reconciliation proposal outbox's, meaning for
		// meaning: when it is due, who holds it, how many attempts it has cost,
		// whether there is any point trying again, and why it last failed. That
		// outbox already solved at-least-once delivery against real PostgreSQL,
		// and a second table with a second lease protocol would be a second set
		// of bugs to find.
		//
		// One row rather than two, because an inbox entry is already the unit of
		// delivery: it is exactly "this event, to this recipient, on this
		// channel", which is what a transport sends. A parallel table would
		// carry the same primary key and have to be kept in step with it.
		//
		// delivery_available_at stays nullable, which is the one deliberate
		// departure from the three-step non-null idiom in
		// docs/operations/schema-migrations.md. NULL is a fact here rather than
		// a missing value: this entry was never queued for a transport, either
		// because it is an IN_APP entry the inbox itself delivers, or because it
		// was recorded before this migration existed. Backfilling history to a
		// due time would make the first start after an upgrade attempt to
		// deliver every notification the Hub ever recorded, which is exactly the
		// outbound behaviour an existing deployment must not gain by upgrading.
		//
		// The three NOT NULL columns carry constant defaults, so PostgreSQL 11+
		// records the default in the catalogue and rewrites no rows; the brief
		// ACCESS EXCLUSIVE lock is the whole cost. No CREATE INDEX CONCURRENTLY,
		// because every migration here runs inside one transaction.
		Name: "0002-notification-delivery",
		Statements: `
ALTER TABLE conduit_notification_inbox
    ADD COLUMN IF NOT EXISTS delivery_available_at timestamptz;
ALTER TABLE conduit_notification_inbox
    ADD COLUMN IF NOT EXISTS delivered_at timestamptz;
ALTER TABLE conduit_notification_inbox
    ADD COLUMN IF NOT EXISTS delivery_lease_token text;
ALTER TABLE conduit_notification_inbox
    ADD COLUMN IF NOT EXISTS delivery_lease_expires_at timestamptz;
ALTER TABLE conduit_notification_inbox
    ADD COLUMN IF NOT EXISTS delivery_attempt integer NOT NULL DEFAULT 0 CHECK (delivery_attempt >= 0);
ALTER TABLE conduit_notification_inbox
    ADD COLUMN IF NOT EXISTS delivery_exhausted boolean NOT NULL DEFAULT false;
ALTER TABLE conduit_notification_inbox
    ADD COLUMN IF NOT EXISTS delivery_error text NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS conduit_notification_inbox_deliverable
    ON conduit_notification_inbox (delivery_available_at, event_id)
    WHERE delivery_available_at IS NOT NULL AND delivered_at IS NULL AND delivery_exhausted = false;`,
	},
	{
		// The address book: one destination per (owner, kind).
		//
		// A table of its own rather than columns on the preference table,
		// because a contact outlives every preference that names it -- Apollo
		// creates one in Hub Settings and a resource only selects it -- and
		// because a preference is edited per (resource, channel) while a contact
		// is not about a resource at all. Folding them together would make
		// deleting one address a matter of finding every row that repeated it.
		//
		// Columns rather than the `document bytea` the three tables above use,
		// which is a deliberate departure from them. The shape is small and
		// fixed, but the reason is what the shape holds: a column can be updated
		// in place, deleted by primary key, and read by an operator answering
		// "what do you hold about me" without decoding a blob. Personal data
		// inside a JSON document is personal data somebody eventually greps.
		//
		// The primary key is the editing grain, so saving the same dialog twice
		// leaves one row. The identifier is derived from that same tuple by
		// application.ContactID, so it is carried rather than generated and is
		// unique without a second constraint restating the key.
		//
		// No CREATE INDEX CONCURRENTLY -- every migration here runs inside one
		// transaction -- and no index beyond the primary key, because every read
		// is by owner and the key's leading columns already answer that.
		Name: "0003-notification-contacts",
		Statements: `
CREATE TABLE IF NOT EXISTS conduit_notification_contacts (
    owner_kind text NOT NULL,
    owner_id text NOT NULL,
    kind text NOT NULL,
    id text NOT NULL,
    address text NOT NULL,
    opt_into_future_resources boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (owner_kind, owner_id, kind)
);`,
	},
}

// Migrations is this store's schema as the runner sees it, and Component names
// it in the ledger. Together they let a caller outside this package ask what a
// start would apply without starting one.
//
// The clone is deliberate, for the same reason the reconciliation store clones:
// the slice is package state Apply reads on every start, so handing out the
// original would let a caller that appended to it change what a later start
// runs, from another package, with nothing to see it.
func Migrations() []pgmigrate.Migration { return slices.Clone(migrations) }

// Component is the name this store's rows carry in the shared ledger.
func Component() string { return component }

// Store is the durable notification adapter. Every Hub replica may hold one;
// nothing here leases, because recording an event is not work to be claimed.
type Store struct {
	database *sql.DB
}

var _ ports.Store = (*Store)(nil)

func New(ctx context.Context, dataSourceName string) (*Store, error) {
	if strings.TrimSpace(dataSourceName) == "" {
		return nil, errors.New("PostgreSQL data source name is required")
	}
	database, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open notification store: %w", err)
	}
	database.SetMaxOpenConns(10)
	database.SetMaxIdleConns(2)
	database.SetConnMaxLifetime(30 * time.Minute)
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("connect notification store: %w", err)
	}
	if err := pgmigrate.Apply(ctx, database, component, migrations); err != nil {
		database.Close()
		return nil, fmt.Errorf("initialize notification store: %w", err)
	}
	return &Store{database: database}, nil
}

func (store *Store) Close() error {
	if store == nil || store.database == nil {
		return nil
	}
	return store.database.Close()
}

// RecordEvent commits the event and every entry it reached in one transaction.
//
// Both inserts skip conflicts, and the reason is a real path rather than
// defensiveness. An emit site raises this from inside a retry loop: it writes
// the notification first and marks its own work terminal second, so that a
// crash between the two loses a duplicate rather than the escalation. The
// duplicate then arrives here with the same event ID, and it must land as
// nothing at all -- not as a second inbox row telling somebody the same thing
// twice, and not as an error that would wedge the retry that produced it.
func (store *Store) RecordEvent(
	ctx context.Context, event domain.Event, entries []domain.InboxEntry,
) error {
	if err := event.Validate(); err != nil {
		return fmt.Errorf("refuse notification event: %w", err)
	}
	document, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode notification event %s: %w", event.ID, err)
	}
	transaction, err := store.begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO conduit_notification_events (id, type, resource_rid, environment, occurred_at, document)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO NOTHING`,
		event.ID, string(event.Type), event.ResourceRID, event.Environment,
		event.OccurredAt.UTC(), document); err != nil {
		return fmt.Errorf("record notification event %s: %w", event.ID, err)
	}
	for _, entry := range entries {
		if entry.Event.ID != event.ID {
			return fmt.Errorf("inbox entry names event %q while recording %q", entry.Event.ID, event.ID)
		}
		// Queued for a transport in the same transaction that creates the entry,
		// and only when the channel needs one. Same transaction because an entry
		// that exists without its due time is one nothing will ever deliver and
		// nothing will ever report as undelivered; only when needed because an
		// IN_APP entry is delivered by being here, and handing it to a webhook
		// would send it somewhere its owner never chose.
		//
		// Queuing happens whether or not a transport is configured. Nothing
		// leaves the process until one is -- the dispatcher is not constructed
		// otherwise -- but the record of what was owed survives the decision, so
		// enabling delivery later is a configuration change rather than a
		// migration.
		dueAt := sql.NullTime{Time: event.OccurredAt.UTC(), Valid: domain.NeedsTransport(entry)}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO conduit_notification_inbox
    (event_id, owner_kind, owner_id, channel, preference_id, occurred_at, delivery_available_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (event_id, owner_kind, owner_id, channel) DO NOTHING`,
			event.ID, string(entry.Recipient.Owner.Kind), entry.Recipient.Owner.ID,
			string(entry.Recipient.Channel), entry.PreferenceID, event.OccurredAt.UTC(),
			dueAt); err != nil {
			return fmt.Errorf("record inbox entry for %s: %w", entry.Recipient.Owner.ID, err)
		}
	}
	return commit(transaction, "notification event")
}

// SavePreference stores one owner's settings for one resource on one channel.
//
// Keyed on (owner, resource, channel) rather than on the preference's own ID,
// because that tuple is what Apollo's settings page edits: saving the page
// twice must leave one row, and an ID-keyed upsert would leave two the moment a
// caller minted a fresh identifier for the same edit.
func (store *Store) SavePreference(ctx context.Context, preference domain.Preference) error {
	if err := preference.Validate(); err != nil {
		return fmt.Errorf("refuse notification preference: %w", err)
	}
	document, err := json.Marshal(preference)
	if err != nil {
		return fmt.Errorf("encode notification preference %s: %w", preference.ID, err)
	}
	updatedAt := preference.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = preference.CreatedAt
	}
	if _, err := store.database.ExecContext(ctx, `
INSERT INTO conduit_notification_preferences
    (id, owner_kind, owner_id, resource_rid, channel, updated_at, document)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (owner_kind, owner_id, resource_rid, channel) DO UPDATE SET
    id = EXCLUDED.id,
    updated_at = EXCLUDED.updated_at,
    document = EXCLUDED.document`,
		preference.ID, string(preference.Owner.Kind), preference.Owner.ID,
		preference.ResourceRID, string(preference.Channel), updatedAt.UTC(), document); err != nil {
		return fmt.Errorf("save notification preference %s: %w", preference.ID, err)
	}
	return nil
}

// PreferencesFor returns every stored preference for a resource, in a stable
// order so two reads of an unchanged table fan out identically.
func (store *Store) PreferencesFor(
	ctx context.Context, resourceRID string,
) ([]domain.Preference, error) {
	if strings.TrimSpace(resourceRID) == "" {
		return nil, errors.New("reading preferences requires the resource they are about")
	}
	rows, err := store.database.QueryContext(ctx, `
SELECT document FROM conduit_notification_preferences
WHERE resource_rid = $1
ORDER BY owner_kind, owner_id, channel, id`, resourceRID)
	if err != nil {
		return nil, fmt.Errorf("read notification preferences for %s: %w", resourceRID, err)
	}
	defer rows.Close()
	preferences := []domain.Preference{}
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return nil, fmt.Errorf("read notification preferences for %s: %w", resourceRID, err)
		}
		var preference domain.Preference
		if err := json.Unmarshal(document, &preference); err != nil {
			return nil, fmt.Errorf("decode stored notification preference: %w", err)
		}
		preferences = append(preferences, preference)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read notification preferences for %s: %w", resourceRID, err)
	}
	return preferences, nil
}

// Inbox reads what reached one recipient, most recent first.
func (store *Store) Inbox(
	ctx context.Context, recipient domain.Recipient, limit int,
) ([]domain.InboxEntry, error) {
	if strings.TrimSpace(recipient.Owner.ID) == "" || limit <= 0 {
		return nil, errors.New("reading an inbox requires an owner and a positive limit")
	}
	rows, err := store.database.QueryContext(ctx, `
SELECT inbox.preference_id, inbox.read_at, event.document
FROM conduit_notification_inbox AS inbox
JOIN conduit_notification_events AS event ON event.id = inbox.event_id
WHERE inbox.owner_kind = $1 AND inbox.owner_id = $2 AND inbox.channel = $3
ORDER BY inbox.occurred_at DESC, inbox.event_id DESC
LIMIT $4`,
		string(recipient.Owner.Kind), recipient.Owner.ID, string(recipient.Channel), limit)
	if err != nil {
		return nil, fmt.Errorf("read notification inbox for %s: %w", recipient.Owner.ID, err)
	}
	defer rows.Close()
	entries := []domain.InboxEntry{}
	for rows.Next() {
		var (
			preferenceID string
			readAt       sql.NullTime
			document     []byte
		)
		if err := rows.Scan(&preferenceID, &readAt, &document); err != nil {
			return nil, fmt.Errorf("read notification inbox for %s: %w", recipient.Owner.ID, err)
		}
		var event domain.Event
		if err := json.Unmarshal(document, &event); err != nil {
			return nil, fmt.Errorf("decode stored notification event: %w", err)
		}
		entry := domain.InboxEntry{Recipient: recipient, Event: event, PreferenceID: preferenceID}
		if readAt.Valid {
			entry.ReadAt = readAt.Time.UTC()
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read notification inbox for %s: %w", recipient.Owner.ID, err)
	}
	return entries, nil
}

// EventsForResource reads what the Hub raised about a resource, subscribed to
// or not, most recent first.
func (store *Store) EventsForResource(
	ctx context.Context, resourceRID string, limit int,
) ([]domain.Event, error) {
	if strings.TrimSpace(resourceRID) == "" || limit <= 0 {
		return nil, errors.New("reading events requires a resource and a positive limit")
	}
	rows, err := store.database.QueryContext(ctx, `
SELECT document FROM conduit_notification_events
WHERE resource_rid = $1
ORDER BY occurred_at DESC, id DESC
LIMIT $2`, resourceRID, limit)
	if err != nil {
		return nil, fmt.Errorf("read notification events for %s: %w", resourceRID, err)
	}
	defer rows.Close()
	events := []domain.Event{}
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return nil, fmt.Errorf("read notification events for %s: %w", resourceRID, err)
		}
		var event domain.Event
		if err := json.Unmarshal(document, &event); err != nil {
			return nil, fmt.Errorf("decode stored notification event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read notification events for %s: %w", resourceRID, err)
	}
	return events, nil
}

func (store *Store) begin(ctx context.Context) (*sql.Tx, error) {
	if store == nil || store.database == nil {
		return nil, errors.New("notification store is closed")
	}
	transaction, err := store.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin notification transaction: %w", err)
	}
	return transaction, nil
}

func commit(transaction *sql.Tx, operation string) error {
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", operation, err)
	}
	return nil
}
