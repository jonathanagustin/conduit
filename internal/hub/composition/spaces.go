package composition

import (
	"context"
	"errors"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	notificationhttp "github.com/jonathanagustin/conduit/internal/notifications/adapters/httpapi"
	spacehttp "github.com/jonathanagustin/conduit/internal/spaces/adapters/httpapi"
	spaceapp "github.com/jonathanagustin/conduit/internal/spaces/application"
	spaces "github.com/jonathanagustin/conduit/internal/spaces/domain"
	spaceports "github.com/jonathanagustin/conduit/internal/spaces/ports"
)

func mountSpaces(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, store spaceports.Store) (http.Handler, error) {
	if store == nil {
		return legacy, nil
	}
	service := controlplane.NewGovernanceService(repository)
	mux := http.NewServeMux()
	if err := spacehttp.Register(mux, func(r *http.Request) (*spaceapp.Service, error) {
		if _, err := operatorFrom(r.Context()); err != nil {
			var status notificationhttp.StatusError
			if errors.As(err, &status) {
				return nil, spacehttp.StatusError{Status: status.Status, Message: "operator identity could not be established"}
			}
			return nil, spacehttp.StatusError{Status: http.StatusUnauthorized, Message: "operator identity is required"}
		}
		return spaceapp.New(store, spaceAuthority{store: store, governance: service, options: options})
	}); err != nil {
		return nil, err
	}
	mux.Handle("/", legacy)
	return withOperatorCarrier(service, options, mux), nil
}

// spaceAuthority asks the same carrier-aware, audited Governance decision the
// other Hub surfaces use. Space selection and membership create no grant.
type spaceAuthority struct {
	store      spaceports.Store
	governance *controlplane.GovernanceService
	options    controlplane.HandlerOptions
}

func (authority spaceAuthority) allow(ctx context.Context, rid, operation string) (string, error) {
	carrier, err := operatorFrom(ctx)
	if err != nil {
		return "", err
	}
	if !authority.governance.AuthorizeResourceOperation(ctx, carrier, authority.options, rid, operation) {
		return "", spaces.ErrForbidden
	}
	return governance.EffectivePrincipal(carrier).Username, nil
}
func (authority spaceAuthority) RequireCreation(ctx context.Context) (string, error) {
	return authority.allow(ctx, "platform/root", governance.OperationManagePermissions)
}
func (authority spaceAuthority) RequireBinding(ctx context.Context, _ string, resource spaces.Resource) (string, error) {
	subject, err := authority.RequireCreation(ctx)
	if err != nil {
		return "", err
	}
	if err := resource.Validate(); err != nil {
		return "", err
	}
	if err := authority.RequireResourceRead(ctx, resource); err != nil {
		return "", err
	}
	return subject, nil
}
func (authority spaceAuthority) RequireResourceRead(ctx context.Context, resource spaces.Resource) error {
	if err := resource.Validate(); err != nil {
		return err
	}
	var rid string
	switch resource.Kind {
	case spaces.Environment:
		rid = "environment/" + resource.ID
	case spaces.Product:
		rid = "product/" + resource.ID
	case spaces.Bundle:
		rid = "platform/root" // Matches the existing Bundle read surface.
	}
	_, err := authority.allow(ctx, rid, governance.OperationReadResource)
	return err
}
func (authority spaceAuthority) RequireRead(ctx context.Context, id string) error {
	if _, err := authority.RequireCreation(ctx); err == nil {
		return nil
	} else if !errors.Is(err, spaces.ErrForbidden) {
		return err
	}
	bindings, err := authority.store.ListBindings(ctx, id)
	if errors.Is(err, spaces.ErrNotFound) {
		return spaces.ErrForbidden
	}
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if err := authority.RequireResourceRead(ctx, binding.Resource); err == nil {
			return nil
		} else if !errors.Is(err, spaces.ErrForbidden) {
			return err
		}
	}
	return spaces.ErrForbidden
}
