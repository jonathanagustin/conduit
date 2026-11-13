package auditpostgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/pgtest"
)

func databaseForTest(t *testing.T) (*sql.DB, string) {
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
	if err := ApplyMigrations(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	return database, isolated
}

func eventForTest(id string) domain.AuthorizationAuditEvent {
	event := domain.NewAuthorizationCheckEvent(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), "operator", "read", "resource", domain.DecisionAllow, "original", nil)
	event.ID = id
	return event
}

func appendEvent(t *testing.T, database *sql.DB, event domain.AuthorizationAuditEvent) domain.AuthorizationAuditEvent {
	t.Helper()
	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	saved, err := New(transaction).AppendAuthorizationAudit(t.Context(), event)
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	return saved
}

func verify(t *testing.T, database *sql.DB, expected *domain.AuditCheckpoint) (domain.AuditCheckpoint, error) {
	t.Helper()
	transaction, err := database.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	return New(transaction).Verify(t.Context(), expected)
}

func TestAuditChainConcurrentPoolsDeduplicateAndRetainOldEvents(t *testing.T) {
	database, dsn := databaseForTest(t)
	old := appendEvent(t, database, eventForTest("old-event"))
	first, err := verify(t, database, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	var workers sync.WaitGroup
	failures := make(chan error, 12)
	for index := range 12 {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			pool := database
			if index%2 == 0 {
				pool = second
			}
			transaction, err := pool.BeginTx(t.Context(), nil)
			if err != nil {
				failures <- err
				return
			}
			defer transaction.Rollback()
			_, err = New(transaction).AppendAuthorizationAudit(t.Context(), eventForTest(fmt.Sprintf("event-%02d", index/2)))
			if err == nil {
				err = transaction.Commit()
			}
			failures <- err
		}(index)
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	changed := eventForTest("old-event")
	changed.Reason = "replacement"
	if saved := appendEvent(t, database, changed); saved.Reason != old.Reason {
		t.Fatal("duplicate ID rewrote the old audit event")
	}
	tip, err := verify(t, database, &first)
	if err != nil || tip.Sequence != 7 {
		t.Fatalf("verified tip=%#v, error=%v", tip, err)
	}
	transaction, err := database.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	records, err := New(transaction).ListAuthorizationAudit(t.Context(), "resource")
	if err != nil || len(records) != 7 {
		t.Fatalf("retained records=%d, error=%v", len(records), err)
	}
	foundOld := false
	for _, event := range records {
		if event.ID == old.ID {
			foundOld = event.CreatedAt.Equal(old.CreatedAt) && event.Reason == old.Reason
		}
	}
	if !foundOld {
		t.Fatal("retention lost or changed the old event")
	}
}

func TestAuditRowsRefuseUpdateDeleteAndTruncate(t *testing.T) {
	database, _ := databaseForTest(t)
	appendEvent(t, database, eventForTest("retained"))
	for _, statement := range []string{
		"UPDATE conduit_authorization_audit_events SET resource_rid = 'other'",
		"DELETE FROM conduit_authorization_audit_events",
		"TRUNCATE conduit_authorization_audit_events",
	} {
		if _, err := database.ExecContext(t.Context(), statement); err == nil {
			t.Fatalf("append-only protection accepted %s", statement)
		}
	}
	tip, err := verify(t, database, nil)
	if err != nil || tip.Sequence != 1 {
		t.Fatalf("protected chain=%#v, %v", tip, err)
	}
}

func TestAuditVerificationDetectsEditsGapsAndUnanchoredRollback(t *testing.T) {
	for _, mutation := range []string{"payload", "middle", "tail", "coherent-rollback", "coherent-rewrite"} {
		t.Run(mutation, func(t *testing.T) {
			database, _ := databaseForTest(t)
			appendEvent(t, database, eventForTest("first"))
			prefix, err := verify(t, database, nil)
			if err != nil {
				t.Fatal(err)
			}
			appendEvent(t, database, eventForTest("second"))
			trusted, err := verify(t, database, &prefix)
			if err != nil {
				t.Fatal(err)
			}
			// A database owner can bypass triggers; only the independently retained
			// checkpoint can expose a coherently rewritten chain or removed suffix.
			if _, err := database.ExecContext(t.Context(), "ALTER TABLE conduit_authorization_audit_events DISABLE TRIGGER USER"); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "payload":
				payload, err := json.Marshal(eventForTest("forged"))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := database.ExecContext(t.Context(), "UPDATE conduit_authorization_audit_events SET payload=$1 WHERE sequence=1", payload); err != nil {
					t.Fatal(err)
				}
			case "middle":
				if _, err := database.ExecContext(t.Context(), "DELETE FROM conduit_authorization_audit_events WHERE sequence=1"); err != nil {
					t.Fatal(err)
				}
			case "tail":
				if _, err := database.ExecContext(t.Context(), "DELETE FROM conduit_authorization_audit_events WHERE sequence=2"); err != nil {
					t.Fatal(err)
				}
			case "coherent-rollback":
				if _, err := database.ExecContext(t.Context(), "DELETE FROM conduit_authorization_audit_events WHERE sequence=2; UPDATE conduit_authorization_audit_head SET sequence=1, digest=(SELECT digest FROM conduit_authorization_audit_events WHERE sequence=1)"); err != nil {
					t.Fatal(err)
				}
			case "coherent-rewrite":
				changed := eventForTest("first")
				changed.Reason = "forged"
				one, err := domain.NewAuditChainRecord(domain.EmptyAuditCheckpoint(), changed, domain.AuditOriginLive)
				if err != nil {
					t.Fatal(err)
				}
				checkpoint := domain.AuditCheckpoint{Version: domain.AuditChainVersion, Sequence: one.Sequence, Digest: one.Digest}
				two, err := domain.NewAuditChainRecord(checkpoint, eventForTest("second"), domain.AuditOriginLive)
				if err != nil {
					t.Fatal(err)
				}
				for _, record := range []domain.AuditChainRecord{one, two} {
					if _, err := database.ExecContext(t.Context(), "UPDATE conduit_authorization_audit_events SET previous_digest=$1,payload=$2,digest=$3 WHERE sequence=$4", record.PreviousDigest, record.Payload, record.Digest, record.Sequence); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := database.ExecContext(t.Context(), "UPDATE conduit_authorization_audit_head SET digest=$1", two.Digest); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := verify(t, database, &trusted); err == nil {
				t.Fatal("tampered chain passed independent checkpoint verification")
			}
			_, internalErr := verify(t, database, nil)
			coherent := mutation == "coherent-rollback" || mutation == "coherent-rewrite"
			if coherent && internalErr != nil {
				t.Fatalf("fixture did not demonstrate a coherent rewrite: %v", internalErr)
			}
			if !coherent && internalErr == nil {
				t.Fatal("internally inconsistent chain verified")
			}
		})
	}
}

func TestAuditNestedSavepointFailureDoesNotLeakIntoOuterCommit(t *testing.T) {
	database, _ := databaseForTest(t)
	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	store := New(transaction)
	if _, err := store.AppendAuthorizationAudit(t.Context(), eventForTest("before")); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("scope failed")
	err = store.Savepoint(t.Context(), func() error {
		if _, err := store.AppendAuthorizationAudit(t.Context(), eventForTest("discarded")); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if _, err := store.AppendAuthorizationAudit(t.Context(), eventForTest("after")); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	tip, err := verify(t, database, nil)
	if err != nil || tip.Sequence != 2 {
		t.Fatalf("nested rollback tip=%#v, error=%v", tip, err)
	}
	read, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback()
	if _, err := New(read).Verify(t.Context(), nil); err == nil {
		t.Fatal("verification accepted changing statement snapshots")
	}
	events, err := New(read).ListAuthorizationAudit(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.ID == "discarded" {
			t.Fatal("failed savepoint leaked an event")
		}
	}
}

func TestAuditWholeTransactionRollbackRetainsTheGenesis(t *testing.T) {
	database, _ := databaseForTest(t)
	transaction, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(transaction).AppendAuthorizationAudit(t.Context(), eventForTest("rolled-back")); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	tip, err := verify(t, database, nil)
	if err != nil || tip != domain.EmptyAuditCheckpoint() {
		t.Fatalf("rolled-back chain=%#v, error=%v", tip, err)
	}
}

func TestAuditAppendRefusesAnInconsistentHeadEvenOnDuplicate(t *testing.T) {
	database, _ := databaseForTest(t)
	event := eventForTest("existing")
	appendEvent(t, database, event)
	if _, err := database.ExecContext(t.Context(), "UPDATE conduit_authorization_audit_head SET sequence=0,digest=$1", domain.EmptyAuditCheckpoint().Digest); err != nil {
		t.Fatal(err)
	}
	transaction, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	if _, err := New(transaction).AppendAuthorizationAudit(t.Context(), event); err == nil {
		t.Fatal("duplicate append accepted a head reset behind its rows")
	}
}

func TestAuditVerificationUsesOneSnapshotWhileAnotherPoolAppends(t *testing.T) {
	database, dsn := databaseForTest(t)
	appendEvent(t, database, eventForTest("first"))
	transaction, err := database.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	var sequence uint64
	if err := transaction.QueryRowContext(t.Context(), "SELECT sequence FROM conduit_authorization_audit_head WHERE singleton=TRUE").Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	second, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	appendEvent(t, second, eventForTest("second"))
	tip, err := New(transaction).Verify(t.Context(), nil)
	if err != nil || tip.Sequence != sequence || tip.Sequence != 1 {
		t.Fatalf("verification mixed database snapshots: %#v, %v", tip, err)
	}
	current, err := verify(t, database, nil)
	if err != nil || current.Sequence != 2 {
		t.Fatalf("concurrent append was not durable: %#v, %v", current, err)
	}
}
