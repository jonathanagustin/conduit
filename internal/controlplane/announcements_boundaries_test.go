package controlplane

import (
	"context"
	"errors"
	"testing"

	governanceports "github.com/jonathanagustin/conduit/internal/governance/ports"
	review "github.com/jonathanagustin/conduit/internal/planreview/domain"
)

type persistedOpeningProbe struct {
	t        *testing.T
	reader   Repository
	openings []governanceports.ChangeRequestOpening
}

func (probe *persistedOpeningProbe) AwaitingApproval(ctx context.Context, opening governanceports.ChangeRequestOpening) error {
	stored, err := probe.reader.GetChangeRequest(ctx, opening.ID)
	if err != nil || stored.Status != ChangePendingApprovals || stored.Revision != opening.Revision {
		probe.t.Errorf("announcement must follow the commit: opening=%#v stored=%#v error=%v", opening, stored, err)
	}
	probe.openings = append(probe.openings, opening)
	return errors.New("an unavailable optional notification destination must not undo this commit")
}

func TestSerializedOverrideProposalsAnnounceAfterCommitAndSkipPreviews(t *testing.T) {
	for kind, create := range moduleOverrideRepositories(t) {
		for _, surface := range []string{"module", "maintenance"} {
			t.Run(kind+"/"+surface, func(t *testing.T) {
				repository := create(t)
				probe := &persistedOpeningProbe{t: t, reader: repository}
				switch surface {
				case "module":
					service, _, _, draft, _ := moduleOverrideFixture(t, repository)
					service.options.Announcements.ChangeRequestOpenings = probe
					if _, err := service.ProposeModuleOverride(t.Context(), draft, true); err != nil {
						t.Fatal(err)
					}
					if len(probe.openings) != 0 {
						t.Fatal("preview announced a proposal", probe.openings)
					}
					result, err := service.ProposeModuleOverride(t.Context(), draft, false)
					if err != nil || len(probe.openings) != 1 || probe.openings[0].ID != result.ReviewID {
						t.Fatalf("committed module review: result=%#v openings=%#v error=%v", result, probe.openings, err)
					}
				case "maintenance":
					service, _, _, _, _ := temporaryMaintenanceFixture(t, repository, "prod", false)
					service.options.Announcements.ChangeRequestOpenings = probe
					draft := temporaryDraft("prod")
					draft.DryRun = true
					if _, err := service.CreateMaintenanceOverrides(t.Context(), draft); err != nil {
						t.Fatal(err)
					}
					if len(probe.openings) != 0 {
						t.Fatal("preview announced a proposal", probe.openings)
					}
					draft.DryRun = false
					result, err := service.CreateMaintenanceOverrides(t.Context(), draft)
					if err != nil || len(result.Requests) != 1 || len(probe.openings) != 1 || probe.openings[0].ID != result.Requests[0].ChangeRequestID {
						t.Fatalf("committed maintenance review: result=%#v openings=%#v error=%v", result, probe.openings, err)
					}
				}
			})
		}
	}
}

// Embedding Repository must preserve transaction scope, as real repository
// fault-injection and authorization decorators do.
type announcementScopeDecorator struct{ TransactionalRepository }

func TestNestedProposalBoundariesNeverAnnounceBeforeTheOuterCommit(t *testing.T) {
	for kind, create := range moduleOverrideRepositories(t) {
		for _, surface := range []string{"module", "maintenance", "membership"} {
			for _, outcome := range []string{"outer rollback", "caught inner rollback", "commit"} {
				t.Run(kind+"/"+surface+"/"+outcome, func(t *testing.T) {
					repository := create(t)
					propose := nestedAnnouncementProposal(t, repository, surface)
					calls := &recordedAnnouncements{}
					rollback := errors.New("enclosing operation failed")
					var opening ChangeRequest
					err := repository.(TransactionalRepository).Transaction(t.Context(), func(outer Repository) error {
						if !outer.InTransaction() {
							t.Fatal("outer view lost its transaction scope")
						}
						innerErr := outer.(TransactionalRepository).Transaction(t.Context(), func(inner Repository) error {
							decorated := &announcementScopeDecorator{inner.(TransactionalRepository)}
							if !decorated.InTransaction() {
								t.Fatal("decorator lost its transaction scope")
							}
							var err error
							opening, err = propose(decorated, calls.all())
							if err != nil {
								return err
							}
							if opening.Status != ChangePendingApprovals {
								t.Fatalf("fixture proposal did not await review: %#v", opening)
							}
							if outcome == "caught inner rollback" {
								return rollback
							}
							return nil
						})
						if outcome == "caught inner rollback" && errors.Is(innerErr, rollback) {
							return nil
						}
						if innerErr != nil {
							return innerErr
						}
						if outcome == "outer rollback" {
							return rollback
						}
						return nil
					})
					if outcome == "outer rollback" {
						if !errors.Is(err, rollback) {
							t.Fatalf("outer operation did not roll back: %v", err)
						}
					} else if err != nil {
						t.Fatal(err)
					}
					if len(calls.openings) != 0 {
						t.Fatalf("transaction-local proposal announced %d opening(s)", len(calls.openings))
					}
					if repository.InTransaction() {
						t.Fatal("committing a local snapshot changed the root handle's scope")
					}
					stored, err := repository.GetChangeRequest(t.Context(), opening.ID)
					if outcome != "commit" {
						if !errors.Is(err, ErrNotFound) {
							t.Fatalf("rolled-back request became visible: %#v, %v", stored, err)
						}
						return
					}
					if err != nil || stored.Status != ChangePendingApprovals {
						t.Fatalf("committed request disappeared: %#v, %v", stored, err)
					}
					// Without a durable producer outbox, the owner explicitly emits
					// after the final commit; inner services must not guess that point.
					service := NewGovernanceService(repository)
					service.SetAnnouncements(calls.all())
					service.announceAwaitingApproval(t.Context(), stored)
					if len(calls.openings) != 1 || calls.openings[0].ID != stored.ID {
						t.Fatalf("root owner did not announce its committed request: %#v", calls.openings)
					}
				})
			}
		}
	}
}

func nestedAnnouncementProposal(t *testing.T, repository Repository, surface string) func(Repository, Announcements) (ChangeRequest, error) {
	t.Helper()
	switch surface {
	case "module":
		service, _, _, draft, _ := moduleOverrideFixture(t, repository)
		return func(local Repository, hooks Announcements) (ChangeRequest, error) {
			inner := *service
			inner.repository, inner.options.Announcements = local, hooks
			receipt, err := inner.ProposeModuleOverride(t.Context(), draft, false)
			if err != nil {
				return ChangeRequest{}, err
			}
			return local.GetChangeRequest(t.Context(), receipt.ReviewID)
		}
	case "maintenance":
		service, _, _, _, _ := temporaryMaintenanceFixture(t, repository, "prod", false)
		return func(local Repository, hooks Announcements) (ChangeRequest, error) {
			inner := *service
			inner.repository, inner.options.Announcements = local, hooks
			receipt, err := inner.CreateMaintenanceOverrides(t.Context(), temporaryDraft("prod"))
			if err != nil {
				return ChangeRequest{}, err
			}
			if len(receipt.Requests) != 1 {
				t.Fatalf("fixture maintenance proposal has %d receipts", len(receipt.Requests))
			}
			return local.GetChangeRequest(t.Context(), receipt.Requests[0].ChangeRequestID)
		}
	case "membership":
		service := NewGovernanceService(repository)
		for _, team := range []Team{
			{Name: "writers", DisplayName: "Writers", AdminUsers: []string{"writer"}, MemberUsers: []string{"writer"}},
			{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}},
		} {
			if _, err := service.PutTeam(t.Context(), team); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := service.PutGrant(t.Context(), ResourceGrant{ResourceRID: "platform/root", Team: "writers", Operations: []string{OperationProposeChange}}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.PutPolicy(t.Context(), ApprovalPolicy{ID: "membership", DisplayName: "Membership", Enabled: true, Expression: `approve("baseline")`}); err != nil {
			t.Fatal(err)
		}
		author, err := service.ResolvePrincipal(t.Context(), IdentityClaims{Username: "writer"})
		if err != nil {
			t.Fatal(err)
		}
		state := foundation()
		state.ReleaseChannels = []ReleaseChannelDefinition{{Name: "STABLE"}, {Name: "CANARY"}}
		for index := range state.Releases {
			state.Releases[index].Channels = []string{"STABLE"}
		}
		baseline, _, err := service.ProposePlan(t.Context(), state, author, "Baseline", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.PutPolicy(t.Context(), ApprovalPolicy{ID: "membership", DisplayName: "Membership", Enabled: true, Revision: 1, Expression: `require_team("reviewers", "membership review")`}); err != nil {
			t.Fatal(err)
		}
		state.Environment.Generation = "nested-membership"
		state.Releases[0].Channels = []string{"CANARY"}
		context := review.Review{Title: "Membership change", Rationale: "CAB-840 nested transaction", ValueTier: review.Native, BaseDesiredStateDigest: baseline.DesiredStateDigest}
		return func(local Repository, hooks Announcements) (ChangeRequest, error) {
			inner := NewGovernanceService(local)
			inner.SetAnnouncements(hooks)
			_, request, err := inner.ProposeMembershipPlan(t.Context(), state, author, context)
			return request, err
		}
	default:
		t.Fatalf("unknown surface %q", surface)
		return nil
	}
}

func TestRolledBackOverrideProposalsAnnounceNothing(t *testing.T) {
	for _, surface := range []string{"module", "maintenance"} {
		t.Run(surface, func(t *testing.T) {
			base := NewMemoryRepository()
			failure := errors.New("audit cannot commit")
			failing := &failingModuleAuditRepository{Repository: base, transaction: base, action: "MODULE_OVERRIDE_PROPOSED", failure: failure}
			probe := &persistedOpeningProbe{t: t, reader: base}
			var err error
			if surface == "module" {
				service, _, _, draft, _ := moduleOverrideFixture(t, failing)
				service.options.Announcements.ChangeRequestOpenings = probe
				_, err = service.ProposeModuleOverride(t.Context(), draft, false)
			} else {
				failing.action = "TEMPORARY_ENTITY_MAINTENANCE_PROPOSED"
				service, _, _, _, _ := temporaryMaintenanceFixture(t, failing, "prod", false)
				service.options.Announcements.ChangeRequestOpenings = probe
				_, err = service.CreateMaintenanceOverrides(t.Context(), temporaryDraft("prod"))
			}
			if !errors.Is(err, failure) {
				t.Fatalf("expected failed transaction, got %v", err)
			}
			stored, err := base.ListChangeRequests(t.Context(), "prod")
			if err != nil || len(stored) != 0 || len(probe.openings) != 0 {
				t.Fatalf("rollback escaped as a notification: requests=%#v openings=%#v error=%v", stored, probe.openings, err)
			}
		})
	}
}
