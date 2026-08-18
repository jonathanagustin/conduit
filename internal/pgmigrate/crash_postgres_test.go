package pgmigrate_test

import (
	"bufio"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
	"github.com/jonathanagustin/conduit/internal/pgtest"
)

// Killing the migrator, over and over, in the window the package's central
// claim is about.
//
// `Apply` says a migration and the row recording it "commit or roll back as
// one -- there is no state in which the ledger is wrong", and
// `docs/operations/schema-migrations.md` repeats it as the reason a restore
// needs no special handling. Until this file, that claim was exercised only by
// runs that finished: every existing test lets Apply return before it looks at
// anything. A claim about what happens when the process does *not* return was
// being carried by the fact that nobody had interrupted one.
//
// So these tests interrupt one, at four named points inside the transaction and
// then at uniformly random instants across the whole of Apply, and assert after
// each kill that the schema and the ledger agree about every migration.
//
// # What the kill is, exactly
//
// SIGKILL delivered to the migrating process by itself, from inside the
// database driver, at the instruction being tested. That is a true crash of the
// migrator: no deferred `Rollback`, no `Close`, no flush -- the kernel closes
// the socket and PostgreSQL discovers the client is gone. It is *not* a power
// cut of the database server, and this file does not claim to be one. The
// server survives every trial here and does the rolling back, which is exactly
// the mechanism the claim rests on; a crash of the server itself tests
// PostgreSQL's WAL rather than this package's transaction structure.
//
// # What a pass proves, and what it does not
//
// It bounds the risk; it does not prove atomicity for all interleavings. The
// four deterministic points are the ones a reader of `applyOne` would name, and
// the randomised trials sample the instants between them, but a sampling test
// cannot exclude a window nobody sampled. What it does do is fail loudly if the
// structure that makes the claim true is ever taken apart, and that was
// measured rather than assumed: with the ledger `INSERT` moved into a second
// transaction -- a mutant that compiles and passes every other test in this
// package -- ten of the twenty deterministic trials failed (both ledger-row
// points, five of five each) and six of forty randomised trials failed, each
// reporting a table in the schema with no row recording it.
//
// The other ten deterministic trials passing under that mutant is the result
// worth keeping. `after-ddl` and `before-commit` straddle the mutant's split
// rather than landing in it, so a file that had only named the window's
// obvious edges would have called the mutant survivable. The randomised trials
// are what cover the instants nobody named.

// The migrations these trials apply. Two, because the interesting crash is one
// with earlier work already committed: a correct result is not "the database is
// empty" but "the first migration is present and recorded, and the second is
// present and recorded or absent and unrecorded".
//
// The DDL is a plain CREATE TABLE with no IF NOT EXISTS, deliberately. If a
// crash ever left a table behind without its ledger row, the recovery Apply
// that every trial runs afterwards would fail with a duplicate-relation error
// rather than quietly papering over the state this file exists to detect.
func crashProbeMigrations() []pgmigrate.Migration {
	return []pgmigrate.Migration{
		{Name: "0001-crash-probe", Statements: `CREATE TABLE crash_probe_first (id integer PRIMARY KEY);`},
		{Name: "0002-crash-probe", Statements: `CREATE TABLE crash_probe_second (id integer PRIMARY KEY);`},
	}
}

// crashProbeTables lists each migration's observable effect, in the same order.
// A migration is "in the schema" exactly when its table resolves.
var crashProbeTables = []string{"crash_probe_first", "crash_probe_second"}

const (
	crashComponent = "crash-probe"
	// The migration the kill points are aimed at. Naming it here rather than
	// indexing keeps the child's matching readable: it kills on the statement
	// that mentions this migration, whichever position it occupies.
	crashTargetName = "0002-crash-probe"

	// The child process is this test binary re-executed with these set. The DSN
	// is passed rather than inherited because each trial runs against a
	// throwaway database, not the shared one.
	crashChildDSNEnvironment   = "CONDUIT_PGMIGRATE_CRASH_CHILD_DSN"
	crashChildPointEnvironment = "CONDUIT_PGMIGRATE_CRASH_CHILD_POINT"
	crashTrialsEnvironment     = "CONDUIT_PGMIGRATE_CRASH_TRIALS"

	// Printed by the child on either side of Apply. READY is the synchronisation
	// point the randomised parent times its kill from -- without it the delay
	// would mostly be spent in process start-up and connection set-up, and the
	// kills would cluster before the transaction rather than inside it.
	crashChildReady   = "CRASH-CHILD-READY"
	crashChildApplied = "CRASH-CHILD-APPLIED"
)

// The four points inside applyOne that a reader would name, each an edge of the
// window the atomicity claim covers.
const (
	// The migration's DDL has executed inside the transaction and the ledger
	// INSERT has not been sent. This is the window named in the package comment
	// and the one a non-transactional implementation loses.
	crashAfterDDL = "after-ddl"
	// The same window from its other edge: the INSERT is about to go out.
	crashBeforeLedgerRow = "before-ledger-row"
	// Both statements have executed; nothing has been committed.
	crashAfterLedgerRow = "after-ledger-row"
	// Everything is staged and COMMIT is about to be sent. The last instant at
	// which the whole transaction is still lost.
	crashBeforeCommit = "before-commit"
	// No injected kill; the parent kills on a timer instead.
	crashNowhere = "nowhere"
)

// TestACrashInsideAMigrationLeavesTheLedgerAgreeingWithTheSchema kills the
// migrator at each named point in turn and asserts the invariant afterwards.
//
// Five trials per point rather than one. The result at a deterministic point is
// deterministic when the code is right, so the repeats cost little; they exist
// because the recovery Apply each trial runs afterwards is what would surface a
// crash that left something behind, and running that path once is a thin basis
// for a claim about every restart of every replica.
func TestACrashInsideAMigrationLeavesTheLedgerAgreeingWithTheSchema(t *testing.T) {
	if isCrashChild() {
		t.Skip("this process is a crash-trial child; the parent's tests do not run in it")
	}
	shared := sharedDatabase(t)
	dataSourceName, database := freshCrashDatabase(t, shared)
	points := []string{crashAfterDDL, crashBeforeLedgerRow, crashAfterLedgerRow, crashBeforeCommit}

	killed := 0
	for _, point := range points {
		for trial := range crashTrials(t, 5) {
			resetCrashProbe(t, database, trial%2 == 0)
			outcome := runCrashChild(t, dataSourceName, point, 0)
			if outcome.applied {
				t.Fatalf("%s trial %d: the child completed Apply; the kill never fired", point, trial)
			}
			killed++
			assertLedgerAgreesWithSchema(t, database, fmt.Sprintf("%s trial %d", point, trial))
			assertRecoveryCompletes(t, database, fmt.Sprintf("%s trial %d", point, trial))
		}
	}
	if !t.Failed() {
		t.Logf("%d crashes at %d named points inside the migration transaction; no disagreement",
			killed, len(points))
	}
}

// TestCrashesAtRandomInstantsLeaveTheLedgerAgreeingWithTheSchema kills the
// migrator at uniformly random instants instead of at a point somebody chose.
//
// The named points above are the windows a reader of applyOne thinks of, which
// is precisely their weakness: a defect nobody thought of sits between them.
// These trials time their kill from the child's READY line, using a delay drawn
// uniformly from zero to the longest uninterrupted run the child measured for
// itself, so the kills land across the whole of Apply -- including inside
// COMMIT, which no deterministic hook in the client can reach.
//
// A trial whose delay outran the migration is not a failure and is counted
// separately: the invariant is asserted either way, and an uninterrupted run
// must end fully applied.
func TestCrashesAtRandomInstantsLeaveTheLedgerAgreeingWithTheSchema(t *testing.T) {
	if isCrashChild() {
		t.Skip("this process is a crash-trial child; the parent's tests do not run in it")
	}
	shared := sharedDatabase(t)
	dataSourceName, database := freshCrashDatabase(t, shared)

	window := measureApplyWindow(t, database, dataSourceName)

	trials := crashTrials(t, 40)
	killed, completed := 0, 0
	for trial := range trials {
		resetCrashProbe(t, database, trial%5 == 0)
		delay := time.Duration(rand.Int64N(int64(window)))
		outcome := runCrashChild(t, dataSourceName, crashNowhere, delay)
		if outcome.applied {
			completed++
		} else {
			killed++
		}
		assertLedgerAgreesWithSchema(t, database, fmt.Sprintf("trial %d, killed %s after READY", trial, delay))
		assertRecoveryCompletes(t, database, fmt.Sprintf("trial %d, killed %s after READY", trial, delay))
	}
	if killed == 0 {
		t.Fatalf("all %d trials outran their kill; the window measurement (%s) is wrong and nothing was tested", trials, window)
	}
	if !t.Failed() {
		t.Logf("%d trials over a %s window: %d killed mid-Apply, %d outran the kill; no disagreement",
			trials, window, killed, completed)
	}
}

// assertLedgerAgreesWithSchema is the invariant, and it is symmetric on
// purpose. A ledger row without its table is the failure everyone pictures; a
// table without its row is the one that actually bites, because the next start
// re-runs DDL against a database that already has it.
func assertLedgerAgreesWithSchema(t *testing.T, database *sql.DB, trial string) {
	t.Helper()
	migrations := crashProbeMigrations()
	recorded := recordedCrashProbeNames(t, database)
	for index, migration := range migrations {
		inSchema := crashProbeTableExists(t, database, crashProbeTables[index])
		_, inLedger := recorded[migration.Name]
		if inSchema == inLedger {
			continue
		}
		if inSchema {
			t.Fatalf("%s: %s is in the schema and not in the ledger; the next start re-runs its DDL",
				trial, migration.Name)
		}
		t.Fatalf("%s: %s is in the ledger and not in the schema; the next start skips DDL that never ran",
			trial, migration.Name)
	}
}

// assertRecoveryCompletes runs the migration the crashed process was running.
//
// Agreement alone is satisfied by a database nothing can ever migrate again --
// a wedged advisory lock, or a table left behind that the plain CREATE TABLE
// cannot be re-run against. The claim is only worth anything if the crash is
// also recoverable by doing the ordinary thing, so every trial does the
// ordinary thing and requires the end state to be complete.
func assertRecoveryCompletes(t *testing.T, database *sql.DB, trial string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := pgmigrate.Apply(ctx, database, crashComponent, crashProbeMigrations()); err != nil {
		t.Fatalf("%s: re-running the migrations after the crash failed: %v", trial, err)
	}
	recorded := recordedCrashProbeNames(t, database)
	for index, migration := range crashProbeMigrations() {
		if !crashProbeTableExists(t, database, crashProbeTables[index]) {
			t.Fatalf("%s: %s is absent from the schema after recovery", trial, migration.Name)
		}
		if _, ok := recorded[migration.Name]; !ok {
			t.Fatalf("%s: %s is absent from the ledger after recovery", trial, migration.Name)
		}
	}
}

func crashProbeTableExists(t *testing.T, database *sql.DB, table string) bool {
	t.Helper()
	var resolved sql.NullString
	if err := database.QueryRowContext(t.Context(),
		`SELECT to_regclass($1)::text`, table).Scan(&resolved); err != nil {
		t.Fatal(err)
	}
	return resolved.Valid
}

// recordedCrashProbeNames reads the ledger, treating an absent ledger table as
// an empty history rather than an error: the first trial against a fresh
// database crashes before anything creates it, and "nothing is recorded" is the
// correct reading of that.
func recordedCrashProbeNames(t *testing.T, database *sql.DB) map[string]struct{} {
	t.Helper()
	var resolved sql.NullString
	if err := database.QueryRowContext(t.Context(),
		`SELECT to_regclass('conduit_schema_migrations')::text`).Scan(&resolved); err != nil {
		t.Fatal(err)
	}
	recorded := map[string]struct{}{}
	if !resolved.Valid {
		return recorded
	}
	rows, err := database.QueryContext(t.Context(),
		`SELECT name FROM conduit_schema_migrations WHERE component = $1`, crashComponent)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		recorded[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return recorded
}

// resetCrashProbe puts the database back to the state a trial starts from.
//
// Dropping the ledger table as well, on some trials, is not tidiness: a crash
// against a database that has never been migrated runs a different path --
// createLedger under the shared lock, with no ledger to read -- and a trial
// that always found the table would never enter it.
func resetCrashProbe(t *testing.T, database *sql.DB, dropLedger bool) {
	t.Helper()
	for _, table := range crashProbeTables {
		if _, err := database.ExecContext(t.Context(), `DROP TABLE IF EXISTS `+table); err != nil {
			t.Fatal(err)
		}
	}
	statement := `DELETE FROM conduit_schema_migrations WHERE component = $1`
	if dropLedger {
		if _, err := database.ExecContext(t.Context(),
			`DROP TABLE IF EXISTS conduit_schema_migrations`); err != nil {
			t.Fatal(err)
		}
		return
	}
	if _, err := database.ExecContext(t.Context(), statement, crashComponent); err != nil {
		// The table is absent when the previous trial dropped it, which is not
		// a failure -- there is nothing to delete.
		if !strings.Contains(err.Error(), "conduit_schema_migrations") {
			t.Fatal(err)
		}
	}
}

// freshCrashDatabase gives these tests a database of their own.
//
// They drop the ledger table repeatedly, which the race test's comment explains
// is a landmine anywhere shared: `go test` runs packages in parallel and one of
// them reading the ledger inside that window fails having done nothing wrong.
// The data source name comes back too, because the child process needs it.
func freshCrashDatabase(t *testing.T, shared *sql.DB) (string, *sql.DB) {
	t.Helper()
	dataSourceName, drop, err := pgtest.Fresh(
		t.Context(), shared, os.Getenv(pgtest.DataSourceNameEnvironment), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drop)
	opened, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { opened.Close() })
	if err := opened.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	return dataSourceName, opened
}

// crashTrials reads the trial count, so a bisect or an investigation can turn
// it up without editing the file. The default is what CI runs.
func crashTrials(t *testing.T, standard int) int {
	t.Helper()
	raw := os.Getenv(crashTrialsEnvironment)
	if raw == "" {
		return standard
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 {
		t.Fatalf("%s=%q is not a positive trial count", crashTrialsEnvironment, raw)
	}
	return parsed
}

type crashOutcome struct {
	// applied is true when the child printed its completion line, meaning Apply
	// returned before anything killed it.
	applied bool
	// applyTook is what the child measured Apply to take, reported on its
	// completion line. Zero when the child was killed.
	applyTook time.Duration
	output    string
}

// measureApplyWindow times an uninterrupted run from READY to completion.
//
// The randomised kills are drawn against this rather than against a constant,
// because the duration of a migration here is a property of the machine and the
// server, not of this file: a constant tuned on a fast runner would put every
// kill after the migration on a slow one, and the test would pass by never
// interrupting anything. The test asserts separately that at least one trial
// was actually killed, which is what catches that failure mode if it happens
// anyway.
func measureApplyWindow(t *testing.T, database *sql.DB, dataSourceName string) time.Duration {
	t.Helper()
	longest := time.Duration(0)
	for range 3 {
		resetCrashProbe(t, database, true)
		outcome := runCrashChild(t, dataSourceName, crashNowhere, 0)
		if !outcome.applied {
			t.Fatalf("an uninterrupted child did not complete: %s", outcome.output)
		}
		if outcome.applyTook > longest {
			longest = outcome.applyTook
		}
	}
	if longest < time.Millisecond {
		longest = time.Millisecond
	}
	return longest
}

// runCrashChild re-executes this test binary as the migrating process and kills
// it.
//
// A child process rather than a goroutine, because the interruption being
// modelled is the death of the process running Apply, and nothing inside a
// process can model that: a panic unwinds, a cancelled context rolls back
// politely, and both leave the driver free to send the ROLLBACK that the crash
// is supposed to prevent. Only a real SIGKILL of a real process ends the
// connection the way a crash does.
//
// With delay zero the child kills itself at its injected point; otherwise the
// parent kills it that long after the child reports READY.
func runCrashChild(t *testing.T, dataSourceName, point string, delay time.Duration) crashOutcome {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run", "^TestTheCrashChildRunsTheMigrations$", "-test.count", "1")
	command.Env = append(os.Environ(),
		crashChildDSNEnvironment+"="+dataSourceName,
		crashChildPointEnvironment+"="+point,
	)
	pipe, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = command.Stdout
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}

	// The reader owns the pipe until end of stream and hands the transcript over
	// by closing `read`. Reading the builder before that would race the
	// scanner, and calling Wait before it would close the pipe underneath it --
	// os/exec documents the second, and -race catches the first.
	lines := make(chan string, 64)
	read := make(chan string, 1)
	go func() {
		collected := &strings.Builder{}
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			line := scanner.Text()
			collected.WriteString(line)
			collected.WriteByte('\n')
			select {
			case lines <- line:
			default:
			}
		}
		close(lines)
		read <- collected.String()
	}()

	if !waitForCrashLine(lines, crashChildReady, 60*time.Second) {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("the child never reported %s: %s", crashChildReady, <-read)
	}
	if delay > 0 {
		time.Sleep(delay)
		_ = command.Process.Signal(syscall.SIGKILL)
	}
	output := <-read
	waitErr := command.Wait()
	applied, applyTook := crashChildCompletion(output)
	if !applied && waitErr == nil {
		t.Fatalf("the child exited without applying and without being killed: %s", output)
	}
	return crashOutcome{applied: applied, applyTook: applyTook, output: output}
}

// crashChildCompletion reads the child's completion line, which carries how
// long its Apply took.
func crashChildCompletion(output string) (bool, time.Duration) {
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, crashChildApplied) {
			continue
		}
		nanoseconds, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, crashChildApplied)), 10, 64)
		if err != nil {
			return true, 0
		}
		return true, time.Duration(nanoseconds)
	}
	return false, 0
}

func waitForCrashLine(lines <-chan string, wanted string, limit time.Duration) bool {
	deadline := time.After(limit)
	for {
		select {
		case line, open := <-lines:
			if !open {
				return false
			}
			if strings.Contains(line, wanted) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func isCrashChild() bool { return os.Getenv(crashChildDSNEnvironment) != "" }

// TestTheCrashChildRunsTheMigrations is the child half of the harness: the
// process that gets killed. It is a test only because re-executing this binary
// is how a Go test starts a process with this package linked into it, and it
// does nothing at all unless the parent set the environment.
//
// It runs Apply under context.Background rather than the test's context on
// purpose. A cancelled context is a polite unwind -- the driver would send a
// ROLLBACK -- and this process exists to model the impolite ending, where
// nothing is sent at all.
func TestTheCrashChildRunsTheMigrations(t *testing.T) {
	dataSourceName := os.Getenv(crashChildDSNEnvironment)
	if dataSourceName == "" {
		t.Skip(crashChildDSNEnvironment + " is set only for the child process of a crash trial")
	}
	database := sql.OpenDB(crashConnector{
		dataSourceName: dataSourceName,
		point:          os.Getenv(crashChildPointEnvironment),
	})
	defer database.Close()
	if err := database.PingContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	fmt.Println(crashChildReady)
	started := time.Now()
	if err := pgmigrate.Apply(context.Background(), database, crashComponent, crashProbeMigrations()); err != nil {
		// Not a failure of the child: a parent that killed a sibling connection,
		// or a context this run lost, is reported to the parent as "not
		// applied", which is what it does with a kill anyway.
		fmt.Println("crash child Apply returned an error:", err)
		return
	}
	// The duration comes back with the completion line because the parent
	// cannot see it: measuring from out there would include this process
	// tearing itself down, which is time no kill can usefully land in, and the
	// randomised delays are drawn against this number.
	fmt.Println(crashChildApplied, time.Since(started).Nanoseconds())
}

// crashConnector opens ordinary pgx connections and wraps them so a statement
// can be the last thing this process does.
//
// The injection lives in the driver rather than in pgmigrate because the point
// of the exercise is to interrupt the package as it ships. A test hook inside
// applyOne would be a different program from the one that runs in production,
// and the window being tested is exactly the one such a hook would perturb.
type crashConnector struct {
	dataSourceName string
	point          string
}

func (c crashConnector) Driver() driver.Driver { return stdlib.GetDefaultDriver() }

func (c crashConnector) Connect(context.Context) (driver.Conn, error) {
	inner, err := stdlib.GetDefaultDriver().Open(c.dataSourceName)
	if err != nil {
		return nil, err
	}
	execer, ok := inner.(driver.ExecerContext)
	if !ok {
		return nil, errors.New("the pgx driver connection cannot execute statements directly")
	}
	beginner, ok := inner.(driver.ConnBeginTx)
	if !ok {
		return nil, errors.New("the pgx driver connection cannot begin a transaction with options")
	}
	return &crashConn{Conn: inner, execer: execer, beginner: beginner, point: c.point}, nil
}

// crashConn forwards everything and kills the process at the configured point.
//
// database/sql discovers a driver connection's abilities by type assertion, so
// the embedded driver.Conn carries Prepare and Close while the interfaces the
// package actually uses -- ExecerContext, QueryerContext, ConnBeginTx and the
// argument checker -- are forwarded explicitly. An unforwarded QueryerContext
// would silently fall back to prepared statements and change the very sequence
// under test.
type crashConn struct {
	driver.Conn
	execer   driver.ExecerContext
	beginner driver.ConnBeginTx
	point    string
	// sawTargetDDL records that the migration under test has executed on this
	// connection, so the commit hook can fire on that transaction rather than
	// on whichever one happens to commit first.
	sawTargetDDL bool
}

func (c *crashConn) ExecContext(
	ctx context.Context, query string, arguments []driver.NamedValue,
) (driver.Result, error) {
	targetDDL := strings.Contains(query, crashProbeTables[1])
	ledgerRow := strings.Contains(query, "INSERT INTO conduit_schema_migrations") &&
		namedValuesMention(arguments, crashTargetName)
	if ledgerRow && c.point == crashBeforeLedgerRow {
		crashThisProcess()
	}
	result, err := c.execer.ExecContext(ctx, query, arguments)
	if targetDDL {
		c.sawTargetDDL = true
		if c.point == crashAfterDDL {
			crashThisProcess()
		}
	}
	if ledgerRow && c.point == crashAfterLedgerRow {
		crashThisProcess()
	}
	return result, err
}

func (c *crashConn) QueryContext(
	ctx context.Context, query string, arguments []driver.NamedValue,
) (driver.Rows, error) {
	querier, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, errors.New("the pgx driver connection cannot query directly")
	}
	return querier.QueryContext(ctx, query, arguments)
}

func (c *crashConn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	transaction, err := c.beginner.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return crashTx{Tx: transaction, connection: c}, nil
}

func (c *crashConn) CheckNamedValue(value *driver.NamedValue) error {
	checker, ok := c.Conn.(driver.NamedValueChecker)
	if !ok {
		return driver.ErrSkip
	}
	return checker.CheckNamedValue(value)
}

func (c *crashConn) ResetSession(ctx context.Context) error {
	resetter, ok := c.Conn.(driver.SessionResetter)
	if !ok {
		return nil
	}
	return resetter.ResetSession(ctx)
}

type crashTx struct {
	driver.Tx
	connection *crashConn
}

func (t crashTx) Commit() error {
	if t.connection.point == crashBeforeCommit && t.connection.sawTargetDDL {
		crashThisProcess()
	}
	return t.Tx.Commit()
}

func namedValuesMention(arguments []driver.NamedValue, wanted string) bool {
	for _, argument := range arguments {
		if text, ok := argument.Value.(string); ok && text == wanted {
			return true
		}
	}
	return false
}

// crashThisProcess ends this process the way a crash ends one.
//
// SIGKILL to self rather than os.Exit: os.Exit runs no deferred functions but
// the Go runtime still closes file descriptors as the process tears down, and
// an orderly FIN on the connection is a politer ending than a crash gives. The
// signal cannot be caught, blocked or handled, so the next instruction after
// this call never runs -- the select is there to say so to the compiler.
func crashThisProcess() {
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGKILL)
	select {}
}
