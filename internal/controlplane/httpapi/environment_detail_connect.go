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

const EnvironmentDetailConnectRoute = "POST /conduit.environments.v1.EnvironmentDetailService/GetEnvironment"

// EnvironmentDetailConfig binds the original authenticated carrier to a
// shared authorized read. The adapter receives no report-write capability.
type EnvironmentDetailConfig struct {
	Authenticate func(*http.Request) (context.Context, error)
	Read         func(context.Context, string, *string) (controlplane.EnvironmentCatalogRecord, error)
}

// RegisterEnvironmentDetailConnect serves only the contracted POST.
// Authentication precedes decoding, including malformed or unsupported bodies.
func RegisterEnvironmentDetailConnect(mux *http.ServeMux, cfg EnvironmentDetailConfig) {
	server := connect.NewServer()
	environmentsv1connect.RegisterEnvironmentDetailServiceHandler(server, &environmentDetailService{cfg: cfg})
	rpc := http.NewServeMux()
	connecthttp.Mount(rpc, server, connecthttp.WithReadMaxBytes(1<<20))
	mux.Handle(EnvironmentDetailConnectRoute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

type environmentDetailService struct{ cfg EnvironmentDetailConfig }

func (service *environmentDetailService) GetEnvironment(ctx context.Context, request *environmentsv1.GetEnvironmentRequest) (*environmentsv1.GetEnvironmentResponse, error) {
	if request.Environment == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "Environment is required")
	}
	if service.cfg.Read == nil {
		return nil, connect.NewError(connect.CodeUnavailable, "Environment detail unavailable")
	}
	record, err := service.cfg.Read(ctx, request.Environment, request.SpaceId)
	if err != nil {
		code := connect.CodeInternal
		switch {
		case errors.Is(err, controlplane.ErrInvalidEnvironmentDetailInput):
			return nil, connect.NewError(connect.CodeInvalidArgument, "invalid Space selection")
		case errors.Is(err, controlplane.ErrEnvironmentDetailUnavailable):
			code = connect.CodeUnavailable
		case errors.Is(err, controlplane.ErrUnauthorized):
			code = connect.CodeUnauthenticated
		case errors.Is(err, controlplane.ErrForbidden):
			code = connect.CodePermissionDenied
		case errors.Is(err, controlplane.ErrNotFound):
			code = connect.CodeNotFound
		}
		return nil, connect.NewError(code, "Environment detail unavailable")
	}
	result, err := environmentDetailMessage(record)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, "Environment detail unavailable")
	}
	return &environmentsv1.GetEnvironmentResponse{Record: result}, nil
}
