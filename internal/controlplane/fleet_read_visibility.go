package controlplane

import "context"

// fleetReadVisibility is constructed once per request, after Space selection
// and query validation. Space membership narrows existing authority; it never
// grants Environment reads. Build the projection only after this predicate is
// available so hidden rows cannot contribute to results, counts or near misses.
func fleetReadVisibility(ctx context.Context, governance *GovernanceService, principal Principal, options HandlerOptions, space indexSpace) func(string) bool {
	administrator := isGovernanceAdministrator(ctx, governance, principal, options)
	// Apply the existing Environment authority and private membership before
	// Reporting builds search results, summaries or near-miss facets. Memoise
	// the authority decision shared by Plans and observed reports.
	readable := map[string]bool{}
	mayRead := func(environment string) bool {
		if administrator {
			return true
		}
		allowed, decided := readable[environment]
		if !decided {
			allowed = governance.Authorize(ctx, effectivePrincipal(principal),
				environmentRID(environment), OperationReadResource)
			readable[environment] = allowed
		}
		return allowed
	}
	return func(environment string) bool { return space.environment(environment) && mayRead(environment) }
}
