// Package auditpostgres persists the authorization audit separately from Hub
// metadata. Callers supply the surrounding transaction so decisions and their
// audit events commit together.
package auditpostgres

import (
	"context"
	"database/sql"
	"strings"

	"github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

func Component() string { return "authorization-audit" }

func Migrations() []pgmigrate.Migration {
	return []pgmigrate.Migration{{Name: "0001-authorization-audit-chain", Statements: strings.ReplaceAll(`
CREATE TABLE conduit_authorization_audit_head (
 singleton boolean PRIMARY KEY DEFAULT TRUE CHECK (singleton),
 sequence bigint NOT NULL CHECK (sequence >= 0),
 digest text NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$')
);
INSERT INTO conduit_authorization_audit_head (singleton, sequence, digest) VALUES (TRUE, 0, '__GENESIS__');
CREATE TABLE conduit_authorization_audit_events (
 sequence bigint PRIMARY KEY CHECK (sequence > 0),
 event_id text NOT NULL UNIQUE CHECK (event_id <> ''),
 resource_rid text NOT NULL,
 origin text NOT NULL CHECK (origin IN ('LIVE', 'LEGACY_IMPORT')),
 previous_digest text NOT NULL CHECK (previous_digest ~ '^[0-9a-f]{64}$'),
 payload bytea NOT NULL CHECK (octet_length(payload) > 0),
 digest text NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$')
);
CREATE INDEX conduit_authorization_audit_resource ON conduit_authorization_audit_events (resource_rid, sequence);
DO $guard$
BEGIN
 EXECUTE format($ddl$CREATE FUNCTION %I.conduit_authorization_audit_immutable() RETURNS trigger LANGUAGE plpgsql AS $function$
 BEGIN
  RAISE EXCEPTION 'authorization audit events are append-only' USING ERRCODE = '55000';
 END;
 $function$$ddl$, current_schema());
 EXECUTE format('CREATE TRIGGER conduit_authorization_audit_no_rewrite BEFORE UPDATE OR DELETE ON %I.conduit_authorization_audit_events FOR EACH ROW EXECUTE FUNCTION %I.conduit_authorization_audit_immutable()', current_schema(), current_schema());
 EXECUTE format('CREATE TRIGGER conduit_authorization_audit_no_truncate BEFORE TRUNCATE ON %I.conduit_authorization_audit_events FOR EACH STATEMENT EXECUTE FUNCTION %I.conduit_authorization_audit_immutable()', current_schema(), current_schema());
END;
$guard$;
`, "__GENESIS__", domain.EmptyAuditCheckpoint().Digest)}}
}

func ApplyMigrations(ctx context.Context, database *sql.DB) error {
	return pgmigrate.Apply(ctx, database, Component(), Migrations())
}
