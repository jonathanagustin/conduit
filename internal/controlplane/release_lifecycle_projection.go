package controlplane

import (
	"context"
	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
	"sort"
)

// releaseLifecycleProjectionPort answers what has been withdrawn and what has been
// promoted from the Plans the repository holds, which is the only copy both Hub
// replicas can see. Apollo asked its own memory before this existed, and its
// memory was written only by the request that accepted the desired state: the
// replica that did not serve that POST reported a recalled Release as not
// recalled, and so did every replica after a restart.
//
// The cost is one repository read per Apollo catalog read. On this Hub that is
// the same unit of work every other read route already pays -- the repository
// materializes its whole state per operation either way -- and it buys an
// answer that does not depend on which process the client reached.
type releaseLifecycleProjectionPort struct {
	repository environmentProjectionPlanReader
}

func (port releaseLifecycleProjectionPort) ReleaseLifecycle(ctx context.Context) (environments.ReleaseLifecycle, error) {
	plans, err := port.repository.ListPlans(ctx, "")
	if err != nil {
		return environments.ReleaseLifecycle{}, err
	}
	states := latestDeclaredStates(plans)
	// Environments in sorted order so that two Environments recalling the same
	// Release do not make the reported reason depend on map iteration, which
	// would change the answer between two identical requests.
	identifiers := make([]string, 0, len(states))
	for environment := range states {
		identifiers = append(identifiers, environment)
	}
	sort.Strings(identifiers)
	var lifecycle environments.ReleaseLifecycle
	for _, environment := range identifiers {
		state := states[environment]
		for _, recall := range state.Recalls {
			lifecycle.Recalls = append(lifecycle.Recalls, environments.ProjectedRecall{
				Product: recall.Product, StartingVersion: recall.StartingVersion,
				EndingVersionExclusive: recall.EndingVersionExclusive,
				ExcludedVersions:       append([]string(nil), recall.ExcludedVersions...),
				SingleVersion:          recall.SingleVersion, Reason: recall.Reason, Reverted: recall.Reverted,
			})
		}
		for _, promotion := range state.Promotions {
			lifecycle.Promotions = append(lifecycle.Promotions, environments.ProjectedPromotion{
				Product: promotion.Product, Version: promotion.Version,
				FromChannel: promotion.FromChannel, ToChannel: promotion.ToChannel,
				Passed: promotion.Status == PromotionPassed,
			})
		}
	}
	return lifecycle, nil
}

// latestDeclaredStates is latestDesiredStates with Entity commands left out. A
// command stores a desired state whose Recalls have been stripped -- commands
// deliberately bypass recall eligibility -- so the newest plan in an
// Environment can be one that says nothing about withdrawals. Reading it as the
// Environment's declaration would report a recalled Release as not recalled the
// moment anyone issued a command there.
func latestDeclaredStates(plans []PlanRecord) map[string]DesiredState {
	declared := make([]PlanRecord, 0, len(plans))
	for _, plan := range plans {
		if plan.Command != nil {
			continue
		}
		declared = append(declared, plan)
	}
	return latestDesiredStates(declared)
}
