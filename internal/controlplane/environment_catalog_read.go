package controlplane

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrInvalidEnvironmentCatalogQuery identifies typed input failures only.
var ErrInvalidEnvironmentCatalogQuery = errors.New("invalid Environment catalog query")

// ErrEnvironmentCatalogUnavailable identifies missing Space capabilities,
// independently of errors returned by configured callbacks or catalog readers.
var ErrEnvironmentCatalogUnavailable = errors.New("Environment catalog unavailable")

// ReadAuthorizedEnvironmentCatalogPage serves the native full-record index.
// Authentication belongs to the caller. Space selection precedes private reads;
// resource visibility and grouping precede search, totals and pagination. The
// observation clock is sampled after reads, as for the native full REST index.
func ReadAuthorizedEnvironmentCatalogPage(ctx context.Context, repository EnvironmentCatalogReader, governance *GovernanceService, principal Principal, options HandlerOptions, search, cursor string, spaceID *string, requestedLimit *int32, now func() time.Time) (EnvironmentCatalogPage, error) {
	id := ""
	if spaceID != nil {
		if !nativeSpaceSelectionID.MatchString(*spaceID) {
			return EnvironmentCatalogPage{}, fmt.Errorf("%w: %v", ErrInvalidEnvironmentCatalogQuery, ErrInvalidSpaceSelection)
		}
		id = *spaceID
	}
	if id != "" && (options.RequireSpaceSelection == nil || options.SpaceIDLookup == nil) {
		return EnvironmentCatalogPage{}, ErrEnvironmentCatalogUnavailable
	}
	if err := authorizeIndexSpaceSelection(ctx, principal, options, id); err != nil {
		return EnvironmentCatalogPage{}, err
	}
	if id != "" && options.LoadSpaceIndexMembership == nil {
		return EnvironmentCatalogPage{}, ErrEnvironmentCatalogUnavailable
	}
	space, err := loadIndexSpaceMembership(ctx, principal, options, id)
	if err != nil {
		return EnvironmentCatalogPage{}, err
	}
	records, err := listAuthorizedEnvironmentCatalogInSpace(ctx, repository, governance, principal, options, space)
	if err != nil {
		return EnvironmentCatalogPage{}, err
	}
	limit := fullCatalogPageLimit
	if requestedLimit != nil {
		limit = int(*requestedLimit)
	}
	if limit < 1 || limit > fullCatalogPageLimit {
		return EnvironmentCatalogPage{}, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidEnvironmentCatalogQuery, fullCatalogPageLimit)
	}
	page, err := paginateEnvironmentCatalogInSpace(records, search, cursor, limit, space.id)
	if err != nil {
		return EnvironmentCatalogPage{}, fmt.Errorf("%w: %v", ErrInvalidEnvironmentCatalogQuery, err)
	}
	if now == nil {
		now = time.Now
	}
	attachSpokeLiveness(page.Environments, now().UTC())
	return page, nil
}
