package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestChangeRequestPageUsesUrgencyAndFilterBoundStrictCursors(t *testing.T) {
	base := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	records := []ChangeRequest{
		{ID: "crq_settled", Environment: "prod", Title: "Recent history", Status: ChangeApproved, UpdatedAt: base.Add(time.Hour)},
		{ID: "crq_waiting_old", Environment: "prod", Title: "Database rollout", Status: ChangePendingApprovals, UpdatedAt: base},
		{ID: "crq_waiting_new", Environment: "stage", Title: "API rollout", Status: ChangePendingApprovals, UpdatedAt: base.Add(time.Minute)},
	}
	first, err := paginateChangeRequests(records, ChangeRequestQuery{}, "", 1)
	if err != nil || first.Total != 3 || first.PageSize != 1 || first.NextCursor == "" || first.HasPrevious {
		t.Fatalf("first page=%#v err=%v", first, err)
	}
	if first.ChangeRequests[0].ID != "crq_waiting_new" {
		t.Fatalf("first request=%s, want newest request awaiting approval", first.ChangeRequests[0].ID)
	}
	second, err := paginateChangeRequests(records, ChangeRequestQuery{}, first.NextCursor, 1)
	if err != nil || second.ChangeRequests[0].ID != "crq_waiting_old" || !second.HasPrevious {
		t.Fatalf("second page=%#v err=%v", second, err)
	}
	if _, err := paginateChangeRequests(records, ChangeRequestQuery{Search: "database"}, first.NextCursor, 1); err == nil {
		t.Fatal("Change Request cursor was accepted with a different search")
	}
	filtered, err := paginateChangeRequests(records, ChangeRequestQuery{Environment: "prod", Status: "approved"}, "", 10)
	if err != nil || filtered.Total != 1 || filtered.ChangeRequests[0].ID != "crq_settled" {
		t.Fatalf("filtered page=%#v err=%v", filtered, err)
	}
}

func TestPromotionPageIsNewestFirstAndFilterBound(t *testing.T) {
	base := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	records := []PromotionRecord{
		{ID: "prm_old", Product: "app.example:payments", Version: "1.0.0", FromChannel: "RELEASE", ToChannel: "STAGING", Status: PromotionBlocked, EvaluatedAt: base},
		{ID: "prm_new", Product: "app.example:payments", Version: "1.0.1", FromChannel: "RELEASE", ToChannel: "STAGING", Status: PromotionPassed, EvaluatedAt: base.Add(time.Minute)},
		{ID: "prm_other", Product: "app.example:ledger", Version: "2.0.0", FromChannel: "STAGING", ToChannel: "PRODUCTION", Status: PromotionPassed, EvaluatedAt: base.Add(2 * time.Minute)},
	}
	first, err := paginatePromotions(records, PromotionQuery{}, "", 1)
	if err != nil || first.Total != 3 || first.Promotions[0].ID != "prm_other" || first.NextCursor == "" {
		t.Fatalf("first page=%#v err=%v", first, err)
	}
	if _, err := paginatePromotions(records, PromotionQuery{Product: "app.example:payments"}, first.NextCursor, 1); err == nil {
		t.Fatal("Promotion cursor was accepted with a different Product filter")
	}
	filtered, err := paginatePromotions(records, PromotionQuery{Search: "payments", Status: "passed"}, "", 10)
	if err != nil || filtered.Total != 1 || filtered.Promotions[0].ID != "prm_new" {
		t.Fatalf("filtered page=%#v err=%v", filtered, err)
	}
}

func TestChangeRequestIndexAuthorizesBeforeFilteringCountingAndPaging(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	for _, record := range []ChangeRequest{
		{ID: "crq_visible", Environment: "visible", Title: "Visible rollout", Author: Principal{Username: "author"}, Status: ChangePendingApprovals, UpdatedAt: time.Now().UTC()},
		{ID: "crq_hidden", Environment: "hidden", Title: "Hidden needle", Author: Principal{Username: "other"}, Status: ChangePendingApprovals, UpdatedAt: time.Now().UTC()},
	} {
		if _, err := repository.CreateChangeRequest(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	governance := NewGovernanceService(repository)
	if _, err := governance.PutTeam(ctx, Team{
		Name: "readers", DisplayName: "Readers", AdminUsers: []string{"team-admin"}, MemberUsers: []string{"reader"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{
		ResourceRID: environmentRID("visible"), Team: "readers", Operations: []string{OperationReadResource},
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})

	for _, target := range []string{
		"/v1/change-requests?limit=1",
		"/v1/change-requests?limit=1&search=hidden",
		"/v1/change-requests?limit=1&environment=hidden",
	} {
		response := governanceRequest(t, handler, http.MethodGet, target, "reader", "", nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s=%d %s", target, response.Code, response.Body.String())
		}
		var page ChangeRequestPage
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if target == "/v1/change-requests?limit=1" {
			if page.Total != 1 || len(page.ChangeRequests) != 1 || page.ChangeRequests[0].ID != "crq_visible" {
				t.Fatalf("visible page=%#v", page)
			}
		} else if page.Total != 0 || len(page.ChangeRequests) != 0 || page.NextCursor != "" {
			t.Fatalf("unauthorized record influenced %s: %#v", target, page)
		}
	}
}
