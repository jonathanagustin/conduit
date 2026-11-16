// Package httpapi serves bounded native control-plane projections over Connect.
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1/plansv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const PlanSummaryConnectRoute = "POST /conduit.plans.v1.PlanReadService/ListPlanSummaries"

// PlanSummaryConfig binds the original authenticated carrier to the native read.
type PlanSummaryConfig struct {
	Authenticate func(*http.Request) (context.Context, error)
	Read         func(context.Context, controlplane.PlanQuery, string, int) (controlplane.PlanSummaryPage, error)
}

// RegisterPlanSummaryConnect exposes only this POST read, with authentication
// before the protobuf body is decoded or the reader is called.
func RegisterPlanSummaryConnect(mux *http.ServeMux, cfg PlanSummaryConfig) {
	server := connect.NewServer()
	plansv1connect.RegisterPlanReadServiceHandler(server, &planSummaryService{cfg: cfg})
	rpc := http.NewServeMux()
	connecthttp.Mount(rpc, server)
	mux.Handle(PlanSummaryConnectRoute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

type planSummaryService struct{ cfg PlanSummaryConfig }

func (s *planSummaryService) ListPlanSummaries(ctx context.Context, request *plansv1.ListPlanSummariesRequest) (*plansv1.ListPlanSummariesResponse, error) {
	limit, err := controlplane.ResolvePlanSummaryLimit(request.Limit)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
	}
	if s.cfg.Read == nil {
		return nil, connect.NewError(connect.CodeUnavailable, "Plan summary index unavailable")
	}
	page, err := s.cfg.Read(ctx, controlplane.PlanQuery{
		View: "summary", Environment: strings.TrimSpace(request.Environment),
		Status: strings.TrimSpace(request.Status), InstallationID: strings.TrimSpace(request.InstallationId),
		Search: request.Search,
	}, request.Cursor, limit)
	if err != nil {
		if errors.Is(err, controlplane.ErrInvalidPlanSummaryQuery) {
			return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
		}
		if errors.Is(err, controlplane.ErrUnauthorized) {
			return nil, connect.NewError(connect.CodeUnauthenticated, "unauthenticated")
		}
		if errors.Is(err, controlplane.ErrForbidden) {
			return nil, connect.NewError(connect.CodePermissionDenied, "permission denied")
		}
		return nil, connect.NewError(connect.CodeInternal, "Plan summary index unavailable")
	}
	out := &plansv1.ListPlanSummariesResponse{
		Total: int64(page.Total), PageSize: int32(page.PageSize), Limit: int32(page.Limit),
		NextCursor: page.NextCursor, PreviousCursor: page.PreviousCursor, HasPrevious: page.HasPrevious,
	}
	for _, plan := range page.Plans {
		created, updated := timestamppb.New(plan.CreatedAt), timestamppb.New(plan.UpdatedAt)
		if created.CheckValid() != nil || updated.CheckValid() != nil {
			return nil, connect.NewError(connect.CodeInternal, "Plan summary index unavailable")
		}
		out.Plans = append(out.Plans, &plansv1.PlanSummary{
			Id: nativePlanString(plan.ID), Environment: nativePlanString(plan.Environment), Status: nativePlanString(string(plan.Status)),
			StepCount: int64(plan.StepCount), ApprovalCount: int64(plan.ApprovalCount), RequiredApprovals: int64(plan.RequiredApprovals),
			CreatedAt: created, UpdatedAt: updated, Revision: plan.Revision,
		})
	}
	return out, nil
}

// Match the replacement of malformed stored UTF-8 in decoded native JSON.
func nativePlanString(value string) string {
	if utf8.ValidString(value) {
		return value
	}
	return string([]rune(value))
}
