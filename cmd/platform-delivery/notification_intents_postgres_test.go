package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

// The shared integration server is never reset. All stores opened by the
// actual executable assembly share one uniquely owned schema instead.
func notificationRuntimeSchema(t *testing.T) string {
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
	schema := "conduit_notification_runtime_" + hex.EncodeToString(suffix[:])
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

func TestPostgresReconciliationBridgeStartsHubAndAdmitsInAppIntentsWithoutTransport(t *testing.T) {
	dsn := notificationRuntimeSchema(t)
	base, err := controlplane.NewPostgresRepository(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	configuration := map[string]string{
		"REPOSITORY_KIND": "postgres", "POSTGRES_DSN_FILE": writeFile(t, "runtime-dsn", []byte(dsn+"\n")),
		"ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS": "true",
	}
	repository, closeStores, supervisor, _, component, err := configureReconciliation(t.Context(), func(name string) string { return configuration[name] }, base)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStores()
	if repository == base || component.Intents == nil || component.Deliveries != nil {
		t.Fatal("executable assembly lost its bridge or tied in-app intent admission to an external transport")
	}
	if _, configured := repository.TransactionAnnouncements(); !configured {
		t.Fatal("source intent factory was not enabled before reconciliation Prime")
	}
	if err := supervisor.Prime(t.Context()); err != nil {
		t.Fatal(err)
	}
	handler, err := composition.NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}, composition.Options{NotificationStore: component.Store, NotificationContacts: component.Contacts})
	if err != nil {
		t.Fatalf("configureReconciliation's returned repository could not assemble the production Hub: %v", err)
	}
	if _, err := controlplane.NewGovernanceService(repository).PutTeam(t.Context(), controlplane.Team{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}}); err != nil {
		t.Fatal(err)
	}
	owner := domain.Owner{Kind: domain.PrincipalUser, ID: "local-developer"}
	if err := component.Store.SavePreference(t.Context(), domain.Preference{ID: "source-consent", Owner: owner, ResourceRID: "environment/dev", Channel: domain.ChannelInApp, AllFutureTypes: true}); err != nil {
		t.Fatal(err)
	}
	call := httptest.NewRequest(http.MethodPost, "/v1/resource-grants", strings.NewReader(`{"resourceRid":"environment/dev","team":"reviewers","roles":["environment:viewer"]}`))
	call.Header.Set("X-Platform-Subject", "local-developer")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, call)
	if response.Code != http.StatusOK {
		t.Fatalf("source mutation failed through the runtime bridge: %d %s", response.Code, response.Body)
	}
	if events, err := component.Store.EventsForResource(t.Context(), "environment/dev", 10); err != nil || len(events) != 0 {
		t.Fatalf("a default live callback raced the committed intent: %#v %v", events, err)
	}
	if err := component.Intents.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	entries, err := component.Store.Inbox(t.Context(), domain.Recipient{Owner: owner, Channel: domain.ChannelInApp}, 10)
	if err != nil || len(entries) != 1 || entries[0].Event.Type != domain.EventRoleChange {
		t.Fatalf("transport-free runtime did not admit its committed source intent: %#v %v", entries, err)
	}
}
