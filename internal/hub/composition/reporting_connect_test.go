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

	reportingv1 "github.com/jonathanagustin/conduit/gen/go/conduit/reporting/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"google.golang.org/protobuf/encoding/protojson"
)

const overviewProcedure = "/conduit.reporting.v1.ReportingService/GetOverview"

type unavailableOverviewRequests struct{ controlplane.Repository }

func (repository unavailableOverviewRequests) ListChangeRequests(context.Context, string) ([]controlplane.ChangeRequest, error) {
	return nil, errors.New("request store unavailable")
}

func TestOverviewConnectMatchesRESTFilteringAndPanelAvailability(t *testing.T) {
	ctx := context.Background()
	repository := controlplane.NewMemoryRepository()
	for _, environment := range []string{"visible", "hidden"} {
		if _, err := repository.CreatePlan(ctx, controlplane.PlanRecord{ID: environment, Plan: controlplane.Plan{Environment: environment}, Status: controlplane.PlanFailed, DesiredStateDigest: environment}); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.PutEnvironmentReport(ctx, controlplane.EnvironmentReport{Environment: environment, ObservedAt: time.Now().Add(-10 * time.Minute), Entities: []controlplane.ReportedEntity{{InstallationID: environment + "-service", Health: "UNHEALTHY"}}}); err != nil {
			t.Fatal(err)
		}
	}
	governance := controlplane.NewGovernanceService(repository)
	if _, err := governance.PutTeam(ctx, controlplane.Team{Name: "readers", DisplayName: "Readers", AdminUsers: []string{"owner"}, MemberUsers: []string{"viewer"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(ctx, controlplane.ResourceGrant{ResourceRID: "environment/visible", Team: "readers", Roles: []string{"environment:viewer"}}); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHub(unavailableOverviewRequests{repository}, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		actor string
		count int
	}{{"viewer", 1}, {"outsider", 0}, {"admin", 2}} {
		t.Run(test.actor, func(t *testing.T) {
			restRequest := httptest.NewRequest(http.MethodGet, "/v1/overview", nil)
			restRequest.Header.Set("X-Platform-Subject", test.actor)
			restResponse := httptest.NewRecorder()
			handler.ServeHTTP(restResponse, restRequest)
			if restResponse.Code != 200 {
				t.Fatalf("REST status=%d body=%s", restResponse.Code, restResponse.Body.String())
			}
			var native controlplane.OverviewProjection
			if err := json.Unmarshal(restResponse.Body.Bytes(), &native); err != nil {
				t.Fatal(err)
			}
			rpcRequest := httptest.NewRequest(http.MethodPost, overviewProcedure, strings.NewReader(`{}`))
			rpcRequest.Header.Set("X-Platform-Subject", test.actor)
			rpcRequest.Header.Set("Content-Type", "application/json")
			rpcRequest.Header.Set("Connect-Protocol-Version", "1")
			rpcResponse := httptest.NewRecorder()
			handler.ServeHTTP(rpcResponse, rpcRequest)
			if rpcResponse.Code != 200 {
				t.Fatalf("Connect status=%d body=%s", rpcResponse.Code, rpcResponse.Body.String())
			}
			converted := &reportingv1.GetOverviewResponse{}
			if err := protojson.Unmarshal(rpcResponse.Body.Bytes(), converted); err != nil {
				t.Fatal(err)
			}
			if native.FailedPlans.Count != test.count || converted.GetFailedPlans().GetCount() != int64(native.FailedPlans.Count) || len(converted.GetFailedPlans().GetItems()) != len(native.FailedPlans.Items) {
				t.Fatalf("filtered Plans native=%+v proto=%+v", native.FailedPlans, converted.GetFailedPlans())
			}
			for index, row := range converted.GetFailedPlans().GetItems() {
				if row.GetId() != native.FailedPlans.Items[index].ID || row.GetEnvironment() != native.FailedPlans.Items[index].Environment {
					t.Fatal("Plan row mismatch")
				}
			}
			if native.UnhealthyEntities.Count != test.count || converted.GetUnhealthyEntities().GetCount() != int64(native.UnhealthyEntities.Count) {
				t.Fatal("installation count widened")
			}
			if converted.GetAwaitingApprovals().GetStatus() != native.AwaitingApprovals.Status || converted.GetAwaitingApprovals().GetStatus() != controlplane.OverviewUnavailable {
				t.Fatal("failed store became a successful empty panel")
			}
			if converted.GetBlockedPromotions().GetStatus() != native.BlockedPromotions.Status || converted.GetStaleReports().GetStatus() != native.StaleReports.Status || converted.GetStaleReports().GetCount() != int64(native.StaleReports.Count) {
				t.Fatal("global authority or stale-report projection changed")
			}
			if converted.GetGeneratedAt().CheckValid() != nil || converted.GetStaleAfterSeconds() != native.StaleAfterSeconds {
				t.Fatal("projection timestamp or stale threshold changed")
			}
		})
	}
	records, err := repository.ListPlans(ctx, "")
	if err != nil || len(records) != 2 {
		t.Fatalf("reads changed Plans: %v", err)
	}
	for _, record := range records {
		if record.Status != controlplane.PlanFailed || len(record.Approvals) != 0 {
			t.Fatal("overview read mutated Plan authority")
		}
	}
}

func TestOverviewConnectMatchesRESTIdentityRefusals(t *testing.T) {
	for _, test := range []struct {
		name    string
		options controlplane.HandlerOptions
		status  int
	}{
		{name: "anonymous", status: 401},
		{name: "forbidden resolver delegation", options: controlplane.HandlerOptions{OperatorIdentityResolver: func(*http.Request) (controlplane.IdentityClaims, error) {
			return controlplane.IdentityClaims{}, controlplane.ErrForbidden
		}}, status: 403},
		{name: "identity backend failed", options: controlplane.HandlerOptions{OperatorIdentityResolver: func(*http.Request) (controlplane.IdentityClaims, error) {
			return controlplane.IdentityClaims{}, errors.New("backend unavailable")
		}}, status: 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, err := NewHub(controlplane.NewMemoryRepository(), test.options, Options{})
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/v1/overview", overviewProcedure} {
				method := http.MethodGet
				if path == overviewProcedure {
					method = http.MethodPost
				}
				request := httptest.NewRequest(method, path, strings.NewReader(`{}`))
				request.Header.Set("X-Platform-Subject", "admin")
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Connect-Protocol-Version", "1")
				result := httptest.NewRecorder()
				handler.ServeHTTP(result, request)
				if result.Code != test.status {
					t.Fatalf("%s status=%d expected=%d body=%s", path, result.Code, test.status, result.Body.String())
				}
			}
			for _, path := range []string{"/conduit.reporting.v1.ReportingService/ApprovePlan", "/conduit.reporting.v1.ReportingService/Subscribe"} {
				result := httptest.NewRecorder()
				handler.ServeHTTP(result, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
				if result.Code != 404 {
					t.Fatalf("unregistered method served: %s status=%d", path, result.Code)
				}
			}
		})
	}
}

func TestOverviewConnectPreservesRealDelegatedScope(t *testing.T) {
	handler, repository, _ := delegationHub(t)
	if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: "private-plan", Plan: controlplane.Plan{Environment: "private"}, Status: controlplane.PlanFailed, DesiredStateDigest: "private"}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		actor, effective string
		status, count    int
	}{{"alice", "bob", 200, 1}, {"robot", "alice", 200, 0}, {"bob", "alice", 403, 0}} {
		t.Run(test.actor+"/"+test.effective, func(t *testing.T) {
			nativeResult := as(t, handler, "/v1/overview", test.actor, test.effective)
			request := httptest.NewRequest(http.MethodPost, overviewProcedure, strings.NewReader(`{}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Connect-Protocol-Version", "1")
			request.Header.Set("X-Platform-Subject", test.actor)
			request.Header.Set("X-Platform-Service-Account", "true")
			request.Header.Set("X-Platform-On-Behalf-Of", test.effective)
			result := httptest.NewRecorder()
			handler.ServeHTTP(result, request)
			if result.Code != nativeResult.Code || result.Code != test.status {
				t.Fatalf("REST=%d Connect=%d expected=%d body=%s", nativeResult.Code, result.Code, test.status, result.Body.String())
			}
			if result.Code != 200 {
				if strings.Contains(result.Body.String(), "private-plan") {
					t.Fatal("forbidden delegate disclosed a Plan")
				}
				return
			}
			var native controlplane.OverviewProjection
			if err := json.Unmarshal(nativeResult.Body.Bytes(), &native); err != nil {
				t.Fatal(err)
			}
			converted := &reportingv1.GetOverviewResponse{}
			if err := protojson.Unmarshal(result.Body.Bytes(), converted); err != nil {
				t.Fatal(err)
			}
			if native.FailedPlans.Count != test.count || converted.GetFailedPlans().GetCount() != int64(test.count) {
				t.Fatalf("delegated scope changed: native=%+v proto=%+v", native.FailedPlans, converted.GetFailedPlans())
			}
		})
	}
}
