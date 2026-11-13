package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/governance/adapters/auditpostgres"
	governancedomain "github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/pgtest"
)

func auditRepositoryForTest(t *testing.T) (*PostgresRepository, string) {
	t.Helper()
	sharedDSN := os.Getenv(pgtest.DataSourceNameEnvironment)
	if sharedDSN == "" {
		t.Skip(pgtest.DataSourceNameEnvironment + " is required")
	}
	shared, err := sql.Open("pgx", sharedDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shared.Close() })
	dsn, drop, err := pgtest.Fresh(t.Context(), shared, sharedDSN, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drop)
	repository, err := NewPostgresRepository(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	return repository, dsn
}

func repositoryAuditEvent(id string) AuthorizationAuditEvent {
	event := governancedomain.NewAuthorizationCheckEvent(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), "operator", "read", "resource", governancedomain.DecisionAllow, "original", nil)
	event.ID = id
	return event
}

func auditTip(t *testing.T, repository *PostgresRepository) governancedomain.AuditCheckpoint {
	t.Helper()
	transaction, err := repository.database.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	tip, err := auditpostgres.New(transaction).Verify(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return tip
}

func TestPostgresAuthorizationAuditAndMetadataRollBackTogether(t *testing.T) {
	repository, _ := auditRepositoryForTest(t)
	failure := errors.New("decision failed")
	err := repository.Transaction(t.Context(), func(view Repository) error {
		if _, err := view.PutTeam(t.Context(), Team{Name: "rolled-back", Revision: 1}); err != nil {
			return err
		}
		if _, err := view.AppendAuthorizationAudit(t.Context(), repositoryAuditEvent("rolled-back")); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if _, err := repository.GetTeam(t.Context(), "rolled-back"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("metadata escaped rollback: %v", err)
	}
	if tip := auditTip(t, repository); tip.Sequence != 0 {
		t.Fatal("audit escaped rollback")
	}
	// A database write failure after the successful audit append must also roll
	// back the audit row and head, not merely failures returned by the callback.
	if _, err := repository.database.ExecContext(t.Context(), `CREATE FUNCTION reject_audit_metadata() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected metadata write failure'; END; $$;
 CREATE TRIGGER reject_audit_metadata BEFORE UPDATE ON conduit_repository_state FOR EACH ROW EXECUTE FUNCTION reject_audit_metadata()`); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AppendAuthorizationAudit(t.Context(), repositoryAuditEvent("write-failed")); err == nil {
		t.Fatal("metadata write unexpectedly succeeded")
	}
	if tip := auditTip(t, repository); tip.Sequence != 0 {
		t.Fatal("audit committed without its metadata write")
	}
}

func TestPostgresCaughtNestedTransactionFailureDiscardsAuditAndMetadata(t *testing.T) {
	repository, _ := auditRepositoryForTest(t)
	failure := errors.New("nested decision failed")
	err := repository.Transaction(t.Context(), func(view Repository) error {
		if _, err := view.PutTeam(t.Context(), Team{Name: "outer", Revision: 1}); err != nil {
			return err
		}
		nested, ok := view.(TransactionalRepository)
		if !ok {
			return errors.New("transaction view lost nested transactions")
		}
		err := nested.Transaction(t.Context(), func(child Repository) error {
			if _, err := child.PutTeam(t.Context(), Team{Name: "inner", Revision: 1}); err != nil {
				return err
			}
			if _, err := child.AppendAuthorizationAudit(t.Context(), repositoryAuditEvent("discarded")); err != nil {
				return err
			}
			return failure
		})
		if !errors.Is(err, failure) {
			return errors.New("nested failure was not returned")
		}
		func() {
			defer func() { _ = recover() }()
			_ = nested.Transaction(t.Context(), func(child Repository) error {
				if _, err := child.AppendAuthorizationAudit(t.Context(), repositoryAuditEvent("panicked")); err != nil {
					t.Error(err)
					return err
				}
				panic("nested decision interrupted")
			})
		}()
		_, err = view.AppendAuthorizationAudit(t.Context(), repositoryAuditEvent("committed"))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetTeam(t.Context(), "outer"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetTeam(t.Context(), "inner"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inner metadata escaped rollback: %v", err)
	}
	events, err := repository.ListAuthorizationAudit(t.Context(), "")
	if err != nil || len(events) != 1 || events[0].ID != "committed" {
		t.Fatalf("nested audit rollback failed: %#v, %v", events, err)
	}
	if tip := auditTip(t, repository); tip.Sequence != 1 {
		t.Fatal("failed nested scope consumed an audit sequence")
	}
	var duplicated bool
	if err := repository.database.QueryRowContext(t.Context(), "SELECT state ? 'authorizationAudit' FROM conduit_repository_state WHERE singleton=TRUE").Scan(&duplicated); err != nil {
		t.Fatal(err)
	}
	if duplicated {
		t.Fatal("audit history was serialized back into the metadata snapshot")
	}
}

func TestPostgresLegacyAuthorizationAuditAdoptionPreservesTableAuthority(t *testing.T) {
	repository, dsn := auditRepositoryForTest(t)
	original := repositoryAuditEvent("already-chained")
	if _, err := repository.AppendAuthorizationAudit(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	before := auditTip(t, repository)
	replaced := original
	replaced.Reason = "legacy replacement"
	legacy := repositoryAuditEvent("legacy-only")
	encoded, err := json.Marshal(map[string]AuthorizationAuditEvent{replaced.ID: replaced, legacy.ID: legacy})
	if err != nil {
		t.Fatal(err)
	}
	writeLegacy := func() {
		t.Helper()
		if _, err := repository.database.ExecContext(t.Context(), "UPDATE conduit_repository_state SET state=jsonb_set(state,'{authorizationAudit}',$1::jsonb)", encoded); err != nil {
			t.Fatal(err)
		}
	}
	writeLegacy()
	events, err := repository.ListAuthorizationAudit(t.Context(), "")
	if err != nil || len(events) != 2 {
		t.Fatalf("legacy rollout read=%#v, %v", events, err)
	}
	for _, event := range events {
		if event.ID == original.ID && event.Reason != original.Reason {
			t.Fatal("legacy snapshot replaced chained audit authority")
		}
	}
	second, err := NewPostgresRepository(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if tip := auditTip(t, second); tip.Sequence != 2 {
		t.Fatal("legacy import lost or duplicated an event")
	}
	var origin string
	if err := repository.database.QueryRowContext(t.Context(), "SELECT origin FROM conduit_authorization_audit_events WHERE event_id=$1", legacy.ID).Scan(&origin); err != nil {
		t.Fatal(err)
	}
	if origin != governancedomain.AuditOriginLegacy {
		t.Fatal("legacy event was attributed as a live append")
	}
	// An old writer may repeat its snapshot; a normal new metadata write adopts
	// it idempotently rather than erasing history or reserializing every event.
	writeLegacy()
	if _, err := second.PutTeam(t.Context(), Team{Name: "after-adoption", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	transaction, err := repository.database.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	tip, err := auditpostgres.New(transaction).Verify(t.Context(), &before)
	if err != nil || tip.Sequence != 2 {
		t.Fatalf("legacy adoption changed the trusted prefix: %#v, %v", tip, err)
	}
	var duplicated bool
	if err := transaction.QueryRowContext(context.Background(), "SELECT state ? 'authorizationAudit' FROM conduit_repository_state WHERE singleton=TRUE").Scan(&duplicated); err != nil {
		t.Fatal(err)
	}
	if duplicated {
		t.Fatal("legacy audit history remains in the metadata snapshot")
	}
}
