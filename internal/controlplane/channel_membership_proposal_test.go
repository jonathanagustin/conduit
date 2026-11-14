package controlplane

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jonathanagustin/conduit/internal/pgtest"
	review "github.com/jonathanagustin/conduit/internal/planreview/domain"
)

func TestMembershipProposalsAreGovernedBoundedAndSerialized(t *testing.T) {
	for _, kind := range []string{"memory", "file", "postgres"} {
		t.Run(kind, func(t *testing.T) {
			var repository Repository = NewMemoryRepository()
			if kind == "file" {
				var err error
				repository, err = NewFileRepository(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
			}
			if kind == "postgres" {
				dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("CONDUIT_TEST_POSTGRES_DSN is not set")
				}
				shared, err := sql.Open("pgx", dsn)
				if err != nil {
					t.Fatal(err)
				}
				defer shared.Close()
				fresh, drop, err := pgtest.Fresh(t.Context(), shared, dsn, t.Name())
				if err != nil {
					t.Fatal(err)
				}
				defer drop()
				pg, err := NewPostgresRepository(t.Context(), fresh)
				if err != nil {
					t.Fatal(err)
				}
				defer pg.Close()
				repository = pg
			}
			service := NewGovernanceService(repository)
			for _, team := range []Team{{Name: "writers", DisplayName: "Writers", AdminUsers: []string{"writer"}, MemberUsers: []string{"writer"}}, {Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}}} {
				if _, err := service.PutTeam(t.Context(), team); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := service.PutGrant(t.Context(), ResourceGrant{ResourceRID: "platform/root", Team: "writers", Operations: []string{OperationProposeChange}}); err != nil {
				t.Fatal(err)
			}
			if _, err := service.PutPolicy(t.Context(), ApprovalPolicy{ID: "membership", DisplayName: "Membership", Enabled: true, Expression: `approve("initial fixture")`}); err != nil {
				t.Fatal(err)
			}
			author, err := service.ResolvePrincipal(t.Context(), IdentityClaims{Username: "writer"})
			if err != nil {
				t.Fatal(err)
			}
			state := foundation()
			state.ReleaseChannels = []ReleaseChannelDefinition{{Name: "STABLE"}, {Name: "CANARY"}}
			for i := range state.Releases {
				state.Releases[i].Channels = []string{"STABLE"}
			}
			baseline, _, err := service.ProposePlan(t.Context(), state, author, "Baseline", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.PutPolicy(t.Context(), ApprovalPolicy{ID: "membership", DisplayName: "Membership", Revision: 1, Enabled: true, Expression: `type == "CONDUIT_NATIVE_UPDATE_RELEASE_CHANNEL_MEMBERSHIP" ? require_team("reviewers", "membership review") : approve("existing workload")`}); err != nil {
				t.Fatal(err)
			}
			context := review.Review{Title: "Membership change", Rationale: "Reason line one\nTicket CAB-123", ValueTier: review.Native, BaseDesiredStateDigest: baseline.DesiredStateDigest}
			candidate := cloneJSON(state)
			candidate.Environment.Generation = "reviewed-membership-one"
			candidate.Releases[0].Channels = []string{"CANARY"}
			before, err := repository.ListPlans(t.Context(), state.Environment.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := service.ProposeMembershipPlan(t.Context(), candidate, Principal{Username: "intruder"}, context); !errors.Is(err, ErrForbidden) {
				t.Fatal("unauthorized", err)
			}
			bad := cloneJSON(candidate)
			bad.Releases[0].Version = "99.0.0"
			if _, _, err := service.ProposeMembershipPlan(t.Context(), bad, author, context); !errors.Is(err, ErrInvalidTransition) {
				t.Fatal("artifact coordinates changed", err)
			}
			bad = cloneJSON(candidate)
			bad.ReleaseChannels[0].Description = "definition overwritten"
			if _, _, err := service.ProposeMembershipPlan(t.Context(), bad, author, context); !errors.Is(err, ErrInvalidTransition) {
				t.Fatal("definition changed", err)
			}
			bad = cloneJSON(candidate)
			bad.Releases[0].Channels = []string{"UNKNOWN"}
			if _, _, err := service.ProposeMembershipPlan(t.Context(), bad, author, context); !errors.Is(err, ErrInvalidTransition) {
				t.Fatal("unknown channel", err)
			}
			after, err := repository.ListPlans(t.Context(), state.Environment.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("failed proposal wrote a Plan", err)
			}
			second := cloneJSON(state)
			second.Environment.Generation = "reviewed-membership-two"
			second.Releases[1].Channels = []string{}
			var wg sync.WaitGroup
			var mu sync.Mutex
			successes, conflicts := 0, 0
			var stored PlanRecord
			var request ChangeRequest
			for _, desired := range []DesiredState{candidate, second} {
				wg.Go(func() {
					plan, cr, err := service.ProposeMembershipPlan(t.Context(), desired, author, context)
					mu.Lock()
					defer mu.Unlock()
					if err == nil {
						successes++
						stored, request = plan, cr
					} else if errors.Is(err, ErrConflict) {
						conflicts++
					} else {
						t.Errorf("proposal: %v", err)
					}
				})
			}
			wg.Wait()
			if successes != 1 || conflicts != 1 {
				t.Fatal("stale overwrite", successes, conflicts)
			}
			if stored.Status != PlanAwaitingApproval || request.Status != ChangePendingApprovals || len(request.Requirements) != 1 || request.Description != context.Rationale {
				t.Fatal("missing review or rationale", request, stored.Status)
			}
			found := 0
			for _, change := range request.Changes {
				if change.Type == ChangeTypeNativeChannelMembership {
					found++
					if change.Request["valueTier"] != review.Native || change.Request["baseDesiredStateDigest"] != context.BaseDesiredStateDigest {
						t.Fatal(change)
					}
				}
			}
			if found != 1 {
				t.Fatal("missing native change", found)
			}
			immutable := cloneJSON(*stored.DesiredState)
			immutable.Environment.Generation = state.Environment.Generation
			for i := range immutable.Releases {
				immutable.Releases[i].Channels = state.Releases[i].Channels
			}
			if !reflect.DeepEqual(immutable, state) {
				t.Fatal("immutable source or channel definitions changed")
			}
			if file, ok := repository.(*FileRepository); ok {
				reopened, err := NewFileRepository(file.root)
				if err != nil {
					t.Fatal(err)
				}
				persisted, err := reopened.GetChangeRequest(t.Context(), request.ID)
				if err != nil || persisted.Description != context.Rationale || !equalMembershipChangeJSON(persisted.Changes, request.Changes) {
					t.Fatal("restart lost review", err)
				}
			}
			reviewer, err := service.ResolvePrincipal(t.Context(), IdentityClaims{Username: "reviewer"})
			if err != nil {
				t.Fatal(err)
			}
			request, stored, err = service.Approve(t.Context(), request.ID, reviewer, "Reviewed memberships")
			if err != nil || request.Status != ChangeApproved || stored.Status != PlanApproved || len(request.Reviews) != 1 || !strings.Contains(request.Description, "CAB-123") {
				t.Fatal("approval failed", request.Status, stored.Status, err)
			}
		})
	}
}

func equalMembershipChangeJSON(left, right []EntityChange) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return bytes.Equal(a, b)
}
