package controlplane

import (
	"context"
	"errors"
	reporting "github.com/jonathanagustin/conduit/internal/reporting/domain"
	"strings"
	"time"
)

var errInvalidFleetCursor = errors.New("invalid fleet cursor")

// Fleet view types now live in internal/reporting, which owns the projection.
// These aliases keep the HTTP layer and its callers spelling them as before; an
// alias is the same type, so nothing converts at the boundary.
type (
	FleetQuery        = reporting.FleetQuery
	FleetInstallation = reporting.FleetInstallation
	FleetSummary      = reporting.FleetSummary
	FleetFacets       = reporting.FleetFacets
	FleetProjection   = reporting.FleetProjection
)

// BuildFleetProjection reads the fleet and hands it to Reporting.
//
// The reading stays here because the repository does, and the projection does
// not: Reporting states the narrow shape it needs and this converts into it.
// That is the direction that keeps the dependency one-way — the monolith knows
// about the context, and the context knows nothing about the monolith.
func BuildFleetProjection(
	ctx context.Context, repository Repository, query FleetQuery, now time.Time,
) (FleetProjection, error) {
	return buildFleetProjection(ctx, repository, query, now, nil)
}

func buildFleetProjection(ctx context.Context, repository Repository, query FleetQuery, now time.Time, retainEnvironment func(string) bool) (FleetProjection, error) {
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	plans, err := repository.ListPlans(ctx, "")
	if err != nil {
		return FleetProjection{}, err
	}
	reports, err := repository.ListEnvironmentReports(ctx)
	if err != nil {
		return FleetProjection{}, err
	}

	if retainEnvironment != nil {
		keptPlans := make([]PlanRecord, 0, len(plans))
		for _, plan := range plans {
			if retainEnvironment(plan.Plan.Environment) {
				keptPlans = append(keptPlans, plan)
			}
		}
		plans = keptPlans
		keptReports := make([]EnvironmentReport, 0, len(reports))
		for _, report := range reports {
			if retainEnvironment(report.Environment) {
				keptReports = append(keptReports, report)
			}
		}
		reports = keptReports
	}

	// Which Environments expect gaps is a property of the Plans just loaded,
	// not a caller's choice, so it is derived here rather than accepted from
	// the query. It was derived in the console overview and nowhere else, so
	// the operator-facing fleet API answered a maxAgeSeconds question by
	// marking every installation in an intermittent Environment stale and
	// counting it in Summary.Stale -- the "a gap is an outage" reading the
	// published behaviour says is wrong for that connection setting. Two
	// surfaces, two answers about the same Environment.
	query.SilenceExpected = expectedSilenceEnvironments(plans)

	desired, observed := fleetInputs(plans, reports)
	return reporting.BuildFleet(desired, observed, query, now), nil
}

// fleetInputs converts what the repository holds into the narrow shape
// Reporting asks for.
//
// It exists once because two callers need it, and a second copy is how the
// overview and the fleet view come to disagree about the same fleet.
func fleetInputs(plans []PlanRecord, reports []EnvironmentReport) ([]reporting.Desired, []reporting.Observed) {
	var desired []reporting.Desired
	for environment, record := range latestPlanRecords(plans) {
		for _, step := range record.Plan.Steps {
			desired = append(desired, reporting.Desired{
				Environment: environment, InstallationID: step.InstallationID,
				Product: step.Product, Namespace: step.Namespace, Version: step.Version,
				ReleaseDigest: step.ReleaseDigest, ConfigDigest: step.ConfigDigest,
			})
		}
	}

	var observed []reporting.Observed
	for _, report := range reports {
		for _, entity := range report.Entities {
			observed = append(observed, reporting.Observed{
				Environment: report.Environment, InstallationID: entity.InstallationID,
				Product: entity.Product, Namespace: entity.Namespace, Version: entity.Version,
				ReleaseDigest: entity.ReleaseDigest, ConfigDigest: entity.ConfigDigest,
				Health: entity.Health, RolloutStatus: entity.RolloutStatus,
				ConfigStatus: entity.ConfigStatus, Lifecycle: entity.Lifecycle,
				ObservedAt: entity.ObservedAt, ReportedAt: report.ObservedAt,
			})
		}
	}

	return desired, observed
}

func latestPlanRecords(plans []PlanRecord) map[string]PlanRecord {
	latest := make(map[string]PlanRecord)
	for _, plan := range plans {
		existing, found := latest[plan.Plan.Environment]
		if !found || existing.CreatedAt.Before(plan.CreatedAt) ||
			(existing.CreatedAt.Equal(plan.CreatedAt) && existing.ID < plan.ID) {
			latest[plan.Plan.Environment] = plan
		}
	}
	return latest
}

func paginateFleetProjection(projection FleetProjection, query FleetQuery, rawCursor string, limit int) (FleetProjection, error) {
	return paginateFleetProjectionInSpace(projection, query, rawCursor, limit, "")
}

func paginateFleetProjectionInSpace(projection FleetProjection, query FleetQuery, rawCursor string, limit int, spaceID string) (FleetProjection, error) {
	fingerprint := fleetQueryFingerprintInSpace(query, spaceID)
	page, metadata, err := paginateOrdered(
		projection.Installations,
		func(item FleetInstallation) string { return reporting.Key(item.Environment, item.InstallationID) },
		fingerprint, rawCursor, limit, errInvalidFleetCursor,
	)
	if err != nil {
		return FleetProjection{}, err
	}
	projection.Installations = page
	projection.PageMetadata = reporting.PageMetadata(metadata)
	return projection, nil
}

func fleetQueryFingerprint(query FleetQuery) string {
	return fleetQueryFingerprintInSpace(query, "")
}

func fleetQueryFingerprintInSpace(query FleetQuery, spaceID string) string {
	normalized := struct {
		Environment    string `json:"environment"`
		InstallationID string `json:"installationId,omitempty"`
		Product        string `json:"product"`
		Health         string `json:"health"`
		Rollout        string `json:"rollout"`
		Config         string `json:"config"`
		Lifecycle      string `json:"lifecycle"`
		Search         string `json:"search"`
		MaxAge         int64  `json:"maxAgeSeconds"`
	}{query.Environment, query.InstallationID, query.Product, query.Health, query.Rollout, query.Config, query.Lifecycle,
		strings.ToLower(strings.TrimSpace(query.Search)), int64(query.MaxAge / time.Second)}
	return fingerprintInSpace(normalized, spaceID)
}
