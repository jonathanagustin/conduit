package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidPlanSummaryQuery identifies a public index validation failure,
// independently of repository failures with potentially private diagnostics.
var ErrInvalidPlanSummaryQuery = errors.New("invalid Plan summary query")

type planIndexPageError struct{ cause error }

func (err planIndexPageError) Error() string { return err.cause.Error() }
func (err planIndexPageError) Unwrap() error { return err.cause }

// ResolvePlanSummaryLimit preserves absent versus explicitly invalid limits.
func ResolvePlanSummaryLimit(value *int32) (int, error) {
	limit := defaultPageLimit
	if value != nil {
		limit = int(*value)
	}
	if err := validatePlanSummaryLimit(limit); err != nil {
		return 0, err
	}
	return limit, nil
}

func validatePlanSummaryLimit(limit int) error {
	if limit < 1 || limit > maxPageLimit {
		return fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidPlanSummaryQuery, maxPageLimit)
	}
	return nil
}

// ReadAuthorizedPlanSummaryPage is the existing native summary index. Its
// principal is the original resolved carrier, including any delegation.
func ReadAuthorizedPlanSummaryPage(
	ctx context.Context, repository Repository, governance *GovernanceService,
	principal Principal, options HandlerOptions, query PlanQuery, cursor string, limit int,
) (PlanSummaryPage, error) {
	if err := validatePlanSummaryLimit(limit); err != nil {
		return PlanSummaryPage{}, err
	}
	query.View = "summary"
	query.Environment = strings.TrimSpace(query.Environment)
	query.Status = strings.TrimSpace(query.Status)
	query.InstallationID = strings.TrimSpace(query.InstallationID)
	page, err := readAuthorizedPlanPage(ctx, repository, governance, principal, options, query, cursor, limit)
	var pagination planIndexPageError
	if errors.As(err, &pagination) {
		return PlanSummaryPage{}, fmt.Errorf("%w: %v", ErrInvalidPlanSummaryQuery, err)
	}
	if err != nil {
		return PlanSummaryPage{}, err
	}
	return summarizePlanPage(page), nil
}

func readAuthorizedPlanPage(
	ctx context.Context, repository Repository, governance *GovernanceService,
	principal Principal, options HandlerOptions, query PlanQuery, cursor string, limit int,
) (PlanPage, error) {
	return readAuthorizedPlanPageInSpace(ctx, repository, governance, principal, options, query, cursor, limit, indexSpace{})
}

func readAuthorizedPlanPageInSpace(ctx context.Context, repository Repository, governance *GovernanceService, principal Principal, options HandlerOptions, query PlanQuery, cursor string, limit int, space indexSpace) (PlanPage, error) {
	records, err := repository.ListPlans(ctx, "")
	if err != nil {
		return PlanPage{}, err
	}
	visible := make([]PlanRecord, 0, len(records))
	for _, record := range records {
		if !canReadPlan(ctx, repository, governance, principal, options, record) || !space.environment(record.Plan.Environment) {
			continue
		}
		visible = append(visible, redactLeaseToken(record))
	}
	page, err := paginatePlansInSpace(visible, query, cursor, limit, space.id)
	if err != nil {
		return PlanPage{}, planIndexPageError{cause: err}
	}
	return page, nil
}
