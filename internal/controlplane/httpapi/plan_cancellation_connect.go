package httpapi

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1/plansv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const PlanCancellationConnectRoute = "POST /conduit.plans.v1.PlanCancellationService/CancelPlan"

// PlanCancellationConfig binds the original authenticated carrier to the native command.
type PlanCancellationConfig struct {
	Authenticate func(*http.Request) (context.Context, error)
	Cancel       func(context.Context, string, controlplane.PlanCancellationCommand) (controlplane.PlanRecord, error)
}

// RegisterPlanCancellationConnect mounts only the existing cancellation command.
// Authentication precedes decoding; command outcomes never trigger a replay.
func RegisterPlanCancellationConnect(mux *http.ServeMux, cfg PlanCancellationConfig) {
	server := connect.NewServer()
	plansv1connect.RegisterPlanCancellationServiceHandler(server, &planCancellationService{cfg: cfg})
	rpc := http.NewServeMux()
	connecthttp.Mount(rpc, server, connecthttp.WithReadMaxBytes(1<<20))
	mux.Handle(PlanCancellationConnectRoute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

type planCancellationService struct{ cfg PlanCancellationConfig }

func (service *planCancellationService) CancelPlan(ctx context.Context, request *plansv1.CancelPlanRequest) (*plansv1.CancelPlanResponse, error) {
	if service.cfg.Cancel == nil {
		return nil, connect.NewError(connect.CodeUnavailable, "Plan cancellation unavailable")
	}
	duration := int(request.SuppressionDurationSeconds)
	if request.PlanId == "" || int64(duration) != request.SuppressionDurationSeconds {
		return nil, connect.NewError(connect.CodeInvalidArgument, "Invalid Plan cancellation request")
	}
	record, err := service.cfg.Cancel(ctx, request.PlanId, controlplane.PlanCancellationCommand{Reason: request.Reason, CreateSuppression: request.CreateSuppression, SuppressionDurationSeconds: duration, InstallationID: request.InstallationId})
	if err != nil {
		code := connect.CodeInternal
		var cancellationValidation controlplane.PlanCancellationValidationError
		var validation controlplane.ValidationErrors
		switch {
		case errors.Is(err, context.Canceled):
			code = connect.CodeCanceled
		case errors.Is(err, context.DeadlineExceeded):
			code = connect.CodeDeadlineExceeded
		case errors.Is(err, controlplane.ErrUnauthorized):
			code = connect.CodeUnauthenticated
		case errors.Is(err, controlplane.ErrForbidden), errors.Is(err, controlplane.ErrLeaseRejected):
			code = connect.CodePermissionDenied
		case errors.Is(err, controlplane.ErrNotFound):
			code = connect.CodeNotFound
		case errors.Is(err, controlplane.ErrConflict):
			code = connect.CodeAborted
		case errors.Is(err, controlplane.ErrInvalidTransition):
			code = connect.CodeFailedPrecondition
		case errors.Is(err, controlplane.ErrDigestMismatch), errors.As(err, &cancellationValidation), errors.As(err, &validation):
			code = connect.CodeInvalidArgument
		}
		// Effects are sequential. A generic command failure is not a no-write receipt.
		return nil, connect.NewError(code, "Plan cancellation failed; outcome may require inspection")
	}
	if record.Cancellation == nil || record.ID != request.PlanId || record.Status != controlplane.PlanCancelled {
		return nil, connect.NewError(connect.CodeInternal, "Invalid native cancellation result; outcome requires inspection")
	}
	summary, err := planSummaryMessage(controlplane.PlanSummaryFor(record))
	if err != nil {
		return nil, completedCancellationError(record.ID)
	}
	created := timestamppb.New(record.Cancellation.CreatedAt)
	if created.CheckValid() != nil {
		return nil, completedCancellationError(record.ID)
	}
	return &plansv1.CancelPlanResponse{Plan: summary, ChangeRequestId: nativeIndexString(record.ChangeRequestID), Cancellation: &plansv1.RecordedPlanCancellation{Subject: nativeIndexString(record.Cancellation.Subject), Reason: nativeIndexString(record.Cancellation.Reason), CreatedAt: created}}, nil
}

func completedCancellationError(planID string) *connect.Error {
	failure := connect.NewError(connect.CodeInternal, "Cancellation command completed; result unavailable")
	detail, err := connectproto.NewErrorDetail(&plansv1.PlanCancellationOutcome{PlanId: nativeIndexString(planID), CommandCompleted: true})
	if err != nil {
		return failure
	}
	return failure.WithDetail(detail)
}
