package application

import (
	"context"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/governance/ports"
)

// platformRoot is the coordinate an administrator's authority is held against.
// A grant of permissions:manage here is what makes somebody an administrator
// once the deployment's bootstrap list has done its job.
const platformRoot = "platform/root"

// localDeveloper is the identity the development header path invents when no
// subject was presented. It is an administrator only in the one configuration
// that has named no administrators at all, which is a workstation.
const localDeveloper = "local-developer"

// AdministratorPolicy is what a deployment's configuration says about who is an
// administrator before any grant exists.
//
// It is data rather than a predicate so that the predicate can be in one place.
// Two copies of "who is an administrator" is the mechanism of #241, and of #209
// before it: both copies looked right, and they disagreed about which principal
// the bootstrap list is matched against.
type AdministratorPolicy struct {
	// BootstrapAdministrators names the operators a deployment declares
	// administrators in its own configuration, before any grant exists to say so.
	BootstrapAdministrators []string
	// AllowLocalDeveloperFallback mirrors the development identity opt-in. It
	// only reaches anything when no administrator has been named, so a
	// deployment that names one has no fallback to be surprised by.
	AllowLocalDeveloperFallback bool
}

// ResourceDecision checks a resource operation and attempts to record the policy
// verdict for the original carrier. Mutations require successful persistence;
// the two explicit read operations retain best-effort auditing.
//
// Administrator is a separate side-effect-free predicate. Administrator-only
// routes that call that predicate directly still write no resource decision;
// this type does not change those routes or their audit behavior.
type ResourceDecision struct {
	// Authorizer answers what the grant table conveys.
	Authorizer *Authorizer
	// Audit is where the verdict is filed, ALLOW or DENY.
	Audit ports.AuditSink
	// Policy is the deployment's standing answer about administrators.
	Policy AdministratorPolicy
	// Now stamps the record. Absent means the wall clock, so a caller that does
	// not care about determinism does not have to say so.
	Now func() time.Time
}

// Administrator reports whether this caller holds administrator authority.
//
// It answers and does not file. Callers that reach it through Allow have their
// record written there; callers that gate on it directly -- every route behind
// requireGovernanceAdministrator -- write nothing, which was true before this
// function existed and is not changed by moving the predicate here.
//
// The two halves are judged against different principals, and the asymmetry is
// the whole point. The bootstrap list is matched against the *carrier* — the
// credential that actually made the request — because a deployment that named
// alice an administrator named a person, not everything holding act-on-behalf
// over her. The grant is judged against the *effective* principal, because a
// grant is authority over a resource and delegation is how it is exercised.
//
// #241 was a second copy of this that judged the bootstrap list on the
// effective principal. A service account holding nothing but act-on-behalf
// became an administrator on the notification routes by naming an
// administrator, and read a resource the REST routes refused it.
func (decision ResourceDecision) Administrator(ctx context.Context, principal domain.Principal) bool {
	for _, administrator := range decision.Policy.BootstrapAdministrators {
		if strings.TrimSpace(administrator) == principal.Username {
			return true
		}
	}
	if decision.Policy.AllowLocalDeveloperFallback && len(decision.Policy.BootstrapAdministrators) == 0 &&
		principal.Username == localDeveloper {
		return true
	}
	return decision.Authorizer.Allows(
		ctx, domain.EffectivePrincipal(principal), platformRoot, domain.OperationManagePermissions)
}

// Allow checks permission and attempts to persist the original carrier's decision.
// Mutations require a persisted verdict. The two explicit read operations retain
// their existing best-effort audit behavior; unknown operations fail closed on an
// audit outage. A missing required sink refuses every operation.
func (decision ResourceDecision) Allow(
	ctx context.Context, principal domain.Principal, resourceRID, operation string,
) bool {
	if decision.Audit == nil {
		return false
	}
	allowed := decision.Administrator(ctx, principal) ||
		decision.Authorizer.Allows(ctx, domain.EffectivePrincipal(principal), resourceRID, operation)
	verdict, reason := domain.DecisionAllow, "resource operation authorized"
	if !allowed {
		verdict, reason = domain.DecisionDeny, "missing operation "+operation
	}
	// The carrier is the subject and the delegation is context, so a reviewer
	// reading the record afterwards can tell which credential was presented and
	// who it claimed to be acting for. Collapsing the two would have made #241
	// unreadable even once it was filed.
	onBehalfOf := ""
	if principal.OnBehalfOf != nil {
		onBehalfOf = principal.OnBehalfOf.Username
	}
	_, err := decision.Audit.AppendAuthorizationAudit(ctx, domain.NewAuthorizationCheckEvent(
		decision.instant(), principal.Username, operation, resourceRID, verdict, reason,
		map[string]any{"onBehalfOf": onBehalfOf}))
	return allowed && (err == nil || operation == domain.OperationReadResource || operation == domain.OperationReadDebugRequest)
}

func (decision ResourceDecision) instant() time.Time {
	if decision.Now == nil {
		return time.Now().UTC()
	}
	return decision.Now().UTC()
}
