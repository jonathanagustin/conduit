// Package httpapi adapts the native authorized Reporting projection to Connect.
package httpapi

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	reportingv1 "github.com/jonathanagustin/conduit/gen/go/conduit/reporting/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/reporting/v1/reportingv1connect"
	"github.com/jonathanagustin/conduit/internal/reporting/domain"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const ConnectRoute = "POST /conduit.reporting.v1.ReportingService/GetOverview"

var (
	ErrUnauthenticated  = errors.New("unauthenticated")
	ErrPermissionDenied = errors.New("permission denied")
)

// Config binds transport authentication and the existing authorized projection.
// Authenticate must preserve the request context and bind its resolved identity.
type Config struct {
	Authenticate func(*http.Request) (context.Context, error)
	Read         func(context.Context) (domain.OverviewProjection, error)
}

// RegisterConnect mounts only the explicitly registered unary read procedure.
func RegisterConnect(mux *http.ServeMux, cfg Config) {
	server := connect.NewServer()
	reportingv1connect.RegisterReportingServiceHandler(server, &service{cfg: cfg})
	rpcMux := http.NewServeMux()
	connecthttp.Mount(rpcMux, server)
	mux.Handle(ConnectRoute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ctx context.Context
		err := ErrUnauthenticated
		if cfg.Authenticate != nil {
			ctx, err = cfg.Authenticate(r)
		}
		if err != nil || ctx == nil {
			code := connect.CodeInternal
			if err == nil || errors.Is(err, ErrUnauthenticated) {
				code = connect.CodeUnauthenticated
			}
			if errors.Is(err, ErrPermissionDenied) {
				code = connect.CodePermissionDenied
			}
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_ = connecthttp.NewErrorWriter().Write(w, r, connect.NewError(code, code.String()))
			return
		}
		rpcMux.ServeHTTP(w, r.WithContext(ctx))
	}))
}

type service struct{ cfg Config }

func (s *service) GetOverview(ctx context.Context, _ *reportingv1.GetOverviewRequest) (*reportingv1.GetOverviewResponse, error) {
	if s.cfg.Read == nil {
		return nil, connect.NewError(connect.CodeUnavailable, "overview unavailable")
	}
	view, err := s.cfg.Read(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, "overview unavailable")
	}
	return response(view)
}

func response(view domain.OverviewProjection) (*reportingv1.GetOverviewResponse, error) {
	timestamp := timestamppb.New(view.GeneratedAt)
	if timestamp.CheckValid() != nil {
		return nil, connect.NewError(connect.CodeInternal, "invalid overview timestamp")
	}
	out := &reportingv1.GetOverviewResponse{
		GeneratedAt: timestamp, StaleAfterSeconds: view.StaleAfterSeconds,
		AwaitingApprovals: &reportingv1.ChangeRequestPanel{Status: view.AwaitingApprovals.Status, Count: int64(view.AwaitingApprovals.Count)},
		FailedPlans:       &reportingv1.PlanPanel{Status: view.FailedPlans.Status, Count: int64(view.FailedPlans.Count)},
		RunningPlans:      &reportingv1.PlanPanel{Status: view.RunningPlans.Status, Count: int64(view.RunningPlans.Count)},
		UnhealthyEntities: &reportingv1.InstallationPanel{Status: view.UnhealthyEntities.Status, Count: int64(view.UnhealthyEntities.Count)},
		StaleReports:      &reportingv1.StaleReportPanel{Status: view.StaleReports.Status, Count: int64(view.StaleReports.Count)},
		BlockedPromotions: &reportingv1.PromotionPanel{Status: view.BlockedPromotions.Status, Count: int64(view.BlockedPromotions.Count)},
	}
	for _, row := range view.AwaitingApprovals.Items {
		out.AwaitingApprovals.Items = append(out.AwaitingApprovals.Items, &reportingv1.OverviewChangeRequest{Id: row.ID, Title: row.Title, Environment: row.Environment})
	}
	for _, row := range view.FailedPlans.Items {
		out.FailedPlans.Items = append(out.FailedPlans.Items, &reportingv1.OverviewPlan{Id: row.ID, Environment: row.Environment})
	}
	for _, row := range view.RunningPlans.Items {
		out.RunningPlans.Items = append(out.RunningPlans.Items, &reportingv1.OverviewPlan{Id: row.ID, Environment: row.Environment})
	}
	for _, row := range view.UnhealthyEntities.Items {
		out.UnhealthyEntities.Items = append(out.UnhealthyEntities.Items, &reportingv1.OverviewInstallation{Environment: row.Environment, InstallationId: row.InstallationID, Health: row.Health})
	}
	for range view.StaleReports.Items {
		out.StaleReports.Items = append(out.StaleReports.Items, &emptypb.Empty{})
	}
	for _, row := range view.BlockedPromotions.Items {
		out.BlockedPromotions.Items = append(out.BlockedPromotions.Items, &reportingv1.OverviewPromotion{Id: row.ID, Product: row.Product, Version: row.Version, FromChannel: row.FromChannel, ToChannel: row.ToChannel})
	}
	return out, nil
}
