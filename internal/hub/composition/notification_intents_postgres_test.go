package composition

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

func TestPostgresAnnouncementsCommitConsentBeforeCancelledRequestAndRestart(t *testing.T) {
	fixture := newPostgresAnnouncementHub(t, "environment/dev-use2")
	body, err := json.Marshal(controlplane.ResourceGrant{
		ResourceRID: fixture.resource, Team: "reviewers", Roles: []string{"environment:viewer"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	call := httptest.NewRequest(http.MethodPost, "/v1/resource-grants", strings.NewReader(string(body))).WithContext(ctx)
	call.Header.Set("X-Platform-Subject", "local-developer")
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, call)
	cancel()
	if response.Code != http.StatusOK {
		t.Fatalf("source mutation = %d %s", response.Code, response.Body)
	}
	stats, err := fixture.store.Intents(t.Context())
	if err != nil || stats.Pending != 1 || stats.Completed != 0 {
		t.Fatalf("committed source lost its pending intent: %#v %v", stats, err)
	}
	fixture.reopen(t)
	// Consent added after the source commit must not join the delayed event.
	if err := fixture.store.SavePreference(t.Context(), domain.Preference{
		ID: "npref_late_email", Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "local-developer"},
		ResourceRID: fixture.resource, Channel: domain.ChannelEmail, AllFutureTypes: true,
	}); err != nil {
		t.Fatal(err)
	}
	fixture.assertEvent(t, domain.EventRoleChange, "environment:viewer")
	email, err := fixture.store.Inbox(t.Context(), domain.Recipient{
		Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "local-developer"}, Channel: domain.ChannelEmail,
	}, 10)
	if err != nil || len(email) != 0 {
		t.Fatalf("restart added a later subscription to historical consent: %#v %v", email, err)
	}
	deliveries, err := fixture.store.Deliveries(t.Context())
	if err != nil || deliveries.Pending != 0 {
		t.Fatalf("an IN_APP-only intent queued an external delivery: %#v %v", deliveries, err)
	}
	stats, err = fixture.store.Intents(t.Context())
	if err != nil || stats.Pending != 0 || stats.Completed != 1 || stats.Exhausted != 0 {
		t.Fatalf("restarted worker did not acknowledge the exact intent: %#v %v", stats, err)
	}
}

func TestPostgresAnnouncementsSourceRollbackIncludesPrivateIntent(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprintf("caught inner rollback=%v", nested), func(t *testing.T) {
			fixture := newPostgresAnnouncementHub(t, "environment/dev-use2")
			governance := controlplane.NewGovernanceService(fixture.repository)
			if _, err := governance.PutPolicy(t.Context(), controlplane.ApprovalPolicy{
				ID: "review", DisplayName: "Review", Enabled: true, Expression: `require_team("reviewers", "review")`,
			}); err != nil {
				t.Fatal(err)
			}
			stopped := errors.New("source boundary rolls back after intent enqueue")
			var requestID string
			operation := func(repository controlplane.Repository) error {
				_, request, err := controlplane.NewGovernanceService(repository).ProposePlan(t.Context(),
					reviewedState("rollback-intent"), controlplane.Principal{Username: "local-developer"}, "Review", "Pending source")
				if err != nil {
					return err
				}
				requestID = request.ID
				return stopped
			}
			err := fixture.repository.Transaction(t.Context(), func(repository controlplane.Repository) error {
				if !nested {
					return operation(repository)
				}
				inner, ok := repository.(controlplane.TransactionalRepository)
				if !ok {
					return errors.New("source view lost its nested transaction")
				}
				if err := inner.Transaction(t.Context(), operation); !errors.Is(err, stopped) {
					return fmt.Errorf("nested rollback result: %w", err)
				}
				_, err := controlplane.NewGovernanceService(repository).PutTeam(t.Context(), controlplane.Team{
					Name: "survived", DisplayName: "Outer transaction survives", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"},
				})
				return err
			})
			if (!nested && !errors.Is(err, stopped)) || (nested && err != nil) || requestID == "" {
				t.Fatalf("source rollback = %v, request %q", err, requestID)
			}
			if _, err := fixture.repository.GetChangeRequest(t.Context(), requestID); !errors.Is(err, controlplane.ErrNotFound) {
				t.Fatalf("rolled-back source request survived: %v", err)
			}
			if nested {
				if _, err := fixture.repository.GetTeam(t.Context(), "survived"); err != nil {
					t.Fatalf("caught savepoint failure poisoned the outer transaction: %v", err)
				}
			}
			fixture.assertNoEvents(t)
		})
	}
}

func TestPostgresAnnouncementsIntentWriteFailureRollsBackSourceGrant(t *testing.T) {
	fixture := newPostgresAnnouncementHub(t, "environment/dev-use2")
	database, err := sql.Open("pgx", fixture.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.ExecContext(t.Context(), `
CREATE FUNCTION reject_test_notification_intent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'simulated intent write failure'; END $$;
CREATE TRIGGER reject_test_notification_intent BEFORE INSERT ON conduit_notification_intents
FOR EACH ROW EXECUTE FUNCTION reject_test_notification_intent();`); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	fixture.post(t, "/v1/resource-grants", controlplane.ResourceGrant{
		ResourceRID: fixture.resource, Team: "reviewers", Roles: []string{"environment:viewer"},
	}, http.StatusInternalServerError, &result)
	grants, err := fixture.repository.ListResourceGrants(t.Context(), fixture.resource)
	if err != nil || len(grants) != 0 {
		t.Fatalf("source grant committed without its notification intent: %#v %v", grants, err)
	}
	fixture.assertNoEvents(t)
}
