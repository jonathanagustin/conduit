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
`, "__GENESIS__", domain.EmptyAuditCheckpoint().Digest)}, {
		Name: "0002-authorization-audit-time-indexes", Statements: `
DO $time_indexes$
DECLARE
 target_schema text := current_schema();
BEGIN
 -- Never cast arbitrary timestamp text inside an IMMUTABLE wrapper: that
 -- parser accepts session-dependent dates, zones and relative values.
 EXECUTE format($ddl$CREATE FUNCTION %I.conduit_authorization_audit_created_at(payload bytea)
 RETURNS timestamp with time zone
 LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE
 SET search_path = pg_catalog
 AS $function$
 DECLARE
  parts text[];
  year_value integer;
  hour_value integer;
  minute_value integer;
  second_value integer;
  zone_hour integer;
  zone_minute integer;
  zone_minutes integer := 0;
  fractional_microseconds integer := 0;
  local_time timestamp without time zone;
 BEGIN
  parts := regexp_match(convert_from(payload, 'UTF8')::jsonb->>'createdAt',
   '^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(?:[.]([0-9]{1,9}))?(Z|[+-][0-9]{2}:[0-9]{2})$' COLLATE "C");
  IF parts IS NULL THEN
   RAISE EXCEPTION 'authorization audit createdAt must be a Go RFC3339 timestamp' USING ERRCODE = '22007';
  END IF;
  year_value := parts[1]::integer;
  -- Go uses astronomical numbering; PostgreSQL's constructor calls year 0
  -- "1 BC" and accepts it as -1. The stored JSON bytes remain untouched.
  IF year_value = 0 THEN year_value := -1; END IF;
  hour_value := parts[4]::integer;
  minute_value := parts[5]::integer;
  second_value := parts[6]::integer;
  IF hour_value > 23 OR minute_value > 59 OR second_value > 59 THEN
   RAISE EXCEPTION 'authorization audit createdAt time is out of range' USING ERRCODE = '22008';
  END IF;
  IF parts[7] IS NOT NULL THEN
   -- Match PostgreSQL ParseFractionalSecond: strtod(frac), then rint at
   -- microsecond resolution. Do not add whole seconds before rounding.
   fractional_microseconds := round(('0.' || parts[7])::double precision * 1000000)::integer;
  END IF;
  IF parts[8] <> 'Z' THEN
   zone_hour := substring(parts[8] FROM 2 FOR 2)::integer;
   zone_minute := substring(parts[8] FROM 5 FOR 2)::integer;
   IF zone_hour > 23 OR zone_minute > 59 THEN
    RAISE EXCEPTION 'authorization audit createdAt offset is out of range' USING ERRCODE = '22008';
   END IF;
   zone_minutes := zone_hour * 60 + zone_minute;
   IF left(parts[8], 1) = '-' THEN zone_minutes := -zone_minutes; END IF;
  END IF;
  local_time := make_timestamp(year_value, parts[2]::integer, parts[3]::integer,
   hour_value, minute_value, second_value::double precision)
   + fractional_microseconds * interval '1 microsecond'
   - zone_minutes * interval '1 minute';
  -- The interval overload is a fixed numeric offset. A text timezone, even
  -- 'UTC', would involve timezone-abbreviation lookup instead.
  RETURN local_time AT TIME ZONE interval '0 seconds';
 END;
 $function$$ddl$, target_schema);
 EXECUTE format('CREATE INDEX conduit_authorization_audit_time ON %I.conduit_authorization_audit_events (%I.conduit_authorization_audit_created_at(payload), sequence)', target_schema, target_schema);
 EXECUTE format('CREATE INDEX conduit_authorization_audit_resource_time ON %I.conduit_authorization_audit_events (resource_rid, %I.conduit_authorization_audit_created_at(payload), sequence)', target_schema, target_schema);
END;
$time_indexes$;
`}}
}

func ApplyMigrations(ctx context.Context, database *sql.DB) error {
	return pgmigrate.Apply(ctx, database, Component(), Migrations())
}
