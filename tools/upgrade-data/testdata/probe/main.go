// This fixture is compiled unchanged inside both source revisions. It uses
// production constructors; it is never linked into a release executable.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	reconciliation "github.com/jonathanagustin/conduit/internal/delivery/reconciliation/adapters/postgres"
	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/domain"
	resolutionlog "github.com/jonathanagustin/conduit/internal/gitless/adapters/resolutionlogpostgres"
	gitless "github.com/jonathanagustin/conduit/internal/gitless/domain"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

var epoch = time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)

func main() {
	if err := run(); err != nil {
		// Driver errors can carry DSNs. The runner keeps the database disposable
		// and exposes only the operation which failed, never connection material.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if len(os.Args) != 2 || (os.Args[1] != "seed" && os.Args[1] != "read") {
		return fmt.Errorf("expected seed or read")
	}
	dsn := os.Getenv("CONDUIT_UPGRADE_DSN")
	started := time.Now()
	repo, err := controlplane.NewPostgresRepository(ctx, dsn)
	if err != nil {
		return fmt.Errorf("open control-plane repository failed")
	}
	defer repo.Close()
	store, err := reconciliation.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("open reconciliation store failed")
	}
	defer store.Close()
	log, err := resolutionlog.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("open resolution log failed")
	}
	defer log.Close()
	constructorTime := time.Since(started)
	if os.Args[1] == "seed" {
		if err := seed(ctx, repo, store, log); err != nil {
			return err
		}
	}
	snapshot, err := read(ctx, repo, store)
	if err != nil {
		return err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open inspection connection failed")
	}
	defer db.Close()
	for component, migrations := range map[string][]pgmigrate.Migration{
		controlplane.PostgresMigrationComponent: controlplane.PostgresMigrations(),
		reconciliation.Component():              reconciliation.Migrations(),
		resolutionlog.Component():               resolutionlog.Migrations(),
	} {
		pending, err := pgmigrate.Pending(ctx, db, component, migrations)
		if err != nil || len(pending) != 0 {
			return fmt.Errorf("pending migrations for %s", component)
		}
	}
	// Preserve complete physical decision/audit/outbox documents and identities,
	// not just their counts. The fixture has no dispatcher consuming these rows.
	for table, order := range map[string]string{
		"conduit_reconciliation_desired":         "environment, installation",
		"conduit_reconciliation_work":            "environment, installation",
		"conduit_reconciliation_decisions":       "id",
		"conduit_reconciliation_audit":           "decision_id",
		"conduit_reconciliation_proposal_outbox": "proposal_id",
		"conduit_gitless_resolutions":            "sequence",
	} {
		rows, err := db.QueryContext(ctx, "SELECT row_to_json(t) FROM (SELECT * FROM "+table+" ORDER BY "+order+") t")
		if err != nil {
			return fmt.Errorf("inspect %s failed", table)
		}
		values := []json.RawMessage{}
		for rows.Next() {
			var value json.RawMessage
			if err := rows.Scan(&value); err != nil {
				rows.Close()
				return fmt.Errorf("decode %s failed", table)
			}
			values = append(values, value)
		}
		err = rows.Err()
		rows.Close()
		if err != nil || len(values) == 0 {
			return fmt.Errorf("empty or unreadable %s", table)
		}
		snapshot[table] = values
	}
	rows, err := db.QueryContext(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_name LIKE 'conduit_%' AND table_type = 'BASE TABLE' ORDER BY table_name`)
	if err != nil {
		return fmt.Errorf("list physical tables failed")
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			rows.Close()
			return fmt.Errorf("decode table name failed")
		}
		tables = append(tables, table)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("read table names failed")
	}
	counts := map[string]int64{}
	for _, table := range tables {
		var count int64
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM "`+strings.ReplaceAll(table, `"`, `""`)+`"`).Scan(&count); err != nil {
			return fmt.Errorf("count %s failed", table)
		}
		counts[table] = count
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"constructorMilliseconds": constructorTime.Milliseconds(), "snapshot": snapshot, "physicalRows": counts,
	})
}

func seed(ctx context.Context, repo *controlplane.PostgresRepository, store *reconciliation.Store, log *resolutionlog.Log) error {
	err := repo.Transaction(ctx, func(tx controlplane.Repository) error {
		for i := 0; i < 500; i++ {
			_, err := tx.PutTeam(ctx, controlplane.Team{Name: fmt.Sprintf("team-%04d", i), DisplayName: fmt.Sprintf("Team %d", i), MemberUsers: []string{fmt.Sprintf("user-%04d", i)}, CreatedAt: epoch, UpdatedAt: epoch, Revision: 1})
			if err != nil {
				return fmt.Errorf("seed team %d failed", i)
			}
		}
		for i := 0; i < 100; i++ {
			_, err := tx.PutApprovalPolicy(ctx, controlplane.ApprovalPolicy{ID: fmt.Sprintf("policy-%04d", i), DisplayName: "Upgrade fixture", Expression: "false", Enabled: false, CreatedAt: epoch, UpdatedAt: epoch, Revision: 1})
			if err != nil {
				return fmt.Errorf("seed policy %d failed", i)
			}
		}
		for i := 0; i < 100; i++ {
			var state controlplane.DesiredState
			if err := json.Unmarshal(foundation, &state); err != nil {
				return fmt.Errorf("decode foundation fixture failed")
			}
			state.Environment.ID = fmt.Sprintf("upgrade-%04d", i)
			if _, err := controlplane.NewLifecycle(tx).CreatePlan(ctx, state); err != nil {
				return fmt.Errorf("seed plan %d failed", i)
			}
		}
		for i := 0; i < 50; i++ {
			_, err := tx.PutEnvironmentReport(ctx, controlplane.EnvironmentReport{Environment: fmt.Sprintf("upgrade-%04d", i), AgentID: fmt.Sprintf("agent-%04d", i), ObservedAt: epoch})
			if err != nil {
				return fmt.Errorf("seed report %d failed", i)
			}
		}
		for i := 0; i < 1000; i++ {
			event := governance.NewAuthorizationCheckEvent(epoch.Add(time.Duration(i)*time.Second), fmt.Sprintf("user-%04d", i), "read", "upgrade-audit", governance.DecisionAllow, fmt.Sprintf("fixture %04d", i), nil)
			if _, err := tx.AppendAuthorizationAudit(ctx, event); err != nil {
				return fmt.Errorf("seed audit %d failed", i)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := 0; i < 10; i++ {
		data := blob(i)
		if _, _, err := repo.PutBlob(ctx, digest(data), "application/octet-stream", bytes.NewReader(data)); err != nil {
			return fmt.Errorf("seed blob %d failed", i)
		}
	}
	for i := 0; i < 100; i++ {
		key := domain.Key{Environment: "upgrade-decisions", Installation: fmt.Sprintf("app-%04d", i)}
		payload := []byte(fmt.Sprintf(`{"installation":%q}`, key.Installation))
		err := store.UpsertDesired(ctx, domain.DesiredSnapshot{Key: key, Generation: fmt.Sprintf("gen-%04d", i), TargetDigest: digest([]byte(key.Installation)), PayloadDigest: digest(payload), Payload: payload, RecordedAt: epoch})
		if err != nil {
			return fmt.Errorf("seed desired %d failed", i)
		}
		lease, leased, err := store.LeaseWork(ctx, epoch, time.Minute)
		if err != nil || !leased || lease.Key != key {
			return fmt.Errorf("seed lease %d failed", i)
		}
		input, err := store.LoadInput(ctx, key)
		if err != nil {
			return fmt.Errorf("seed input %d failed", i)
		}
		decision, proposal, err := domain.Decide(input, lease.Attempt, epoch, 0)
		if err != nil || proposal == nil {
			return fmt.Errorf("seed decision %d failed", i)
		}
		if err := store.CommitDecision(ctx, lease, decision, proposal); err != nil {
			return fmt.Errorf("commit decision %d failed", i)
		}
	}
	for i := 0; i < 50; i++ {
		err := log.Record(ctx, gitless.Resolution{Environment: fmt.Sprintf("upgrade-%04d", i), Selector: gitless.Selector{Kind: gitless.ByChannel, Repository: "registry.example/upgrade", Channel: "stable"}, Pin: gitless.Pin{Repository: "registry.example/upgrade", Digest: digest([]byte(fmt.Sprint(i)))}, Publisher: "https://github.com/example/product/.github/workflows/release.yml@refs/tags/v1.0.0"})
		if err != nil {
			return fmt.Errorf("seed resolution %d failed", i)
		}
	}
	// Committing decisions consumes their work rows. Seed a separate backlog so
	// work preservation cannot pass vacuously on an empty queue.
	for i := 0; i < 100; i++ {
		payload := []byte(fmt.Sprintf(`{"queued":%d}`, i))
		err := store.UpsertDesired(ctx, domain.DesiredSnapshot{Key: domain.Key{Environment: "upgrade-backlog", Installation: fmt.Sprintf("queued-%04d", i)}, Generation: "queued", TargetDigest: digest(payload), PayloadDigest: digest(payload), Payload: payload, RecordedAt: epoch})
		if err != nil {
			return fmt.Errorf("seed backlog %d failed", i)
		}
	}
	return nil
}

// Loaded by main from the exact archived source version, then kept fixed by the
// runner between the two builds so the comparison cannot drift with examples.
var foundation []byte

func init() {
	var err error
	foundation, err = os.ReadFile(os.Getenv("CONDUIT_UPGRADE_FOUNDATION"))
	if err != nil {
		panic("foundation fixture unavailable")
	}
}

func blob(i int) []byte { return bytes.Repeat([]byte(fmt.Sprintf("upgrade artifact %04d\n", i)), 4096) }
func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func read(ctx context.Context, repo *controlplane.PostgresRepository, store *reconciliation.Store) (map[string]any, error) {
	snapshot := map[string]any{}
	teams, err := repo.ListTeams(ctx)
	if err != nil || len(teams) != 500 {
		return nil, fmt.Errorf("read teams failed")
	}
	for _, team := range teams {
		v, err := repo.GetTeam(ctx, team.Name)
		if err != nil {
			return nil, fmt.Errorf("get team failed")
		}
		snapshot["team/"+team.Name] = v
	}
	policies, err := repo.ListApprovalPolicies(ctx)
	if err != nil || len(policies) != 100 {
		return nil, fmt.Errorf("read policies failed")
	}
	for _, policy := range policies {
		v, err := repo.GetApprovalPolicy(ctx, policy.ID)
		if err != nil {
			return nil, fmt.Errorf("get policy failed")
		}
		snapshot["policy/"+policy.ID] = v
	}
	plans, err := repo.ListPlans(ctx, "")
	if err != nil || len(plans) != 100 {
		return nil, fmt.Errorf("read plans failed")
	}
	for _, plan := range plans {
		v, err := repo.GetPlan(ctx, plan.ID)
		if err != nil {
			return nil, fmt.Errorf("get plan failed")
		}
		snapshot["plan/"+plan.ID] = v
	}
	reports, err := repo.ListEnvironmentReports(ctx)
	if err != nil || len(reports) != 50 {
		return nil, fmt.Errorf("read reports failed")
	}
	for _, report := range reports {
		v, err := repo.GetEnvironmentReport(ctx, report.Environment)
		if err != nil {
			return nil, fmt.Errorf("get report failed")
		}
		snapshot["report/"+report.Environment] = v
	}
	audits, err := repo.ListAuthorizationAudit(ctx, "upgrade-audit")
	if err != nil || len(audits) != 1000 {
		return nil, fmt.Errorf("read audits failed")
	}
	for _, audit := range audits {
		snapshot["audit/"+audit.ID] = audit
	}
	for i := 0; i < 10; i++ {
		expected := blob(i)
		descriptor, reader, err := repo.OpenBlob(ctx, digest(expected))
		if err != nil {
			return nil, fmt.Errorf("open blob %d failed", i)
		}
		actual, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || !bytes.Equal(expected, actual) {
			return nil, fmt.Errorf("blob %d bytes changed", i)
		}
		snapshot[fmt.Sprintf("blob/%d", i)] = map[string]any{"descriptor": descriptor, "digest": digest(actual), "bytes": len(actual)}
	}
	for i := 0; i < 100; i++ {
		key := domain.Key{Environment: "upgrade-decisions", Installation: fmt.Sprintf("app-%04d", i)}
		value, found, err := store.CurrentDesired(ctx, key)
		if err != nil || !found {
			return nil, fmt.Errorf("read desired %d failed", i)
		}
		snapshot["desired/"+key.Installation] = value
	}
	return snapshot, nil
}
