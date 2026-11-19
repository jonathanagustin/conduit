package controlplane

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrInvalidFleetReadQuery distinguishes query validation from store failures.
var ErrInvalidFleetReadQuery = errors.New("invalid Fleet read query")

// FleetReadRequest is the native read input, without caller-supplied authority,
// observation clocks or declarations that reporting silence should be expected.
type FleetReadRequest struct {
	Environment, InstallationID, Product, Health, RolloutStatus, ConfigStatus, Lifecycle, Search string
	Cursor, SpaceID                                                                              string
	Limit, MaxAgeSeconds                                                                         *int32
}

// ReadAuthorizedFleetPage uses the original carrier, current Space membership
// and Environment authority before projecting results, summaries and facets.
func ReadAuthorizedFleetPage(ctx context.Context, repository FleetProjectionReader, governance *GovernanceService, principal Principal, options HandlerOptions, request FleetReadRequest) (FleetProjection, error) {
	return readAuthorizedFleetPageWithClock(ctx, repository, governance, principal, options, request, time.Now)
}

func readAuthorizedFleetPageWithClock(ctx context.Context, repository FleetProjectionReader, governance *GovernanceService, principal Principal, options HandlerOptions, request FleetReadRequest, clock func() time.Time) (FleetProjection, error) {
	if err := authorizeIndexSpaceSelection(ctx, principal, options, request.SpaceID); err != nil {
		return FleetProjection{}, err
	}
	space, err := loadIndexSpaceMembership(ctx, principal, options, request.SpaceID)
	if err != nil {
		return FleetProjection{}, err
	}
	maxAge := time.Duration(0)
	if request.MaxAgeSeconds != nil {
		seconds := *request.MaxAgeSeconds
		if seconds < 1 || seconds > 86400 {
			return FleetProjection{}, fmt.Errorf("%w: maxAgeSeconds must be between 1 and 86400", ErrInvalidFleetReadQuery)
		}
		maxAge = time.Duration(seconds) * time.Second
	}
	limit := defaultPageLimit
	if request.Limit != nil {
		limit = int(*request.Limit)
	}
	if limit < 1 || limit > maxPageLimit {
		return FleetProjection{}, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidFleetReadQuery, maxPageLimit)
	}
	query := FleetQuery{Environment: request.Environment, InstallationID: request.InstallationID, Product: request.Product, Health: request.Health, Rollout: request.RolloutStatus, Config: request.ConfigStatus, Lifecycle: request.Lifecycle, Search: request.Search, MaxAge: maxAge}
	retain := fleetReadVisibility(ctx, governance, principal, options, space)
	// Match the REST stage order: resolve visibility before sampling the clock.
	projection, err := buildFleetProjection(ctx, repository, query, clock(), retain)
	if err != nil {
		return FleetProjection{}, err
	}
	projection, err = paginateFleetProjectionInSpace(projection, query, request.Cursor, limit, space.id)
	if err != nil {
		return FleetProjection{}, fmt.Errorf("%w: %v", ErrInvalidFleetReadQuery, err)
	}
	return projection, nil
}
