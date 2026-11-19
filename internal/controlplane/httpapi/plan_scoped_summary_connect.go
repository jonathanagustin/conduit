package httpapi

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1/plansv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

const PlanScopedSummaryConnectRoute = "POST /conduit.plans.v1.PlanScopedReadService/ListPlanSummariesInSpace"

// PlanScopedSummaryConfig binds the original authenticated carrier to a
// shared authorized read. Space membership confers no Plan authority.
type PlanScopedSummaryConfig struct {
	Authenticate func(*http.Request) (context.Context, error)
	Read         func(context.Context, string, controlplane.PlanQuery, string, *int32) (controlplane.PlanSummaryPage, error)
}

// RegisterPlanScopedSummaryConnect serves only the contracted POST.
// Authentication precedes decoding, including malformed or unsupported bodies.
func RegisterPlanScopedSummaryConnect(mux *http.ServeMux, cfg PlanScopedSummaryConfig) {
	server := connect.NewServer()
	plansv1connect.RegisterPlanScopedReadServiceHandler(server, &planScopedSummaryService{cfg: cfg})
	rpc := http.NewServeMux()
	connecthttp.Mount(rpc, server, connecthttp.WithReadMaxBytes(1<<20))
	mux.Handle(PlanScopedSummaryConnectRoute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

type planScopedSummaryService struct{ cfg PlanScopedSummaryConfig }

func (service *planScopedSummaryService) ListPlanSummariesInSpace(ctx context.Context, request *plansv1.ListPlanSummariesInSpaceRequest) (*plansv1.ListPlanSummariesInSpaceResponse, error) {
	if request.SpaceId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "Space is required")
	}
	if service.cfg.Read == nil {
		return nil, connect.NewError(connect.CodeUnavailable, "scoped Plan summary unavailable")
	}
	page, err := service.cfg.Read(ctx, request.SpaceId, controlplane.PlanQuery{View: "summary", Environment: request.Environment, Status: request.Status, InstallationID: request.InstallationId, Search: request.Search}, request.Cursor, request.Limit)
	if err != nil {
		code := connect.CodeInternal
		switch {
		case errors.Is(err, controlplane.ErrScopedPlanSummaryLifecycleFailure):
			code = scopedPlanLifecycleCode(err)
		case errors.Is(err, controlplane.ErrInvalidScopedPlanSummarySelection), errors.Is(err, controlplane.ErrInvalidPlanSummaryQuery):
			code = connect.CodeInvalidArgument
		case errors.Is(err, controlplane.ErrScopedPlanSummaryUnavailable):
			code = connect.CodeUnavailable
		default:
			code = scopedPlanLifecycleCode(err)
		}
		return nil, connect.NewError(code, "scoped Plan summary unavailable")
	}
	response, err := planSummaryPageMessage(page)
	if err != nil {
		return nil, err
	}
	return &plansv1.ListPlanSummariesInSpaceResponse{SpaceId: request.SpaceId, Page: response}, nil
}

func scopedPlanLifecycleCode(err error) connect.Code {
	switch {
	case errors.Is(err, controlplane.ErrUnauthorized):
		return connect.CodeUnauthenticated
	case errors.Is(err, controlplane.ErrForbidden):
		return connect.CodePermissionDenied
	case errors.Is(err, controlplane.ErrNotFound):
		return connect.CodeNotFound
	default:
		return connect.CodeInternal
	}
}
