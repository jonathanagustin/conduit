package composition

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	apolloapplication "github.com/jonathanagustin/conduit/internal/apollo/application"
	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	notificationpostgres "github.com/jonathanagustin/conduit/internal/notifications/adapters/postgres"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

// These are emit-site tests, rather than tests that call the notification port
// themselves: each event starts at an operator or Agent route on the assembled
// Hub. Both the source record and the subscriber's inbox must survive closing
// and reopening their independent PostgreSQL adapters.
func TestPostgresAnnouncementsOutliveTheHubThatRaisedThem(t *testing.T) {
	t.Run("pending approval", func(t *testing.T) {
		fixture := newPostgresAnnouncementHub(t, apolloapplication.EnvironmentRID("dev-use2"))
		governance := controlplane.NewGovernanceService(fixture.repository)
		if _, err := governance.PutPolicy(t.Context(), controlplane.ApprovalPolicy{
			ID: "review", DisplayName: "Review", Enabled: true,
			Expression: `require_team("reviewers", "review")`,
		}); err != nil {
			t.Fatal(err)
		}
		var plan controlplane.PlanRecord
		fixture.post(t, "/v1/plans", reviewedState("durable-approval"), http.StatusCreated, &plan)
		if plan.Status != controlplane.PlanAwaitingApproval || plan.ChangeRequestID == "" {
			t.Fatalf("proposal did not wait for review: %#v", plan)
		}
		// A repeated proposal must neither lose nor duplicate the durable event.
		fixture.post(t, "/v1/plans", reviewedState("durable-approval"), http.StatusCreated, &plan)
		fixture.reopen(t)
		stored, err := fixture.repository.GetChangeRequest(t.Context(), plan.ChangeRequestID)
		if err != nil || stored.Status != controlplane.ChangePendingApprovals {
			t.Fatalf("pending request after reopen = %#v, %v", stored, err)
		}
		fixture.assertEvent(t, domain.EventChangeRequestPendingApproval, "waiting for approval")
	})

	t.Run("failed execution", func(t *testing.T) {
		fixture := newPostgresAnnouncementHub(t, apolloapplication.EnvironmentRID("dev-use2"))
		lifecycle := controlplane.NewLifecycle(fixture.repository)
		running, err := lifecycle.CreatePlan(t.Context(), reviewedState("durable-failure"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = lifecycle.ApprovePlan(t.Context(), running.ID, "reviewer", "approved"); err != nil {
			t.Fatal(err)
		}
		if running, err = lifecycle.LeaseNextPlan(t.Context(), "dev-use2", "agent", time.Minute); err != nil {
			t.Fatal(err)
		}
		if running, err = lifecycle.StartPlan(t.Context(), running.ID, running.Lease.Token); err != nil {
			t.Fatal(err)
		}
		step := running.Plan.Steps[0]
		now := time.Now().UTC()
		body := map[string]any{
			"status": controlplane.PlanFailed, "failure": "chart rollout failed",
			"failedInstallationId": step.InstallationID,
			"execution": controlplane.PlanExecution{
				Attempt: running.Lease.Attempt, StartedAt: now, FinishedAt: now,
				Tasks: []controlplane.PlanExecutionTask{{
					Sequence: 1, Phase: controlplane.ExecutionPhaseApply, Status: controlplane.ExecutionTaskFailed,
					InstallationID: step.InstallationID, Namespace: step.Namespace,
					StartedAt: now, FinishedAt: now, Failure: "chart rollout failed",
				}},
			},
			"entities": []controlplane.ReportedEntity{{
				InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
				ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
				Health: controlplane.HealthUnhealthy, RolloutStatus: controlplane.RolloutBlocked,
				ConfigStatus: controlplane.ConfigApplied, Lifecycle: controlplane.LifecycleInstalled,
			}},
		}
		finish := func(token string) *httptest.ResponseRecorder {
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/v1/plans/"+running.ID+"/finish", strings.NewReader(string(encoded)))
			request.Header.Set("X-Platform-Environment-ID", "dev-use2")
			request.Header.Set("X-Platform-Agent-ID", "agent")
			request.Header.Set("X-Platform-Lease-Token", token)
			response := httptest.NewRecorder()
			fixture.handler.ServeHTTP(response, request)
			return response
		}
		if response := finish("not-the-lease-token"); response.Code == http.StatusOK {
			t.Fatalf("refused report was accepted: %s", response.Body)
		}
		fixture.assertNoEvents(t)
		if response := finish(running.Lease.Token); response.Code != http.StatusOK {
			t.Fatalf("finish = %d %s", response.Code, response.Body)
		}
		fixture.reopen(t)
		stored, err := fixture.repository.GetPlan(t.Context(), running.ID)
		if err != nil || stored.Status != controlplane.PlanFailed || stored.Execution == nil || stored.FailedInstallation != step.InstallationID {
			t.Fatalf("failed Plan after reopen = %#v, %v", stored, err)
		}
		fixture.assertEvent(t, domain.EventPlanFailures, "chart rollout failed")
	})

	t.Run("aborted promotion", func(t *testing.T) {
		const product = "app.example:example-api"
		fixture := newPostgresAnnouncementHub(t, "product/"+product)
		release := reviewedState("durable-promotion").Releases[0]
		release.Channels = []string{"DEV"}
		evaluation := controlplane.PromotionEvaluationRequest{
			Release: release, FromChannel: "DEV", ToChannel: "STABLE",
			Policy: controlplane.PromotionPolicy{MinimumHealthyEntities: 1},
		}
		var running controlplane.PromotionRecord
		fixture.post(t, "/v1/promotions/evaluations", evaluation, http.StatusCreated, &running)
		if running.Lifecycle != catalog.PromotionRunning {
			t.Fatalf("fixture promotion is not waiting for evidence: %#v", running)
		}
		fixture.assertNoEvents(t)
		evaluation.PreviousEvaluationID = running.ID
		evaluation.Termination = catalog.PromotionAborted
		evaluation.TerminationReason = "canary error rate doubled"
		var aborted controlplane.PromotionRecord
		fixture.post(t, "/v1/promotions/evaluations", evaluation, http.StatusCreated, &aborted)
		fixture.reopen(t)
		promotions, err := fixture.repository.ListPromotions(t.Context(), product)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, stored := range promotions {
			if stored.ID == aborted.ID && stored.Lifecycle == catalog.PromotionAborted && stored.LifecycleReason == evaluation.TerminationReason {
				found = true
			}
		}
		if !found {
			t.Fatalf("aborted promotion did not persist: %#v", promotions)
		}
		fixture.assertEvent(t, domain.EventPromotionFailed, evaluation.TerminationReason)
	})

	t.Run("grant change", func(t *testing.T) {
		fixture := newPostgresAnnouncementHub(t, "environment/dev-use2")
		var grant controlplane.ResourceGrant
		fixture.post(t, "/v1/resource-grants", controlplane.ResourceGrant{
			ResourceRID: fixture.resource, Team: "reviewers", Roles: []string{"environment:viewer"},
		}, http.StatusOK, &grant)
		fixture.reopen(t)
		grants, err := fixture.repository.ListResourceGrants(t.Context(), fixture.resource)
		if err != nil || len(grants) != 1 || grants[0].ID != grant.ID || grants[0].Roles[0] != "environment:viewer" {
			t.Fatalf("grant after reopen = %#v, %v", grants, err)
		}
		fixture.assertEvent(t, domain.EventRoleChange, "environment:viewer")
	})
}

type postgresAnnouncementHub struct {
	dsn        string
	resource   string
	repository *controlplane.PostgresRepository
	store      *notificationpostgres.Store
	handler    http.Handler
}

func newPostgresAnnouncementHub(t *testing.T, resource string) *postgresAnnouncementHub {
	t.Helper()
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is required for the PostgreSQL integration gate")
	}
	// Keep this test independent of every other integration package's resets.
	// The identifier is generated here, never supplied by a request or a DSN.
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("conduit_announcements_%d", time.Now().UnixNano())
	if _, err := database.ExecContext(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := database.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
		_ = database.Close()
	})
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.RuntimeParams["search_path"] = schema
	// ConnConfig.ConnString preserves its original input, so register the
	// modified config rather than accidentally reconnecting to public.
	isolatedDSN := stdlib.RegisterConnConfig(config)
	t.Cleanup(func() { stdlib.UnregisterConnConfig(isolatedDSN) })
	fixture := &postgresAnnouncementHub{dsn: isolatedDSN, resource: resource}
	t.Cleanup(func() { fixture.close(t) })
	fixture.open(t)
	governance := controlplane.NewGovernanceService(fixture.repository)
	if _, err := governance.PutTeam(t.Context(), controlplane.Team{
		Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, preference := range []domain.Preference{
		{ID: "npref_durable", Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "local-developer"}, ResourceRID: resource},
		{ID: "npref_unrelated", Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "unrelated"}, ResourceRID: "environment/another-environment"},
	} {
		preference.Channel = domain.ChannelInApp
		preference.AllFutureTypes = true
		if err := fixture.store.SavePreference(t.Context(), preference); err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

func (fixture *postgresAnnouncementHub) open(t *testing.T) {
	t.Helper()
	var err error
	fixture.repository, err = controlplane.NewPostgresRepository(t.Context(), fixture.dsn)
	if err != nil {
		t.Fatal(err)
	}
	fixture.store, err = notificationpostgres.New(t.Context(), fixture.dsn)
	if err != nil {
		t.Fatal(err)
	}
	fixture.handler, err = NewHub(fixture.repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, AllowInsecureAgentIdentityHeaders: true,
	}, Options{NotificationStore: fixture.store})
	if err != nil {
		t.Fatal(err)
	}
}

func (fixture *postgresAnnouncementHub) close(t *testing.T) {
	t.Helper()
	if err := fixture.store.Close(); err != nil {
		t.Error(err)
	}
	if err := fixture.repository.Close(); err != nil {
		t.Error(err)
	}
}

func (fixture *postgresAnnouncementHub) reopen(t *testing.T) {
	t.Helper()
	fixture.close(t)
	fixture.open(t)
}

func (fixture *postgresAnnouncementHub) post(t *testing.T, path string, body any, status int, result any) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, fixture.handler, http.MethodPost, path, "local-developer", string(encoded))
	if response.Code != status {
		t.Fatalf("POST %s = %d %s", path, response.Code, response.Body)
	}
	if err := json.Unmarshal(response.Body.Bytes(), result); err != nil {
		t.Fatal(err)
	}
}

func (fixture *postgresAnnouncementHub) assertNoEvents(t *testing.T) {
	t.Helper()
	events, err := fixture.store.EventsForResource(t.Context(), fixture.resource, 10)
	if err != nil || len(events) != 0 {
		t.Fatalf("a non-event was announced: %#v, %v", events, err)
	}
}

func (fixture *postgresAnnouncementHub) assertEvent(t *testing.T, eventType domain.EventType, summary string) {
	t.Helper()
	events, err := fixture.store.EventsForResource(t.Context(), fixture.resource, 10)
	if err != nil || len(events) != 1 || events[0].Type != eventType || !strings.Contains(events[0].Summary, summary) {
		t.Fatalf("durable %s event = %#v, %v", eventType, events, err)
	}
	entries, err := fixture.store.Inbox(t.Context(), domain.Recipient{
		Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "local-developer"}, Channel: domain.ChannelInApp,
	}, 10)
	if err != nil || len(entries) != 1 || entries[0].Event.ID != events[0].ID || entries[0].PreferenceID != "npref_durable" {
		t.Fatalf("subscriber's durable inbox = %#v, %v", entries, err)
	}
	unrelated, err := fixture.store.Inbox(t.Context(), domain.Recipient{
		Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "unrelated"}, Channel: domain.ChannelInApp,
	}, 10)
	if err != nil || len(unrelated) != 0 {
		t.Fatalf("subscriber to another resource received this event: %#v, %v", unrelated, err)
	}
}
