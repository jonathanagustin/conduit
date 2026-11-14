package composition

import (
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	overridehttp "github.com/jonathanagustin/conduit/internal/installoverride/adapters/httpapi"
	override "github.com/jonathanagustin/conduit/internal/installoverride/domain"
	"github.com/jonathanagustin/conduit/internal/installoverride/ports"
	"net/http"
)

func mountModuleOverrides(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) (http.Handler, error) {
	mux := http.NewServeMux()
	err := overridehttp.Register(mux, overridehttp.Config{Bind: func(r *http.Request) (ports.ModuleController, error) {
		actor, err := operatorFrom(r.Context())
		if err != nil {
			return nil, overridehttp.StatusError{Status: 401, Message: "operator identity could not be established"}
		}
		return moduleOverrideController{controlplane.NewModuleOverrideService(repository, options, actor)}, nil
	}})
	if err != nil {
		return nil, err
	}
	mux.Handle("/", legacy)
	return withOperatorCarrier(controlplane.NewGovernanceService(repository), options, mux), nil
}

type moduleOverrideController struct {
	core *controlplane.ModuleOverrideService
}

func (c moduleOverrideController) ProposeModuleOverride(ctx context.Context, draft ports.ModuleDraft, dryRun bool) (ports.ModuleReview, error) {
	result, err := c.core.ProposeModuleOverride(ctx, draft, dryRun)
	return result, moduleOverrideError(err)
}
func (c moduleOverrideController) ApplyModuleOverride(ctx context.Context, id string) (ports.ModuleAdmission, error) {
	result, err := c.core.ApplyModuleOverride(ctx, id)
	return result, moduleOverrideError(err)
}
func moduleOverrideError(err error) error {
	if err == nil {
		return nil
	}
	status, message := 500, "Module override operation unavailable"
	var validation controlplane.ValidationErrors
	switch {
	case errors.Is(err, controlplane.ErrUnauthorized):
		status, message = 401, "operator identity required"
	case errors.Is(err, controlplane.ErrForbidden):
		status, message = 403, "Module override operation forbidden"
	case errors.Is(err, controlplane.ErrNotFound):
		status, message = 404, "Module override target or review not found"
	case errors.Is(err, override.ErrInvalid), errors.Is(err, controlplane.ErrInvalidTransition), errors.As(err, &validation):
		status, message = 400, "invalid Module override intent or review"
	case errors.Is(err, override.ErrBlocked), errors.Is(err, override.ErrCorrupt), errors.Is(err, controlplane.ErrConflict):
		status, message = 409, "Module override review is stale, blocked or inconsistent"
	}
	return overridehttp.StatusError{Status: status, Message: message}
}
