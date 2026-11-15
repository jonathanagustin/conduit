package controlplane

import (
	"context"
	"errors"

	live "github.com/jonathanagustin/conduit/internal/liveevents/domain"
)

// EventReadAccess is a current native authorization snapshot. It contains no
// credentials and is not browser input. Event adapters persist only its hash.
type EventReadAccess struct {
	Grants        []ResourceGrant
	Administrator bool
	Allowed       bool
}

// EventSubscriberAccess requires native administrator authority or an explicit
// resource-read capability. Per-event visibility is still checked separately.
// The carrier's grants are retained too: revoking delegation must change the
// authorization stamp even when represented-user resource grants are unchanged.
func (service *GovernanceService) EventSubscriberAccess(ctx context.Context, principal Principal, options HandlerOptions) (EventReadAccess, error) {
	grants, err := service.repository.ListResourceGrants(ctx, "")
	if err != nil {
		return EventReadAccess{}, err
	}
	access := EventReadAccess{Administrator: isGovernanceAdministrator(ctx, service, principal, options)}
	access.Allowed = access.Administrator
	teams := map[string]bool{}
	depth := 0
	for current := &principal; current != nil; current = current.OnBehalfOf {
		depth++
		if depth > 8 {
			return EventReadAccess{}, ErrForbidden
		}
		for _, team := range current.Teams {
			teams[team] = true
		}
	}
	for _, grant := range grants {
		if !teams[grant.Team] {
			continue
		}
		access.Grants = append(access.Grants, cloneJSON(grant))
		if service.Authorize(ctx, effectivePrincipal(principal), grant.ResourceRID, OperationReadResource) {
			access.Allowed = true
		}
	}
	return access, nil
}

// EventResourceVisible reuses the native single-resource/index predicates. A
// missing resource is hidden; an unavailable repository cannot authorize it.
// Packaging and assistant resources are owned by their separate adapters.
func EventResourceVisible(ctx context.Context, repository Repository, service *GovernanceService, principal Principal, options HandlerOptions, subject live.Subject) (bool, error) {
	switch subject.Type {
	case "plan":
		record, err := repository.GetPlan(ctx, subject.ID)
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if record.ChangeRequestID != "" {
			request, err := repository.GetChangeRequest(ctx, record.ChangeRequestID)
			if errors.Is(err, ErrNotFound) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			return canReadChangeRequest(ctx, service, principal, options, request), nil
		}
		return canReadPlan(ctx, repository, service, principal, options, record), nil
	case "change-request":
		request, err := repository.GetChangeRequest(ctx, subject.ID)
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return canReadChangeRequest(ctx, service, principal, options, request), nil
	case "environment":
		if _, err := repository.GetEnvironmentReport(ctx, subject.ID); errors.Is(err, ErrNotFound) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		return isGovernanceAdministrator(ctx, service, principal, options) || service.Authorize(ctx, effectivePrincipal(principal), environmentRID(subject.ID), OperationReadResource), nil
	default:
		return false, nil
	}
}
