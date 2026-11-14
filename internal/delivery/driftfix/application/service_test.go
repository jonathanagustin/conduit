package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestProposeFixOpensAChangeRequestAndRefusesSelfApproval(t *testing.T) {
	ctx := context.Background()
	repository := controlplane.NewMemoryRepository()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	if _, err := repository.CreatePlan(ctx, controlplane.PlanRecord{
		ID: "pln_east", CreatedAt: now, UpdatedAt: now, Status: controlplane.PlanSucceeded,
		Plan: controlplane.Plan{Environment: "east", Steps: []controlplane.PlanStep{{
			InstallationID: "ledger", Product: "ledger", Version: "1.2.0",
			ReleaseDigest: "sha256:desired", ConfigDigest: "sha256:config",
		}}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PutEnvironmentReport(ctx, controlplane.EnvironmentReport{
		Environment: "east", ObservedAt: now, Entities: []controlplane.ReportedEntity{{
			InstallationID: "ledger", Product: "ledger", Version: "1.1.0",
			ReleaseDigest: "sha256:observed", ConfigDigest: "sha256:config",
			ConfigStatus: controlplane.ConfigApplied, ObservedAt: now,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	service := Service{Store: repository, Now: func() time.Time { return now }}
	actor := controlplane.Principal{Username: "ada"}
	result, err := service.Propose(ctx, actor, "east", "ledger")
	if err != nil || !result.Allowed || result.ChangeRequestID == "" || result.Status != controlplane.ChangePendingApprovals {
		t.Fatalf("propose = %#v %v", result, err)
	}
	if !strings.Contains(result.Reason, "1.2.0") || !strings.Contains(result.Reason, "1.1.0") {
		t.Fatalf("reason = %q", result.Reason)
	}
	plans, err := repository.ListPlans(ctx, "east")
	if err != nil || plans[0].Plan.Steps[0].Version != "1.2.0" {
		t.Fatalf("desired state changed: %#v %v", plans, err)
	}
	governance := controlplane.NewGovernanceService(repository)
	if _, _, err := governance.Approve(ctx, result.ChangeRequestID, actor, "the author"); !errors.Is(err, controlplane.ErrForbidden) {
		t.Fatalf("self approval = %v", err)
	}
}

func TestProposeFixRefusesARowThatIsNotDrifted(t *testing.T) {
	ctx := context.Background()
	repository := controlplane.NewMemoryRepository()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	if _, err := repository.CreatePlan(ctx, controlplane.PlanRecord{
		ID: "pln_east", CreatedAt: now, Status: controlplane.PlanSucceeded,
		Plan: controlplane.Plan{Environment: "east", Steps: []controlplane.PlanStep{{
			InstallationID: "ledger", Product: "ledger", Version: "1.2.0", ReleaseDigest: "sha256:same",
		}}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PutEnvironmentReport(ctx, controlplane.EnvironmentReport{
		Environment: "east", ObservedAt: now, Entities: []controlplane.ReportedEntity{{
			InstallationID: "ledger", Product: "ledger", Version: "1.2.0", ReleaseDigest: "sha256:same", ObservedAt: now,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := (Service{Store: repository, Now: func() time.Time { return now }}).Propose(ctx, controlplane.Principal{Username: "ada"}, "east", "ledger")
	if err != nil || result.Allowed || result.ChangeRequestID != "" || result.Reason != "installation is not drifted" || result.AuditID == "" {
		t.Fatalf("propose = %#v %v", result, err)
	}
	requests, err := repository.ListChangeRequests(ctx, "east")
	if err != nil || len(requests) != 0 {
		t.Fatalf("requests = %#v %v", requests, err)
	}
}
