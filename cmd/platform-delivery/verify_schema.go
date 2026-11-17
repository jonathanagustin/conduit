package main

// The operator's schema-drift check: the second half of gap 3 in
// docs/operations/PRODUCTION-READINESS.md.
//
// `pgmigrate.Pending` answers "is this database up to date". This answers the
// question the ledger cannot: of the migrations that claim to have run, is the
// result still in the database. It belongs in this package because this is the
// only one that knows every component sharing the Hub's DSN -- and Verify needs
// all of them at once, since nothing in the PostgreSQL catalogue records which
// component created which table.
//
// It is a subcommand rather than a start-time check. See pgmigrate.Verify for
// why; the short form is that a Hub refusing to start on a schema difference
// converts an anomaly into an outage at the moment an operator most needs a Hub
// to look at, and half the differences a diff can find need a human to judge.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"

	_ "github.com/jackc/pgx/v5/stdlib"
	appgenpostgres "github.com/jonathanagustin/conduit/internal/appgen/adapters/postgres"
	assistantthreads "github.com/jonathanagustin/conduit/internal/assistant/adapters/threadpostgres"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	reconciliationpostgres "github.com/jonathanagustin/conduit/internal/delivery/reconciliation/adapters/postgres"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/resolutionlogpostgres"
	"github.com/jonathanagustin/conduit/internal/governance/adapters/auditpostgres"
	agentcredentials "github.com/jonathanagustin/conduit/internal/identity/adapters/revocationpostgres"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/sessionpostgres"
	modelgovernancepg "github.com/jonathanagustin/conduit/internal/modelgovernor/adapters/postgres"
	notificationspostgres "github.com/jonathanagustin/conduit/internal/notifications/adapters/postgres"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

// verifySchemaCommand is the one argument this binary answers to. Anything else
// falls through to a normal start, which is what every deployment passes today
// -- an unrecognised argument turning a Hub start into an exit would be a
// behaviour change delivered by a check that is meant to be read, not obeyed.
const verifySchemaCommand = "verify-schema"

// hubMigrationSets is every component that migrates the Hub's database.
//
// The Gitless resolution log is included even though it is applied only under
// GITLESS_RESOLUTION_LOG_KIND=postgres. A component that has never migrated
// this database is reported as exactly that by Verify, whereas leaving it out
// would make every table it does own read as one no migration builds.
func hubMigrationSets() []pgmigrate.Set {
	return []pgmigrate.Set{
		{Component: controlplane.PostgresMigrationComponent, Migrations: controlplane.PostgresMigrations()},
		{Component: auditpostgres.Component(), Migrations: auditpostgres.Migrations()},
		{Component: reconciliationpostgres.Component(), Migrations: reconciliationpostgres.Migrations()},
		{Component: notificationspostgres.Component(), Migrations: notificationspostgres.Migrations()},
		{Component: assistantthreads.Component, Migrations: assistantthreads.Migrations()},
		{Component: appgenpostgres.Component, Migrations: appgenpostgres.Migrations()},
		{Component: agentcredentials.Component, Migrations: agentcredentials.Migrations()},
		{Component: modelgovernancepg.Component, Migrations: modelgovernancepg.Migrations()},
		{Component: resolutionlogpostgres.Component(), Migrations: resolutionlogpostgres.Migrations()},
		// Applied by every PostgreSQL Hub, whether or not sign-in is configured.
		{Component: sessionpostgres.Component, Migrations: sessionpostgres.Migrations()},
	}
}

// verifySchema reads the same DSN the Hub starts with and reports the schema
// against the migrations recorded in it.
//
// The report is written whether or not it found anything, because "nothing to
// report" is the answer an operator ran this for. The error is returned only for
// a fault: an advisory is a difference a person decides about, so exiting
// non-zero on one would make the check useless in anything automated.
func verifySchema(ctx context.Context, getenv func(string) string, output io.Writer) error {
	if getenv == nil {
		return errors.New("an environment reader is required")
	}
	dataSourceName, err := readSingleLineSecretFile(getenv("POSTGRES_DSN_FILE"), "POSTGRES_DSN_FILE")
	if err != nil {
		return err
	}
	database, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		return fmt.Errorf("open the Hub's database: %w", err)
	}
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to the Hub's database: %w", err)
	}
	report, err := pgmigrate.Verify(ctx, database, hubMigrationSets()...)
	if err != nil {
		return fmt.Errorf("verify the Hub's schema: %w", err)
	}
	if _, err := fmt.Fprint(output, report); err != nil {
		return err
	}
	if !report.Sound() {
		return fmt.Errorf(
			"%d schema faults: the database does not match the migrations recorded against it", len(report.Faults()))
	}
	return nil
}
