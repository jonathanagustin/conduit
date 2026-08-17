package postgres

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/domain"
)

// Every Hub start applies the schema. New runs it unconditionally, so a rolling
// upgrade applies it against a populated database, and a second replica applies
// it against a database the first replica is already using.
//
// There IS a migration idiom here, and it is worth naming because it is easy to
// miss: the proposal-outbox columns were added with ADD COLUMN IF NOT EXISTS,
// backfilled by an UPDATE guarded on IS NULL, and then constrained with
// ALTER COLUMN SET NOT NULL. Forward-only, idempotent, re-applied on every
// start. What is missing is a schema version, an ordering guarantee, a down
// path, and any test that an upgrade over populated data works -- recorded in
// docs/operations/PRODUCTION-READINESS.md.
//
// These tests do not close that gap. They pin the properties the current idiom
// depends on, so the first change that breaks one fails here rather than in an
// upgrade.

// destructive statements that would make re-applying the schema lose data.
//
// ALTER is deliberately NOT on this list wholesale, and the first draft of this
// test had it there and failed. The schema already carries a migration -- two
// ADD COLUMN IF NOT EXISTS, a backfill UPDATE guarded by IS NULL, and two
// ALTER COLUMN SET NOT NULL -- and every one of those is idempotent and
// forward-safe. Banning ALTER would have banned the one migration idiom this
// repository actually has. What is banned is the subset that destroys or
// rewrites: dropping, renaming, and retyping.
//
// Each is spelled separately rather than as one alternation so a failure names
// the specific thing that was written.
var destructiveDDL = map[string]*regexp.Regexp{
	"DROP TABLE":        regexp.MustCompile(`(?i)\bDROP\s+TABLE\b`),
	"DROP INDEX":        regexp.MustCompile(`(?i)\bDROP\s+INDEX\b`),
	"DROP COLUMN":       regexp.MustCompile(`(?i)\bDROP\s+COLUMN\b`),
	"TRUNCATE":          regexp.MustCompile(`(?i)\bTRUNCATE\b`),
	"DELETE FROM":       regexp.MustCompile(`(?i)\bDELETE\s+FROM\b`),
	"CREATE OR REPLACE": regexp.MustCompile(`(?i)\bCREATE\s+OR\s+REPLACE\b`),
	"RENAME":            regexp.MustCompile(`(?i)\bRENAME\b`),
	"column retype":     regexp.MustCompile(`(?i)\bALTER\s+COLUMN\s+\w+\s+TYPE\b`),
}

// The schema has to be safe to apply to a database that already has data in it,
// and that safety is currently a property of how the DDL is written rather than
// of anything enforcing it. This reads the DDL and says so.
//
// It needs no database, which is the point: a contributor who adds a DROP finds
// out from `go test ./...` rather than from a gate that only runs where Docker
// does.
func TestSchemaIsSafeToReapplyByConstruction(t *testing.T) {
	t.Parallel()
	if strings.TrimSpace(schema) == "" {
		t.Fatal("the schema constant is empty; this test is checking nothing")
	}
	for name, pattern := range destructiveDDL {
		if pattern.MatchString(schema) {
			t.Errorf(
				"the schema contains %s. It is applied on every Hub start, including against a "+
					"populated database during a rolling upgrade and by every additional replica, so a "+
					"statement of this kind is a data-loss event on restart rather than a migration",
				name,
			)
		}
	}
}

// Idempotence is what makes the above safe, and it is carried entirely by
// IF NOT EXISTS. A CREATE without it fails the second start rather than
// corrupting anything, which is a smaller problem than data loss but still one
// that takes the Hub down on upgrade rather than at review time.
func TestEveryCreateInTheSchemaIsConditional(t *testing.T) {
	t.Parallel()
	creates := regexp.MustCompile(`(?i)\bCREATE\s+(?:UNIQUE\s+)?(TABLE|INDEX)\b(\s+IF\s+NOT\s+EXISTS\b)?`)
	found := 0
	for _, match := range creates.FindAllStringSubmatch(schema, -1) {
		found++
		if strings.TrimSpace(match[2]) == "" {
			t.Errorf(
				"the schema has a CREATE %s without IF NOT EXISTS; the second Hub start will fail on it",
				strings.ToUpper(match[1]),
			)
		}
	}
	// A pattern that matched nothing would pass the loop above silently, and
	// then keep passing after somebody rewrote the DDL into a shape it does not
	// recognise.
	if found == 0 {
		t.Fatal("no CREATE statements matched; the pattern no longer fits the schema it is guarding")
	}
}

// The end-to-end half of the same property, against a real server: data written
// before the schema is applied again must still be there afterwards.
//
// This is the rolling upgrade in miniature. It passes trivially today, because
// the schema has not changed; it is here so that the first change which is not
// backward compatible fails as a test rather than as an outage. That is worth
// saying plainly -- a test that cannot fail today is only worth keeping if it
// can fail on the change it is guarding, and this one can.
func TestPostgresSchemaReappliedPreservesExistingRows(t *testing.T) {
	ctx := t.Context()
	first, _ := newStorePair(t, ctx)
	resetStore(t, ctx, first)

	key := domain.Key{Environment: "prod", Installation: "checkout"}
	recordedAt := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	if err := first.UpsertDesired(ctx, testDesired(key, "gen-1", "target-1", recordedAt)); err != nil {
		t.Fatal(err)
	}

	// A fresh Store re-runs the schema, exactly as a restarted Hub or a newly
	// scheduled replica does.
	second := reopenStore(t, ctx)

	snapshot, found, err := second.CurrentDesired(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("re-applying the schema lost desired state written before it")
	}
	if snapshot.Generation != "gen-1" {
		t.Errorf("generation survived as %q, want gen-1", snapshot.Generation)
	}

	// And the store still works afterwards, which is the half a row count would
	// miss: a schema that preserved rows but broke writes is no better.
	later := recordedAt.Add(time.Minute)
	if err := second.UpsertDesired(ctx, testDesired(key, "gen-2", "target-2", later)); err != nil {
		t.Fatalf("the store could not write after the schema was re-applied: %v", err)
	}
	snapshot, found, err = second.CurrentDesired(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if !found || snapshot.Generation != "gen-2" {
		t.Errorf("post-reapply write did not take: found=%v generation=%q", found, snapshot.Generation)
	}
}

func reopenStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()
	_, reopened := newStorePair(t, ctx)
	return reopened
}
