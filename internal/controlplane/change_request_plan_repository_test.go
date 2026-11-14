package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/jonathanagustin/conduit/internal/pgtest"
)

func TestCurrentRequestVerdictAndConcurrentLabelsProjectTogether(t *testing.T) {
	factories := map[string]func(*testing.T) Repository{
		"memory": func(*testing.T) Repository { return NewMemoryRepository() },
		"file": func(t *testing.T) Repository {
			r, e := NewFileRepository(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			return r
		},
		"postgres": func(t *testing.T) Repository {
			dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
			if dsn == "" {
				t.Skip("CONDUIT_TEST_POSTGRES_DSN is not set")
			}
			shared, e := sql.Open("pgx", dsn)
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { _ = shared.Close() })
			fresh, drop, e := pgtest.Fresh(t.Context(), shared, dsn, t.Name())
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(drop)
			r, e := NewPostgresRepository(t.Context(), fresh)
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { _ = r.Close() })
			return r
		},
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			repository := factory(t)
			service := NewGovernanceService(repository)
			if _, e := service.PutPolicy(t.Context(), ApprovalPolicy{ID: "automatic-projection-fixture", DisplayName: "Automatic projection fixture", Enabled: true, Expression: `approve("projection fixture")`}); e != nil {
				t.Fatal(e)
			}
			author, e := service.ResolvePrincipal(t.Context(), IdentityClaims{Username: "author@example.test"})
			if e != nil {
				t.Fatal(e)
			}
			_, stale, e := service.ProposePlan(t.Context(), foundation(), author, "projection fixture", "")
			if e != nil {
				t.Fatal(e)
			}
			if stale.Status != ChangeApproved {
				t.Fatalf("fixture is not approved: %s: %s %#v", stale.Status, stale.Error, stale.Evaluations)
			}
			held, _, _, e := service.SetLabels(t.Context(), stale.ID, author, []string{"Do not merge"})
			if e != nil {
				t.Fatal(e)
			}
			projected, e := NewGovernanceService(repository).syncPlan(t.Context(), stale)
			if e != nil || projected.Status != PlanAwaitingApproval {
				t.Fatal("stale caller restored approval", projected.Status, e)
			}
			unchanged, e := repository.GetChangeRequest(t.Context(), stale.ID)
			if e != nil || unchanged.Revision != held.Revision {
				t.Fatal("projection mutated the request", unchanged.Revision, e)
			}
			var group sync.WaitGroup
			for i := range 20 {
				group.Go(func() {
					_, _, e := repository.UpdateChangeRequestAndPlan(t.Context(), stale.ID, true, func(request *ChangeRequest, plan *PlanRecord) error {
						request.Labels = append(request.Labels, fmt.Sprintf("label-%02d", i))
						return projectChangeRequestPlan(plan, *request, request.Status)
					})
					if e != nil {
						t.Error(e)
					}
				})
			}
			group.Wait()
			saved, e := repository.GetChangeRequest(t.Context(), stale.ID)
			if e != nil {
				t.Fatal(e)
			}
			if len(normalizedStrings(saved.Labels)) != 21 || saved.Revision != held.Revision+20 {
				t.Fatal("concurrent mutation lost labels", saved.Labels, saved.Revision)
			}
			if _, _, approved, err := service.MutateChangeRequestLabel(t.Context(), saved.ID, author, "label-00", "REMOVE", false); err != nil || approved {
				t.Fatal("legacy attached-label removal failed", approved, err)
			}
			saved, e = repository.GetChangeRequest(t.Context(), stale.ID)
			if e != nil || len(saved.Labels) != 20 || !hasDoNotMerge(saved.Labels) {
				t.Fatal("native mutation lost unrelated labels", saved.Labels, e)
			}
			plan, e := repository.GetPlan(t.Context(), saved.PlanID)
			if e != nil || plan.Status != PlanAwaitingApproval {
				t.Fatal("held request has an approved Plan", plan.Status, e)
			}
			// A callback that fails or tries to modify a read-only request changes neither record.
			for _, readonly := range []bool{false, true} {
				sentinel := errors.New("rejected mutation")
				_, _, e := repository.UpdateChangeRequestAndPlan(t.Context(), saved.ID, !readonly, func(request *ChangeRequest, plan *PlanRecord) error {
					request.Title = "changed"
					plan.Status = PlanApproved
					if readonly {
						return nil
					}
					return sentinel
				})
				if readonly {
					if !errors.Is(e, ErrConflict) {
						t.Fatal("read-only projection wrote request", e)
					}
				} else if !errors.Is(e, sentinel) {
					t.Fatal(e)
				}
				current, e := repository.GetChangeRequest(t.Context(), saved.ID)
				if e != nil || !reflect.DeepEqual(current, saved) {
					t.Fatal("failed callback changed request", e)
				}
				currentPlan, e := repository.GetPlan(t.Context(), saved.PlanID)
				if e != nil || !reflect.DeepEqual(currentPlan, plan) {
					t.Fatal("failed callback changed Plan", e)
				}
			}
			canceled, cancel := context.WithCancel(t.Context())
			cancel()
			if _, _, e := repository.UpdateChangeRequestAndPlan(canceled, saved.ID, true, func(*ChangeRequest, *PlanRecord) error { t.Error("canceled callback ran"); return nil }); !errors.Is(e, context.Canceled) {
				t.Fatal(e)
			}
		})
	}
}
