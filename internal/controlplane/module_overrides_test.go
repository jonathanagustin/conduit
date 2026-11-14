package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"testing"

	override "github.com/jonathanagustin/conduit/internal/installoverride/domain"
	"github.com/jonathanagustin/conduit/internal/installoverride/ports"
	"github.com/jonathanagustin/conduit/internal/pgtest"
)

func moduleOverrideFixture(t *testing.T, repository Repository) (*ModuleOverrideService, *GovernanceService, Principal, ports.ModuleDraft, ModuleRelease) {
	t.Helper()
	ctx := t.Context()
	governance := NewGovernanceService(repository)
	for _, team := range []Team{{Name: "ops", DisplayName: "Operators", AdminUsers: []string{"operator"}, MemberUsers: []string{"operator"}}, {Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}}} {
		if _, err := governance.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	for _, grant := range []ResourceGrant{{ResourceRID: environmentRID("prod"), Team: "ops", Roles: []string{"environment:operator"}}, {ResourceRID: environmentRID("prod"), Team: "reviewers", Roles: []string{"environment:module-override-reviewer"}}} {
		if _, err := governance.PutGrant(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutPolicy(ctx, ApprovalPolicy{ID: "module-override-fixture", DisplayName: "Fixture", Enabled: true, Expression: `approve("normal policy decision")`}); err != nil {
		t.Fatal(err)
	}
	definitions := []ModuleDefinition{{Name: "reviewed-module", Entities: []ModuleEntityDefinition{{Type: "helmChart", HelmChart: ModuleHelmChartEntity{ProductID: "example:api", KubernetesNamespace: "apps", SecretRequirements: map[string]any{"database": map[string]any{"type": "single"}}}}}}}
	modules := NewModuleService(repository)
	published, err := modules.PublishBatch(ctx, definitions)
	if err != nil {
		t.Fatal(err)
	}
	release, err := modules.RecallRelease(ctx, published[0].Name, published[0].Version)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := governance.ResolvePrincipal(ctx, IdentityClaims{Username: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	reviewer, err := governance.ResolvePrincipal(ctx, IdentityClaims{Username: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	service := NewModuleOverrideService(repository, HandlerOptions{BootstrapAdministrators: []string{"bootstrap"}}, actor)
	draft := ports.ModuleDraft{Action: "INSTALL", Environment: "prod", Module: release.Name, Version: release.Version, Flags: override.Flags{IgnoreRecalls: true, IgnoreSecretRequirements: true}, Rationale: "CAB-305: review metadata override"}
	return service, governance, reviewer, draft, release
}

func moduleOverrideRepositories(t *testing.T) map[string]func(*testing.T) Repository {
	t.Helper()
	return map[string]func(*testing.T) Repository{
		"memory": func(*testing.T) Repository { return NewMemoryRepository() },
		"file": func(t *testing.T) Repository {
			repository, err := NewFileRepository(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			return repository
		},
		"postgres": func(t *testing.T) Repository {
			dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
			if dsn == "" {
				t.Skip("CONDUIT_TEST_POSTGRES_DSN is not set")
			}
			shared, err := sql.Open("pgx", dsn)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = shared.Close() })
			fresh, drop, err := pgtest.Fresh(t.Context(), shared, dsn, t.Name())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(drop)
			repository, err := NewPostgresRepository(t.Context(), fresh)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = repository.Close() })
			return repository
		},
	}
}

func TestReviewedModuleAdmissionPreservesMetadataAndCannotAutoApprove(t *testing.T) {
	for name, factory := range moduleOverrideRepositories(t) {
		t.Run(name, func(t *testing.T) {
			repository := factory(t)
			service, governance, reviewer, draft, release := moduleOverrideFixture(t, repository)
			if _, err := NewModuleService(repository).Install(t.Context(), "prod", draft.Module, draft.Version, nil); !errors.Is(err, ErrConflict) {
				t.Fatal("ordinary path stopped enforcing recall", err)
			}
			beforeRequests, _ := repository.ListChangeRequests(t.Context(), "prod")
			preview, err := service.ProposeModuleOverride(t.Context(), draft, true)
			if err != nil || !preview.DryRun || preview.ReviewID != "" || !preview.Proposal.Snapshot.SecretRequirementsIntentOnly {
				t.Fatal("bad preview", preview, err)
			}
			afterRequests, _ := repository.ListChangeRequests(t.Context(), "prod")
			if !reflect.DeepEqual(beforeRequests, afterRequests) {
				t.Fatal("preview wrote a review")
			}
			proposed, err := service.ProposeModuleOverride(t.Context(), draft, false)
			if err != nil || proposed.Status != ChangePendingApprovals || proposed.SecretRequirementsEffect != "INTENT_ONLY" {
				t.Fatal(proposed, err)
			}
			stored, err := repository.GetChangeRequest(t.Context(), proposed.ReviewID)
			if err != nil || len(stored.Requirements) != 1 || stored.Requirements[0].Operation != OperationApproveModuleOverride || stored.Description != draft.Rationale {
				t.Fatal("missing mandatory review", stored, err)
			}
			if _, err := service.ApplyModuleOverride(t.Context(), stored.ID); !errors.Is(err, override.ErrBlocked) {
				t.Fatal("pending review applied", err)
			}
			if _, _, err := governance.Approve(t.Context(), stored.ID, service.actor, "self"); !errors.Is(err, ErrForbidden) {
				t.Fatal("author self approval", err)
			}
			approved, _, err := governance.Approve(t.Context(), stored.ID, reviewer, "Independent review of exact target")
			if err != nil || approved.Status != ChangeApproved {
				t.Fatal("independent approval failed", approved.Status, err)
			}
			result, err := service.ApplyModuleOverride(t.Context(), stored.ID)
			if err != nil || result.Status != "APPLIED" || result.Revision != 1 || result.Version != release.Version {
				t.Fatal("admission failed", result, err)
			}
			installation, err := repository.GetModuleInstallation(t.Context(), result.InstallationID)
			if err != nil || len(installation.Entities) != 1 || len(installation.Entities[0].SecretRequirements) != 1 {
				t.Fatal("secret declarations were discarded or fabricated", installation, err)
			}
			if installation.Entities[0].SecretRequirements["database"] == nil {
				t.Fatal("reviewed intent removed declared requirement")
			}
			plans, err := repository.ListPlans(t.Context(), "prod")
			if err != nil || len(plans) != 0 {
				t.Fatal("metadata admission created executable Plans", err)
			}
			second, err := service.ApplyModuleOverride(t.Context(), stored.ID)
			if err != nil || second.Status != "ALREADY_APPLIED" || second.Version != release.Version {
				t.Fatal("retry not a receipt read", second, err)
			}
			reloaded, _ := repository.GetModuleInstallation(t.Context(), installation.ID)
			if reloaded.Revision != installation.Revision {
				t.Fatal("retry performed another write")
			}
			events, err := repository.ListAuthorizationAudit(t.Context(), environmentRID("prod"))
			if err != nil {
				t.Fatal(err)
			}
			proposedCount, appliedCount := 0, 0
			for _, event := range events {
				if event.Action == "MODULE_OVERRIDE_PROPOSED" {
					proposedCount++
				}
				if event.Action == "MODULE_OVERRIDE_METADATA_APPLIED" {
					appliedCount++
					if event.Context["secretRequirementsEffect"] != "INTENT_ONLY" || event.Context["proposalId"] != proposed.Proposal.ID {
						t.Fatal("audit lost intent binding", event)
					}
				}
			}
			if proposedCount != 1 || appliedCount != 1 {
				t.Fatal("missing or replayed audit", proposedCount, appliedCount)
			}
			if file, ok := repository.(*FileRepository); ok {
				reopened, err := NewFileRepository(file.root)
				if err != nil {
					t.Fatal(err)
				}
				cr, err := reopened.GetChangeRequest(t.Context(), stored.ID)
				if err != nil || cr.Execution == nil || cr.Execution.Status != ExecutionSucceeded {
					t.Fatal("restart lost admission receipt", err)
				}
			}
		})
	}
}

func TestModuleOverrideRejectsStaleAuthorityCorruptDraftAndOldReviewers(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, Repository, *GovernanceService, string)
	}{
		{"changed-lock", func(t *testing.T, r Repository, _ *GovernanceService, _ string) {
			// Simulate corrupted storage: the supported release update API correctly
			// refuses changes to the content-addressed lock.
			memory := r.(*MemoryRepository)
			memory.mu.Lock()
			defer memory.mu.Unlock()
			release := cloneJSON(memory.moduleReleases["reviewed-module:0.1.0"])
			release.Lock.Entities[0].ConfigOverrides = "changed: true\n"
			memory.moduleReleases["reviewed-module:0.1.0"] = release
		}},
		{"changed-team", func(t *testing.T, r Repository, g *GovernanceService, _ string) {
			team, err := r.GetTeam(t.Context(), "ops")
			if err != nil {
				t.Fatal(err)
			}
			team.Description = "new authority revision"
			if _, err := g.PutTeam(t.Context(), team); err != nil {
				t.Fatal(err)
			}
		}},
		{"changed-draft", func(t *testing.T, r Repository, _ *GovernanceService, id string) {
			_, err := r.UpdateChangeRequest(t.Context(), id, func(request *ChangeRequest) error {
				request.Changes[0].Request["draft"] = `{"action":"INSTALL","environment":"other","module":"reviewed-module","version":"0.1.0"}`
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"old-hub-reviewer", func(t *testing.T, r Repository, _ *GovernanceService, id string) {
			_, err := r.UpdateChangeRequest(t.Context(), id, func(request *ChangeRequest) error { request.Changes[0].Metadata = nil; return nil })
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"hold", func(t *testing.T, r Repository, _ *GovernanceService, id string) {
			_, err := r.UpdateChangeRequest(t.Context(), id, func(request *ChangeRequest) error { request.Labels = []string{"Do not merge"}; return nil })
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"cancelled", func(t *testing.T, r Repository, _ *GovernanceService, id string) {
			_, err := r.UpdateChangeRequest(t.Context(), id, func(request *ChangeRequest) error { request.Status = ChangeCancelled; return nil })
			if err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := NewMemoryRepository()
			s, g, reviewer, draft, _ := moduleOverrideFixture(t, r)
			proposed, err := s.ProposeModuleOverride(t.Context(), draft, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := g.Approve(t.Context(), proposed.ReviewID, reviewer, "Review"); err != nil {
				t.Fatal(err)
			}
			test.mutate(t, r, g, proposed.ReviewID)
			if _, err := s.ApplyModuleOverride(t.Context(), proposed.ReviewID); err == nil {
				t.Fatal("unreviewed state applied")
			}
			installations, _ := r.ListModuleInstallations(t.Context(), "prod")
			if len(installations) != 0 {
				t.Fatal("blocked operation wrote metadata")
			}
		})
	}
}

type failingModuleAuditRepository struct {
	Repository
	transaction TransactionalRepository
	action      string
	failure     error
}

func (r *failingModuleAuditRepository) AppendAuthorizationAudit(ctx context.Context, event AuthorizationAuditEvent) (AuthorizationAuditEvent, error) {
	if event.Action == r.action {
		return AuthorizationAuditEvent{}, r.failure
	}
	return r.Repository.AppendAuthorizationAudit(ctx, event)
}
func (r *failingModuleAuditRepository) Transaction(ctx context.Context, operation func(Repository) error) error {
	return r.transaction.Transaction(ctx, func(working Repository) error {
		return operation(&failingModuleAuditRepository{Repository: working, transaction: working.(TransactionalRepository), action: r.action, failure: r.failure})
	})
}

func TestModuleOverrideAuditFailureRollsBackTransactionalAdmission(t *testing.T) {
	base := NewMemoryRepository()
	failure := errors.New("audit persistence failed")
	repository := &failingModuleAuditRepository{Repository: base, transaction: base, action: "MODULE_OVERRIDE_METADATA_APPLIED", failure: failure}
	service, governance, reviewer, draft, _ := moduleOverrideFixture(t, repository)
	proposed, err := service.ProposeModuleOverride(t.Context(), draft, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := governance.Approve(t.Context(), proposed.ReviewID, reviewer, "Review"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyModuleOverride(t.Context(), proposed.ReviewID); !errors.Is(err, failure) {
		t.Fatal("audit error hidden", err)
	}
	rows, _ := base.ListModuleInstallations(t.Context(), "prod")
	if len(rows) != 0 {
		t.Fatal("failed transaction left admitted metadata")
	}
	cr, _ := base.GetChangeRequest(t.Context(), proposed.ReviewID)
	if cr.Execution != nil {
		t.Fatal("failed transaction recorded success")
	}
}

func TestSecretOnlyModuleIntentDoesNotInventSecretOrRecallWaivers(t *testing.T) {
	r := NewMemoryRepository()
	s, g, reviewer, draft, _ := moduleOverrideFixture(t, r)
	draft.Flags = override.Flags{IgnoreSecretRequirements: true}
	pending, err := s.ProposeModuleOverride(t.Context(), draft, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.Approve(t.Context(), pending.ReviewID, reviewer, "Review secret metadata intent only"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyModuleOverride(t.Context(), pending.ReviewID); err == nil {
		t.Fatal("secret intent waived recall")
	}
	installations, err := r.ListModuleInstallations(t.Context(), draft.Environment)
	if err != nil || len(installations) != 0 {
		t.Fatal("blocked recall changed metadata", installations, err)
	}

	// Remove the recall in the fixture, then admission is only reviewed intent.
	if _, err := r.UpdateModuleRelease(t.Context(), draft.Module, draft.Version, func(release *ModuleRelease) error { release.Recalled = false; return nil }); err != nil {
		t.Fatal(err)
	}
	p, err := s.ProposeModuleOverride(t.Context(), draft, false)
	if err != nil || len(p.Proposal.Snapshot.Blockers) != 0 || !p.Proposal.Snapshot.SecretRequirementsIntentOnly {
		t.Fatal("intent invented a runtime secret check", p, err)
	}
	if _, _, err := g.Approve(t.Context(), p.ReviewID, reviewer, "Review metadata intent only"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyModuleOverride(t.Context(), p.ReviewID); err != nil {
		t.Fatal(err)
	}
}

func TestModuleOverrideUpdateBindsBaselineAndPreservesExistingState(t *testing.T) {
	for name, create := range moduleOverrideRepositories(t) {
		t.Run(name, func(t *testing.T) {
			repository := create(t)
			service, governance, reviewer, draft, first := moduleOverrideFixture(t, repository)
			if _, err := repository.UpdateModuleRelease(t.Context(), first.Name, first.Version, func(release *ModuleRelease) error { release.Recalled = false; return nil }); err != nil {
				t.Fatal(err)
			}
			modules := NewModuleService(repository)
			installed, err := modules.InstallTrackingChannel(t.Context(), draft.Environment, draft.Module, draft.Version, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			definition := cloneJSON(first.Definition)
			definition.Entities[0].HelmChart.ConfigOverrides = "changed: true\n"
			published, err := modules.PublishBatch(t.Context(), []ModuleDefinition{definition})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := modules.RecallRelease(t.Context(), draft.Module, published[0].Version); err != nil {
				t.Fatal(err)
			}
			draft.Action = "UPDATE"
			draft.InstallationID = installed.ID
			draft.Version = published[0].Version
			draft.Module = "" // The server resolves the installation's immutable Module identity.
			proposals := make([]ports.ModuleReview, 2)
			for i := range proposals {
				proposals[i], err = service.ProposeModuleOverride(t.Context(), draft, false)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err = governance.Approve(t.Context(), proposals[i].ReviewID, reviewer, "Review exact installed Module baseline"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = service.ApplyModuleOverride(t.Context(), proposals[0].ReviewID); err != nil {
				t.Fatal(err)
			}
			if _, err = service.ApplyModuleOverride(t.Context(), proposals[1].ReviewID); err == nil {
				t.Fatal("second approval reused stale baseline")
			}
			stored, err := repository.GetModuleInstallation(t.Context(), installed.ID)
			if err != nil || stored.Version != draft.Version || stored.Revision != installed.Revision+1 || stored.Environment != installed.Environment || stored.ReleaseChannel != installed.ReleaseChannel || !reflect.DeepEqual(stored.Variables, installed.Variables) {
				t.Fatal("update lost baseline fields", stored, err)
			}
			plans, err := repository.ListPlans(t.Context(), draft.Environment)
			if err != nil || len(plans) != 0 {
				t.Fatal("metadata update invented Plan", err)
			}
		})
	}
}
