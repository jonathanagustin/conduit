package composition

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func driftRepairHub(t *testing.T) (http.Handler, controlplane.Repository, *controlplane.GovernanceService) {
	t.Helper()
	repository := controlplane.NewMemoryRepository()
	service := controlplane.NewGovernanceService(repository)
	for _, grant := range []struct {
		user, environment string
		operations        []string
	}{
		{"reader", "east", []string{controlplane.OperationReadResource}},
		{"elsewhere", "west", []string{controlplane.OperationProposeChange}},
		{"author", "east", []string{controlplane.OperationProposeChange}},
	} {
		if _, err := service.PutTeam(t.Context(), controlplane.Team{Name: grant.user, DisplayName: grant.user, AdminUsers: []string{"root"}, MemberUsers: []string{grant.user}}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: "environment/" + grant.environment, Team: grant.user, Operations: grant.operations}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{
		ID: "east-plan", CreatedAt: now, UpdatedAt: now, Status: controlplane.PlanSucceeded,
		Plan: controlplane.Plan{Environment: "east", Steps: []controlplane.PlanStep{{InstallationID: "ledger", Product: "ledger", Version: "2", ReleaseDigest: "sha256:desired"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PutEnvironmentReport(t.Context(), controlplane.EnvironmentReport{Environment: "east", ObservedAt: now, Entities: []controlplane.ReportedEntity{{InstallationID: "ledger", Product: "ledger", Version: "1", ReleaseDigest: "sha256:observed", ObservedAt: now}}}); err != nil {
		t.Fatal(err)
	}
	return NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}}, Options{}), repository, service
}

func TestDriftRepairRequiresTheRequestedEnvironmentProposalGrant(t *testing.T) {
	for _, user := range []string{"ungranted", "reader", "elsewhere"} {
		t.Run(user, func(t *testing.T) {
			handler, repository, _ := driftRepairHub(t)
			response := postAssistant(handler, user, "", "/v1/fleet/propose-fix", `{"environment":"east","installationId":"ledger"}`)
			if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "change:propose") {
				t.Fatalf("denial = %d: %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "sha256:") || strings.Contains(response.Body.String(), "changeRequestId") {
				t.Fatal("denial disclosed drift or a success receipt", response.Body.String())
			}
			missing := postAssistant(handler, user, "", "/v1/fleet/propose-fix", `{"environment":"east","installationId":"not-present"}`)
			if missing.Code != http.StatusForbidden {
				t.Fatalf("missing target bypassed authorization: %d: %s", missing.Code, missing.Body.String())
			}
			requests, err := repository.ListChangeRequests(t.Context(), "east")
			if err != nil || len(requests) != 0 {
				t.Fatalf("refused proposal stored requests: %#v, %v", requests, err)
			}
		})
	}
}

func TestDriftRepairKeepsAllowedProposalsAndRechecksChangedGrants(t *testing.T) {
	for _, user := range []string{"author", "root"} {
		t.Run(user, func(t *testing.T) {
			handler, repository, service := driftRepairHub(t)
			response := postAssistant(handler, user, "", "/v1/fleet/propose-fix", `{"environment":"east","installationId":"ledger"}`)
			if response.Code != http.StatusCreated {
				t.Fatalf("admitted proposal = %d: %s", response.Code, response.Body.String())
			}
			requests, err := repository.ListChangeRequests(t.Context(), "east")
			if err != nil || len(requests) != 1 || requests[0].Status != controlplane.ChangePendingApprovals || requests[0].RequirementsSatisfied {
				t.Fatalf("proposal bypassed review: %#v, %v", requests, err)
			}
			if user == "root" {
				return
			}
			if !strings.Contains(response.Body.String(), "PENDING_APPROVALS") {
				t.Fatalf("repair bypassed human review: %s", response.Body.String())
			}
			if user == "root" {
				return
			}
			grants, err := repository.ListResourceGrants(t.Context(), "environment/east")
			if err != nil {
				t.Fatal(err)
			}
			for _, grant := range grants {
				if grant.Team == "author" {
					grant.Operations = []string{controlplane.OperationReadResource}
					if _, err := service.PutGrant(t.Context(), grant); err != nil {
						t.Fatal(err)
					}
				}
			}
			response = postAssistant(handler, user, "", "/v1/fleet/propose-fix", `{"environment":"east","installationId":"ledger"}`)
			if response.Code != http.StatusForbidden {
				t.Fatalf("changed grant admitted repair: %d: %s", response.Code, response.Body.String())
			}
			requests, err = repository.ListChangeRequests(t.Context(), "east")
			if err != nil || len(requests) != 1 {
				t.Fatalf("refusal changed requests: %#v, %v", requests, err)
			}
		})
	}
}
