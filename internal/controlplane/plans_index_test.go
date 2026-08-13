package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// planIndexFixture creates Plans in two Environments so authorization and
// filtering can be told apart: a filter that happens to exclude everything a
// principal may not see would otherwise look like a working access check.
func planIndexFixture(t *testing.T) (*MemoryRepository, []PlanRecord) {
	t.Helper()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	lifecycle.newToken = func() (string, error) { return "lease-token", nil }

	first, err := lifecycle.CreatePlan(context.Background(), foundation())
	if err != nil {
		t.Fatal(err)
	}
	second := foundation()
	second.Environment.ID = "prod"
	other, err := lifecycle.CreatePlan(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	return repository, []PlanRecord{first, other}
}

func listPlans(t *testing.T, handler http.Handler, subject, query string) (int, []PlanRecord) {
	t.Helper()
	target := "/v1/plans"
	if query != "" {
		target += "?" + query
	}
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set("X-Platform-Subject", subject)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		return response.Code, nil
	}
	var decoded struct {
		Plans []PlanRecord `json:"plans"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode %s: %v", response.Body.String(), err)
	}
	return response.Code, decoded.Plans
}

func TestPlanIndexFiltersWithoutWideningWhatIsVisible(t *testing.T) {
	repository, created := planIndexFixture(t)
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"operator"},
	})

	t.Run("an administrator sees every Plan", func(t *testing.T) {
		status, plans := listPlans(t, handler, "operator", "")
		if status != http.StatusOK || len(plans) != len(created) {
			t.Fatalf("status=%d plans=%d, want %d", status, len(plans), len(created))
		}
	})

	t.Run("the Environment filter selects", func(t *testing.T) {
		_, plans := listPlans(t, handler, "operator", "environment=prod")
		if len(plans) != 1 || plans[0].Plan.Environment != "prod" {
			t.Fatalf("plans = %#v", plans)
		}
	})

	t.Run("the status filter is case-insensitive", func(t *testing.T) {
		// Statuses are upper case on the wire; a console building a query from
		// a lowercased URL parameter must not silently return nothing.
		_, upper := listPlans(t, handler, "operator", "status="+string(created[0].Status))
		_, lower := listPlans(t, handler, "operator", "status="+strings.ToLower(string(created[0].Status)))
		if len(upper) == 0 || len(upper) != len(lower) {
			t.Errorf("upper=%d lower=%d, want equal and non-empty", len(upper), len(lower))
		}
	})

	t.Run("the installation filter matches a step, not the Environment", func(t *testing.T) {
		installation := created[0].Plan.Steps[0].InstallationID
		_, matching := listPlans(t, handler, "operator", "installationId="+installation)
		if len(matching) == 0 {
			t.Fatalf("no Plan matched installation %q", installation)
		}
		for _, record := range matching {
			found := false
			for _, step := range record.Plan.Steps {
				if step.InstallationID == installation {
					found = true
				}
			}
			if !found {
				t.Errorf("Plan %s was returned but has no step for %s", record.ID, installation)
			}
		}
		_, absent := listPlans(t, handler, "operator", "installationId=not-an-installation")
		if len(absent) != 0 {
			t.Errorf("an unknown Installation matched %d Plans", len(absent))
		}
	})

	t.Run("an unauthorised principal sees nothing, filtered or not", func(t *testing.T) {
		// The filter must not be what hides these. A principal with no grants
		// sees an empty index however the query is written.
		for _, query := range []string{"", "environment=dev", "status=PENDING_APPROVALS"} {
			status, plans := listPlans(t, handler, "stranger", query)
			if status != http.StatusOK {
				t.Errorf("query %q: status=%d", query, status)
				continue
			}
			if len(plans) != 0 {
				t.Errorf("query %q returned %d Plans to a principal with no grants", query, len(plans))
			}
		}
	})

	t.Run("lease tokens never reach the index", func(t *testing.T) {
		lifecycle := NewLifecycle(repository)
		lifecycle.newToken = func() (string, error) { return "secret-lease-token", nil }
		if _, err := lifecycle.ApprovePlan(context.Background(), created[0].ID, "reviewer", "approved"); err != nil {
			t.Fatal(err)
		}
		if _, err := lifecycle.LeaseNextPlan(context.Background(), created[0].Plan.Environment, "agent-dev", time.Minute); err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodGet, "/v1/plans", nil)
		request.Header.Set("X-Platform-Subject", "operator")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		// The token authorises an Agent to act on a Plan; an index that leaks
		// it hands that authority to every reader.
		if body := response.Body.String(); strings.Contains(body, "secret-lease-token") {
			t.Errorf("the index leaked a lease token: %s", body)
		}
	})
}

// failingChangeRequests is a Repository whose Change Request reads fail. A
// Plan produced by a Change Request is readable only through that Change
// Request, so if the read fails there is no basis on which to decide -- and
// the index must withhold the Plan rather than return it.
type failingChangeRequests struct {
	Repository
}

func (failingChangeRequests) GetChangeRequest(context.Context, string) (ChangeRequest, error) {
	return ChangeRequest{}, errors.New("change request store unavailable")
}

func TestPlanIndexWithholdsPlansItCannotAuthorise(t *testing.T) {
	repository, created := planIndexFixture(t)

	// Attach a Change Request to a Plan so its authorisation depends on one.
	if _, err := repository.UpdatePlan(context.Background(), created[0].ID, func(record *PlanRecord) error {
		record.ChangeRequestID = "cr_unreadable"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	handler := NewHandler(failingChangeRequests{Repository: repository}, HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"operator"},
	})
	status, plans := listPlans(t, handler, "operator", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	for _, record := range plans {
		if record.ID == created[0].ID {
			t.Errorf("a Plan whose Change Request could not be read was returned anyway: %s", record.ID)
		}
	}
	// The Plans that do not depend on that Change Request are still served, so
	// one unreadable record does not empty the index.
	if len(plans) == 0 {
		t.Error("an unreadable Change Request emptied the whole index")
	}
}

func TestPlanIndexSummaryUsesFilterBoundStrictCursors(t *testing.T) {
	repository, created := planIndexFixture(t)
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	for index, record := range created {
		if _, err := repository.UpdatePlan(context.Background(), record.ID, func(stored *PlanRecord) error {
			stored.CreatedAt = base.Add(time.Duration(index) * time.Minute)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"operator"},
	})

	requestPage := func(target string) (int, PlanSummaryPage) {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("X-Platform-Subject", "operator")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var page PlanSummaryPage
		if response.Code == http.StatusOK {
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
		}
		return response.Code, page
	}

	status, first := requestPage("/v1/plans?view=summary&limit=1")
	if status != http.StatusOK || first.Total != 2 || first.PageSize != 1 || first.NextCursor == "" || first.HasPrevious {
		t.Fatalf("first page status=%d page=%#v", status, first)
	}
	if first.Plans[0].ID != created[1].ID || first.Plans[0].StepCount == 0 {
		t.Fatalf("newest summary = %#v", first.Plans)
	}
	status, second := requestPage("/v1/plans?view=summary&limit=1&cursor=" + first.NextCursor)
	if status != http.StatusOK || second.PageSize != 1 || second.Plans[0].ID != created[0].ID || !second.HasPrevious || second.NextCursor != "" {
		t.Fatalf("second page status=%d page=%#v", status, second)
	}
	status, _ = requestPage("/v1/plans?view=summary&limit=1&search=prod&cursor=" + first.NextCursor)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("cursor reused with changed search status=%d, want 422", status)
	}
	status, defaults := requestPage("/v1/plans?view=summary")
	if status != http.StatusOK || defaults.Limit != defaultPageLimit {
		t.Fatalf("summary defaults status=%d limit=%d, want %d", status, defaults.Limit, defaultPageLimit)
	}
	status, _ = requestPage("/v1/plans?view=summary&limit=201")
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("oversized summary limit status=%d, want 422", status)
	}
}

func TestPlanIndexAuthorizesBeforeSearchingCountingAndPaging(t *testing.T) {
	repository, _ := planIndexFixture(t)
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	request := httptest.NewRequest(http.MethodGet, "/v1/plans?view=summary&limit=1&search=prod", nil)
	request.Header.Set("X-Platform-Subject", "stranger")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var page PlanSummaryPage
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &page) != nil {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if page.Total != 0 || page.PageSize != 0 || len(page.Plans) != 0 || page.NextCursor != "" {
		t.Fatalf("unauthorized records influenced page: %#v", page)
	}
}

func TestPlanSummaryPayloadDoesNotScaleWithStepsOrExecution(t *testing.T) {
	record := PlanRecord{ID: "pln_dense", Plan: Plan{Environment: "prod"}, Status: PlanSucceeded, CreatedAt: time.Now().UTC()}
	for index := 0; index < 100; index++ {
		record.Plan.Steps = append(record.Plan.Steps, PlanStep{Position: index + 1, InstallationID: strings.Repeat("installation", 8)})
	}
	record.Execution = &PlanExecution{Tasks: make([]PlanExecutionTask, 100)}
	page, err := paginatePlans([]PlanRecord{record}, PlanQuery{View: "summary"}, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	full, _ := json.Marshal(page)
	summary, _ := json.Marshal(summarizePlanPage(page))
	t.Logf("Plan full=%d bytes summary=%d bytes", len(full), len(summary))
	if len(full) <= len(summary)*10 {
		t.Fatalf("summary did not remove nested scale: full=%d summary=%d", len(full), len(summary))
	}
}
