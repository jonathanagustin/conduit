package composition

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	notificationhttp "github.com/jonathanagustin/conduit/internal/notifications/adapters/httpapi"
	assethttp "github.com/jonathanagustin/conduit/internal/productassets/adapters/httpapi"
	assetapp "github.com/jonathanagustin/conduit/internal/productassets/application"
	assets "github.com/jonathanagustin/conduit/internal/productassets/domain"
	assetports "github.com/jonathanagustin/conduit/internal/productassets/ports"
	spaces "github.com/jonathanagustin/conduit/internal/spaces/domain"
)

func mountProductAssets(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, store assetports.Store) (http.Handler, error) {
	if store == nil {
		return legacy, nil
	}
	service := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	err := assethttp.Register(mux, func(r *http.Request) (*assetapp.Service, error) {
		carrier, err := operatorFrom(r.Context())
		if err != nil {
			var status notificationhttp.StatusError
			if errors.As(err, &status) {
				return nil, assethttp.StatusError{Status: status.Status, Message: "operator identity could not be established"}
			}
			return nil, assethttp.StatusError{Status: http.StatusUnauthorized, Message: "operator identity is required"}
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			return nil, assethttp.StatusError{Status: http.StatusBadRequest, Message: "invalid query encoding"}
		}
		var grouping assetports.Grouping
		if values, present := query["spaceId"]; present {
			if r.Method != http.MethodPost || len(values) != 1 || !spaces.ValidID(values[0]) {
				return nil, assethttp.StatusError{Status: http.StatusBadRequest, Message: "spaceId must select one native Space for publication"}
			}
			if options.RequireSpaceSelection == nil || options.ReserveProductSpace == nil {
				return nil, assethttp.StatusError{Status: http.StatusServiceUnavailable, Message: "native Space publication is not configured"}
			}
			if err := options.RequireSpaceSelection(r.Context(), carrier, values[0]); err != nil {
				return nil, assetOperationError(err)
			}
			grouping = assetGrouping{options: options, carrier: carrier, id: values[0]}
		}
		return assetapp.New(store, assetCatalogue{repository: repository}, assetAuthority{repository: repository, governance: service, options: options}, grouping)
	})
	if err != nil {
		return nil, err
	}
	mux.Handle("/", legacy)
	return withOperatorCarrier(service, options, mux), nil
}

type assetAuthority struct {
	repository controlplane.Repository
	governance *controlplane.GovernanceService
	options    controlplane.HandlerOptions
}

func (a assetAuthority) allow(ctx context.Context, resource, operation string) (string, error) {
	carrier, err := operatorFrom(ctx)
	if err != nil {
		return "", err
	}
	if !a.governance.AuthorizeResourceOperation(ctx, carrier, a.options, resource, operation) {
		return "", assets.ErrForbidden
	}
	return governance.EffectivePrincipal(carrier).Username, nil
}
func (a assetAuthority) RequirePublication(ctx context.Context, product string) (string, error) {
	if _, err := a.allow(ctx, "global/artifact", governance.OperationCreateArtifact); err != nil {
		return "", err
	}
	products, err := controlplane.ListProductCatalog(ctx, a.repository)
	if err != nil {
		return "", err
	}
	exists := false
	for _, p := range products {
		if p.Product == product {
			exists = true
			break
		}
	}
	if !exists {
		if _, err := a.allow(ctx, "global/product", governance.OperationCreateProduct); err != nil {
			return "", err
		}
	}
	return a.allow(ctx, "product/"+product, governance.OperationCreateRelease)
}
func (a assetAuthority) RequireRead(ctx context.Context, product string) error {
	_, err := a.allow(ctx, "product/"+product, governance.OperationReadResource)
	return err
}

type assetGrouping struct {
	options controlplane.HandlerOptions
	carrier controlplane.Principal
	id      string
}

func (g assetGrouping) Reserve(ctx context.Context, product string) error {
	return assetOperationError(g.options.ReserveProductSpace(ctx, g.carrier, g.id, product))
}
func assetOperationError(err error) error {
	switch {
	case errors.Is(err, controlplane.ErrForbidden):
		return assets.ErrForbidden
	case errors.Is(err, controlplane.ErrNotFound):
		return assets.ErrNotFound
	case errors.Is(err, controlplane.ErrConflict):
		return assets.ErrConflict
	}
	return err
}
