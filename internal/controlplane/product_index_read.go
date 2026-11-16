package controlplane

import (
	"context"
	"errors"
	"fmt"
)

// ErrInvalidProductSummaryQuery distinguishes public input failures from store diagnostics.
var ErrInvalidProductSummaryQuery = errors.New("invalid Product summary query")

// ResolveProductSummaryLimit preserves absent versus explicitly invalid limits.
func ResolveProductSummaryLimit(value *int32) (int, error) {
	limit := defaultPageLimit
	if value != nil {
		limit = int(*value)
	}
	if err := validateProductSummaryLimit(limit); err != nil {
		return 0, err
	}
	return limit, nil
}

func validateProductSummaryLimit(limit int) error {
	if limit < 1 || limit > maxPageLimit {
		return fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidProductSummaryQuery, maxPageLimit)
	}
	return nil
}

// ReadAuthorizedProductSummaryPage shares the native REST visibility and pagination.
// The original credential carrier is retained for the administrator decision.
func ReadAuthorizedProductSummaryPage(ctx context.Context, repository Repository, governance *GovernanceService, principal Principal, options HandlerOptions, search, cursor string, limit int) (ProductCatalogSummaryPage, error) {
	if err := validateProductSummaryLimit(limit); err != nil {
		return ProductCatalogSummaryPage{}, err
	}
	records, err := listAuthorizedProductCatalog(ctx, repository, governance, principal, options)
	if err != nil {
		return ProductCatalogSummaryPage{}, err
	}
	page, err := paginateProductCatalog(records, search, cursor, limit)
	if err != nil {
		return ProductCatalogSummaryPage{}, fmt.Errorf("%w: %v", ErrInvalidProductSummaryQuery, err)
	}
	return summarizeProductCatalog(page), nil
}

func listAuthorizedProductCatalog(ctx context.Context, repository Repository, governance *GovernanceService, principal Principal, options HandlerOptions) ([]ProductCatalogRecord, error) {
	records, err := ListProductCatalog(ctx, repository)
	if err != nil {
		return nil, err
	}
	visible := make([]ProductCatalogRecord, 0, len(records))
	administrator := isGovernanceAdministrator(ctx, governance, principal, options)
	for _, record := range records {
		if administrator || governance.Authorize(ctx, effectivePrincipal(principal), productRID(record.Product), OperationReadResource) {
			visible = append(visible, record)
		}
	}
	return visible, nil
}
