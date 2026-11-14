package controlplane

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func labelMutationFixture(t *testing.T, repository Repository) (*GovernanceService, Principal, ChangeRequest) {
	t.Helper()
	service := NewGovernanceService(repository)
	if _, err := service.PutPolicy(t.Context(), ApprovalPolicy{ID: "label-fixture", DisplayName: "Label fixture", Enabled: true, Expression: `approve("fixture")`}); err != nil {
		t.Fatal(err)
	}
	author, err := service.ResolvePrincipal(t.Context(), IdentityClaims{Username: "author@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	_, request, err := service.ProposePlan(t.Context(), foundation(), author, "label fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	if request.Status != ChangeApproved {
		t.Fatal(request.Status, request.Error)
	}
	definitions := NewLabelDefinitionService(repository)
	for i := range 12 {
		prepared, _, err := definitions.Prepare(t.Context(), LabelDefinition{ID: fmt.Sprintf("priority-%02d", i), Description: "Fixture label", SuggestedResourceTypes: []string{"change-request"}, AcceptedValues: []string{"present"}}, author.Username, "fixture", false)
		if err != nil {
			t.Fatal(err)
		}
		prepared.ChangeRequestID = "crq_label_fixture"
		if _, err := repository.PutLabelDefinition(t.Context(), prepared); err != nil {
			t.Fatal(err)
		}
	}
	return service, author, request
}
func TestLabelAddRemovePreserveConcurrentEditsAndPreviewHasNoWrites(t *testing.T) {
	for _, kind := range []string{"memory", "file"} {
		t.Run(kind, func(t *testing.T) {
			var repository Repository = NewMemoryRepository()
			if kind == "file" {
				var err error
				repository, err = NewFileRepository(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
			}
			service, author, initial := labelMutationFixture(t, repository)
			held, plan, approved, err := service.MutateChangeRequestLabel(t.Context(), initial.ID, author, "Do not merge", "ADD", false)
			if err != nil || approved || held.Status != ChangePendingApprovals || plan.Status != PlanAwaitingApproval {
				t.Fatal("hold failed", held.Status, plan.Status, approved, err)
			}
			beforeAudit, err := repository.ListAuthorizationAudit(t.Context(), initial.ID)
			if err != nil {
				t.Fatal(err)
			}
			preview, previewPlan, wouldApprove, err := service.MutateChangeRequestLabel(t.Context(), initial.ID, author, "Do not merge", "REMOVE", true)
			if err != nil || !wouldApprove || preview.Status != ChangeApproved || previewPlan.Status != PlanApproved {
				t.Fatal("preview did not compute approval", err)
			}
			saved, err := repository.GetChangeRequest(t.Context(), initial.ID)
			if err != nil || !reflect.DeepEqual(saved, held) {
				t.Fatal("preview mutated request", err)
			}
			savedPlan, err := repository.GetPlan(t.Context(), held.PlanID)
			if err != nil || !reflect.DeepEqual(savedPlan, plan) {
				t.Fatal("preview mutated Plan", err)
			}
			afterAudit, err := repository.ListAuthorizationAudit(t.Context(), initial.ID)
			if err != nil || !reflect.DeepEqual(afterAudit, beforeAudit) {
				t.Fatal("preview mutated audit", err)
			}
			var group sync.WaitGroup
			for i := range 12 {
				group.Go(func() {
					_, _, _, err := NewGovernanceService(repository).MutateChangeRequestLabel(t.Context(), initial.ID, author, fmt.Sprintf("priority-%02d", i), "ADD", false)
					if err != nil {
						t.Error(err)
					}
				})
			}
			group.Wait()
			saved, err = repository.GetChangeRequest(t.Context(), initial.ID)
			if err != nil || len(saved.Labels) != 13 || !hasDoNotMerge(saved.Labels) {
				t.Fatal("concurrent edits lost labels or hold", saved.Labels, err)
			}
			if _, _, _, err := service.MutateChangeRequestLabel(t.Context(), initial.ID, Principal{Username: "intruder"}, "missing", "ADD", false); !errors.Is(err, ErrForbidden) {
				t.Fatal("label lookup preceded access check", err)
			}
			if _, _, _, err := service.MutateChangeRequestLabel(t.Context(), initial.ID, author, "missing", "ADD", false); !errors.Is(err, ErrNotFound) {
				t.Fatal("unknown label accepted", err)
			}
			result, plan, approved, err := service.MutateChangeRequestLabel(t.Context(), initial.ID, author, "Do not merge", "REMOVE", false)
			if err != nil || !approved || result.Status != ChangeApproved || plan.Status != PlanApproved || len(result.Labels) != 12 {
				t.Fatal("hold removal lost labels or approval transition", result.Labels, plan.Status, approved, err)
			}
			_, _, approved, err = service.MutateChangeRequestLabel(t.Context(), initial.ID, author, "priority-00", "ADD", false)
			if err != nil || approved {
				t.Fatal("unrelated idempotent label replayed approval", approved, err)
			}
			audits, err := repository.ListAuthorizationAudit(t.Context(), initial.ID)
			if err != nil || len(audits) < 15 {
				t.Fatal("label mutations lack audit", len(audits), err)
			}
		})
	}
}

// A controlled concurrent policy-round change makes stale authorization
// observable without relying on goroutine scheduling.
type changedRequirementsRepository struct {
	Repository
	changed bool
}

func (repository *changedRequirementsRepository) UpdateChangeRequestAndPlan(ctx context.Context, id string, write bool, update func(*ChangeRequest, *PlanRecord) error) (ChangeRequest, PlanRecord, error) {
	if write && !repository.changed {
		repository.changed = true
		if _, err := repository.Repository.UpdateChangeRequest(ctx, id, func(request *ChangeRequest) error {
			request.Requirements = []ReviewerRequirement{{Kind: RequirementTeam, Team: "other-reviewers"}}
			return nil
		}); err != nil {
			return ChangeRequest{}, PlanRecord{}, err
		}
	}
	return repository.Repository.UpdateChangeRequestAndPlan(ctx, id, write, update)
}
func TestLabelMutationReauthorizesAfterConcurrentRequirementChanges(t *testing.T) {
	base := NewMemoryRepository()
	service, _, request := labelMutationFixture(t, base)
	current, err := base.UpdateChangeRequest(t.Context(), request.ID, func(request *ChangeRequest) error {
		request.Requirements = []ReviewerRequirement{{Kind: RequirementTeam, Team: "reviewers"}}
		request.RequirementsSatisfied = false
		request.Status = ChangePendingApprovals
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.syncPlan(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	wrapped := &changedRequirementsRepository{Repository: base}
	_, _, _, err = NewGovernanceService(wrapped).MutateChangeRequestLabel(t.Context(), request.ID, Principal{Username: "reviewer", Teams: []string{"reviewers"}}, "priority-00", "ADD", false)
	if !errors.Is(err, ErrForbidden) || !wrapped.changed {
		t.Fatal("stale review authority committed a label", err)
	}
	saved, err := base.GetChangeRequest(t.Context(), request.ID)
	if err != nil || len(saved.Labels) != 0 {
		t.Fatal("denied retry mutated labels", saved.Labels, err)
	}
}
