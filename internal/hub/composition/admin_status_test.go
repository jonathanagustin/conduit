package composition

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	admin "github.com/jonathanagustin/conduit/internal/adminstatus/domain"
	adminports "github.com/jonathanagustin/conduit/internal/adminstatus/ports"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	packaging "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

type adminCapacity struct{ fail bool }

func (reader adminCapacity) Capacity(context.Context) (int64, int64, error) {
	if reader.fail {
		return 0, 0, errors.New("private storage path")
	}
	return 95, 100, nil
}

type adminRuns struct{}

func (adminRuns) Read(context.Context, string) (packaging.RunStatus, error) {
	return packaging.RunStatus{}, nil
}
func (adminRuns) Events(context.Context, string) ([]packaging.StepEvent, error) { return nil, nil }
func (adminRuns) List(context.Context) ([]packaging.RunStatus, error) {
	return []packaging.RunStatus{{Phase: packaging.PhaseAssess}, {Phase: packaging.PhaseReview}, {Phase: packaging.PhaseComplete}, {Phase: packaging.PhaseBlocked}}, nil
}

type adminReadFailure struct{ controlplane.Repository }

func (adminReadFailure) ListChangeRequests(context.Context, string) ([]controlplane.ChangeRequest, error) {
	return nil, errors.New("private database credential")
}

func TestAdminSummaryRequiresExistingAdministratorAndNeverWrites(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}}, Options{PackagingStatus: adminRuns{}})
	for _, actor := range []string{"ordinary", "root"} {
		request := httptest.NewRequest(http.MethodGet, "/v1/admin/status", nil)
		request.Header.Set("X-Platform-Subject", actor)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if actor == "ordinary" && response.Code != 403 {
			t.Fatalf("ordinary operator got platform counts: %d", response.Code)
		}
		if actor == "root" {
			var summary admin.Summary
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &summary) != nil {
				t.Fatalf("administrator: %d %s", response.Code, response.Body.String())
			}
			if len(summary.Counters) == 0 || len(summary.Storage) != 3 || summary.Storage[0].UsedBytes != nil {
				t.Fatalf("unconfigured capacity was fabricated: %+v", summary)
			}
			if summary.Audit.VerifiedAt != nil || summary.Audit.Status != admin.Unavailable {
				t.Fatalf("memory backend claimed stored-chain verification: %+v", summary.Audit)
			}
		}
	}
	records, err := repository.ListAuthorizationAudit(t.Context(), "")
	if err != nil || len(records) != 0 {
		t.Fatalf("read-only summary mutated the audit: %+v %v", records, err)
	}
	production := NewHubHandler(repository, controlplane.HandlerOptions{})
	response := httptest.NewRecorder()
	production.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/admin/status", nil))
	if response.Code != 401 {
		t.Fatalf("anonymous platform status: %d", response.Code)
	}
}

func TestAdminSummaryCountsRealRecordsAndPreservesPartialFailures(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	for _, record := range []controlplane.PlanRecord{
		{ID: "old", Plan: controlplane.Plan{Environment: "prod"}, CreatedAt: now.Add(-time.Hour), DesiredState: &controlplane.DesiredState{Environment: controlplane.Environment{ID: "prod"}, Recalls: []controlplane.Recall{{ID: "obsolete"}}}},
		{ID: "new", Plan: controlplane.Plan{Environment: "prod"}, CreatedAt: now, DesiredState: &controlplane.DesiredState{Environment: controlplane.Environment{ID: "prod"}, Recalls: []controlplane.Recall{{ID: "open"}, {ID: "reverted", Reverted: true}}}},
		{ID: "unreachable", Plan: controlplane.Plan{Environment: "silent"}, CreatedAt: now, DesiredState: &controlplane.DesiredState{Environment: controlplane.Environment{ID: "silent"}}},
	} {
		if _, err := repository.CreatePlan(t.Context(), record); err != nil {
			t.Fatal(err)
		}
	}
	for _, report := range []controlplane.EnvironmentReport{{Environment: "prod", AgentID: "prod-agent", ObservedAt: now.Add(-45 * time.Second)}, {Environment: "stale", AgentID: "stale-agent", ObservedAt: now.Add(-46 * time.Second)}} {
		if _, err := repository.PutEnvironmentReport(t.Context(), report); err != nil {
			t.Fatal(err)
		}
	}
	source := adminStatusSource{repository: adminReadFailure{repository}, packaging: adminRuns{}, orchestrator: "native", now: func() time.Time { return now }, storage: map[string]adminports.CapacityReader{"data": adminCapacity{}, "registry": adminCapacity{fail: true}}}
	summary, err := source.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]admin.Counter{}
	for _, counter := range summary.Counters {
		values[counter.ID] = counter
	}
	if values["pendingChangeRequests"].Value != nil || values["pendingChangeRequests"].Status != admin.Unavailable {
		t.Fatal("failed counter became a healthy zero")
	}
	for id, want := range map[string]int64{"openRecallDeclarations": 1, "activePackagingRuns": 2, "liveSpokes": 1, "staleSpokes": 1, "unreachableSpokes": 1, "auditRecords": 0} {
		counter := values[id]
		if counter.Value == nil || *counter.Value != want || counter.Status != admin.Available {
			t.Fatalf("%s: %+v, want %d", id, counter, want)
		}
	}
	if summary.Storage[0].Status != "CRITICAL" || summary.Storage[1].UsedBytes != nil || summary.Storage[2].Status != admin.Unavailable {
		t.Fatalf("storage observations: %+v", summary.Storage)
	}
	encoded, _ := json.Marshal(summary)
	if strings.Contains(string(encoded), "private") {
		t.Fatal("backend error leaked into measurements")
	}
}

func TestAdminStatusUsesCarrierForBootstrapAndExistingPermissionGrants(t *testing.T) {
	handler, repository, _ := delegationHub(t)
	for _, credential := range []struct {
		actor, delegate string
		want            int
	}{{"robot", "alice", 403}, {"alice", "bob", 200}, {"alice", "", 200}} {
		response := as(t, handler, "/v1/admin/status", credential.actor, credential.delegate)
		if response.Code != credential.want {
			t.Fatalf("%s acting for %s: %d, want %d", credential.actor, credential.delegate, response.Code, credential.want)
		}
	}
	service := controlplane.NewGovernanceService(repository)
	if _, err := service.PutTeam(t.Context(), controlplane.Team{Name: "status-admins", DisplayName: "Status administrators", AdminUsers: []string{"grant-admin"}, MemberUsers: []string{"grant-admin"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: "platform/root", Team: "status-admins", Operations: []string{controlplane.OperationManagePermissions}}); err != nil {
		t.Fatal(err)
	}
	response := as(t, handler, "/v1/admin/status", "grant-admin", "")
	if response.Code != 200 {
		t.Fatalf("existing permissions administrator was refused: %d %s", response.Code, response.Body.String())
	}
}
