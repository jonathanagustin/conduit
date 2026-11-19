package controlplane

import "context"

// EnvironmentReportReader exposes only the existing reported-state read.
type EnvironmentReportReader interface {
	GetEnvironmentReport(context.Context, string) (EnvironmentReport, error)
}

// ReadAuthorizedEnvironmentReport shares the REST resource decision with native
// adapters. The caller authenticates the original carrier before this read.
// Like the reported-state REST endpoint, it adds no Space-selection semantics.
func ReadAuthorizedEnvironmentReport(ctx context.Context, repository EnvironmentReportReader, governance *GovernanceService, principal Principal, options HandlerOptions, environment string) (EnvironmentReport, error) {
	if !governance.AuthorizeResourceOperation(ctx, principal, options, environmentRID(environment), OperationReadResource) {
		return EnvironmentReport{}, ErrForbidden
	}
	report, err := repository.GetEnvironmentReport(ctx, environment)
	if err != nil {
		return EnvironmentReport{}, err
	}
	return report, nil
}
