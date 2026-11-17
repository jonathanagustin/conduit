package controlplane

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	governancenotifications "github.com/jonathanagustin/conduit/internal/governance/adapters/notifications"
	notificationpostgres "github.com/jonathanagustin/conduit/internal/notifications/adapters/postgres"
)

type nestedCancelRecallObserver struct{}

func (nestedCancelRecallObserver) RecallsInForce(context.Context, string, []catalog.Recall) error {
	return nil
}

func nestedCancelSchema(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is required")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	schema := "conduit_nested_cancel_" + hex.EncodeToString(suffix[:])
	if _, err := database.ExecContext(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := database.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.RuntimeParams["search_path"] = schema
	isolated := stdlib.RegisterConnConfig(config)
	t.Cleanup(func() { stdlib.UnregisterConnConfig(isolated) })
	return isolated
}

func TestPostgresNestedChildCancellationDoesNotCommitSourceWithoutIntent(t *testing.T) {
	dsn := nestedCancelSchema(t)
	repository, err := NewPostgresRepository(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	store, err := notificationpostgres.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, _, author, _ := announcingGovernance(t, repository)
	var sourceTransaction *sql.Tx
	if err := repository.ConfigureNotificationIntents(func(transaction *sql.Tx) (Announcements, error) {
		sourceTransaction = transaction
		recorder, err := notificationpostgres.NewIntentRecorder(transaction)
		if err != nil {
			return Announcements{}, err
		}
		openings, err := governancenotifications.NewAnnouncements(recorder, time.Now)
		if err != nil {
			return Announcements{}, err
		}
		announcements := (&recordedAnnouncements{}).all()
		announcements.ChangeRequestOpenings = openings
		announcements.RecallOpenings = nestedCancelRecallObserver{}
		return announcements, nil
	}); err != nil {
		t.Fatal(err)
	}
	var requestID string
	err = repository.Transaction(t.Context(), func(outer Repository) error {
		child, cancel := context.WithCancel(t.Context())
		defer cancel()
		innerErr := outer.(TransactionalRepository).Transaction(child, func(inner Repository) error {
			_, request, err := NewGovernanceService(inner).ProposePlan(child, foundation(), author, "Nested review", "source intent must roll back with its source")
			if err != nil {
				return err
			}
			requestID = request.ID
			var intents int
			if err := sourceTransaction.QueryRowContext(child, "SELECT count(*) FROM conduit_notification_intents").Scan(&intents); err != nil {
				return err
			}
			if intents != 1 {
				return fmt.Errorf("fixture source did not enqueue inside its SQL savepoint: %d", intents)
			}
			// The callback succeeded, but its context is canceled immediately
			// before RELEASE SAVEPOINT. The enclosing transaction stays usable.
			cancel()
			return nil
		})
		if !errors.Is(innerErr, context.Canceled) {
			return fmt.Errorf("nested child cancellation was not reported: %w", innerErr)
		}
		_, err := NewGovernanceService(outer).PutTeam(t.Context(), Team{Name: "survived", DisplayName: "Survived inner rollback", AdminUsers: []string{"owner"}, MemberUsers: []string{"owner"}})
		return err
	})
	if err != nil {
		t.Fatalf("caught child cancellation poisoned the outer commit: %v", err)
	}
	if _, err := repository.GetTeam(t.Context(), "survived"); err != nil {
		t.Fatalf("uncanceled outer work did not commit: %v", err)
	}
	stats, err := store.Intents(t.Context())
	if err != nil || stats.Pending != 0 || stats.Completed != 0 || stats.Exhausted != 0 {
		t.Fatalf("canceled savepoint retained SQL intent: %#v %v", stats, err)
	}
	audits, err := repository.ListAuthorizationAudit(t.Context(), "")
	if err != nil || len(audits) != 0 {
		t.Fatalf("canceled savepoint retained SQL audit: %#v %v", audits, err)
	}
	request, requestErr := repository.GetChangeRequest(t.Context(), requestID)
	plans, plansErr := repository.ListPlans(t.Context(), "")
	if !errors.Is(requestErr, ErrNotFound) || plansErr != nil || len(plans) != 0 {
		t.Fatalf("canceled savepoint committed source metadata after its SQL intent rolled back: request=%q status=%q error=%v plans=%d error=%v intents=%#v", request.ID, request.Status, requestErr, len(plans), plansErr, stats)
	}
}
