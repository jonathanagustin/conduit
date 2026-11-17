package auditpostgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
	"github.com/jonathanagustin/conduit/internal/pgtest"
)

func databaseBeforeTimeIndex(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv(pgtest.DataSourceNameEnvironment)
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is required")
	}
	shared, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shared.Close() })
	isolated, drop, err := pgtest.Fresh(t.Context(), shared, dsn, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drop)
	database, err := sql.Open("pgx", isolated)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := pgmigrate.Apply(t.Context(), database, Component(), Migrations()[:1]); err != nil {
		t.Fatal(err)
	}
	return database
}

func timestampPayload(t *testing.T, stamp string) []byte {
	t.Helper()
	encoded, err := json.Marshal(map[string]string{"createdAt": stamp})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestAuditTimeExtractorMatchesExistingPredicateAcrossSessionSettings(t *testing.T) {
	database, _ := databaseForTest(t)
	stamps := []string{
		"0001-01-01T00:00:00Z", "0001-01-01T00:00:00+15:59",
		"0099-12-31T23:59:59Z", "0100-03-01T00:00:00Z",
		"1582-10-15T00:00:00Z", "1900-02-28T23:59:59.9999995Z",
		"2000-02-29T23:59:59.9999995-05:45", "2011-12-30T12:00:00+13:00",
		"2020-03-08T02:30:00-05:00", "2020-11-01T01:30:00-04:00",
		"9999-12-31T23:59:59.9999999-15:59",
	}
	for _, second := range []string{"00", "59"} {
		for _, fraction := range []string{"", ".0", ".0000001", ".000000499", ".0000005", ".000000501", ".0000015", ".1234565", ".1234575", ".9999994", ".9999995", ".9999999"} {
			for _, zone := range []string{"Z", "+00:00", "-00:00", "+05:45", "-15:59"} {
				stamps = append(stamps, "2000-12-31T23:59:"+second+fraction+zone)
			}
		}
	}
	// Reproducible samples span every Go JSON year, nanosecond fraction and
	// numeric offsets accepted by the old PostgreSQL timestamp parser.
	random := rand.New(rand.NewSource(841))
	for range 1024 {
		offset := (random.Intn(1919) - 959) * 60
		stamp := time.Date(1+random.Intn(9999), time.Month(1+random.Intn(12)), 1+random.Intn(28),
			random.Intn(24), random.Intn(60), random.Intn(60), random.Intn(1e9), time.FixedZone("", offset))
		stamps = append(stamps, stamp.Format(time.RFC3339Nano))
	}
	encoded, err := json.Marshal(stamps)
	if err != nil {
		t.Fatal(err)
	}
	settings := []struct {
		zone, date, interval, abbreviations, floats, strings string
	}{
		{"UTC", "ISO, MDY", "postgres", "Default", "3", "on"},
		{"Pacific/Apia", "SQL, DMY", "sql_standard", "Australia", "-15", "off"},
		{"Asia/Kathmandu", "German, DMY", "iso_8601", "India", "0", "on"},
		{"Australia/Lord_Howe", "Postgres, MDY", "postgres_verbose", "Default", "1", "off"},
	}
	for _, setting := range settings {
		t.Run(setting.zone, func(t *testing.T) {
			connection, err := database.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			defer connection.ExecContext(context.Background(), "RESET ALL")
			if _, err := connection.ExecContext(t.Context(), `SELECT
 set_config('TimeZone',$1,false), set_config('DateStyle',$2,false), set_config('IntervalStyle',$3,false),
 set_config('timezone_abbreviations',$4,false), set_config('extra_float_digits',$5,false), set_config('standard_conforming_strings',$6,false)`,
				setting.zone, setting.date, setting.interval, setting.abbreviations, setting.floats, setting.strings); err != nil {
				t.Fatal(err)
			}
			rows, err := connection.QueryContext(t.Context(), `SELECT stamp,
 conduit_authorization_audit_created_at(convert_to(jsonb_build_object('createdAt',stamp)::text,'UTF8')),
 stamp::timestamptz FROM jsonb_array_elements_text($1::jsonb) AS stamps(stamp)`, encoded)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			count := 0
			for rows.Next() {
				var stamp string
				var indexed, existing time.Time
				if err := rows.Scan(&stamp, &indexed, &existing); err != nil {
					t.Fatal(err)
				}
				if !indexed.Equal(existing) {
					t.Fatalf("%s: immutable=%s, old predicate=%s", stamp, indexed.Format(time.RFC3339Nano), existing.Format(time.RFC3339Nano))
				}
				count++
			}
			if err := rows.Err(); err != nil || count != len(stamps) {
				t.Fatalf("compared %d/%d timestamps: %v", count, len(stamps), err)
			}
		})
	}
}

func TestAuditTimeExtractorCoversGoBoundaryYearsAndNumericOffsets(t *testing.T) {
	database, _ := databaseForTest(t)
	for _, stamp := range []string{
		"0000-01-01T00:00:00Z", "0000-02-29T23:59:59.9999995Z",
		"0000-01-01T00:00:00+23:59", "0001-01-01T00:00:00+23:59",
		"2000-01-01T00:00:00.0000005+23:59", "2000-01-01T00:00:00.0000015-23:59",
		"9999-12-31T23:59:59.9999995-23:59",
	} {
		t.Run(stamp, func(t *testing.T) {
			parsed, err := time.Parse(time.RFC3339Nano, stamp)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parsed.MarshalJSON(); err != nil {
				t.Fatalf("fixture is not a Go JSON timestamp: %v", err)
			}
			fraction, err := strconv.ParseFloat(fmt.Sprintf("0.%09d", parsed.Nanosecond()), 64)
			if err != nil {
				t.Fatal(err)
			}
			want := parsed.Add(-time.Duration(parsed.Nanosecond())).Add(time.Duration(math.RoundToEven(fraction*1e6)) * time.Microsecond)
			var got time.Time
			if err := database.QueryRowContext(t.Context(), "SELECT conduit_authorization_audit_created_at($1)", timestampPayload(t, stamp)).Scan(&got); err != nil || !got.Equal(want) {
				t.Fatalf("derived=%s, expected=%s, error=%v", got.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano), err)
			}
		})
	}
	var strict, immutable, parallelSafe, fixedPath bool
	if err := database.QueryRowContext(t.Context(), `SELECT proisstrict, provolatile='i', proparallel='s',
 'search_path=pg_catalog'=ANY(proconfig) FROM pg_proc WHERE oid='conduit_authorization_audit_created_at(bytea)'::regprocedure`).Scan(&strict, &immutable, &parallelSafe, &fixedPath); err != nil || !strict || !immutable || !parallelSafe || !fixedPath {
		t.Fatalf("extractor contract strict=%v immutable=%v parallel=%v fixedPath=%v error=%v", strict, immutable, parallelSafe, fixedPath, err)
	}
	var nullResult bool
	if err := database.QueryRowContext(t.Context(), "SELECT conduit_authorization_audit_created_at(NULL::bytea) IS NULL").Scan(&nullResult); err != nil || !nullResult {
		t.Fatalf("strict NULL input=%v, %v", nullResult, err)
	}
	for _, stamp := range []string{"now", "infinity", "2000-01-01", "2000-01-01T00:00:00", "2000-01-01T00:00:00UTC",
		"2000-01-01t00:00:00z", "2000-01-01T00:00:00Z ", "2000-02-30T00:00:00Z", "2001-02-29T00:00:00Z",
		"2000-00-01T00:00:00Z", "2000-01-00T00:00:00Z", "2000-01-01T24:00:00Z", "2000-01-01T00:60:00Z",
		"2000-01-01T00:00:60Z", "2000-01-01T00:00:00+24:00", "2000-01-01T00:00:00+00:60", "2000-01-01T00:00:00.1234567890Z"} {
		var ignored time.Time
		if err := database.QueryRowContext(t.Context(), "SELECT conduit_authorization_audit_created_at($1)", timestampPayload(t, stamp)).Scan(&ignored); err == nil {
			t.Fatalf("non-Go timestamp accepted: %q", stamp)
		}
	}
}

func TestAuditIndexedWindowMatchesExistingMicrosecondBounds(t *testing.T) {
	database, _ := databaseForTest(t)
	epoch := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for index, nanoseconds := range []int{0, 499, 500, 501, 1499, 1500, 1501, 999999499, 999999500, 999999999} {
		at := epoch.Add(time.Duration(nanoseconds))
		if index%2 == 1 {
			at = at.In(time.FixedZone("", 5*3600+45*60))
		}
		event := domain.NewAuthorizationCheckEvent(at, "operator", "read", fmt.Sprintf("resource-%d", index%2), domain.DecisionAllow, "boundary", nil)
		event.ID = fmt.Sprintf("fraction-%d", index)
		appendEvent(t, database, event)
	}
	for _, bounds := range [][2]time.Duration{
		{0, time.Microsecond}, {time.Microsecond, 2 * time.Microsecond},
		{999999 * time.Microsecond, time.Second}, {time.Nanosecond, 2001 * time.Nanosecond},
	} {
		for _, resource := range []string{"", "resource-1"} {
			since, until := epoch.Add(bounds[0]), epoch.Add(bounds[1])
			statement := `SELECT event_id FROM conduit_authorization_audit_events
 WHERE (convert_from(payload,'UTF8')::jsonb->>'createdAt')::timestamptz >= $1
 AND (convert_from(payload,'UTF8')::jsonb->>'createdAt')::timestamptz < $2`
			args := []any{since, until}
			if resource != "" {
				statement += " AND resource_rid=$3"
				args = append(args, resource)
			}
			rows, err := database.QueryContext(t.Context(), statement+" ORDER BY sequence", args...)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					t.Fatal(err)
				}
				want = append(want, id)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			page, total := pageForTest(t, database, PageQuery{Since: since, Until: until, Resource: resource, Limit: 200})
			if got := rowIDs(page); !reflect.DeepEqual(got, want) || total != len(want) {
				t.Fatalf("bounds=%v resource=%s: indexed=%v total=%d, old=%v", bounds, resource, got, total, want)
			}
		}
	}
}

func TestAuditTimeIndexObjectsStayInTheRepositorySchema(t *testing.T) {
	database, dsn := databaseForTest(t)
	if _, err := database.ExecContext(t.Context(), "CREATE SCHEMA audit_other"); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", "audit_other,pg_catalog")
	parsed.RawQuery = query.Encode()
	other, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := ApplyMigrations(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := other.QueryRowContext(t.Context(), `SELECT n.nspname FROM pg_proc p JOIN pg_namespace n ON p.pronamespace=n.oid
 WHERE p.oid='conduit_authorization_audit_created_at(bytea)'::regprocedure`).Scan(&schema); err != nil || schema != "audit_other" {
		t.Fatalf("derived timestamp function schema=%s, error=%v", schema, err)
	}
	for _, name := range []string{"conduit_authorization_audit_time", "conduit_authorization_audit_resource_time"} {
		var definition string
		if err := other.QueryRowContext(t.Context(), "SELECT indexdef FROM pg_indexes WHERE schemaname='audit_other' AND indexname=$1", name).Scan(&definition); err != nil || !strings.Contains(definition, "ON audit_other.conduit_authorization_audit_events") {
			t.Fatalf("index %s escaped repository schema: %s, %v", name, definition, err)
		}
	}
	event := eventForTest("schema-local")
	event.CreatedAt = time.Date(0, 2, 29, 0, 0, 0, 0, time.UTC)
	appendEvent(t, other, event)
	// January 1 of year 1 is Go's zero-time sentinel (an open bound here).
	// January 2 forces this read through the schema-local time expression.
	page, total := pageForTest(t, other, PageQuery{Until: time.Date(1, 1, 2, 0, 0, 0, 0, time.UTC), Limit: 1})
	if total != 1 || len(page) != 1 || page[0].Event.ID != event.ID {
		t.Fatalf("schema-local year-zero indexed read=%v total=%d", rowIDs(page), total)
	}
}

func TestAuditTimeIndexBuildHoldsWriteBlockingLockUntilCommit(t *testing.T) {
	database := databaseBeforeTimeIndex(t)
	appendEvent(t, database, eventForTest("before-index"))
	migration, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer migration.Rollback()
	if _, err := migration.ExecContext(t.Context(), Migrations()[1].Statements); err != nil {
		t.Fatal(err)
	}
	var holdsShareLock bool
	if err := migration.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_locks
 WHERE pid=pg_backend_pid() AND relation='conduit_authorization_audit_events'::regclass AND mode='ShareLock' AND granted)`).Scan(&holdsShareLock); err != nil || !holdsShareLock {
		t.Fatalf("transactional index build lacks expected table SHARE lock: %v, %v", holdsShareLock, err)
	}
	writer, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	if _, err := writer.ExecContext(t.Context(), "SET LOCAL lock_timeout='100ms'"); err != nil {
		t.Fatal(err)
	}
	_, err = New(writer).AppendAuthorizationAudit(t.Context(), eventForTest("after-index"))
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "55P03" {
		t.Fatalf("append did not hit the index-build lock timeout: %v", err)
	}
	if err := writer.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := migration.Commit(); err != nil {
		t.Fatal(err)
	}
	appendEvent(t, database, eventForTest("after-index"))
	if tip, err := verify(t, database, nil); err != nil || tip.Sequence != 2 {
		t.Fatalf("failed concurrent append leaked or committed append failed: %#v, %v", tip, err)
	}
}

type indexedLedgerSnapshot struct {
	Head    domain.AuditCheckpoint
	Records []domain.AuditChainRecord
}

func snapshotIndexedLedger(t *testing.T, database *sql.DB) indexedLedgerSnapshot {
	t.Helper()
	result := indexedLedgerSnapshot{Head: domain.AuditCheckpoint{Version: domain.AuditChainVersion}}
	if err := database.QueryRowContext(t.Context(), "SELECT sequence, digest FROM conduit_authorization_audit_head WHERE singleton=TRUE").Scan(&result.Head.Sequence, &result.Head.Digest); err != nil {
		t.Fatal(err)
	}
	rows, err := database.QueryContext(t.Context(), "SELECT sequence, previous_digest, event_id, resource_rid, origin, payload, digest FROM conduit_authorization_audit_events ORDER BY sequence")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var record domain.AuditChainRecord
		if err := rows.Scan(&record.Sequence, &record.PreviousDigest, &record.EventID, &record.ResourceRID, &record.Origin, &record.Payload, &record.Digest); err != nil {
			t.Fatal(err)
		}
		result.Records = append(result.Records, record)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func seedIndexedLedger(t *testing.T, database *sql.DB, count int) domain.AuditCheckpoint {
	t.Helper()
	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	previous := domain.EmptyAuditCheckpoint()
	epoch := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for start := 0; start < count; start += 256 {
		var statement strings.Builder
		statement.WriteString("INSERT INTO conduit_authorization_audit_events (sequence, previous_digest, event_id, resource_rid, origin, payload, digest) VALUES ")
		args := []any{}
		for index := start; index < min(start+256, count); index++ {
			event := domain.NewAuthorizationCheckEvent(epoch.Add(time.Duration(index)*time.Second), "operator", "read", fmt.Sprintf("resource-%02d", index%32), domain.DecisionAllow, "synthetic index fixture", nil)
			event.ID = fmt.Sprintf("index-event-%05d", index)
			record, err := domain.NewAuditChainRecord(previous, event, domain.AuditOriginLive)
			if err != nil {
				t.Fatal(err)
			}
			if index > start {
				statement.WriteByte(',')
			}
			base := len(args)
			fmt.Fprintf(&statement, "($%d,$%d,$%d,$%d,$%d,$%d,$%d)", base+1, base+2, base+3, base+4, base+5, base+6, base+7)
			args = append(args, record.Sequence, record.PreviousDigest, record.EventID, record.ResourceRID, record.Origin, record.Payload, record.Digest)
			previous = domain.AuditCheckpoint{Version: domain.AuditChainVersion, Sequence: record.Sequence, Digest: record.Digest}
		}
		if _, err := transaction.ExecContext(t.Context(), statement.String(), args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := transaction.ExecContext(t.Context(), "UPDATE conduit_authorization_audit_head SET sequence=$1,digest=$2 WHERE singleton=TRUE", previous.Sequence, previous.Digest); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	return previous
}

func TestAuditTimeIndexMigrationPreservesLedgerAndSupportsPlanner(t *testing.T) {
	database := databaseBeforeTimeIndex(t)
	const records = 16384
	trusted := seedIndexedLedger(t, database, records)
	before := snapshotIndexedLedger(t, database)
	started := time.Now()
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	buildDuration := time.Since(started)
	after := snapshotIndexedLedger(t, database)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("index migration changed event bytes, coordinates, digests or head")
	}
	if tip, err := verify(t, database, &trusted); err != nil || tip != trusted {
		t.Fatalf("independent checkpoint no longer verifies: %#v, %v", tip, err)
	}
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatalf("repeat migration was not harmless: %v", err)
	}
	for _, statement := range []string{"UPDATE conduit_authorization_audit_events SET resource_rid='rewritten'", "DELETE FROM conduit_authorization_audit_events", "TRUNCATE conduit_authorization_audit_events"} {
		if _, err := database.ExecContext(t.Context(), statement); err == nil {
			t.Fatalf("index migration disabled the append-only guard: %s", statement)
		}
	}
	if _, err := database.ExecContext(t.Context(), "ANALYZE conduit_authorization_audit_events"); err != nil {
		t.Fatal(err)
	}
	epoch := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	since := epoch.Add((records - 64) * time.Second)
	until := since.Add(32 * time.Second)
	for _, test := range []struct {
		name, resource, index string
		want                  int
	}{
		{"time", "", "conduit_authorization_audit_time", 32},
		{"resource-time", "resource-00", "conduit_authorization_audit_resource_time", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := PageQuery{Since: since, Until: until, Resource: test.resource, Limit: 200}
			where, args := query.predicate(false)
			for _, selectSQL := range []string{"SELECT COUNT(*)", "SELECT " + columns} {
				statement := selectSQL + " FROM conduit_authorization_audit_events" + where
				if selectSQL != "SELECT COUNT(*)" {
					statement += " ORDER BY sequence LIMIT 200"
				}
				var plan []byte
				if err := database.QueryRowContext(t.Context(), "EXPLAIN (ANALYZE, FORMAT JSON) "+statement, args...).Scan(&plan); err != nil {
					t.Fatal(err)
				}
				if !bytesContainIndex(plan, test.index) {
					t.Fatalf("planner did not use %s for %s: %s", test.index, selectSQL, plan)
				}
				t.Logf("%s plan: %s", selectSQL, plan)
			}
			page, total := pageForTest(t, database, query)
			if total != test.want || len(page) != test.want {
				t.Fatalf("indexed window rows=%d total=%d want=%d", len(page), total, test.want)
			}
		})
	}
	var version string
	var payloadBytes int64
	if err := database.QueryRowContext(t.Context(), "SELECT version(), (SELECT SUM(octet_length(payload)) FROM conduit_authorization_audit_events)").Scan(&version, &payloadBytes); err != nil {
		t.Fatal(err)
	}
	t.Logf("synthetic transactional index build: rows=%d payloadBytes=%d elapsed=%s server=%s", records, payloadBytes, buildDuration, version)
}

func bytesContainIndex(plan []byte, expected string) bool {
	var documents []struct {
		Plan map[string]any `json:"Plan"`
	}
	if json.Unmarshal(plan, &documents) != nil || len(documents) != 1 {
		return false
	}
	var contains func(map[string]any) bool
	contains = func(node map[string]any) bool {
		condition, _ := node["Index Cond"].(string)
		if node["Index Name"] == expected && strings.Contains(condition, "conduit_authorization_audit_created_at(payload)") &&
			strings.Contains(condition, ">=") && strings.Contains(condition, "<") {
			return expected != "conduit_authorization_audit_resource_time" || strings.Contains(condition, "resource_rid =")
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			if next, ok := child.(map[string]any); ok && contains(next) {
				return true
			}
		}
		return false
	}
	return contains(documents[0].Plan)
}
