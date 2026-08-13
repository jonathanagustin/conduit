package controlplane

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOverviewProjectionIsBoundedAndUsesOneAuthorizedSnapshot(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	for index := 0; index < maximumOverviewItems+2; index++ {
		environment := fmt.Sprintf("environment-%02d", index)
		repository.plans["failed-"+environment] = PlanRecord{
			ID: "failed-" + environment, Plan: Plan{Environment: environment}, Status: PlanFailed,
			CreatedAt: now.Add(time.Duration(index) * time.Minute),
		}
		repository.changeRequests["change-"+environment] = ChangeRequest{
			ID: "change-" + environment, Environment: environment, Title: "Review " + environment,
			Status: ChangePendingApprovals,
		}
		repository.reports[environment] = EnvironmentReport{
			Environment: environment, ObservedAt: now.Add(-10 * time.Minute),
			Entities: []ReportedEntity{{InstallationID: "service", Health: "UNHEALTHY"}},
		}
		repository.promotions["promotion-"+environment] = PromotionRecord{
			ID: "promotion-" + environment, Product: "app.example:service", Version: "1.0.0",
			FromChannel: "STAGING", ToChannel: "RELEASE", Status: PromotionBlocked,
		}
	}
	configuration := HandlerOptions{BootstrapAdministrators: []string{"admin"}}
	counting := &overviewCountingRepository{Repository: repository}
	projection := BuildOverviewProjection(
		context.Background(), counting, NewGovernanceService(repository), Principal{Username: "admin"}, configuration, now,
	)
	if projection.StaleAfterSeconds != 300 {
		t.Fatalf("stale threshold = %d", projection.StaleAfterSeconds)
	}
	if counting.planLists != 1 || counting.reportLists != 1 || counting.changeRequestLists != 1 || counting.promotionLists != 1 {
		t.Fatalf("repository calls: plans=%d reports=%d changes=%d promotions=%d", counting.planLists, counting.reportLists, counting.changeRequestLists, counting.promotionLists)
	}

	for name, evidence := range map[string]struct {
		status string
		count  int
		items  int
	}{
		"approvals":  {projection.AwaitingApprovals.Status, projection.AwaitingApprovals.Count, len(projection.AwaitingApprovals.Items)},
		"failed":     {projection.FailedPlans.Status, projection.FailedPlans.Count, len(projection.FailedPlans.Items)},
		"unhealthy":  {projection.UnhealthyEntities.Status, projection.UnhealthyEntities.Count, len(projection.UnhealthyEntities.Items)},
		"promotions": {projection.BlockedPromotions.Status, projection.BlockedPromotions.Count, len(projection.BlockedPromotions.Items)},
	} {
		if evidence.status != OverviewCurrent || evidence.count != maximumOverviewItems+2 || evidence.items != maximumOverviewItems {
			t.Fatalf("%s evidence = %#v", name, evidence)
		}
	}
	if projection.StaleReports.Status != OverviewCurrent || projection.StaleReports.Count != maximumOverviewItems+2 || len(projection.StaleReports.Items) != 0 {
		t.Fatalf("stale evidence = %#v", projection.StaleReports)
	}
}

func TestOverviewProjectionPreservesPartialFailureAndAuthorization(t *testing.T) {
	t.Parallel()
	base := NewMemoryRepository()
	base.plans["visible"] = PlanRecord{ID: "visible", Plan: Plan{Environment: "visible"}, Status: PlanFailed}
	base.plans["hidden"] = PlanRecord{ID: "hidden", Plan: Plan{Environment: "hidden"}, Status: PlanFailed}
	base.reports["visible"] = EnvironmentReport{Environment: "visible", Entities: []ReportedEntity{{InstallationID: "visible-service", Health: "UNHEALTHY"}}}
	base.reports["hidden"] = EnvironmentReport{Environment: "hidden", Entities: []ReportedEntity{{InstallationID: "hidden-service", Health: "UNHEALTHY"}}}
	governance := NewGovernanceService(base)
	ctx := context.Background()
	if _, err := governance.PutTeam(ctx, Team{
		Name: "readers", DisplayName: "Readers", AdminUsers: []string{"owner"}, MemberUsers: []string{"viewer"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{ResourceRID: environmentRID("visible"), Team: "readers", Roles: []string{"environment:viewer"}}); err != nil {
		t.Fatal(err)
	}

	projection := BuildOverviewProjection(ctx, overviewFailureRepository{
		Repository: base, changeRequests: true,
	}, governance, Principal{Username: "viewer", Teams: []string{"readers"}}, HandlerOptions{}, time.Now())
	if projection.AwaitingApprovals.Status != OverviewUnavailable || projection.FailedPlans.Status != OverviewCurrent ||
		projection.FailedPlans.Count != 1 || projection.FailedPlans.Items[0].ID != "visible" ||
		projection.UnhealthyEntities.Count != 1 || projection.UnhealthyEntities.Items[0].InstallationID != "visible-service" {
		t.Fatalf("partial authorized projection = %#v", projection)
	}
	if projection.BlockedPromotions.Status != OverviewUnavailable {
		t.Fatalf("global promotion authority was widened: %#v", projection.BlockedPromotions)
	}
}

func TestOverviewRequiresAuthenticatedOperatorIdentity(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	NewHandler(NewMemoryRepository()).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/overview", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

type overviewFailureRepository struct {
	Repository
	changeRequests bool
}

type overviewCountingRepository struct {
	Repository
	planLists, reportLists, changeRequestLists, promotionLists int
}

func (repository *overviewCountingRepository) ListPlans(ctx context.Context, environment string) ([]PlanRecord, error) {
	repository.planLists++
	return repository.Repository.ListPlans(ctx, environment)
}

func (repository *overviewCountingRepository) ListEnvironmentReports(ctx context.Context) ([]EnvironmentReport, error) {
	repository.reportLists++
	return repository.Repository.ListEnvironmentReports(ctx)
}

func (repository *overviewCountingRepository) ListChangeRequests(ctx context.Context, environment string) ([]ChangeRequest, error) {
	repository.changeRequestLists++
	return repository.Repository.ListChangeRequests(ctx, environment)
}

func (repository *overviewCountingRepository) ListPromotions(ctx context.Context, product string) ([]PromotionRecord, error) {
	repository.promotionLists++
	return repository.Repository.ListPromotions(ctx, product)
}

func (repository overviewFailureRepository) ListChangeRequests(context.Context, string) ([]ChangeRequest, error) {
	if repository.changeRequests {
		return nil, errors.New("change request store unavailable")
	}
	return repository.Repository.ListChangeRequests(context.Background(), "")
}
