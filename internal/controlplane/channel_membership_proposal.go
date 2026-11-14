package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	review "github.com/jonathanagustin/conduit/internal/planreview/domain"
)

const ChangeTypeNativeChannelMembership = "CONDUIT_NATIVE_UPDATE_RELEASE_CHANNEL_MEMBERSHIP"

// ProposeMembershipPlan submits a serialized, baseline-bound membership change.
//
// CONDUIT-INVENTED: channelmembership.GovernedProposal
// Displaced-by: a published or sanitized channel-membership proposal, concurrency, authorization and Change Type contract.
func (service *GovernanceService) ProposeMembershipPlan(ctx context.Context, state DesiredState, author Principal, context review.Review) (PlanRecord, ChangeRequest, error) {
	if err := context.Validate(); err != nil {
		return PlanRecord{}, ChangeRequest{}, err
	}
	if err := ctx.Err(); err != nil {
		return PlanRecord{}, ChangeRequest{}, err
	}
	var plan PlanRecord
	var request ChangeRequest
	operation := func(repository Repository) error {
		working := NewGovernanceService(repository)
		working.now, working.trust = service.now, service.trust
		changes, err := working.membershipProposalChanges(ctx, state, author, context.BaseDesiredStateDigest)
		if err != nil {
			return err
		}
		plan, request, err = working.proposePlanWithAdditionalChanges(ctx, state, author, context.Title, context.Rationale, nil, changes)
		return err
	}
	if transactional, ok := service.repository.(TransactionalRepository); ok {
		if err := transactional.Transaction(ctx, operation); err != nil {
			return PlanRecord{}, ChangeRequest{}, err
		}
		return plan, request, nil
	}
	if file, ok := service.repository.(*FileRepository); ok {
		// File storage has one process owner. Hold that owner's visibility lock
		// for the baseline check and writes; the private delegate uses the same
		// files without reentering the owner's mutex. This does not promise a
		// crash-atomic multi-file transaction.
		file.mu.Lock()
		defer file.mu.Unlock()
		if err := operation(&FileRepository{root: file.root}); err != nil {
			return PlanRecord{}, ChangeRequest{}, err
		}
		return plan, request, nil
	}
	return PlanRecord{}, ChangeRequest{}, fmt.Errorf("%w: membership proposals require a serialized repository", ErrInvalidTransition)
}

func (service *GovernanceService) membershipProposalChanges(ctx context.Context, state DesiredState, author Principal, baselineDigest string) ([]EntityChange, error) {
	plans, err := service.repository.ListPlans(ctx, state.Environment.ID)
	if err != nil {
		return nil, err
	}
	latest, exists := latestPlansByEnvironment(plans)[state.Environment.ID]
	if !exists || latest.DesiredState == nil || latest.DesiredStateDigest != baselineDigest {
		return nil, fmt.Errorf("%w: desired-state baseline is stale or missing", ErrConflict)
	}
	baseline := *latest.DesiredState
	if len(state.Releases) != len(baseline.Releases) {
		return nil, ErrInvalidTransition
	}
	if state.Environment.Generation == baseline.Environment.Generation || state.Environment.Generation == "" {
		return nil, ErrInvalidTransition
	}
	unchanged := cloneJSON(state)
	unchanged.Environment.Generation = baseline.Environment.Generation
	for i := range unchanged.Releases {
		unchanged.Releases[i].Channels = cloneJSON(baseline.Releases[i].Channels)
	}
	if !reflect.DeepEqual(unchanged, baseline) {
		return nil, fmt.Errorf("%w: membership proposals may change only Release channel memberships", ErrInvalidTransition)
	}
	definitions := map[string]ReleaseChannelDefinition{}
	for _, definition := range state.ReleaseChannels {
		definitions[definition.Name] = definition
	}
	principal := effectivePrincipal(author)
	var changes []EntityChange
	for i, release := range state.Releases {
		before, after := normalizedStrings(baseline.Releases[i].Channels), normalizedStrings(release.Channels)
		if slices.Equal(before, after) {
			continue
		}
		for _, channel := range normalizedStrings(append(slices.Clone(before), after...)) {
			if slices.Contains(before, channel) == slices.Contains(after, channel) {
				continue
			}
			definition, known := definitions[channel]
			if !known || (definition.Archived && !slices.Contains(before, channel) && slices.Contains(after, channel)) {
				return nil, ErrInvalidTransition
			}
			if !service.Authorize(ctx, principal, "platform/root", OperationProposeChange) && !service.AuthorizeReleaseChannelContribution(ctx, principal, channel, release.Product) {
				return nil, ErrForbidden
			}
		}
		coordinate := release.Product + ":" + release.Version
		encoded, _ := json.Marshal(struct {
			Coordinate    string
			Before, After []string
		}{coordinate, before, after})
		digest := sha256.Sum256(encoded)
		changes = append(changes, EntityChange{
			ID: "chg_" + hex.EncodeToString(digest[:16]), Type: ChangeTypeNativeChannelMembership,
			Diff: map[string]ChangeDiffEntry{"channels": {Before: before, After: after}},
			ReferencedResources: map[string]ReferencedResource{
				"environment": service.referencedResource(ctx, environmentRID(state.Environment.ID), state.Environment.ID, principal),
				"product":     service.referencedResource(ctx, productRID(release.Product), release.Product, principal),
			},
			Request: map[string]any{"mavenCoordinate": coordinate, "before": before, "after": after, "baseDesiredStateDigest": baselineDigest, "valueTier": review.Native},
		})
	}
	if len(changes) == 0 {
		return nil, fmt.Errorf("%w: no membership changes", ErrInvalidTransition)
	}
	return changes, nil
}
