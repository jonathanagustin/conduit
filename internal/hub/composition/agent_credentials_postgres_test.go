package composition

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/revocationhttp"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/revocationpostgres"
	identity "github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/pgtest"
)

// Drive the mounted removal route and the real Agent guard with two database
// pools. A pending or canceled removal must never admit the revoked Agent.
func TestPostgresAgentCredentialRemovalAndAuditShareTheVisibilityBoundary(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed", true: "canceled after delete"}[canceled], func(t *testing.T) {
			dsn := os.Getenv(pgtest.DataSourceNameEnvironment)
			if dsn == "" {
				t.Skip(pgtest.DataSourceNameEnvironment + " is not set")
			}
			shared, err := sql.Open("pgx", dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer shared.Close()
			fresh, drop, err := pgtest.Fresh(t.Context(), shared, dsn, t.Name())
			if err != nil {
				t.Fatal(err)
			}
			defer drop()
			repository, err := controlplane.NewPostgresRepository(t.Context(), fresh)
			if err != nil {
				t.Fatal(err)
			}
			defer repository.Close()
			store, err := revocationpostgres.New(t.Context(), fresh)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			now := time.Now().UTC().Truncate(time.Second)
			held, err := identity.NewAgentRevocation(identity.AgentRevocation{
				Scope: identity.RevocationEnvironment, Environment: revocationEnvironment, Reason: "incident",
			}, "root", now)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.PutRevocation(t.Context(), held); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			options := controlplane.HandlerOptions{
				AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"},
			}
			mounted, err := mountAgentCredentials(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusNoContent)
			}), repository, options, store, func() time.Time {
				close(entered) // Called while building the audit, after the uncommitted DELETE.
				select {
				case <-release:
				case <-ctx.Done():
				}
				return now
			})
			if err != nil {
				t.Fatal(err)
			}
			hub := revocationHub{
				handler: guardAgentCredentials(mounted, options, store, time.Now, slog.Default()),
			}
			certificate := agentCertificate(t, revocationEnvironment, "primary", 1, now.Add(-time.Hour))
			requireRevoked(t, "before removal", hub.poll(certificate))
			request := httptest.NewRequest(http.MethodDelete,
				"/v1/environments/"+revocationEnvironment+"/agent-revocations/"+held.ID, nil).WithContext(ctx)
			request.Header.Set("X-Platform-Subject", "root")
			response := httptest.NewRecorder()
			finished := make(chan struct{})
			go func() { hub.handler.ServeHTTP(response, request); close(finished) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("removal did not reach audit boundary")
			}
			requireRevoked(t, "while removal is pending", hub.poll(certificate))
			if canceled {
				cancel()
			}
			close(release)
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Fatal("removal did not finish")
			}
			if canceled {
				if response.Code != http.StatusServiceUnavailable {
					t.Fatalf("canceled removal: %d %s", response.Code, response.Body.String())
				}
				requireRevoked(t, "after canceled removal", hub.poll(certificate))
			} else {
				if response.Code != http.StatusOK {
					t.Fatalf("committed removal: %d %s", response.Code, response.Body.String())
				}
				requireAdmitted(t, "after committed removal", hub.poll(certificate))
			}
			events, err := repository.ListAuthorizationAudit(t.Context(), "environment/"+revocationEnvironment)
			if err != nil {
				t.Fatal(err)
			}
			removals := 0
			for _, event := range events {
				if event.Action == revocationhttp.ActionRemove {
					removals++
				}
			}
			if canceled && removals != 0 || !canceled && removals != 1 {
				t.Fatalf("canceled=%v: %d committed removal audits", canceled, removals)
			}
		})
	}
}
