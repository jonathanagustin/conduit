package composition

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	releaseapp "github.com/jonathanagustin/conduit/internal/delivery/releasestatus/application"
	releasedomain "github.com/jonathanagustin/conduit/internal/delivery/releasestatus/domain"
	releasehttp "github.com/jonathanagustin/conduit/internal/delivery/releasestatus/httpapi"
	wavedomain "github.com/jonathanagustin/conduit/internal/delivery/waves/domain"
)

type releaseRolloutPrincipalKey struct{}

type releaseRolloutSource struct {
	repository controlplane.Repository
	waves      func() []wavedomain.Rollout
}

// A single plans/reports read is reused by the existing catalog/fleet builders.
// Publication and in-memory wave reads are independent, not a SQL transaction.
type releaseReadSnapshot struct {
	controlplane.Repository
	plans   []controlplane.PlanRecord
	reports []controlplane.EnvironmentReport
}

func (s releaseReadSnapshot) ListPlans(context.Context, string) ([]controlplane.PlanRecord, error) {
	return s.plans, nil
}
func (s releaseReadSnapshot) ListEnvironmentReports(context.Context) ([]controlplane.EnvironmentReport, error) {
	return s.reports, nil
}

func (s releaseRolloutSource) Read(ctx context.Context, product, version string, at time.Time) (releasedomain.Snapshot, error) {
	plans, err := s.repository.ListPlans(ctx, "")
	if err != nil {
		return releasedomain.Snapshot{}, err
	}
	snapshot := releaseReadSnapshot{Repository: s.repository, plans: plans}
	catalog, err := controlplane.ListProductCatalog(ctx, snapshot)
	if err != nil {
		return releasedomain.Snapshot{}, err
	}
	result := releasedomain.Snapshot{}
	for _, record := range catalog {
		if record.Product == product {
			for _, release := range record.Releases {
				if release.Version == version {
					result.Found = true
				}
			}
		}
	}
	if !result.Found {
		return result, nil
	}
	snapshot.reports, err = s.repository.ListEnvironmentReports(ctx)
	if err != nil {
		return releasedomain.Snapshot{}, err
	}
	fleet, err := controlplane.BuildFleetProjection(ctx, snapshot, controlplane.FleetQuery{Product: product}, at)
	if err != nil {
		return releasedomain.Snapshot{}, err
	}
	for _, row := range fleet.Installations {
		result.Installations = append(result.Installations, releasedomain.Installation{Environment: row.Environment, InstallationID: row.InstallationID, Product: row.Product, Namespace: row.Namespace, DesiredVersion: row.DesiredVersion, ObservedVersion: row.ObservedVersion, DesiredReleaseDigest: row.DesiredReleaseDigest, ObservedReleaseDigest: row.ObservedReleaseDigest, DesiredConfigDigest: row.DesiredConfigDigest, ObservedConfigDigest: row.ObservedConfigDigest, Health: row.Health, RolloutStatus: row.RolloutStatus, ConfigStatus: row.ConfigStatus, Lifecycle: row.Lifecycle, ObservedAt: row.ObservedAt, ObservationAgeSeconds: row.ObservationAgeSeconds, Stale: row.Stale})
	}
	latest := map[string]controlplane.PlanRecord{}
	for _, plan := range plans {
		previous, found := latest[plan.Plan.Environment]
		if !found || previous.CreatedAt.Before(plan.CreatedAt) || (previous.CreatedAt.Equal(plan.CreatedAt) && previous.ID < plan.ID) {
			latest[plan.Plan.Environment] = plan
		}
	}
	for environment, plan := range latest {
		if plan.DesiredState == nil {
			continue
		}
		for _, settings := range plan.DesiredState.ProductSettings {
			if settings.Product != product || settings.PromotionPipeline == nil {
				continue
			}
			graph := releasedomain.Graph{Environment: environment, Product: product, PlanID: plan.ID, Enabled: settings.PromotionPipeline.Enabled, Edges: []releasedomain.Edge{}}
			for _, stage := range settings.PromotionPipeline.Stages {
				graph.Edges = append(graph.Edges, releasedomain.Edge{FromChannel: stage.FromChannel, ToChannel: stage.ToChannel})
			}
			result.Graphs = append(result.Graphs, graph)
		}
	}
	if s.waves == nil {
		return releasedomain.Snapshot{}, releasedomain.ErrUnavailable
	}
	for _, rollout := range s.waves() {
		if rollout.Product != product || rollout.Version != version {
			continue
		}
		copy := releasedomain.Rollout{ID: rollout.ID, Product: rollout.Product, Version: rollout.Version, Status: rollout.Status, Waves: []releasedomain.Wave{}}
		for _, wave := range rollout.Waves {
			copy.Waves = append(copy.Waves, releasedomain.Wave{Name: wave.Name, Environments: append([]string{}, wave.Environments...), Gate: wave.Gate, Status: wave.Status})
		}
		result.Rollouts = append(result.Rollouts, copy)
	}
	return result, nil
}

func registerReleaseRollout(mux *http.ServeMux, repository controlplane.Repository, options controlplane.HandlerOptions, waves func() []wavedomain.Rollout) {
	governance := controlplane.NewGovernanceService(repository)
	service := releaseapp.Service{Source: releaseRolloutSource{repository: repository, waves: waves}}
	releasehttp.Register(mux, releasehttp.Config{
		Authorize: func(r *http.Request) int {
			// Preserve carrier/delegation in the native principal, but require an actual
			// operator identity even when localhost development headers are enabled.
			if options.OperatorIdentityResolver == nil && strings.TrimSpace(r.Header.Get("X-Platform-Subject")) == "" {
				return http.StatusUnauthorized
			}
			principal, ok := packagingPrincipal(r, governance, options)
			if !ok {
				return http.StatusUnauthorized
			}
			if !governance.AuthorizeResourceOperation(r.Context(), principal, options, "product/"+r.PathValue("product"), controlplane.OperationReadResource) {
				return http.StatusForbidden
			}
			*r = *r.WithContext(context.WithValue(r.Context(), releaseRolloutPrincipalKey{}, principal))
			return 0
		},
		Read: func(r *http.Request, product, version string) (releasedomain.View, error) {
			principal, ok := r.Context().Value(releaseRolloutPrincipalKey{}).(controlplane.Principal)
			if !ok {
				return releasedomain.View{}, releasedomain.ErrUnavailable
			}
			return service.Read(r.Context(), product, version, func(environment string) bool {
				return governance.AuthorizeResourceOperation(r.Context(), principal, options, "environment/"+environment, controlplane.OperationReadResource)
			})
		},
	})
}
