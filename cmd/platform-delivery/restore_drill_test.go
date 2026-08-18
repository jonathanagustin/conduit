package main

// The backup and restore drill: gap 4 of docs/operations/PRODUCTION-READINESS.md.
//
// A backup procedure nobody has restored from is a document, not a recovery
// plan, and the document is written -- docs/operations/backup-and-restore.md --
// against what this test proves rather than the other way round. It belongs in
// this package because this is the only one that opens all persistence
// components the Hub opens: `cmd/platform-delivery` constructs the control-plane
// repository, reconciliation store and Gitless resolution log against one DSN,
// which is exactly the coupling a restore has to reproduce and no package can
// test alone.
//
// What it asserts, and why each one:
//
//   - Nothing is pending for any component after the restore. A dump that
//     carried a migration's DDL without the ledger row recording it, or the row
//     without the DDL, produces a database that starts and then diverges; the
//     ledger surviving intact is what makes a restored database a database the
//     Hub can start against rather than one it will migrate again.
//   - State matches through the real repositories. Row counts would pass on a
//     database whose bytes restored and whose decoding did not, and artifact
//     contents are bytea in this database rather than in a registry -- so the
//     bytes are read back through OpenBlob and compared.
//   - No torn decision/audit/outbox triple. CommitDecision writes all three in
//     one transaction precisely so that a Plan cannot exist without the audit
//     record of why it was proposed; a backup that tears them apart is worthless
//     however complete it looks, and the tear is only detectable if the dump is
//     taken while decisions are being committed. So it is.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	reconciliationpostgres "github.com/jonathanagustin/conduit/internal/delivery/reconciliation/adapters/postgres"
	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/domain"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/resolutionlogpostgres"
	gitlessdomain "github.com/jonathanagustin/conduit/internal/gitless/domain"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
	"github.com/jonathanagustin/conduit/internal/pgtest"
)

// drillEnvironment is this test's own environment name in the reconciliation
// tables. Its own database too -- the drill dumps and restores a whole database,
// which is not something to do to the one the rest of the suite shares.
const drillEnvironment = "restore-drill"

func TestPostgresBackupAndRestoreDrill(t *testing.T) {
	dataSourceName := os.Getenv(pgtest.DataSourceNameEnvironment)
	if dataSourceName == "" {
		t.Skip(pgtest.DataSourceNameEnvironment + " is required for the PostgreSQL integration gate")
	}
	ctx := t.Context()
	shared, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	if err := shared.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	backup := newClient(t, ctx, shared, dataSourceName)

	sourceDSN, dropSource, err := pgtest.Fresh(ctx, shared, dataSourceName, t.Name()+"-source")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dropSource)

	seeded := seedSource(t, ctx, sourceDSN)

	// The dump runs while decisions are being committed. A dump of a quiescent
	// database cannot tear anything, so it would prove the backup readable and
	// say nothing whatever about whether it is consistent -- which is the only
	// interesting question about a backup of a system that writes three tables
	// in one transaction.
	archive := filepath.Join(t.TempDir(), "conduit.dump")
	stopWriting := commitDecisionsUntilStopped(t, ctx, sourceDSN)
	backup.dump(t, ctx, sourceDSN, archive)
	committed := stopWriting()
	if committed == 0 {
		t.Fatal("no decisions were committed during the dump; the drill would prove nothing about a torn backup")
	}
	t.Logf("%d decisions committed while the dump ran", committed)

	targetDSN, dropTarget, err := pgtest.Fresh(ctx, shared, dataSourceName, t.Name()+"-restored")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dropTarget)
	backup.restore(t, ctx, targetDSN, archive)

	restored, err := sql.Open("pgx", targetDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()

	// Order matters here and the test is worthless without it. Opening either
	// repository runs Apply, which would create whatever the restore failed to
	// bring across and then report a healthy database -- repairing the evidence
	// before reading it. So the ledger is interrogated on a plain connection
	// first, and the repositories are constructed afterwards.
	assertNothingPending(t, ctx, restored)
	assertNoTornDecisionTriple(t, ctx, restored)
	assertStateSurvived(t, ctx, targetDSN, seeded)
}

// seededState is what the drill wrote before the dump and expects to read back
// through the real repositories afterwards.
type seededState struct {
	environment           string
	planID                string
	blobDigest            string
	blobContents          []byte
	key                   domain.Key
	generation            string
	resolutionEnvironment string
	resolutionDigest      string
}

func seedSource(t *testing.T, ctx context.Context, dataSourceName string) seededState {
	t.Helper()
	repository, err := controlplane.NewPostgresRepository(ctx, dataSourceName)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()

	contents, err := os.ReadFile(filepath.Join("..", "..", "examples", "example-foundation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state controlplane.DesiredState
	if err := json.Unmarshal(contents, &state); err != nil {
		t.Fatal(err)
	}
	record, err := controlplane.NewLifecycle(repository).CreatePlan(ctx, state)
	if err != nil {
		t.Fatal(err)
	}

	// An artifact, because in this product the artifact bytes are in the
	// database: conduit_repository_blobs holds `contents bytea`, and the OCI
	// registry on the gitless path is a delivery source rather than Hub state.
	// One pg_dump therefore covers metadata and artifacts both, which is worth
	// proving rather than assuming -- and is why the dump is as large as the
	// artifacts.
	blob := []byte("conduit restore drill artifact\n")
	sum := sha256.Sum256(blob)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if _, _, err := repository.PutBlob(ctx, digest, "application/octet-stream", bytes.NewReader(blob)); err != nil {
		t.Fatal(err)
	}

	store, err := reconciliationpostgres.New(ctx, dataSourceName)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key := domain.Key{Environment: drillEnvironment, Installation: "seeded"}
	if err := commitOneDecision(ctx, store, key, "gen-seeded", time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	resolutionLog, err := resolutionlogpostgres.New(ctx, dataSourceName)
	if err != nil {
		t.Fatal(err)
	}
	defer resolutionLog.Close()
	resolutionEnvironment := "restore-drill-gitless"
	resolutionDigest := "sha256:" + strings.Repeat("a", 64)
	if err := resolutionLog.Record(ctx, gitlessdomain.Resolution{
		Environment: resolutionEnvironment,
		Selector: gitlessdomain.Selector{
			Kind: gitlessdomain.ByChannel, Repository: "registry.example/foundry/runtime", Channel: "stable",
		},
		Pin: gitlessdomain.Pin{
			Repository: "registry.example/foundry/runtime", Digest: resolutionDigest,
		},
		Publisher: "https://github.com/example/foundry/.github/workflows/release.yml@refs/tags/v1.2.3",
	}); err != nil {
		t.Fatal(err)
	}
	return seededState{
		environment: state.Environment.ID, planID: record.ID,
		blobDigest: digest, blobContents: blob, key: key, generation: "gen-seeded",
		resolutionEnvironment: resolutionEnvironment, resolutionDigest: resolutionDigest,
	}
}

// commitDecisionsUntilStopped keeps committing decision/audit/outbox triples
// until the returned function is called, which returns how many committed.
//
// One goroutine rather than many, deliberately. The drill is about whether a
// dump can land in the middle of a transaction, and one writer committing
// steadily is enough for that; concurrency here would only add ways for the
// test to fail that have nothing to do with backups.
func commitDecisionsUntilStopped(t *testing.T, ctx context.Context, dataSourceName string) func() int {
	t.Helper()
	store, err := reconciliationpostgres.New(ctx, dataSourceName)
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan int)
	go func() {
		defer store.Close()
		committed := 0
		for index := 0; ; index++ {
			select {
			case <-stop:
				done <- committed
				return
			default:
			}
			key := domain.Key{Environment: drillEnvironment, Installation: fmt.Sprintf("app-%04d", index)}
			generation := fmt.Sprintf("gen-%04d", index)
			if err := commitOneDecision(ctx, store, key, generation, time.Now().UTC()); err != nil {
				// Not a t.Fatal: this runs on another goroutine, where Fatal
				// only stops the goroutine and leaves the test reporting a
				// failure it cannot locate. The count is the assertion -- if
				// this path is taken every time, committed stays 0 and the
				// caller says so.
				t.Errorf("committing a decision during the dump failed: %v", err)
				done <- committed
				return
			}
			committed++
		}
	}()
	stopped := false
	return func() int {
		if stopped {
			return 0
		}
		stopped = true
		close(stop)
		return <-done
	}
}

// commitOneDecision drives one key through the real path: record desired state,
// lease the work it queues, decide, and commit the decision with its audit
// record and proposal in one transaction.
func commitOneDecision(
	ctx context.Context, store *reconciliationpostgres.Store,
	key domain.Key, generation string, now time.Time,
) error {
	payload := []byte(`{"installation":"` + key.Installation + `"}`)
	payloadSum := sha256.Sum256(payload)
	targetSum := sha256.Sum256([]byte(generation))
	if err := store.UpsertDesired(ctx, domain.DesiredSnapshot{
		Key: key, Generation: generation,
		TargetDigest:  "sha256:" + hex.EncodeToString(targetSum[:]),
		PayloadDigest: "sha256:" + hex.EncodeToString(payloadSum[:]),
		Payload:       payload, RecordedAt: now,
	}); err != nil {
		return fmt.Errorf("record desired state: %w", err)
	}
	lease, leased, err := store.LeaseWork(ctx, now, time.Minute)
	if err != nil {
		return fmt.Errorf("lease work: %w", err)
	}
	if !leased {
		// The upsert above queued work, so nothing available means another
		// writer took it -- which this drill does not have. Reporting it beats
		// looping, because a silent retry would turn a lost notification into a
		// slow test rather than a finding.
		return fmt.Errorf("no work available after recording desired state for %s", key.Installation)
	}
	input, err := store.LoadInput(ctx, lease.Key)
	if err != nil {
		return fmt.Errorf("load input: %w", err)
	}
	decision, proposal, err := domain.Decide(input, lease.Attempt, now, 0)
	if err != nil {
		return fmt.Errorf("decide: %w", err)
	}
	if proposal == nil {
		return fmt.Errorf("decision %s produced no proposal, so no outbox row would be written", decision.Kind)
	}
	if err := store.CommitDecision(ctx, lease, decision, proposal); err != nil {
		return fmt.Errorf("commit decision: %w", err)
	}
	return nil
}

// assertNothingPending is the ledger half: a restored database must need no
// migration. Read on a plain connection, before any repository is constructed,
// because constructing one applies whatever is missing.
func assertNothingPending(t *testing.T, ctx context.Context, restored *sql.DB) {
	t.Helper()
	for component, migrations := range map[string][]pgmigrate.Migration{
		controlplane.PostgresMigrationComponent: controlplane.PostgresMigrations(),
		reconciliationpostgres.Component():      reconciliationpostgres.Migrations(),
		resolutionlogpostgres.Component():       resolutionlogpostgres.Migrations(),
	} {
		pending, err := pgmigrate.Pending(ctx, restored, component, migrations)
		if err != nil {
			t.Fatalf("reading %s's pending migrations from the restored database: %v", component, err)
		}
		if len(pending) != 0 {
			names := make([]string, 0, len(pending))
			for _, migration := range pending {
				names = append(names, migration.Name)
			}
			t.Errorf(
				"the restored database reports %s migrations %v pending. The ledger did not survive the "+
					"backup intact, so the Hub would migrate a database that is already migrated",
				component, names,
			)
		}
	}
}

// assertNoTornDecisionTriple is the consistency half.
//
// Each query is a cross-table invariant that one transaction in CommitDecision
// establishes, so a non-zero answer means the dump captured part of a committed
// transaction and not the rest. The row counts are asserted first and are not a
// formality: every predicate below is trivially satisfied by an empty table, and
// an assertion that passes because there is nothing to check is the exact
// failure #146's adoption test had.
func assertNoTornDecisionTriple(t *testing.T, ctx context.Context, restored *sql.DB) {
	t.Helper()
	for _, table := range []string{
		"conduit_reconciliation_decisions",
		"conduit_reconciliation_audit",
		"conduit_reconciliation_proposal_outbox",
	} {
		var rows int
		if err := restored.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows == 0 {
			t.Fatalf("the restored %s is empty; the invariants below would pass without checking anything", table)
		}
	}
	for name, query := range map[string]string{
		"a decision with no audit record": `
SELECT count(*) FROM conduit_reconciliation_decisions AS decision
WHERE NOT EXISTS (
    SELECT 1 FROM conduit_reconciliation_audit AS audit WHERE audit.decision_id = decision.id
)`,
		"an audit record with no decision": `
SELECT count(*) FROM conduit_reconciliation_audit AS audit
WHERE NOT EXISTS (
    SELECT 1 FROM conduit_reconciliation_decisions AS decision WHERE decision.id = audit.decision_id
)`,
		// The outbox carries no decision id, so the join is on the input digest
		// inside the serialised proposal -- which is the same value the decision
		// row carries, and the only link between them that exists.
		"a queued proposal with no decision": `
SELECT count(*) FROM conduit_reconciliation_proposal_outbox AS outbox
WHERE NOT EXISTS (
    SELECT 1 FROM conduit_reconciliation_decisions AS decision
    WHERE decision.environment = outbox.environment
      AND decision.installation = outbox.installation
      AND decision.input_digest = convert_from(outbox.document, 'UTF8')::jsonb ->> 'inputDigest'
)`,
	} {
		var torn int
		if err := restored.QueryRowContext(ctx, query).Scan(&torn); err != nil {
			t.Fatal(err)
		}
		if torn != 0 {
			t.Errorf(
				"%s: %d in the restored database. CommitDecision writes the decision, its audit record "+
					"and its proposal in one transaction, so a backup that separates them has captured a "+
					"state the Hub never committed",
				name, torn,
			)
		}
	}
}

// assertStateSurvived reads the restored database back through the constructors
// production uses. Row counts prove the bytes arrived; only the real
// repositories prove they still decode into the state they came from.
func assertStateSurvived(t *testing.T, ctx context.Context, dataSourceName string, seeded seededState) {
	t.Helper()
	repository, err := controlplane.NewPostgresRepository(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("opening the control-plane repository on the restored database: %v", err)
	}
	defer repository.Close()
	plans, err := repository.ListPlans(ctx, seeded.environment)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].ID != seeded.planID {
		t.Fatalf("the restored repository holds %d plans for %s, want the one %s",
			len(plans), seeded.environment, seeded.planID)
	}

	_, reader, err := repository.OpenBlob(ctx, seeded.blobDigest)
	if err != nil {
		t.Fatalf("the artifact bytes did not survive the restore: %v", err)
	}
	defer reader.Close()
	restoredBlob, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restoredBlob, seeded.blobContents) {
		t.Errorf("the restored artifact is %q, want %q", restoredBlob, seeded.blobContents)
	}

	store, err := reconciliationpostgres.New(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("opening the reconciliation store on the restored database: %v", err)
	}
	defer store.Close()
	snapshot, found, err := store.CurrentDesired(ctx, seeded.key)
	if err != nil {
		t.Fatal(err)
	}
	if !found || snapshot.Generation != seeded.generation {
		t.Errorf("desired state for %s restored as (found=%v, generation=%q), want %q",
			seeded.key.Installation, found, snapshot.Generation, seeded.generation)
	}
	resolutionLog, err := resolutionlogpostgres.New(ctx, dataSourceName)
	if err != nil {
		t.Fatalf("opening the Gitless resolution log on the restored database: %v", err)
	}
	defer resolutionLog.Close()
	database, err := sql.Open("pgx", dataSourceName)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var resolutionDigest string
	if err := database.QueryRowContext(ctx, `
SELECT digest FROM conduit_gitless_resolutions WHERE environment = $1`,
		seeded.resolutionEnvironment).Scan(&resolutionDigest); err != nil {
		t.Fatalf("the Gitless resolution history did not survive the restore: %v", err)
	}
	if resolutionDigest != seeded.resolutionDigest {
		t.Errorf("restored Gitless digest is %q, want %q", resolutionDigest, seeded.resolutionDigest)
	}
}

// client runs pg_dump and pg_restore, wherever they are.
//
// They are not necessarily on this machine's PATH, and preferring the machine's
// copy would be wrong even when they are. pg_dump refuses a server newer than
// itself, and the two versions here are chosen by different people: the server
// is the digest-pinned image in scripts/test-postgres-repository.sh -- today
// PostgreSQL 18 -- while the client is whatever the CI runner image happens to
// ship, which is 16. Those already diverge, so a drill built on the runner's
// client would have failed on its first dispatch.
//
// The fix is to use the pinned image's own client, which matches its server by
// construction and needs no package installed on the runner. The gate script
// sets prefix to `docker exec -i <container>` and dsn to the server's address
// from inside that container; both empty means the tools are local, which is
// the path a developer with PostgreSQL installed takes.
//
// The archive therefore moves over stdin and stdout rather than a file path:
// the client may not share a filesystem with this process.
type client struct {
	prefix []string
	dsn    string
}

// PgToolPrefixEnvironment and PgToolDSNEnvironment are read from the
// environment rather than passed, because the gate script owns the container
// and the test owns the assertions, and neither should have to know the other's
// business beyond these two strings.
const (
	PgToolPrefixEnvironment = "CONDUIT_TEST_PG_TOOL_PREFIX"
	PgToolDSNEnvironment    = "CONDUIT_TEST_PG_TOOL_DSN"
)

// newClient resolves the tools and refuses to continue unless they can take a
// backup of this server.
//
// Every branch here fails rather than skips. A skip on a gate that promises a
// restore drill reports green for a backup nobody tested -- the same class of
// mistake as reading a piped exit code -- and the version coupling is the
// specific way this breaks later, so it has to say so out loud.
func newClient(t *testing.T, ctx context.Context, database *sql.DB, dataSourceName string) client {
	t.Helper()
	resolved := client{dsn: dataSourceName}
	if prefix := strings.Fields(os.Getenv(PgToolPrefixEnvironment)); len(prefix) > 0 {
		resolved.prefix = prefix
	}
	if dsn := os.Getenv(PgToolDSNEnvironment); dsn != "" {
		resolved.dsn = dsn
	}
	if len(resolved.prefix) == 0 {
		for _, tool := range []string{"pg_dump", "pg_restore"} {
			if _, err := exec.LookPath(tool); err != nil {
				t.Fatalf(
					"%s is not on PATH and %s is unset. The PostgreSQL gate is running, so the restore "+
						"drill has to run too: either install the PostgreSQL client, or point %s at a "+
						"container that has one. Skipping here would report a green backup gate: %v",
					tool, PgToolPrefixEnvironment, PgToolPrefixEnvironment, err,
				)
			}
		}
	}

	var serverVersion int
	if err := database.QueryRowContext(ctx, `SHOW server_version_num`).Scan(&serverVersion); err != nil {
		t.Fatal(err)
	}
	output, err := resolved.command(ctx, "pg_dump", "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("pg_dump --version: %v\n%s", err, output)
	}
	match := regexp.MustCompile(`(\d+)\.\d+`).FindSubmatch(output)
	if match == nil {
		t.Fatalf("could not read a version out of %q", output)
	}
	clientVersion, err := strconv.Atoi(string(match[1]))
	if err != nil {
		t.Fatal(err)
	}
	server := serverVersion / 10000
	if clientVersion < server {
		t.Fatalf(
			"pg_dump is %d and the server is %d, so this pair cannot take a backup at all -- pg_dump "+
				"refuses a server newer than itself. Point %s at the pinned server image's own client "+
				"rather than the runner's; a drill that skipped here would hide that backups had stopped "+
				"being tested",
			clientVersion, server, PgToolPrefixEnvironment,
		)
	}
	t.Logf("backup client pg_dump %d against server %d%s", clientVersion, server,
		map[bool]string{true: " (via " + strings.Join(resolved.prefix, " ") + ")"}[len(resolved.prefix) > 0])
	return resolved
}

func (resolved client) command(ctx context.Context, name string, arguments ...string) *exec.Cmd {
	argv := append(append([]string{}, resolved.prefix...), name)
	argv = append(argv, arguments...)
	return exec.CommandContext(ctx, argv[0], argv[1:]...)
}

// forDatabase points the client's DSN at one database. The client may reach the
// server by a different address than the test does, so this rewrites the
// client's own DSN rather than the test's.
func (resolved client) forDatabase(t *testing.T, testDSN string) string {
	t.Helper()
	parsed, err := url.Parse(testDSN)
	if err != nil {
		t.Fatal(err)
	}
	dsn, err := pgtest.WithDatabase(resolved.dsn, strings.TrimPrefix(parsed.Path, "/"))
	if err != nil {
		t.Fatal(err)
	}
	return dsn
}

// dump writes a custom-format archive of one database to a local file.
//
// Over stdout rather than --file, because the client may be running in a
// container with no view of this filesystem. stderr is kept separate: it would
// otherwise be interleaved into the archive and corrupt it.
func (resolved client) dump(t *testing.T, ctx context.Context, testDSN, path string) {
	t.Helper()
	archive, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	command := resolved.command(ctx, "pg_dump", "--format=custom", resolved.forDatabase(t, testDSN))
	var problems bytes.Buffer
	command.Stdout, command.Stderr = archive, &problems
	if err := command.Run(); err != nil {
		t.Fatalf("pg_dump failed: %v\n%s", err, problems.String())
	}
	if problems.Len() > 0 {
		t.Logf("pg_dump: %s", problems.String())
	}
	information, err := archive.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if information.Size() == 0 {
		t.Fatal("pg_dump produced an empty archive and reported success")
	}
}

// restore reads the archive back into an empty database.
//
// --exit-on-error because pg_restore's default is to report a failed statement
// and carry on to the end with status 0, which would hand the assertions below
// a half-restored database that looked like a successful restore.
func (resolved client) restore(t *testing.T, ctx context.Context, testDSN, path string) {
	t.Helper()
	archive, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	command := resolved.command(ctx, "pg_restore",
		"--dbname="+resolved.forDatabase(t, testDSN), "--exit-on-error", "--no-owner", "--no-privileges")
	command.Stdin = archive
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("pg_restore failed: %v\n%s", err, output)
	}
	if len(output) > 0 {
		// With --exit-on-error anything printed here is a warning rather than a
		// failure, and a restore that warns is one somebody should look at.
		t.Logf("pg_restore: %s", output)
	}
}
