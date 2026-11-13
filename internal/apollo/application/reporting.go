package application

import (
	"context"

	"github.com/jonathanagustin/conduit/internal/apollo"
	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
	environmentports "github.com/jonathanagustin/conduit/internal/environments/ports"
)

// AgentReportInputs retains the two independently posted public API snapshots.
// The compatibility context owns these transport types; native domains do not.
type AgentReportInputs struct {
	Info   []apollo.EntityInfo
	Health []apollo.EntityHealth
}

// A non-nil patch member replaces that complete snapshot, including an empty
// snapshot. A nil member leaves the other request's last committed input alone.
type AgentReportPatch struct {
	Info   *[]apollo.EntityInfo
	Health *[]apollo.EntityHealth
}

type AgentReportSnapshot struct {
	Environment string
	Inputs      AgentReportInputs
	Plan        environments.PlanProjection
	Report      environments.Report
}

// ReportingPort makes input replacement and normalized report publication one
// transaction. Every replica projects from the complete shared input set while
// holding the same storage boundary, so an older projection cannot commit last.
type ReportingPort interface {
	environmentports.ReportingPort
	UpdateAgentReports(context.Context, string, AgentReportPatch,
		func(AgentReportSnapshot) (*environments.Report, error)) error
}

func (service *Service) projectReportedState(snapshot AgentReportSnapshot) (*environments.Report, error) {
	if snapshot.Plan.ID == "" {
		return nil, nil
	}
	existing := make(map[string]environments.ReportedEntity, len(snapshot.Report.Entities))
	for _, entity := range snapshot.Report.Entities {
		existing[entity.InstallationID] = entity
	}
	now := service.now().UTC()
	reported := make([]environments.ReportedEntity, 0, len(snapshot.Plan.Steps))
	for _, step := range snapshot.Plan.Steps {
		entity := existing[step.InstallationID]
		if entity.InstallationID == "" {
			entity = environments.ReportedEntity{
				InstallationID: step.InstallationID, Product: step.Product, Version: step.Version, Namespace: step.Namespace,
				Health: environments.HealthUnknown, RolloutStatus: environments.RolloutOutOfDate,
				ConfigStatus: environments.ConfigIndeterminate, Lifecycle: environments.LifecycleUnknown,
			}
		}
		info, observed := observedInfo(snapshot.Inputs.Info, step)
		health := MatchHealth(snapshot.Inputs.Health, info, step.Product)
		if len(dereference(info.NodesInfo)) > 0 || len(health) > 0 {
			details, result := AggregateHealth(quorumFor(info, observed, entity), health)
			entity.Health, entity.HealthDetails, entity.ObservedAt = result, &details, now
			if info.K8sNamespace != nil && *info.K8sNamespace != "" {
				entity.Namespace = *info.K8sNamespace
			}
			if len(health) > 0 {
				entity.Version = health[0].ProductVersion
			}
			if len(dereference(info.NodesInfo)) > 0 {
				entity.Lifecycle = environments.LifecycleInstalled
			}
		}
		reported = append(reported, entity)
	}
	return &environments.Report{
		Environment: snapshot.Environment, AgentID: "apollo-public-api", ObservedAt: now, Entities: reported,
	}, nil
}
