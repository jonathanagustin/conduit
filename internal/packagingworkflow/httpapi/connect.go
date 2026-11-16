package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	packagingv1 "github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1/packagingv1connect"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ConnectRoute is the Connect procedure for the packaging-run read.
// It is the same read as Route, mounted beside it.
const ConnectRoute = "POST /conduit.packaging.v1.PackagingRunService/GetPackagingRun"

// ConnectListRoute is the filtered packaging-run index procedure.
const ConnectListRoute = "POST /conduit.packaging.v1.PackagingRunService/ListPackagingRuns"

// ConnectEventsRoute reads one visible run's recorded history.
const ConnectEventsRoute = "POST /conduit.packaging.v1.PackagingRunService/ListPackagingRunEvents"

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
	mux.Handle(ConnectEventsRoute, handler)
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
		Tier:              nativeJSONString(status.Tier),
		Configured:        status.Configured,
		Authority:         nativeJSONString(status.Authority),
		PlanAuthority:     nativeJSONString(status.PlanAuthority),
		ApprovalAuthority: nativeJSONString(status.ApprovalAuthority),
		RunId:             nativeJSONString(status.RunID),
		Phase:             nativeJSONString(status.Phase),
		Outcome:           nativeJSONString(status.Outcome),
		Reason:            nativeJSONString(status.Reason),
		SourceDigest:      nativeJSONString(status.SourceDigest),
		ArtifactDigest:    nativeJSONString(status.ArtifactDigest),
		ProposalRevision:  nativeJSONString(status.ProposalRevision),
		TargetPlanRef:     nativeJSONString(status.TargetPlanRef),
		PlanId:            nativeJSONString(status.PlanID),
		HandoffReconciled: status.HandoffReconciled,
		LastReview:        nativeJSONString(status.LastReview),
		DuplicateReviews:  int32(status.DuplicateReviews),
		TemplateVersion:   nativeJSONString(status.TemplateVersion),
		ToolVersions:      nativeJSONString(status.ToolVersions),
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

func (s *connectService) ListPackagingRunEvents(ctx context.Context, input *packagingv1.ListPackagingRunEventsRequest) (*packagingv1.ListPackagingRunEventsResponse, error) {
	request, ok := ctx.Value(packagingRequestKey{}).(*http.Request)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "unauthorized")
	}
	values := url.Values{"view": {input.GetView()}, "cursor": {input.GetCursor()}}
	if input.Limit != nil {
		values.Set("limit", strconv.FormatInt(int64(input.GetLimit()), 10))
	}
	page, err := readEventPage(s.cfg, request.WithContext(ctx), input.GetRunId(), values)
	if err != nil {
		var coded StatusError
		if errors.As(err, &coded) {
			switch coded.Status {
			case http.StatusUnprocessableEntity:
				return nil, connect.NewError(connect.CodeInvalidArgument, coded.Message)
			case http.StatusNotFound:
				return nil, connect.NewError(connect.CodeNotFound, coded.Message)
			}
		}
		return nil, connect.NewError(connect.CodeUnavailable, "packaging run history is unavailable")
	}
	response := &packagingv1.ListPackagingRunEventsResponse{Tier: page.Tier, Configured: page.Configured, Authority: page.Authority, PlanAuthority: page.PlanAuthority, ApprovalAuthority: page.ApprovalAuthority, RunId: page.RunID, PageSize: int32(page.PageSize), Limit: int32(page.Limit), HasPrevious: page.HasPrevious, NextCursor: page.NextCursor, PreviousCursor: page.PreviousCursor, Total: int64(page.Total)}
	for _, event := range page.Items {
		at := timestamppb.New(event.At)
		if at.CheckValid() != nil {
			return nil, connect.NewError(connect.CodeUnavailable, "packaging run history is unavailable")
		}
		response.Items = append(response.Items, &packagingv1.PackagingRunEvent{Sequence: int64(event.Sequence), At: at, Step: nativeJSONString(event.Step), Outcome: nativeJSONString(event.Outcome), Reason: nativeJSONString(event.Reason), SourceDigest: nativeJSONString(event.SourceDigest), ArtifactDigest: nativeJSONString(event.ArtifactDigest), ProposalRevision: nativeJSONString(event.ProposalRevision), ApprovalId: nativeJSONString(event.ApprovalID), PlanId: nativeJSONString(event.PlanID), TargetPlanRef: nativeJSONString(event.TargetPlanRef)})
	}
	return response, nil
}

// nativeJSONString preserves the decoded REST JSON string when a native
// byte-bounded reason ends inside a UTF-8 encoding. JSON replaces each malformed
// byte with U+FFFD; protobuf requires the resulting string to be valid UTF-8.
func nativeJSONString(value string) string {
	if utf8.ValidString(value) {
		return value
	}
	return string([]rune(value))
}
