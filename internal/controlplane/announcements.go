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
// Ordinary optional observers run after commit and cannot undo a recorded
// write. PostgreSQL may additionally provide transaction-bound intent ports:
// those only enqueue private intent in the source SQL transaction, and their
// errors must roll back that transaction. They never admit an inbox row or
// send a message before commit.
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
	// RecallOpenings records recalls carried by an accepted desired state.
	RecallOpenings catalogports.RecallOpenings
}

// governanceAnnouncements is the half a GovernanceService holds.
type governanceAnnouncements struct {
	openings governanceports.ChangeRequestOpenings
	grants   governanceports.GrantChanges
}

// SetAnnouncements names who is told about change requests and grants this
// service commits. A transaction-local service rejects supplied live hooks;
// only private intent recorders provided by its own repository may run there.
func (service *GovernanceService) SetAnnouncements(announcements Announcements) {
	if service.repository != nil && service.repository.InTransaction() {
		// Supplied live observers must stay silent inside a transaction. Only
		// the repository's own transaction-bound intent recorders may run.
		announcements, _ = service.repository.TransactionAnnouncements()
	}
	service.announcements = governanceAnnouncements{
		openings: announcements.ChangeRequestOpenings, grants: announcements.GrantChanges,
	}
}

// announceAwaitingApproval tells the port about a stored request that is
// waiting for a person, and about no other. A request the policies approved,
// rejected or could not evaluate has no approver to tell.
func (service *GovernanceService) announceAwaitingApproval(ctx context.Context, request ChangeRequest) error {
	openings := service.announcements.openings
	if openings == nil || request.Status != ChangePendingApprovals {
		return nil
	}
	err := openings.AwaitingApproval(ctx, governanceports.ChangeRequestOpening{
		ID: request.ID, Environment: request.Environment,
		TargetKind: request.TargetKind, TargetID: request.TargetID,
		Title: request.Title, Author: request.Author.Username,
		Revision: request.Revision, At: request.UpdatedAt,
	})
	if service.repository != nil {
		if _, intents := service.repository.TransactionAnnouncements(); intents && service.repository.InTransaction() {
			return err
		}
	}
	return nil
}

func (service *GovernanceService) announceGrantChanged(
	ctx context.Context, actor string, before *ResourceGrant, after ResourceGrant,
) error {
	if grants := service.announcements.grants; grants != nil {
		err := grants.GrantChanged(ctx, governanceports.GrantChange{Before: before, After: after, Actor: actor})
		if service.repository != nil {
			if _, intents := service.repository.TransactionAnnouncements(); intents && service.repository.InTransaction() {
				return err
			}
		}
	}
	return nil
}

// planFinished tells the port about a finish report that left the Plan FAILED.
// A repeated report of the same failure reaches here again and is announced
// again; the implementation's event identity is what makes that one event.
func (announcements Announcements) planFinished(ctx context.Context, record PlanRecord) error {
	if announcements.PlanFailures == nil || record.Status != PlanFailed {
		return nil
	}
	return announcements.PlanFailures.PlanFailed(ctx, planports.PlanFailure{
		PlanID: record.ID, Environment: record.Plan.Environment,
		Installation: record.FailedInstallation, Reason: record.Failure, At: record.UpdatedAt,
	})
}

func (announcements Announcements) promotionEvaluated(ctx context.Context, record PromotionRecord) error {
	if announcements.PromotionFailures != nil {
		return announcements.PromotionFailures.PromotionEvaluated(ctx, record)
	}
	return nil
}
