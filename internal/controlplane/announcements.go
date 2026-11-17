package controlplane

import (
	"context"

	catalogports "github.com/jonathanagustin/conduit/internal/catalog/ports"
	planports "github.com/jonathanagustin/conduit/internal/delivery/plan/ports"
	governanceports "github.com/jonathanagustin/conduit/internal/governance/ports"
)

// Announcements are the ports this package tells when something a person
// should hear about has been committed. Every field is optional, and a nil one
// raises nothing: a deployment with no notification store has nobody to tell.
//
// All four share RecallOpenings' contract, and the reason is the same in each
// case. They are called after the write they describe has committed, never
// inside its transaction, so a write that rolled back announces nothing. And
// the error is dropped at the call, because the write happened: reporting a
// notification failure as a refusal would have an operator or an Agent retry
// work that was recorded. Each implementation logs its own failure.
type Announcements struct {
	// ChangeRequestOpenings is told when a change request is stored waiting
	// for an approver.
	ChangeRequestOpenings governanceports.ChangeRequestOpenings
	// GrantChanges is told when a resource grant was written and audited.
	GrantChanges governanceports.GrantChanges
	// PlanFailures is told when an Agent's finish report left a Plan FAILED.
	PlanFailures planports.Failures
	// PromotionFailures is told about every stored promotion evaluation.
	PromotionFailures catalogports.PromotionFailures
}

// governanceAnnouncements is the half a GovernanceService holds.
type governanceAnnouncements struct {
	openings governanceports.ChangeRequestOpenings
	grants   governanceports.GrantChanges
}

// SetAnnouncements names who is told about change requests and grants this
// service commits. Unset, the service announces nothing, which is what every
// service built over a transaction's repository must do.
func (service *GovernanceService) SetAnnouncements(announcements Announcements) {
	service.announcements = governanceAnnouncements{
		openings: announcements.ChangeRequestOpenings, grants: announcements.GrantChanges,
	}
}

// announceAwaitingApproval tells the port about a stored request that is
// waiting for a person, and about no other. A request the policies approved,
// rejected or could not evaluate has no approver to tell.
func (service *GovernanceService) announceAwaitingApproval(ctx context.Context, request ChangeRequest) {
	openings := service.announcements.openings
	if openings == nil || request.Status != ChangePendingApprovals {
		return
	}
	_ = openings.AwaitingApproval(ctx, governanceports.ChangeRequestOpening{
		ID: request.ID, Environment: request.Environment,
		TargetKind: request.TargetKind, TargetID: request.TargetID,
		Title: request.Title, Author: request.Author.Username,
		Revision: request.Revision, At: request.UpdatedAt,
	})
}

func (service *GovernanceService) announceGrantChanged(
	ctx context.Context, actor string, before *ResourceGrant, after ResourceGrant,
) {
	if grants := service.announcements.grants; grants != nil {
		_ = grants.GrantChanged(ctx, governanceports.GrantChange{Before: before, After: after, Actor: actor})
	}
}

// planFinished tells the port about a finish report that left the Plan FAILED.
// A repeated report of the same failure reaches here again and is announced
// again; the implementation's event identity is what makes that one event.
func (announcements Announcements) planFinished(ctx context.Context, record PlanRecord) {
	if announcements.PlanFailures == nil || record.Status != PlanFailed {
		return
	}
	_ = announcements.PlanFailures.PlanFailed(ctx, planports.PlanFailure{
		PlanID: record.ID, Environment: record.Plan.Environment,
		Installation: record.FailedInstallation, Reason: record.Failure, At: record.UpdatedAt,
	})
}

func (announcements Announcements) promotionEvaluated(ctx context.Context, record PromotionRecord) {
	if announcements.PromotionFailures != nil {
		_ = announcements.PromotionFailures.PromotionEvaluated(ctx, record)
	}
}
