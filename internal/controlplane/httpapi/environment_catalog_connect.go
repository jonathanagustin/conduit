package httpapi

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1/environmentsv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

const EnvironmentCatalogConnectRoute = "POST /conduit.environments.v1.EnvironmentCatalogService/ListEnvironments"

// EnvironmentCatalogConfig binds the original authenticated carrier to a
// shared authorized read. The adapter receives no report-write capability.
type EnvironmentCatalogConfig struct {
	Authenticate func(*http.Request) (context.Context, error)
	Read         func(context.Context, string, string, *string, *int32) (controlplane.EnvironmentCatalogPage, error)
}

// RegisterEnvironmentCatalogConnect serves only the contracted POST.
// Authentication precedes decoding, including malformed or unsupported bodies.
func RegisterEnvironmentCatalogConnect(mux *http.ServeMux, cfg EnvironmentCatalogConfig) {
	server := connect.NewServer()
	environmentsv1connect.RegisterEnvironmentCatalogServiceHandler(server, &environmentCatalogService{cfg: cfg})
	rpc := http.NewServeMux()
	connecthttp.Mount(rpc, server, connecthttp.WithReadMaxBytes(1<<20))
	mux.Handle(EnvironmentCatalogConnectRoute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ctx context.Context
		err := controlplane.ErrUnauthorized
		if cfg.Authenticate != nil {
			ctx, err = cfg.Authenticate(r)
		}
		if err != nil || ctx == nil {
			code := connect.CodeInternal
			if err == nil || errors.Is(err, controlplane.ErrUnauthorized) {
				code = connect.CodeUnauthenticated
			} else if errors.Is(err, controlplane.ErrForbidden) {
				code = connect.CodePermissionDenied
			}
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_ = connecthttp.NewErrorWriter().Write(w, r, connect.NewError(code, code.String()))
			return
		}
		rpc.ServeHTTP(w, r.WithContext(ctx))
	}))
}

type environmentCatalogService struct{ cfg EnvironmentCatalogConfig }

func (service *environmentCatalogService) ListEnvironments(ctx context.Context, request *environmentsv1.ListEnvironmentsRequest) (*environmentsv1.ListEnvironmentsResponse, error) {
	if service.cfg.Read == nil {
		return nil, connect.NewError(connect.CodeUnavailable, "Environment catalog unavailable")
	}
	page, err := service.cfg.Read(ctx, request.Search, request.Cursor, request.SpaceId, request.Limit)
	if err != nil {
		code := connect.CodeInternal
		switch {
		case errors.Is(err, controlplane.ErrInvalidEnvironmentCatalogQuery):
			return nil, connect.NewError(connect.CodeInvalidArgument, "invalid Environment catalog query")
		case errors.Is(err, controlplane.ErrEnvironmentCatalogUnavailable):
			code = connect.CodeUnavailable
		case errors.Is(err, controlplane.ErrUnauthorized):
			code = connect.CodeUnauthenticated
		case errors.Is(err, controlplane.ErrForbidden):
			code = connect.CodePermissionDenied
		case errors.Is(err, controlplane.ErrNotFound):
			code = connect.CodeNotFound
		}
		return nil, connect.NewError(code, "Environment catalog unavailable")
	}
	response := &environmentsv1.ListEnvironmentsResponse{
		Total: int64(page.Total), PageSize: int32(page.PageSize), Limit: int32(page.Limit),
		NextCursor: page.NextCursor, PreviousCursor: page.PreviousCursor, HasPrevious: page.HasPrevious,
	}
	for _, record := range page.Environments {
		item, err := environmentDetailMessage(record)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, "Environment catalog unavailable")
		}
		response.Environments = append(response.Environments, item)
	}
	return response, nil
}
