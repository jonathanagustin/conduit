package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCatalogProjectionUsesLatestGitOwnedStateAndReportedOnlyEnvironment(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	states := []DesiredState{
		{Environment: Environment{ID: "dev", Generation: "1"}, Releases: []Release{{Product: "com.example:app", Version: "1.0.0"}}},
		{Environment: Environment{ID: "dev", Generation: "2"}, Releases: []Release{{Product: "com.example:app", Version: "1.1.0"}}},
	}
	for index := range states {
		_, err := repository.CreatePlan(context.Background(), PlanRecord{
			ID: "plan_" + states[index].Environment.Generation, DesiredState: &states[index],
			Plan:   Plan{Environment: "dev", Generation: states[index].Environment.Generation},
			Status: PlanAwaitingApproval, CreatedAt: time.Unix(int64(index+1), 0), UpdatedAt: time.Unix(int64(index+1), 0),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repository.PutEnvironmentReport(context.Background(), EnvironmentReport{Environment: "reported-only", AgentID: "agent"}); err != nil {
		t.Fatal(err)
	}
	environments, err := ListEnvironmentCatalog(context.Background(), repository)
	if err != nil || len(environments) != 2 || environments[0].Environment.ID != "dev" || environments[1].Reported == nil {
		t.Fatalf("environments=%#v err=%v", environments, err)
	}
	products, err := ListProductCatalog(context.Background(), repository)
	if err != nil || len(products) != 1 || len(products[0].Releases) != 1 || products[0].Releases[0].Version != "1.1.0" {
		t.Fatalf("products=%#v err=%v", products, err)
	}
}

func TestCatalogPagesUseFilterBoundStrictCursorsAndNarrowSummaries(t *testing.T) {
	t.Parallel()
	environments := []EnvironmentCatalogRecord{
		{Environment: Environment{ID: "a-prod"}, Reported: &EnvironmentReport{Environment: "a-prod", Entities: []ReportedEntity{{InstallationID: "one"}, {InstallationID: "two"}}}},
		{Environment: Environment{ID: "b-prod"}},
		{Environment: Environment{ID: "c-stage"}},
	}
	first, err := paginateEnvironmentCatalog(environments, "PROD", "", 1)
	if err != nil || first.Total != 2 || first.PageSize != 1 || first.NextCursor == "" || first.HasPrevious {
		t.Fatalf("first Environment page=%#v err=%v", first, err)
	}
	second, err := paginateEnvironmentCatalog(environments, " prod ", first.NextCursor, 1)
	if err != nil || second.Total != 2 || second.Environments[0].Environment.ID != "b-prod" || !second.HasPrevious || second.NextCursor != "" {
		t.Fatalf("second Environment page=%#v err=%v", second, err)
	}
	if _, err := paginateEnvironmentCatalog(environments, "stage", first.NextCursor, 1); !errors.Is(err, errInvalidEnvironmentCursor) {
		t.Fatalf("cross-search Environment cursor=%v", err)
	}
	summary := summarizeEnvironmentCatalog(first)
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Environments[0].Reported == nil || summary.Environments[0].Reported.EntityCount != 2 || strings.Contains(string(encoded), "entities") {
		t.Fatalf("Environment summary=%s", encoded)
	}

	products := []ProductCatalogRecord{
		{Product: "app.example:a", Releases: []Release{{Version: "1.0.0"}, {Version: "2.0.0"}}},
		{Product: "app.example:b", Releases: []Release{{Version: "3.0.0"}}},
	}
	productPage, err := paginateProductCatalog(products, "example", "", 1)
	if err != nil || productPage.Total != 2 || productPage.NextCursor == "" {
		t.Fatalf("Product page=%#v err=%v", productPage, err)
	}
	productSummary := summarizeProductCatalog(productPage)
	encoded, err = json.Marshal(productSummary)
	if err != nil {
		t.Fatal(err)
	}
	if productSummary.Products[0].ReleaseCount != 2 || productSummary.Products[0].LatestVersion != "2.0.0" || strings.Contains(string(encoded), "releases") {
		t.Fatalf("Product summary=%s", encoded)
	}
}

func TestEnvironmentIndexAuthorizesBeforePagingAndCounting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	for _, environment := range []string{"a-visible", "b-hidden", "c-visible"} {
		if _, err := repository.PutEnvironmentReport(ctx, EnvironmentReport{
			Environment: environment, AgentID: "agent-" + environment,
			Entities: []ReportedEntity{{InstallationID: "api"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	governance := NewGovernanceService(repository)
	if _, err := governance.PutTeam(ctx, Team{
		Name: "catalog-readers", DisplayName: "Catalog readers", AdminUsers: []string{"team-admin"}, MemberUsers: []string{"reader"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, environment := range []string{"a-visible", "c-visible"} {
		if _, err := governance.PutGrant(ctx, ResourceGrant{
			ResourceRID: environmentRID(environment), Team: "catalog-readers", Operations: []string{OperationReadResource},
		}); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := governanceRequest(t, handler, http.MethodGet, "/v1/environments?limit=1&view=summary", "reader", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("index=%d %s", response.Code, response.Body.String())
	}
	var first EnvironmentCatalogSummaryPage
	if err := json.Unmarshal(response.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.Total != 2 || first.PageSize != 1 || first.Environments[0].Environment.ID != "a-visible" || first.NextCursor == "" {
		t.Fatalf("first page=%#v", first)
	}
	response = governanceRequest(t, handler, http.MethodGet,
		"/v1/environments?limit=1&view=summary&cursor="+first.NextCursor, "reader", "", nil)
	var second EnvironmentCatalogSummaryPage
	if response.Code != http.StatusOK {
		t.Fatalf("next=%d %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if second.Total != 2 || second.Environments[0].Environment.ID != "c-visible" || !second.HasPrevious {
		t.Fatalf("second page=%#v", second)
	}
}

func TestCatalogSummaryPayloadDoesNotScaleWithNestedDetails(t *testing.T) {
	t.Parallel()
	entities := make([]ReportedEntity, 100)
	releases := make([]Release, 100)
	for index := range entities {
		entities[index] = ReportedEntity{
			InstallationID: fmt.Sprintf("entity-%03d", index), Product: "app.example:payload",
			Version: "1.2.3", Health: HealthHealthy, RolloutStatus: RolloutUpToDate,
		}
		releases[index] = Release{
			Product: "app.example:payload", Version: fmt.Sprintf("1.0.%d", index),
			Chart: Artifact{Repository: "registry.example/payload", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		}
	}
	environmentPage := EnvironmentCatalogPage{
		Environments: []EnvironmentCatalogRecord{{
			Environment: Environment{ID: "prod"},
			Reported:    &EnvironmentReport{Environment: "prod", AgentID: "agent-prod", Entities: entities},
		}},
		Total: 1, PageMetadata: PageMetadata{PageSize: 1, Limit: 100},
	}
	productPage := ProductCatalogPage{
		Products: []ProductCatalogRecord{{Product: "app.example:payload", Releases: releases}},
		Total:    1, PageMetadata: PageMetadata{PageSize: 1, Limit: 100},
	}
	for _, measurement := range []struct {
		name    string
		full    any
		summary any
	}{
		{"Environment", environmentPage, summarizeEnvironmentCatalog(environmentPage)},
		{"Product", productPage, summarizeProductCatalog(productPage)},
	} {
		full, err := json.Marshal(measurement.full)
		if err != nil {
			t.Fatal(err)
		}
		summary, err := json.Marshal(measurement.summary)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s full_bytes=%d summary_bytes=%d", measurement.name, len(full), len(summary))
		if len(summary)*10 >= len(full) {
			t.Errorf("%s summary is not at least 10x smaller: full=%d summary=%d", measurement.name, len(full), len(summary))
		}
	}
}

func TestSpokeLivenessKeepsEverySpokeExplicit(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	live := spokeLiveness(&EnvironmentReport{AgentID: "spoke-live", ObservedAt: now.Add(-SpokeLivenessFreshFor)}, now)
	if live.State != SpokeLive || live.AgentID != "spoke-live" {
		t.Fatalf("fresh report = %#v", live)
	}
	stale := spokeLiveness(&EnvironmentReport{AgentID: "spoke-stale", ObservedAt: now.Add(-SpokeLivenessFreshFor - time.Second)}, now)
	if stale.State != SpokeStale || stale.ObservedAt.IsZero() {
		t.Fatalf("old report = %#v", stale)
	}
	unreachable := spokeLiveness(&EnvironmentReport{AgentID: "spoke-quiet"}, now)
	if unreachable.State != SpokeUnreachable || unreachable.AgentID != "spoke-quiet" {
		t.Fatalf("report without a time = %#v", unreachable)
	}
	missing := spokeLiveness(nil, now)
	if missing.State != SpokeUnreachable || missing.AgentID != "" {
		t.Fatalf("missing report = %#v", missing)
	}

	ctx := context.Background()
	repository := NewMemoryRepository()
	observed := time.Now().UTC()
	if _, err := repository.CreatePlan(ctx, PlanRecord{
		ID: "plan_quiet", Status: PlanAwaitingApproval,
		Plan:         Plan{Environment: "quiet", Generation: "1"},
		DesiredState: &DesiredState{Environment: Environment{ID: "quiet", Generation: "1"}},
		CreatedAt:    observed, UpdatedAt: observed,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PutEnvironmentReport(ctx, EnvironmentReport{
		Environment: "fresh", AgentID: "spoke-fresh", ObservedAt: observed, Entities: []ReportedEntity{{InstallationID: "api"}},
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"operator"},
	})
	response := governanceRequest(t, handler, http.MethodGet, "/v1/environments?view=summary", "operator", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("catalog=%d %s", response.Code, response.Body.String())
	}
	var page EnvironmentCatalogSummaryPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	byID := map[string]EnvironmentCatalogSummary{}
	for _, row := range page.Environments {
		byID[row.Environment.ID] = row
		if row.Spoke.State == "" {
			t.Fatalf("environment %s omitted spoke liveness", row.Environment.ID)
		}
	}
	if byID["quiet"].Spoke.State != SpokeUnreachable {
		t.Fatalf("quiet spoke = %#v", byID["quiet"].Spoke)
	}
	if byID["fresh"].Spoke.State != SpokeLive || byID["fresh"].Spoke.AgentID != "spoke-fresh" {
		t.Fatalf("fresh spoke = %#v", byID["fresh"].Spoke)
	}
	if !strings.Contains(response.Body.String(), `"state":"unreachable"`) {
		t.Fatalf("unreachable spoke was not explicit: %s", response.Body.String())
	}
}
