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
	service := newGovernanceService(repository, options)
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

// withSpaceLookup binds GraphQL's narrow lookup to the same original carrier and
// canonical resource-read operation as other Hub reads. Assignment is not access.
func withSpaceLookup(repository controlplane.Repository, options controlplane.HandlerOptions, store spaceports.Store) controlplane.HandlerOptions {
	if store == nil {
		return options
	}
	authorizationOptions := options
	service := newGovernanceService(repository, options)
	options.SpaceIDLookup = func(ctx context.Context, carrier controlplane.Principal, kind, id string) (string, error) {
		resource := spaces.Resource{Kind: kind, ID: id}
		if err := resource.Validate(); err != nil {
			return "", err
		}
		rid := "platform/root"
		switch kind {
		case spaces.Environment:
			rid = "environment/" + id
		case spaces.Product:
			rid = "product/" + id
		}
		if !service.AuthorizeResourceOperation(ctx, carrier, authorizationOptions, rid, governance.OperationReadResource) {
			return "", spaces.ErrForbidden
		}
		binding, err := store.GetBinding(ctx, resource)
		if errors.Is(err, spaces.ErrNotFound) {
			return "", nil
		}
		if err != nil {
			return "", errors.New("native Space assignment could not be read")
		}
		return binding.SpaceID, nil
	}
	bind := func() (*spaceapp.Service, error) {
		return spaceapp.New(store, spaceAuthority{store: store, governance: service, options: authorizationOptions})
	}
	options.RequireSpaceSelection = func(ctx context.Context, carrier controlplane.Principal, id string) error {
		ctx = context.WithValue(ctx, operatorCarrierKey{}, func() (governance.Principal, error) { return carrier, nil })
		spacesService, err := bind()
		if err != nil {
			return spaceSelectionError(err)
		}
		_, err = spacesService.Get(ctx, id)
		return spaceSelectionError(err)
	}
	// The HTTP adapter already validated caller visibility of this Space. Load
	// its immutable anchors once, without adding Environment-read requirements
	// to Plans/change requests the caller may read through review authority.
	options.LoadSpaceIndexMembership = func(ctx context.Context, _ controlplane.Principal, id string) (controlplane.SpaceIndexMembership, error) {
		bindings, err := store.ListBindings(ctx, id)
		if err != nil {
			return controlplane.SpaceIndexMembership{}, err
		}
		var result controlplane.SpaceIndexMembership
		for _, binding := range bindings {
			switch binding.Resource.Kind {
			case spaces.Environment:
				result.EnvironmentIDs = append(result.EnvironmentIDs, binding.Resource.ID)
			case spaces.Product:
				result.ProductIDs = append(result.ProductIDs, binding.Resource.ID)
			case spaces.Bundle:
				result.BundleIDs = append(result.BundleIDs, binding.Resource.ID)
			}
		}
		return result, nil
	}
	options.ReserveProductSpace = func(ctx context.Context, carrier controlplane.Principal, id, product string) error {
		ctx = context.WithValue(ctx, operatorCarrierKey{}, func() (governance.Principal, error) { return carrier, nil })
		spacesService, err := bind()
		if err != nil {
			return spaceSelectionError(err)
		}
		_, err = spacesService.ReserveProductPublication(ctx, id, product)
		return spaceSelectionError(err)
	}
	return options
}

func (authority spaceAuthority) RequireProductPublication(ctx context.Context, id, product string) (string, error) {
	if _, err := authority.allow(ctx, "global/artifact", controlplane.OperationCreateArtifact); err != nil {
		return "", err
	}
	subject, err := authority.allow(ctx, "product/"+product, controlplane.OperationCreateRelease)
	if err != nil {
		return "", err
	}
	if err := authority.RequireRead(ctx, id); err != nil {
		return "", err
	}
	return subject, nil
}

// Native errors crossing the legacy transport boundary retain status without
// exposing storage/provider diagnostics.
func spaceSelectionError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, spaces.ErrForbidden):
		return controlplane.ErrForbidden
	case errors.Is(err, spaces.ErrNotFound):
		return controlplane.ErrNotFound
	case errors.Is(err, spaces.ErrConflict):
		return controlplane.ErrConflict
	default:
		return errors.New("native Space operation failed")
	}
}
