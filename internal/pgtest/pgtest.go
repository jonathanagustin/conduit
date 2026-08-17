// Package pgtest builds throwaway PostgreSQL databases for tests that need one
// with nothing in it at all.
//
// Most database-backed tests here share one server and one database and clean
// up after themselves with TRUNCATE, which is right: creating a database per
// test would be slower than the tests are. Two questions cannot be asked that
// way, and both are about absence rather than content:
//
//   - what a dry run reports on a database that has never been migrated, where
//     the ledger table itself must not exist; and
//   - whether a restore reconstructs a database, which needs somewhere empty to
//     restore into.
//
// The shared database has a ledger and every table in the product, and the one
// existing way to reach the fresh-database case -- dropping the ledger in place
// -- is deliberately confined to the store that can put it back
// (TestDifferentComponentsRacingAFreshDatabaseDoNotCollide). This package is
// how the rest of the suite asks the same question without a suite-wide side
// effect.
//
// It takes no dependency on `testing`, so it is ordinary code that returns
// ordinary errors and the caller decides whether a failure is fatal.
package pgtest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// DataSourceNameEnvironment names the variable every PostgreSQL-backed test in
// this repository reads. Spelled once, here, because a test that reads a
// misspelled variable skips silently and looks like a passing test.
const DataSourceNameEnvironment = "CONDUIT_TEST_POSTGRES_DSN"

// Fresh creates an empty database on the server the shared handle is connected
// to and returns a data source name for it, plus a function that drops it.
//
// The drop is returned rather than deferred internally because the caller owns
// the lifetime -- a restore drill needs its database to outlive the function
// that made it. Calling the returned function twice is safe.
//
// seed is a human-readable hint, normally the test's name. It is sanitised
// rather than trusted: CREATE DATABASE cannot be parameterised, so the
// identifier is built here from a fixed alphabet plus random bytes, and never
// interpolated from anything a caller supplies verbatim.
func Fresh(
	ctx context.Context, shared *sql.DB, dataSourceName, seed string,
) (fresh string, drop func(), err error) {
	name, err := databaseName(seed)
	if err != nil {
		return "", nil, err
	}
	fresh, err = WithDatabase(dataSourceName, name)
	if err != nil {
		return "", nil, err
	}
	if _, err := shared.ExecContext(ctx, `CREATE DATABASE `+quoteIdentifier(name)); err != nil {
		return "", nil, fmt.Errorf("create throwaway database %s: %w", name, err)
	}
	dropped := false
	return fresh, func() {
		if dropped {
			return
		}
		dropped = true
		// WITH (FORCE) rather than a plain DROP. A pool that has been used and
		// closed can still hold a connection the server has not yet reaped, and
		// a DROP that loses that race fails -- leaving a database behind on a
		// server every other test in the run shares. The context is detached
		// because cleanup has to happen even when a cancelled context is why
		// the caller is unwinding.
		_, _ = shared.ExecContext(context.WithoutCancel(ctx),
			`DROP DATABASE IF EXISTS `+quoteIdentifier(name)+` WITH (FORCE)`)
	}, nil
}

// WithDatabase points a data source name at a different database on the same
// server, keeping the credentials and parameters it already carries.
//
// Exported because the restore drill needs it twice over: once for the database
// it creates, and once to rewrite the DSN it hands to pg_dump, which may reach
// the same server by a different address than the test does.
func WithDatabase(dataSourceName, name string) (string, error) {
	parsed, err := url.Parse(dataSourceName)
	if err != nil {
		return "", fmt.Errorf("parse the PostgreSQL data source name: %w", err)
	}
	if parsed.Scheme == "" {
		// The key/value form (host=... dbname=...) would need a different
		// rewrite and nothing here produces it. Refusing beats returning a
		// string that silently still points at the shared database.
		return "", fmt.Errorf("expected a postgres:// data source name, got %q", dataSourceName)
	}
	parsed.Path = "/" + name
	return parsed.String(), nil
}

// databaseName builds an identifier that is unique within a run and readable in
// a `\l` listing when one is left behind by a killed test.
func databaseName(seed string) (string, error) {
	var cleaned strings.Builder
	for _, character := range strings.ToLower(seed) {
		switch {
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9':
			cleaned.WriteRune(character)
		default:
			cleaned.WriteByte('_')
		}
	}
	// PostgreSQL truncates identifiers at 63 bytes, and a truncated name could
	// collide with another test's. Bounding the readable part keeps the random
	// suffix -- the part that makes it unique -- inside the limit.
	prefix := cleaned.String()
	if len(prefix) > 40 {
		prefix = prefix[:40]
	}
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("name a throwaway database: %w", err)
	}
	return "conduit_t_" + prefix + "_" + hex.EncodeToString(suffix), nil
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
