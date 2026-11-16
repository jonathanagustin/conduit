package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	packagingv1 "github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1/packagingv1connect"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// ConnectRoute is the Connect procedure for the packaging-run read.
// It is the same read as Route, mounted beside it.
const ConnectRoute = "POST /conduit.packaging.v1.PackagingRunService/GetPackagingRun"

// ConnectListRoute is the filtered packaging-run index procedure.
const ConnectListRoute = "POST /conduit.packaging.v1.PackagingRunService/ListPackagingRuns"

type packagingRequestKey struct{}

// RegisterConnect mounts the Connect procedure beside the REST route.
// Both use the same reader and the same caller check.
func RegisterConnect(mux *http.ServeMux, cfg Config) {
	rpcServer := connect.NewServer()
	packagingv1connect.RegisterPackagingRunServiceHandler(rpcServer, &connectService{cfg: cfg})
	rpcMux := http.NewServeMux()
	connecthttp.Mount(rpcMux, rpcServer)
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := authorize(cfg, request); err != nil {
			writeConnectRefusal(writer, request, err)
			return
		}
		ctx := context.WithValue(request.Context(), packagingRequestKey{}, request)
		rpcMux.ServeHTTP(writer, request.WithContext(ctx))
	})
	mux.Handle(ConnectRoute, handler)
	mux.Handle(ConnectListRoute, handler)
}

type connectService struct {
	cfg Config
}

func (s *connectService) GetPackagingRun(
	ctx context.Context, request *packagingv1.GetPackagingRunRequest,
) (*packagingv1.GetPackagingRunResponse, error) {
	runID := request.GetRunId()
	status, err := readStatus(ctx, s.cfg, runID)
	if errors.Is(err, ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, err.Error())
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, "packaging run status is unavailable")
	}
	return packagingRunResponse(status), nil
}

func packagingRunResponse(status domain.RunStatus) *packagingv1.GetPackagingRunResponse {
	return &packagingv1.GetPackagingRunResponse{
		Tier:              status.Tier,
		Configured:        status.Configured,
		Authority:         status.Authority,
		PlanAuthority:     status.PlanAuthority,
		ApprovalAuthority: status.ApprovalAuthority,
		RunId:             status.RunID,
		Phase:             status.Phase,
		Outcome:           status.Outcome,
		Reason:            status.Reason,
		SourceDigest:      status.SourceDigest,
		ArtifactDigest:    status.ArtifactDigest,
		ProposalRevision:  status.ProposalRevision,
		TargetPlanRef:     status.TargetPlanRef,
		PlanId:            status.PlanID,
		HandoffReconciled: status.HandoffReconciled,
		LastReview:        status.LastReview,
		DuplicateReviews:  int32(status.DuplicateReviews),
		TemplateVersion:   status.TemplateVersion,
		ToolVersions:      status.ToolVersions,
	}
}

func writeConnectRefusal(writer http.ResponseWriter, request *http.Request, err error) {
	status, message := refusalStatus(err)
	code := connect.CodeUnauthenticated
	switch status {
	case http.StatusUnauthorized:
		code = connect.CodeUnauthenticated
	case http.StatusForbidden:
		code = connect.CodePermissionDenied
	case http.StatusNotFound:
		code = connect.CodeNotFound
	default:
		code = connect.CodePermissionDenied
	}
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	_ = connecthttp.NewErrorWriter().Write(writer, request, connect.NewError(code, message))
}

func (s *connectService) ListPackagingRuns(ctx context.Context, input *packagingv1.ListPackagingRunsRequest) (*packagingv1.ListPackagingRunsResponse, error) {
	request, ok := ctx.Value(packagingRequestKey{}).(*http.Request)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "unauthorized")
	}
	values := url.Values{"view": {input.GetView()}, "cursor": {input.GetCursor()}, "phase": {input.GetPhase()}, "outcome": {input.GetOutcome()}, "planId": {input.GetPlanId()}, "search": {input.GetSearch()}}
	if input.Limit != nil {
		values.Set("limit", strconv.FormatInt(int64(input.GetLimit()), 10))
	}
	page, err := readRunPage(s.cfg, request.WithContext(ctx), values)
	if err != nil {
		var coded StatusError
		if errors.As(err, &coded) && coded.Status == http.StatusUnprocessableEntity {
			return nil, connect.NewError(connect.CodeInvalidArgument, coded.Message)
		}
		return nil, connect.NewError(connect.CodeUnavailable, "packaging run index is unavailable")
	}
	response := &packagingv1.ListPackagingRunsResponse{Tier: page.Tier, Configured: page.Configured, Authority: page.Authority, PlanAuthority: page.PlanAuthority, ApprovalAuthority: page.ApprovalAuthority, PageSize: int32(page.PageSize), Limit: int32(page.Limit), HasPrevious: page.HasPrevious, NextCursor: page.NextCursor, PreviousCursor: page.PreviousCursor, Total: int64(page.Total)}
	for _, row := range page.Items {
		response.Items = append(response.Items, packagingRunResponse(row))
	}
	return response, nil
}
