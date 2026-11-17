package composition

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/maintenance"
	maintenancehttp "github.com/jonathanagustin/conduit/internal/delivery/maintenance/httpapi"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/httpidentity"
)

func mountTemporaryMaintenance(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) (http.Handler, error) {
	mux := http.NewServeMux()
	err := maintenancehttp.Register(mux, maintenancehttp.Config{
		BindOperator: func(r *http.Request) (maintenance.OverrideController, error) {
			actor, err := operatorFrom(r.Context())
			if err != nil {
				return nil, maintenancehttp.StatusError{Status: 401, Message: "operator identity required"}
			}
			return temporaryMaintenanceOperator{controlplane.NewTemporaryMaintenanceService(repository, options, actor)}, nil
		},
		BindAgent: func(r *http.Request) (maintenancehttp.AgentController, error) {
			identity, err := httpidentity.AgentIdentityFromRequest(r, options.AllowInsecureAgentIdentityHeaders)
			if err != nil {
				return nil, maintenancehttp.StatusError{Status: 401, Message: "Agent identity required"}
			}
			if environment := r.PathValue("environmentID"); environment != "" && environment != identity.Environment {
				return nil, maintenancehttp.StatusError{Status: 403, Message: "Agent Environment mismatch"}
			}
			return temporaryMaintenanceAgent{controlplane.NewTemporaryMaintenanceAdmission(repository), identity.Environment, identity.AgentID}, nil
		},
	})
	if err != nil {
		return nil, err
	}
	mux.Handle("/", legacy)
	return withOperatorCarrier(newGovernanceService(repository, options), options, mux), nil
}

type temporaryMaintenanceOperator struct {
	core *controlplane.TemporaryMaintenanceService
}

func (c temporaryMaintenanceOperator) CreateMaintenanceOverrides(ctx context.Context, draft maintenance.OverrideDraft) (maintenance.OverrideBatch, error) {
	result, err := c.core.CreateMaintenanceOverrides(ctx, draft)
	return result, temporaryMaintenanceError(err)
}

type temporaryMaintenanceAgent struct {
	core                 *controlplane.TemporaryMaintenanceAdmission
	environment, agentID string
}

func (c temporaryMaintenanceAgent) Lease(ctx context.Context, duration time.Duration) (any, error) {
	result, err := c.core.Lease(ctx, c.environment, c.agentID, duration)
	return result, temporaryMaintenanceError(err)
}
func (c temporaryMaintenanceAgent) Start(ctx context.Context, id, token string) (any, error) {
	result, err := c.core.Start(ctx, c.environment, c.agentID, id, token)
	result.Lease = nil
	return result, temporaryMaintenanceError(err)
}
func temporaryMaintenanceError(err error) error {
	if err == nil {
		return nil
	}
	status, message := 500, "temporary maintenance operation unavailable"
	switch {
	case errors.Is(err, controlplane.ErrForbidden):
		status, message = 403, "temporary maintenance operation forbidden"
	case errors.Is(err, controlplane.ErrNotFound):
		status, message = 404, "temporary maintenance target not found"
	case errors.Is(err, controlplane.ErrNoPlanAvailable):
		status, message = 204, ""
	case errors.Is(err, controlplane.ErrInvalidTransition):
		status, message = 400, "invalid temporary maintenance operation"
	case errors.Is(err, controlplane.ErrConflict), errors.Is(err, controlplane.ErrLeaseRejected), errors.Is(err, controlplane.ErrPlanCancelled):
		status, message = 409, "temporary maintenance admission is stale or blocked"
	}
	return maintenancehttp.StatusError{Status: status, Message: message}
}
