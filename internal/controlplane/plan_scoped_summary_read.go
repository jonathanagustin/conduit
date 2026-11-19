package controlplane

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrInvalidScopedPlanSummarySelection = errors.New("invalid scoped Plan summary Space selection")
	ErrScopedPlanSummaryUnavailable      = errors.New("scoped Plan summary unavailable")
	// Lifecycle failures must not be relabeled as input/configuration errors when
	// a configured callback or repository happens to return a similarly named error.
	ErrScopedPlanSummaryLifecycleFailure = errors.New("scoped Plan summary lifecycle read failed")
)

// ReadAuthorizedPlanSummaryPageInSpace narrows existing Plan visibility with a
// required readable native Space. Authentication belongs to the caller. Space
// selection and private membership precede limits, then private Plan reads, as
// in the native REST index. Membership grants no read authority over a Plan.
func ReadAuthorizedPlanSummaryPageInSpace(ctx context.Context, repository PlanIndexReader, governance *GovernanceService, principal Principal, options HandlerOptions, spaceID string, query PlanQuery, cursor string, requestedLimit *int32) (PlanSummaryPage, error) {
	if !nativeSpaceSelectionID.MatchString(spaceID) {
		return PlanSummaryPage{}, ErrInvalidScopedPlanSummarySelection
	}
	if options.RequireSpaceSelection == nil || options.SpaceIDLookup == nil {
		return PlanSummaryPage{}, ErrScopedPlanSummaryUnavailable
	}
	if err := authorizeIndexSpaceSelection(ctx, principal, options, spaceID); err != nil {
		if errors.Is(err, ErrSpaceSelectionUnavailable) {
			return PlanSummaryPage{}, fmt.Errorf("%w: %w", ErrScopedPlanSummaryUnavailable, err)
		}
		return PlanSummaryPage{}, fmt.Errorf("%w: %w", ErrScopedPlanSummaryLifecycleFailure, err)
	}
	if options.LoadSpaceIndexMembership == nil {
		return PlanSummaryPage{}, ErrScopedPlanSummaryUnavailable
	}
	space, err := loadIndexSpaceMembership(ctx, principal, options, spaceID)
	if err != nil {
		return PlanSummaryPage{}, fmt.Errorf("%w: %w", ErrScopedPlanSummaryLifecycleFailure, err)
	}
	limit, err := ResolvePlanSummaryLimit(requestedLimit)
	if err != nil {
		return PlanSummaryPage{}, err
	}
	page, err := readAuthorizedPlanPageInSpace(ctx, repository, governance, principal, options, normalizePlanSummaryQuery(query), cursor, limit, space)
	var pagination planIndexPageError
	if err != nil && !errors.As(err, &pagination) {
		return PlanSummaryPage{}, fmt.Errorf("%w: %w", ErrScopedPlanSummaryLifecycleFailure, err)
	}
	return planSummaryPageResult(page, err)
}
