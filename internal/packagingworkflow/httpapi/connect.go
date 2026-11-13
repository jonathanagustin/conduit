package httpapi

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"
	packagingv1 "github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1/packagingv1connect"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// ConnectRoute is the Connect procedure for the packaging-run read.
// It is the same read as Route, mounted beside it.
const ConnectRoute = "POST /conduit.packaging.v1.PackagingRunService/GetPackagingRun"

// RegisterConnect mounts the Connect procedure beside the REST route.
// Both use the same reader and the same caller check.
func RegisterConnect(mux *http.ServeMux, cfg Config) {
	_, handler := packagingv1connect.NewPackagingRunServiceHandler(&connectService{cfg: cfg})
	mux.Handle(ConnectRoute, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := authorize(cfg, request); err != nil {
			writeConnectRefusal(writer, request, err)
			return
		}
		handler.ServeHTTP(writer, request)
	}))
}

type connectService struct {
	cfg Config
}

func (s *connectService) GetPackagingRun(
	ctx context.Context, request *connect.Request[packagingv1.GetPackagingRunRequest],
) (*connect.Response[packagingv1.GetPackagingRunResponse], error) {
	runID := ""
	if request.Msg != nil {
		runID = request.Msg.GetRunId()
	}
	status, err := readStatus(ctx, s.cfg, runID)
	if errors.Is(err, ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("packaging run status is unavailable"))
	}
	return connect.NewResponse(packagingRunResponse(status)), nil
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
	_ = connect.NewErrorWriter().Write(writer, request, connect.NewError(code, errors.New(message)))
}
