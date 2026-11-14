package controlplane

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/maintenance"
)

func temporaryMaintenanceFixture(t *testing.T, repository Repository, environment string, downtime bool) (*TemporaryMaintenanceService, *GovernanceService, Principal, PlanRecord, *time.Time) {
	t.Helper()
	start := time.Date(2026, 8, 4, 11, 0, 0, 0, time.UTC)
	state := foundation()
	state.Environment.ID = environment
	state.Environment.Maintenance.NoDowntime = weeklyWindow("MONDAY/10:00-MONDAY/11:00")
	state.Environment.Maintenance.Downtime = weeklyWindow("MONDAY/10:00-MONDAY/11:00")
	state.Entities = state.Entities[1:]
	state.Entities[0].RequiresDowntime = downtime
	lifecycle := NewLifecycle(repository)
	lifecycle.now = func() time.Time { return start }
	plan, err := lifecycle.CreatePlan(t.Context(), state)
	if err != nil {
		t.Fatal(err)
	}
	if plan, err = lifecycle.ApprovePlan(t.Context(), plan.ID, "baseline-reviewer", "normal workload approved"); err != nil {
		t.Fatal(err)
	}
	governance := NewGovernanceService(repository)
	governance.now = func() time.Time { return start }
	for _, team := range []Team{{Name: "writers", DisplayName: "Writers", AdminUsers: []string{"writer"}, MemberUsers: []string{"writer"}}, {Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}}} {
		if _, err := repository.GetTeam(t.Context(), team.Name); err == nil {
			continue
		}
		if _, err := governance.PutTeam(t.Context(), team); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutGrant(t.Context(), ResourceGrant{ResourceRID: environmentRID(environment), Team: "writers", Roles: []string{"environment:editor"}}); err != nil {
		t.Fatal(err)
	}
	policies, err := repository.ListApprovalPolicies(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) == 0 {
		if _, err := governance.PutPolicy(t.Context(), ApprovalPolicy{ID: "temporary-maintenance", DisplayName: "Temporary maintenance", Enabled: true, Expression: `require_team("reviewers", "review bounded maintenance")`}); err != nil {
			t.Fatal(err)
		}
	}
	actor, err := governance.ResolvePrincipal(t.Context(), IdentityClaims{Username: "writer"})
	if err != nil {
		t.Fatal(err)
	}
	reviewer, err := governance.ResolvePrincipal(t.Context(), IdentityClaims{Username: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	service := NewTemporaryMaintenanceService(repository, HandlerOptions{BootstrapAdministrators: []string{"bootstrap"}}, actor)
	service.now = func() time.Time { return start }
	return service, governance, reviewer, plan, &start
}
func temporaryDraft(environment string) maintenance.OverrideDraft {
	return maintenance.OverrideDraft{Entities: []maintenance.EntityTarget{{Environment: environment, Entity: "example-api"}}, Duration: "2h", Reason: "CAB-364 bounded interval", ComplianceTicket: "FRCCB-364"}
}

func TestTemporaryMaintenanceReviewExpiryAndOrdinaryLeaseIsolation(t *testing.T) {
	for name, create := range moduleOverrideRepositories(t) {
		t.Run(name, func(t *testing.T) {
			repository := create(t)
			service, governance, reviewer, base, clock := temporaryMaintenanceFixture(t, repository, "dev-use2", false)
			draft := temporaryDraft("dev-use2")
			draft.DryRun = true
			preview, err := service.CreateMaintenanceOverrides(t.Context(), draft)
			if err != nil || len(preview.Requests) != 1 {
				t.Fatal(preview, err)
			}
			requests, err := repository.ListChangeRequests(t.Context(), "dev-use2")
			if err != nil || len(requests) != 0 {
				t.Fatal("dry run persisted", requests, err)
			}
			draft.DryRun = false
			submitted, err := service.CreateMaintenanceOverrides(t.Context(), draft)
			if err != nil || submitted.Failure != "" || len(submitted.Requests) != 1 {
				t.Fatal(submitted, err)
			}
			receipt := submitted.Requests[0]
			if receipt.Type != maintenance.OverrideNoDowntime || receipt.Status != ChangePendingApprovals {
				t.Fatal(receipt)
			}
			request, err := repository.GetChangeRequest(t.Context(), receipt.ChangeRequestID)
			if err != nil {
				t.Fatal(err)
			}
			if request.Description != draft.Reason || request.Changes[0].Metadata["complianceTicket"] != draft.ComplianceTicket {
				t.Fatal("lost review metadata", request)
			}
			admission := NewTemporaryMaintenanceAdmission(repository)
			admission.now = func() time.Time { return *clock }
			if _, err := admission.Lease(t.Context(), "dev-use2", "agent", time.Minute); !errors.Is(err, ErrNoPlanAvailable) {
				t.Fatal("pending override leased", err)
			}
			*clock = clock.Add(30 * time.Minute)
			if _, _, err := governance.Approve(t.Context(), request.ID, reviewer, "review original interval"); err != nil {
				t.Fatal(err)
			}
			ordinary := NewLifecycle(repository)
			ordinary.now = func() time.Time { return *clock }
			if _, err := ordinary.LeaseNextPlan(t.Context(), "dev-use2", "old-agent", time.Minute); !errors.Is(err, ErrNoPlanAvailable) {
				t.Fatal("ordinary lease gained temporary authority", err)
			}
			leased, err := admission.Lease(t.Context(), "dev-use2", "agent", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if len(leased.Plan.Steps[0].Maintenance.Overrides) != 1 || !leased.Plan.Steps[0].Maintenance.Overrides[0].EndsAt.Equal(receipt.EndsAt) {
				t.Fatal("approval reset interval", leased)
			}
			events, err := repository.ListAuthorizationAudit(t.Context(), environmentRID("dev-use2"))
			if err != nil {
				t.Fatal(err)
			}
			admissionAudits := 0
			for _, event := range events {
				if event.Action == "TEMPORARY_ENTITY_MAINTENANCE_LEASE_ADMITTED" {
					admissionAudits++
					if event.Context["planId"] != base.ID || string(mustModuleOverrideJSON(event.Context["changeRequestIds"])) != string(mustModuleOverrideJSON([]string{request.ID})) {
						t.Fatal("admission audit lost grant binding", event)
					}
				}
			}
			if admissionAudits != 1 {
				t.Fatal("missing admission audit", admissionAudits)
			}

			persisted, err := repository.GetPlan(t.Context(), base.ID)
			if err != nil || !reflect.DeepEqual(persisted.Plan, base.Plan) {
				t.Fatal("temporary authority contaminated stored Plan", err)
			}
			*clock = receipt.EndsAt
			// Keep the lease live so that refusal specifically tests the window end.
			if _, err := repository.UpdatePlan(t.Context(), base.ID, func(p *PlanRecord) error { p.Lease.ExpiresAt = clock.Add(time.Minute); return nil }); err != nil {
				t.Fatal(err)
			}
			if _, err := admission.Start(t.Context(), "dev-use2", "agent", leased.ID, leased.Lease.Token); !errors.Is(err, ErrLeaseRejected) {
				t.Fatal("expired window started", err)
			}
			persisted, _ = repository.GetPlan(t.Context(), base.ID)
			if persisted.Status != PlanLeased || !reflect.DeepEqual(persisted.Plan, base.Plan) {
				t.Fatal("failed admission changed plan")
			}
			if file, ok := repository.(*FileRepository); ok {
				reopened, err := NewFileRepository(file.root)
				if err != nil {
					t.Fatal(err)
				}
				stored, err := reopened.GetChangeRequest(t.Context(), request.ID)
				if err != nil {
					t.Fatal(err)
				}
				window, err := decodeTemporaryMaintenance(stored)
				if err != nil || !window.EndsAt.Equal(receipt.EndsAt) {
					t.Fatal("restart lost fixed window", err)
				}
			}
		})
	}
}

func TestTemporaryMaintenanceCrossEnvironmentDedupAndPartialStop(t *testing.T) {
	repository := NewMemoryRepository()
	service, _, _, _, _ := temporaryMaintenanceFixture(t, repository, "env-a", false)
	_, _, _, _, _ = temporaryMaintenanceFixture(t, repository, "env-b", false)
	draft := temporaryDraft("env-a")
	draft.Entities = append(draft.Entities, draft.Entities[0], maintenance.EntityTarget{Environment: "env-b", Entity: "example-api"}, maintenance.EntityTarget{Environment: "env-b", Entity: "missing"}, maintenance.EntityTarget{Environment: "env-a", Entity: "not-submitted"})
	result, err := service.CreateMaintenanceOverrides(t.Context(), draft)
	if err != nil || result.Failure == "" || len(result.Requests) != 2 || result.FailedTarget.Entity != "missing" {
		t.Fatal(result, err)
	}
	if !result.Requests[0].StartsAt.Equal(result.Requests[1].StartsAt) || !result.Requests[0].EndsAt.Equal(result.Requests[1].EndsAt) {
		t.Fatal("batch interval drifted")
	}
	requests, err := repository.ListChangeRequests(t.Context(), "")
	if err != nil || len(requests) != 2 {
		t.Fatal("did not stop/deduplicate", len(requests), err)
	}
}

func TestTemporaryMaintenanceRejectsUnauthorizedAndWrongDowntime(t *testing.T) {
	for _, downtime := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-downtime", true: "downtime"}[downtime], func(t *testing.T) {
			repository := NewMemoryRepository()
			service, governance, reviewer, _, clock := temporaryMaintenanceFixture(t, repository, "prod", true)
			outsider := NewTemporaryMaintenanceService(repository, service.options, Principal{Username: "outsider"})
			if _, err := outsider.CreateMaintenanceOverrides(t.Context(), temporaryDraft("prod")); !errors.Is(err, ErrForbidden) {
				t.Fatal("ungranted proposal", err)
			}
			draft := temporaryDraft("prod")
			draft.Downtime = downtime
			submitted, err := service.CreateMaintenanceOverrides(t.Context(), draft)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := governance.Approve(t.Context(), submitted.Requests[0].ChangeRequestID, reviewer, "normal review"); err != nil {
				t.Fatal(err)
			}
			admission := NewTemporaryMaintenanceAdmission(repository)
			admission.now = func() time.Time { return *clock }
			_, err = admission.Lease(t.Context(), "prod", "agent", time.Minute)
			if downtime && err != nil {
				t.Fatal(err)
			}
			if !downtime && !errors.Is(err, ErrNoPlanAvailable) {
				t.Fatal("no-downtime grant permitted downtime", err)
			}
		})
	}
}

func TestTemporaryMaintenanceExpiredBeforeApprovalAndHeldRequests(t *testing.T) {
	for _, state := range []string{"expired", "held", "cancelled", "tampered"} {
		t.Run(state, func(t *testing.T) {
			repository := NewMemoryRepository()
			service, governance, reviewer, _, clock := temporaryMaintenanceFixture(t, repository, "prod", false)
			submitted, err := service.CreateMaintenanceOverrides(t.Context(), temporaryDraft("prod"))
			if err != nil {
				t.Fatal(err)
			}
			id := submitted.Requests[0].ChangeRequestID
			if state == "expired" {
				*clock = submitted.Requests[0].EndsAt.Add(time.Minute)
			}
			if _, _, err := governance.Approve(t.Context(), id, reviewer, "review bounded interval"); err != nil {
				t.Fatal(err)
			}
			if state != "expired" {
				_, err = repository.UpdateChangeRequest(t.Context(), id, func(request *ChangeRequest) error {
					switch state {
					case "held":
						request.Labels = []string{"Do not merge"}
					case "cancelled":
						request.Status = ChangeCancelled
					case "tampered":
						request.Changes[0].Request["nativeWindow"] = `{"environment":"prod"}`
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			admission := NewTemporaryMaintenanceAdmission(repository)
			admission.now = func() time.Time { return *clock }
			if _, err := admission.Lease(t.Context(), "prod", "agent", time.Minute); err == nil {
				t.Fatal("invalid grant leased")
			}
		})
	}
}

func TestTemporaryMaintenanceAuditFailureRollsBackRequest(t *testing.T) {
	base := NewMemoryRepository()
	failure := errors.New("audit write failed")
	repository := &failingModuleAuditRepository{Repository: base, transaction: base, action: "TEMPORARY_ENTITY_MAINTENANCE_PROPOSED", failure: failure}
	service, _, _, _, _ := temporaryMaintenanceFixture(t, repository, "prod", false)
	if _, err := service.CreateMaintenanceOverrides(t.Context(), temporaryDraft("prod")); !errors.Is(err, failure) {
		t.Fatal("audit failure hidden", err)
	}
	requests, err := base.ListChangeRequests(t.Context(), "prod")
	if err != nil || len(requests) != 0 {
		t.Fatal("failed proposal left durable request", requests, err)
	}
}

func TestTemporaryMaintenanceStartRechecksHoldAfterLease(t *testing.T) {
	repository := NewMemoryRepository()
	service, governance, reviewer, _, clock := temporaryMaintenanceFixture(t, repository, "prod", false)
	submitted, err := service.CreateMaintenanceOverrides(t.Context(), temporaryDraft("prod"))
	if err != nil {
		t.Fatal(err)
	}
	id := submitted.Requests[0].ChangeRequestID
	if _, _, err := governance.Approve(t.Context(), id, reviewer, "review window"); err != nil {
		t.Fatal(err)
	}
	admission := NewTemporaryMaintenanceAdmission(repository)
	admission.now = func() time.Time { return *clock }
	leased, err := admission.Lease(t.Context(), "prod", "agent", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpdateChangeRequest(t.Context(), id, func(request *ChangeRequest) error { request.Labels = []string{"Do not merge"}; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := admission.Start(t.Context(), "prod", "agent", leased.ID, leased.Lease.Token); !errors.Is(err, ErrLeaseRejected) {
		t.Fatal("held grant survived start recheck", err)
	}
}

func TestTemporaryMaintenanceHonorsNormalAutoApprovalPolicy(t *testing.T) {
	repository := NewMemoryRepository()
	service, governance, _, _, clock := temporaryMaintenanceFixture(t, repository, "prod", false)
	policies, err := repository.ListApprovalPolicies(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	policy := policies[0]
	policy.Expression = `approve("reviewed normal maintenance policy")`
	if _, err := governance.PutPolicy(t.Context(), policy); err != nil {
		t.Fatal(err)
	}
	submitted, err := service.CreateMaintenanceOverrides(t.Context(), temporaryDraft("prod"))
	if err != nil || submitted.Requests[0].Status != ChangeApproved {
		t.Fatal(submitted, err)
	}
	admission := NewTemporaryMaintenanceAdmission(repository)
	admission.now = func() time.Time { return *clock }
	if _, err := admission.Lease(t.Context(), "prod", "agent", time.Minute); err != nil {
		t.Fatal("normal auto-approval ignored", err)
	}
}

func TestTemporaryMaintenanceDoesNotOpenUnrelatedEntity(t *testing.T) {
	repository := NewMemoryRepository()
	service, governance, reviewer, old, clock := temporaryMaintenanceFixture(t, repository, "prod", false)
	lifecycle := NewLifecycle(repository)
	lifecycle.now = func() time.Time { return *clock }
	if _, err := lifecycle.CancelPlan(t.Context(), old.ID, "operator", "replace baseline"); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(time.Second)
	state := cloneJSON(*old.DesiredState)
	state.Environment.Generation = "two-entity-maintenance"
	other := state.Entities[0]
	other.Name = "unrelated"
	other.Namespace = "unrelated"
	state.Entities = append(state.Entities, other)
	plan, err := lifecycle.CreatePlan(t.Context(), state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.ApprovePlan(t.Context(), plan.ID, "reviewer", "normal two-Entity plan"); err != nil {
		t.Fatal(err)
	}
	grant, err := service.CreateMaintenanceOverrides(t.Context(), temporaryDraft("prod"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := governance.Approve(t.Context(), grant.Requests[0].ChangeRequestID, reviewer, "only the API Entity"); err != nil {
		t.Fatal(err)
	}
	admission := NewTemporaryMaintenanceAdmission(repository)
	admission.now = func() time.Time { return *clock }
	if _, err := admission.Lease(t.Context(), "prod", "agent", time.Minute); !errors.Is(err, ErrNoPlanAvailable) {
		t.Fatal("grant opened unrelated Entity", err)
	}
	draft := temporaryDraft("prod")
	draft.Entities[0].Entity = "unrelated"
	grant, err = service.CreateMaintenanceOverrides(t.Context(), draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := governance.Approve(t.Context(), grant.Requests[0].ChangeRequestID, reviewer, "separately review unrelated Entity"); err != nil {
		t.Fatal(err)
	}
	leased, err := admission.Lease(t.Context(), "prod", "agent", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(leased.Plan.Steps) != 2 {
		t.Fatal("lost entity")
	}
	for _, step := range leased.Plan.Steps {
		if len(step.Maintenance.Overrides) != 1 || step.Maintenance.Overrides[0].InstallationID != step.InstallationID {
			t.Fatal("grant was not scoped", step)
		}
	}
}

func TestTemporaryMaintenanceAdmissionAuditFailureRollsBackLease(t *testing.T) {
	base := NewMemoryRepository()
	service, governance, reviewer, plan, clock := temporaryMaintenanceFixture(t, base, "prod", false)
	proposed, err := service.CreateMaintenanceOverrides(t.Context(), temporaryDraft("prod"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := governance.Approve(t.Context(), proposed.Requests[0].ChangeRequestID, reviewer, "review window"); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("admission audit failed")
	repository := &failingModuleAuditRepository{Repository: base, transaction: base, action: "TEMPORARY_ENTITY_MAINTENANCE_LEASE_ADMITTED", failure: failure}
	admission := NewTemporaryMaintenanceAdmission(repository)
	admission.now = func() time.Time { return *clock }
	if _, err := admission.Lease(t.Context(), "prod", "agent", time.Minute); !errors.Is(err, failure) {
		t.Fatal("audit failure hidden", err)
	}
	stored, err := base.GetPlan(t.Context(), plan.ID)
	if err != nil || stored.Status != PlanApproved || stored.Lease != nil || !reflect.DeepEqual(stored.Plan, plan.Plan) {
		t.Fatal("failed admission persisted lease", stored, err)
	}
}

func TestTemporaryMaintenanceUsesEnvironmentQualifiedInstallationAuthority(t *testing.T) {
	repository := NewMemoryRepository()
	service, governance, reviewer, plan, _ := temporaryMaintenanceFixture(t, repository, "prod", false)
	rid := installationRID("prod", plan.Plan.Steps[0].InstallationID)
	if _, err := governance.PutGrant(t.Context(), ResourceGrant{ResourceRID: rid, Team: "reviewers", Operations: []string{OperationOperate}}); err != nil {
		t.Fatal(err)
	}
	policies, err := repository.ListApprovalPolicies(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	policy := policies[0]
	policy.Expression = `require_operation(referencedResources.installation.rid, "resource:operate", "review this Environment installation")`
	if _, err := governance.PutPolicy(t.Context(), policy); err != nil {
		t.Fatal(err)
	}
	proposed, err := service.CreateMaintenanceOverrides(t.Context(), temporaryDraft("prod"))
	if err != nil {
		t.Fatal(err)
	}
	request, _, err := governance.Approve(t.Context(), proposed.Requests[0].ChangeRequestID, reviewer, "Environment-qualified installation review")
	if err != nil || request.Status != ChangeApproved {
		t.Fatal("normal installation-scope review failed", request, err)
	}
	if request.Changes[0].ReferencedResources["installation"].RID != rid {
		t.Fatal("installation reference lost Environment scope")
	}
}
