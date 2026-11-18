package controlplane

import "context"

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
