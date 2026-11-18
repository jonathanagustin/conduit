package controlplane

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrInvalidEnvironmentSummaryQuery identifies public index input failures.
var ErrInvalidEnvironmentSummaryQuery = errors.New("invalid Environment summary query")

// ReadAuthorizedEnvironmentSummaryPage retains the native principal, resource
// filtering and Space-bound cursor. The observation clock comes from the Hub.
func ReadAuthorizedEnvironmentSummaryPage(ctx context.Context, repository Repository, governance *GovernanceService, principal Principal, options HandlerOptions, search, cursor, spaceID string, requestedLimit *int32, now time.Time) (EnvironmentCatalogSummaryPage, error) {
	if err := authorizeIndexSpaceSelection(ctx, principal, options, spaceID); err != nil {
		return EnvironmentCatalogSummaryPage{}, err
	}
	space, err := loadIndexSpaceMembership(ctx, principal, options, spaceID)
	if err != nil {
		return EnvironmentCatalogSummaryPage{}, err
	}
	records, err := listAuthorizedEnvironmentCatalogInSpace(ctx, repository, governance, principal, options, space)
	if err != nil {
		return EnvironmentCatalogSummaryPage{}, err
	}
	limit := defaultPageLimit
	if requestedLimit != nil {
		limit = int(*requestedLimit)
	}
	if limit < 1 || limit > maxPageLimit {
		return EnvironmentCatalogSummaryPage{}, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidEnvironmentSummaryQuery, maxPageLimit)
	}
	page, err := paginateEnvironmentCatalogInSpace(records, search, cursor, limit, space.id)
	if err != nil {
		return EnvironmentCatalogSummaryPage{}, fmt.Errorf("%w: %v", ErrInvalidEnvironmentSummaryQuery, err)
	}
	attachSpokeLiveness(page.Environments, now.UTC())
	return summarizeEnvironmentCatalog(page), nil
}

// listAuthorizedEnvironmentCatalogInSpace preserves the native index visibility
// before any search, pagination, totals or summary projection. Space membership
// narrows the result; it never grants read authority over an Environment.
func listAuthorizedEnvironmentCatalogInSpace(ctx context.Context, repository Repository, governance *GovernanceService, principal Principal, options HandlerOptions, space indexSpace) ([]EnvironmentCatalogRecord, error) {
	records, err := ListEnvironmentCatalog(ctx, repository)
	if err != nil {
		return nil, err
	}
	visible := make([]EnvironmentCatalogRecord, 0, len(records))
	administrator := isGovernanceAdministrator(ctx, governance, principal, options)
	for _, record := range records {
		if administrator || governance.AuthorizeResourceOperation(ctx, principal, options, environmentRID(record.Environment.ID), OperationReadResource) {
			if !space.environment(record.Environment.ID) {
				continue
			}
			visible = append(visible, record)
		}
	}
	return visible, nil
}
