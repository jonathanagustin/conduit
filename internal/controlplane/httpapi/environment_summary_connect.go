package httpapi

import (
	"context"
	"errors"
	"google.golang.org/protobuf/types/known/timestamppb"
	"net/http"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1/environmentsv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

const EnvironmentSummaryConnectRoute = "POST /conduit.environments.v1.EnvironmentReadService/ListEnvironmentSummaries"

// EnvironmentSummaryConfig binds an authenticated native carrier to the shared read.
type EnvironmentSummaryConfig struct {
	Authenticate func(*http.Request) (context.Context, error)
	Read         func(context.Context, string, string, string, *int32) (controlplane.EnvironmentCatalogSummaryPage, error)
}

// RegisterEnvironmentSummaryConnect mounts only the contracted read. Authentication
// precedes protobuf decoding, including malformed or unsupported requests.
func RegisterEnvironmentSummaryConnect(mux *http.ServeMux, cfg EnvironmentSummaryConfig) {
	server := connect.NewServer()
	environmentsv1connect.RegisterEnvironmentReadServiceHandler(server, &environmentSummaryService{cfg: cfg})
	rpc := http.NewServeMux()
	connecthttp.Mount(rpc, server)
	mux.Handle(EnvironmentSummaryConnectRoute, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var ctx context.Context
		err := controlplane.ErrUnauthorized
		if cfg.Authenticate != nil {
			ctx, err = cfg.Authenticate(request)
		}
		if err != nil || ctx == nil {
			code := connect.CodeInternal
			if err == nil || errors.Is(err, controlplane.ErrUnauthorized) {
				code = connect.CodeUnauthenticated
			} else if errors.Is(err, controlplane.ErrForbidden) {
				code = connect.CodePermissionDenied
			}
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_ = connecthttp.NewErrorWriter().Write(w, request, connect.NewError(code, code.String()))
			return
		}
		rpc.ServeHTTP(w, request.WithContext(ctx))
	}))
}

type environmentSummaryService struct{ cfg EnvironmentSummaryConfig }

func (service *environmentSummaryService) ListEnvironmentSummaries(ctx context.Context, request *environmentsv1.ListEnvironmentSummariesRequest) (*environmentsv1.ListEnvironmentSummariesResponse, error) {
	if service.cfg.Read == nil {
		return nil, connect.NewError(connect.CodeUnavailable, "Environment summary index unavailable")
	}
	page, err := service.cfg.Read(ctx, request.Search, request.Cursor, request.SpaceId, request.Limit)
	if err != nil {
		code := connect.CodeInternal
		switch {
		case errors.Is(err, controlplane.ErrInvalidEnvironmentSummaryQuery), errors.Is(err, controlplane.ErrInvalidSpaceSelection):
			return nil, connect.NewError(connect.CodeInvalidArgument, err.Error())
		case errors.Is(err, controlplane.ErrUnauthorized):
			code = connect.CodeUnauthenticated
		case errors.Is(err, controlplane.ErrForbidden):
			code = connect.CodePermissionDenied
		case errors.Is(err, controlplane.ErrNotFound):
			code = connect.CodeNotFound
		case errors.Is(err, controlplane.ErrSpaceSelectionUnavailable), errors.Is(err, controlplane.ErrSpaceMembershipUnavailable):
			code = connect.CodeUnavailable
		}
		return nil, connect.NewError(code, "Environment summary index unavailable")
	}
	response := &environmentsv1.ListEnvironmentSummariesResponse{
		Total: int64(page.Total), PageSize: int32(page.PageSize), Limit: int32(page.Limit),
		NextCursor: page.NextCursor, PreviousCursor: page.PreviousCursor, HasPrevious: page.HasPrevious,
	}
	for _, record := range page.Environments {
		item := &environmentsv1.EnvironmentSummary{
			Environment: &environmentsv1.EnvironmentIdentity{Id: nativeIndexString(record.Environment.ID), Generation: nativeIndexString(record.Environment.Generation)},
			Spoke:       &environmentsv1.SpokeLiveness{AgentId: nativeIndexString(record.Spoke.AgentID), State: nativeIndexString(record.Spoke.State)},
		}
		var err error
		item.Spoke.ObservedAt, err = environmentSummaryTimestamp(record.Spoke.ObservedAt)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, "invalid Environment observation time")
		}
		if record.Reported != nil {
			observed, err := environmentSummaryTimestamp(record.Reported.ObservedAt)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, "invalid Environment observation time")
			}
			item.Reported = &environmentsv1.EnvironmentReportSummary{Environment: nativeIndexString(record.Reported.Environment), AgentId: nativeIndexString(record.Reported.AgentID), ObservedAt: observed, EntityCount: int64(record.Reported.EntityCount)}
		}
		response.Environments = append(response.Environments, item)
	}
	return response, nil
}

func environmentSummaryTimestamp(value time.Time) (*timestamppb.Timestamp, error) {
	if value.IsZero() {
		return nil, nil
	}
	result := timestamppb.New(value.UTC())
	return result, result.CheckValid()
}
