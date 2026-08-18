package pgmigrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Set is one component's migrations, named.
//
// Verify takes every set that shares a database rather than one at a time, and
// that is forced by the DDL rather than chosen. Every migration in this
// repository creates its tables unqualified, so all four components land in the
// same schema -- `search_path`'s first entry, which for the Hub's role is
// `public`. Nothing in the catalogue records which component created which
// table, so "this table belongs to reconciliation" is not a fact a single-
// component Verify could read; it could only be a naming rule somebody invented.
// Applying every set to a shadow schema makes ownership an observation instead:
// whatever appears while a set's DDL runs is that set's, by construction.
//
// A caller that passes only some of the sets sharing a database still gets a
// correct answer about what those components build. It will additionally see
// the other components' tables reported as unexpected -- which is why an
// unexpected object is never a fault. See Report.
type Set struct {
	Component  string
	Migrations []Migration
}

// Subject is what a difference is about.
type Subject string

const (
	SubjectTable      Subject = "table"
	SubjectColumn     Subject = "column"
	SubjectIndex      Subject = "index"
	SubjectConstraint Subject = "constraint"
)

// Kind is the direction a difference points.
type Kind string

const (
	// KindMissing: the migrations build it, the database does not have it.
	KindMissing Kind = "missing"
	// KindUnexpected: the database has it, no migration builds it.
	KindUnexpected Kind = "unexpected"
	// KindMismatched: both have it, under the same name, differently defined.
	KindMismatched Kind = "mismatched"
)

// Severity separates the differences that mean the database is wrong from the
// ones that mean somebody added something.
type Severity string

const (
	// SeverityFault: the live schema lacks or contradicts something the
	// recorded migrations built. Nothing benign produces this. The application
	// will read, write, or plan against a shape that is not there.
	SeverityFault Severity = "fault"
	// SeverityAdvisory: the live schema has something no recorded migration
	// builds. This is reported and never a fault, because two legitimate causes
	// are indistinguishable from drift here: an operator's own index for their
	// query load, and a component sharing the database that the caller did not
	// enumerate. A human decides; a machine cannot.
	SeverityAdvisory Severity = "advisory"
)

// Difference is one way the live schema departs from the migrations.
//
// It names the object, both definitions, and which component's DDL was
// executing when the object appeared in the shadow schema. "column
// conduit_reconciliation_work.attempt is missing" is the point of this package:
// a report that said only "the schema does not match" would leave an operator
// exactly where the ledger already left them.
type Difference struct {
	// Component is the set whose migrations build the table this is about, or
	// empty for the ledger table and for tables no set builds.
	Component string
	Subject   Subject
	Kind      Kind
	// Name is `table` for a table and `table.object` for everything else.
	Name string
	// Expected and Live are the two definitions. Exactly one is empty for a
	// missing or unexpected difference.
	Expected string
	Live     string
	Severity Severity
}

func (difference Difference) String() string {
	where := difference.Name
	if difference.Component != "" {
		where = fmt.Sprintf("%s (%s)", difference.Name, difference.Component)
	}
	switch difference.Kind {
	case KindMissing:
		return fmt.Sprintf("%s %s is missing: the migrations build %q and the database has nothing",
			difference.Subject, where, difference.Expected)
	case KindUnexpected:
		return fmt.Sprintf("%s %s is in the database and in no migration: %q",
			difference.Subject, where, difference.Live)
	default:
		return fmt.Sprintf("%s %s differs: the migrations build %q and the database has %q",
			difference.Subject, where, difference.Expected, difference.Live)
	}
}

// Report is what Verify found.
type Report struct {
	// Differences are sorted, so two runs against the same database produce the
	// same text and a diff of two reports is a diff of two databases.
	Differences []Difference
	// Notes record what the ledger said that changes how the differences should
	// be read -- a component that has never migrated this database, a ledger row
	// naming a migration this binary does not have. They are not differences:
	// none of them is the schema being wrong.
	Notes []string
}

// Faults are the differences that mean the database is wrong.
func (report Report) Faults() []Difference { return report.severity(SeverityFault) }

// Advisories are the differences that mean somebody added something.
func (report Report) Advisories() []Difference { return report.severity(SeverityAdvisory) }

func (report Report) severity(wanted Severity) []Difference {
	matching := make([]Difference, 0, len(report.Differences))
	for _, difference := range report.Differences {
		if difference.Severity == wanted {
			matching = append(matching, difference)
		}
	}
	return matching
}

// Sound reports whether the database matches the migrations the ledger claims
// built it. Advisories do not make it false; see SeverityAdvisory.
func (report Report) Sound() bool { return len(report.Faults()) == 0 }

func (report Report) String() string {
	var text strings.Builder
	for _, note := range report.Notes {
		text.WriteString("note: " + note + "\n")
	}
	for _, difference := range report.Differences {
		text.WriteString(string(difference.Severity) + ": " + difference.String() + "\n")
	}
	if text.Len() == 0 {
		return "the schema matches the migrations recorded against it\n"
	}
	return text.String()
}

// Verify diffs a database's schema against the migrations its ledger claims
// built it, and reports what differs.
//
// # Why a shadow schema rather than a fingerprint
//
// The obvious cheaper design records a hash of the schema when Apply commits
// and compares it later. That can only ever detect drift that happened after
// the fingerprint was taken, it is computed by the same code that ran the DDL
// so it agrees with a mistake in that DDL, and a hash cannot say what differs --
// which is the entire operational value. Rebuilding the schema from the
// migrations answers all three: it is derived from the DDL rather than from a
// previous observation of the database, and it produces two catalogues that can
// be compared object by object.
//
// The shadow is built in this session's temporary schema, inside a transaction
// that is always rolled back. Temporary rather than a named schema because
// `CREATE SCHEMA` needs CREATE on the database while a temporary table needs
// nothing beyond what the migrating role already has, and the rollback means an
// interrupted Verify leaves no debris to clean up. Same server, same session,
// same PostgreSQL version and encoding as the live schema, so a difference
// cannot be an artefact of comparing two servers.
//
// # It verifies the ledger's claim, not the migration list
//
// The expected schema is built from the migrations the ledger records as
// applied, in slice order -- not from every migration the binary carries. A
// database that is one migration behind is a Pending answer, not drift, and
// building the full list would report every object that migration adds as
// missing. Verify answers the narrower question the ledger cannot: of the
// migrations that claim to have run, is the result still there.
//
// # Operator-invoked, not a start-time check
//
// Nothing calls this on a Hub start, deliberately. A start that refused on
// drift would convert a schema anomaly into a fleet-wide outage at the moment
// an operator most needs a Hub to look at, and this check is not free -- it
// executes every recorded migration's DDL into a shadow schema on every
// invocation, per replica. Pending already answers the start-time question,
// which is whether the schema is up to date. Whether the schema is *intact* is
// a question with a human in the loop, because half its answer -- see
// SeverityAdvisory -- cannot be decided without one.
func Verify(ctx context.Context, database *sql.DB, sets ...Set) (Report, error) {
	if len(sets) == 0 {
		return Report{}, errors.New(
			"no migration sets to verify against; an empty list would report every table in the database as unexpected")
	}
	seen := map[string]struct{}{}
	for _, set := range sets {
		if err := Validate(set.Component, set.Migrations); err != nil {
			return Report{}, err
		}
		if _, duplicate := seen[set.Component]; duplicate {
			return Report{}, fmt.Errorf(
				"component %q appears twice; its migrations would be applied to the shadow schema twice", set.Component)
		}
		seen[set.Component] = struct{}{}
	}
	connection, err := database.Conn(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("acquire verification connection: %w", err)
	}
	defer connection.Close()

	present, err := ledgerExists(ctx, connection)
	if err != nil {
		return Report{}, err
	}
	if !present {
		// No claim, so nothing to verify against. Reporting every table as
		// unexpected here would be technically true and useless: this is the
		// database Pending exists for.
		return Report{Notes: []string{
			"there is no " + ledgerTable + " table, so no migration claims to have run against this database",
		}}, nil
	}
	recorded, notes, err := recordedSets(ctx, connection, sets)
	if err != nil {
		return Report{}, err
	}
	expected, owners, err := shadowSchema(ctx, connection, recorded)
	if err != nil {
		return Report{}, err
	}
	live, err := liveSchema(ctx, connection)
	if err != nil {
		return Report{}, err
	}
	return Report{Differences: diff(expected, live, owners), Notes: notes}, nil
}

// recordedSets narrows each set to the migrations the ledger records, and
// reports what the ledger said that the caller could not have known.
func recordedSets(ctx context.Context, connection *sql.Conn, sets []Set) ([]Set, []string, error) {
	recorded := make([]Set, 0, len(sets))
	notes := []string{}
	for _, set := range sets {
		applied, err := appliedNames(ctx, connection, set.Component)
		if err != nil {
			return nil, nil, err
		}
		known := make([]Migration, 0, len(set.Migrations))
		for _, migration := range set.Migrations {
			if _, done := applied[migration.Name]; done {
				known = append(known, migration)
				delete(applied, migration.Name)
			}
		}
		// A ledger row this binary has no migration for means the database was
		// migrated by a newer build. The shadow cannot contain what that
		// migration created, so every object it added would be reported
		// unexpected -- true, and misleading. Saying so is the honest answer.
		for _, name := range sortedKeys(applied) {
			notes = append(notes, fmt.Sprintf(
				"the ledger records %s migration %q, which this build does not carry; anything it created is "+
					"reported as unexpected", set.Component, name))
		}
		if len(known) == 0 {
			notes = append(notes, fmt.Sprintf(
				"%s has never migrated this database, so nothing of its schema is expected here", set.Component))
			continue
		}
		recorded = append(recorded, Set{Component: set.Component, Migrations: known})
	}
	return recorded, notes, nil
}

// shadowSchema derives the expected catalogue by executing the recorded DDL,
// and records which component was executing when each table appeared.
//
// Everything happens inside one transaction that is always rolled back, and the
// migrations run without the advisory locks Apply takes: nothing here is
// visible to another session, so there is nothing to serialise against, and
// taking the component lock would make a read-only question block a replica
// that is starting.
func shadowSchema(ctx context.Context, connection *sql.Conn, sets []Set) (schema, map[string]string, error) {
	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return schema{}, nil, fmt.Errorf("begin shadow schema: %w", err)
	}
	// Rollback rather than commit, on every path including success.
	defer func() { _ = transaction.Rollback() }()

	// SET LOCAL, so the search path returns with the rollback. pg_temp resolves
	// to this session's temporary schema and creates it on demand, which is what
	// sends the migrations' unqualified CREATE TABLE somewhere private.
	if _, err := transaction.ExecContext(ctx, `SET LOCAL search_path TO pg_temp`); err != nil {
		return schema{}, nil, fmt.Errorf("isolate the shadow schema: %w", err)
	}
	// The ledger is part of the expected schema, not scaffolding around it: it
	// is a table in the live database like any other, and a column dropped from
	// it is drift the same way. Building it here is also what stops it being
	// reported as unexpected.
	if _, err := transaction.ExecContext(ctx, ledger); err != nil {
		return schema{}, nil, fmt.Errorf("build the shadow ledger: %w", err)
	}
	owners := map[string]string{}
	// Claimed by nobody, before any set can claim it. The ledger is shared by
	// every component, so attributing it to whichever one happened to be first
	// in the slice would put a name on a difference that names the wrong owner.
	ledgerTables, err := shadowTables(ctx, transaction)
	if err != nil {
		return schema{}, nil, err
	}
	for _, table := range ledgerTables {
		owners[table] = ""
	}
	for _, set := range sets {
		for _, migration := range set.Migrations {
			if _, err := transaction.ExecContext(ctx, migration.Statements); err != nil {
				return schema{}, nil, fmt.Errorf(
					"rebuild %s/%s in the shadow schema: %w", set.Component, migration.Name, err)
			}
		}
		tables, err := shadowTables(ctx, transaction)
		if err != nil {
			return schema{}, nil, err
		}
		for _, table := range tables {
			if _, claimed := owners[table]; !claimed {
				owners[table] = set.Component
			}
		}
	}
	var namespace int64
	if err := transaction.QueryRowContext(ctx, `SELECT pg_my_temp_schema()`).Scan(&namespace); err != nil {
		return schema{}, nil, fmt.Errorf("locate the shadow schema: %w", err)
	}
	built, err := readSchema(ctx, transaction, namespace, "pg_temp")
	if err != nil {
		return schema{}, nil, fmt.Errorf("read the shadow schema: %w", err)
	}
	return built, owners, nil
}

func shadowTables(ctx context.Context, transaction *sql.Tx) ([]string, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT relname FROM pg_class
WHERE relnamespace = pg_my_temp_schema() AND relkind IN ('r', 'p')`)
	if err != nil {
		return nil, fmt.Errorf("list the shadow schema's tables: %w", err)
	}
	defer rows.Close()
	tables := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("list the shadow schema's tables: %w", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list the shadow schema's tables: %w", err)
	}
	return tables, nil
}

// liveSchema reads the schema the migrations actually built.
//
// current_schema() rather than a configured name, because that is where
// unqualified DDL lands and every migration here is unqualified. Verify holds
// a connection from the same pool, opened from the same data source name by the
// same role, so its search path is the one Apply migrated under.
func liveSchema(ctx context.Context, connection *sql.Conn) (schema, error) {
	var namespace int64
	var name string
	if err := connection.QueryRowContext(ctx,
		`SELECT oid, nspname FROM pg_namespace WHERE nspname = current_schema()`).Scan(&namespace, &name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return schema{}, errors.New(
				"this connection's search_path resolves to no schema, so there is nowhere the migrations could have run")
		}
		return schema{}, fmt.Errorf("locate the live schema: %w", err)
	}
	read, err := readSchema(ctx, connection, namespace, name)
	if err != nil {
		return schema{}, fmt.Errorf("read the live schema: %w", err)
	}
	return read, nil
}

// table is one table's shape, by object name.
type table struct {
	columns     map[string]string
	constraints map[string]string
	indexes     map[string]string
}

type schema struct {
	tables map[string]*table
}

// querier is what readSchema needs, so it can read the shadow inside its
// transaction and the live schema outside one.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func readSchema(ctx context.Context, from querier, namespace int64, qualifier string) (schema, error) {
	read := schema{tables: map[string]*table{}}
	if err := scan(ctx, from, `
SELECT relname FROM pg_class WHERE relnamespace = $1 AND relkind IN ('r', 'p')`,
		namespace, func(rows *sql.Rows) error {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			read.tables[name] = &table{
				columns: map[string]string{}, constraints: map[string]string{}, indexes: map[string]string{},
			}
			return nil
		}); err != nil {
		return schema{}, err
	}
	if err := scan(ctx, from, `
SELECT class.relname, attribute.attname,
       format_type(attribute.atttypid, attribute.atttypmod),
       attribute.attnotnull,
       COALESCE(pg_get_expr(fallback.adbin, fallback.adrelid), ''),
       attribute.attidentity, attribute.attgenerated
FROM pg_attribute AS attribute
JOIN pg_class AS class ON class.oid = attribute.attrelid
LEFT JOIN pg_attrdef AS fallback
    ON fallback.adrelid = attribute.attrelid AND fallback.adnum = attribute.attnum
WHERE class.relnamespace = $1 AND class.relkind IN ('r', 'p')
  AND attribute.attnum > 0 AND NOT attribute.attisdropped`,
		namespace, func(rows *sql.Rows) error {
			var owner, column, dataType, fallback, identity, generated string
			var notNull bool
			if err := rows.Scan(&owner, &column, &dataType, &notNull, &fallback, &identity, &generated); err != nil {
				return err
			}
			if shape := read.tables[owner]; shape != nil {
				shape.columns[column] = columnDefinition(
					dataType, notNull, unqualify(fallback, qualifier), identity, generated)
			}
			return nil
		}); err != nil {
		return schema{}, err
	}
	// contype 'n' is excluded because PostgreSQL 18 catalogues NOT NULL as a
	// constraint as well as a column attribute, and reporting one dropped NOT
	// NULL twice makes a report harder to read rather than more complete. The
	// column half above carries it. 't' is a constraint trigger, which is not
	// a schema shape this DDL creates.
	if err := scan(ctx, from, `
SELECT class.relname, constraint_.conname, pg_get_constraintdef(constraint_.oid)
FROM pg_constraint AS constraint_
JOIN pg_class AS class ON class.oid = constraint_.conrelid
WHERE class.relnamespace = $1 AND constraint_.contype NOT IN ('n', 't')`,
		namespace, func(rows *sql.Rows) error {
			var owner, name, definition string
			if err := rows.Scan(&owner, &name, &definition); err != nil {
				return err
			}
			if shape := read.tables[owner]; shape != nil {
				shape.constraints[name] = unqualify(definition, qualifier)
			}
			return nil
		}); err != nil {
		return schema{}, err
	}
	// Indexes that back a constraint are excluded for the same reason: a
	// primary key is already reported as a constraint, and its index is the
	// same fact told twice.
	if err := scan(ctx, from, `
SELECT class.relname, index_.relname, pg_get_indexdef(index_.oid)
FROM pg_index AS catalogued
JOIN pg_class AS index_ ON index_.oid = catalogued.indexrelid
JOIN pg_class AS class ON class.oid = catalogued.indrelid
WHERE class.relnamespace = $1
  AND NOT EXISTS (SELECT 1 FROM pg_constraint AS backed WHERE backed.conindid = index_.oid)`,
		namespace, func(rows *sql.Rows) error {
			var owner, name, definition string
			if err := rows.Scan(&owner, &name, &definition); err != nil {
				return err
			}
			if shape := read.tables[owner]; shape != nil {
				shape.indexes[name] = unqualify(definition, qualifier)
			}
			return nil
		}); err != nil {
		return schema{}, err
	}
	return read, nil
}

func scan(ctx context.Context, from querier, query string, namespace int64, row func(*sql.Rows) error) error {
	rows, err := from.QueryContext(ctx, query, namespace)
	if err != nil {
		return fmt.Errorf("read the catalogue: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := row(rows); err != nil {
			return fmt.Errorf("read the catalogue: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read the catalogue: %w", err)
	}
	return nil
}

// unqualify removes the schema name PostgreSQL prints into a definition, so the
// shadow's `ON pg_temp.t` and the live `ON public.t` compare equal. Both sides
// are stripped of their own qualifier only, so a genuine cross-schema reference
// -- which none of these migrations makes -- survives as a difference rather
// than being normalised away.
func unqualify(definition, qualifier string) string {
	return strings.ReplaceAll(definition, qualifier+".", "")
}

func columnDefinition(dataType string, notNull bool, fallback, identity, generated string) string {
	definition := dataType
	if notNull {
		definition += " NOT NULL"
	}
	switch {
	case generated == "s":
		definition += " GENERATED ALWAYS AS (" + fallback + ") STORED"
	case identity == "a":
		definition += " GENERATED ALWAYS AS IDENTITY"
	case identity == "d":
		definition += " GENERATED BY DEFAULT AS IDENTITY"
	case fallback != "":
		definition += " DEFAULT " + fallback
	}
	return definition
}

func diff(expected, live schema, owners map[string]string) []Difference {
	differences := []Difference{}
	for _, name := range sortedTables(expected) {
		component := owners[name]
		shape := expected.tables[name]
		liveShape, present := live.tables[name]
		if !present {
			differences = append(differences, Difference{
				Component: component, Subject: SubjectTable, Kind: KindMissing, Name: name,
				Expected: "a table these migrations create", Severity: SeverityFault,
			})
			// No cascade into columns: a report that also listed every column of
			// a missing table would bury the one line that matters.
			continue
		}
		differences = append(differences,
			compare(component, name, SubjectColumn, shape.columns, liveShape.columns, SeverityFault)...)
		differences = append(differences,
			compare(component, name, SubjectConstraint, shape.constraints, liveShape.constraints, SeverityFault)...)
		// An index the migrations do not build is the one difference with a
		// routine benign cause: an operator adding one for their own query load
		// changes no shape the application depends on. A missing or redefined
		// index is still a fault -- the application's queries were planned
		// against it.
		differences = append(differences,
			compare(component, name, SubjectIndex, shape.indexes, liveShape.indexes, SeverityAdvisory)...)
	}
	for _, name := range sortedTables(live) {
		if _, known := expected.tables[name]; known {
			continue
		}
		differences = append(differences, Difference{
			Subject: SubjectTable, Kind: KindUnexpected, Name: name,
			Live: "a table no recorded migration creates", Severity: SeverityAdvisory,
		})
	}
	sort.Slice(differences, func(one, two int) bool {
		left, right := differences[one], differences[two]
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		if left.Subject != right.Subject {
			return left.Subject < right.Subject
		}
		return left.Kind < right.Kind
	})
	return differences
}

// compare diffs one class of object on one table. unexpected is the severity an
// object present only in the live database carries; everything else is a fault.
func compare(
	component, owner string, subject Subject, expected, live map[string]string, unexpected Severity,
) []Difference {
	differences := []Difference{}
	for _, name := range sortedKeys(expected) {
		qualified := owner + "." + name
		actual, present := live[name]
		switch {
		case !present:
			differences = append(differences, Difference{
				Component: component, Subject: subject, Kind: KindMissing, Name: qualified,
				Expected: expected[name], Severity: SeverityFault,
			})
		case actual != expected[name]:
			differences = append(differences, Difference{
				Component: component, Subject: subject, Kind: KindMismatched, Name: qualified,
				Expected: expected[name], Live: actual, Severity: SeverityFault,
			})
		}
	}
	for _, name := range sortedKeys(live) {
		if _, known := expected[name]; known {
			continue
		}
		differences = append(differences, Difference{
			Component: component, Subject: subject, Kind: KindUnexpected, Name: owner + "." + name,
			Live: live[name], Severity: unexpected,
		})
	}
	return differences
}

func sortedTables(read schema) []string {
	names := make([]string, 0, len(read.tables))
	for name := range read.tables {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func sortedKeys[value any](from map[string]value) []string {
	names := make([]string, 0, len(from))
	for name := range from {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
