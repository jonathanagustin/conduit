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

const PlanApprovalConnectRoute = "POST /conduit.plans.v1.PlanApprovalService/ApprovePlan"

// PlanApprovalConfig binds the original authenticated carrier to the entire native command.
type PlanApprovalConfig struct {
	Authenticate func(*http.Request) (context.Context, error)
	Approve      func(context.Context, string, string) (controlplane.PlanRecord, error)
}

// RegisterPlanApprovalConnect exposes the existing governed command, never a
// read POST. It adds no retries, REST fallback, or caller-selected identity.
func RegisterPlanApprovalConnect(mux *http.ServeMux, cfg PlanApprovalConfig) {
	server := connect.NewServer()
	plansv1connect.RegisterPlanApprovalServiceHandler(server, &planApprovalService{cfg: cfg})
	rpc := http.NewServeMux()
	connecthttp.Mount(rpc, server, connecthttp.WithReadMaxBytes(1<<20))
	mux.Handle(PlanApprovalConnectRoute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

type planApprovalService struct{ cfg PlanApprovalConfig }

func (service *planApprovalService) ApprovePlan(ctx context.Context, request *plansv1.ApprovePlanRequest) (*plansv1.ApprovePlanResponse, error) {
	if service.cfg.Approve == nil {
		return nil, connect.NewError(connect.CodeUnavailable, "Plan approval unavailable")
	}
	record, err := service.cfg.Approve(ctx, request.PlanId, request.Rationale)
	if err != nil {
		var handoff *controlplane.PlanApprovalHandoffError
		if errors.As(err, &handoff) {
			return nil, recordedApprovalError(connect.CodeUnavailable, "Approval recorded; Plan sequencing unavailable", handoff.PlanID, true)
		}
		code := connect.CodeInternal
		var validation controlplane.ValidationErrors
		switch {
		case errors.Is(err, controlplane.ErrUnauthorized):
			code = connect.CodeUnauthenticated
		case errors.Is(err, controlplane.ErrForbidden), errors.Is(err, controlplane.ErrLeaseRejected):
			code = connect.CodePermissionDenied
		case errors.Is(err, controlplane.ErrNotFound):
			code = connect.CodeNotFound
		case errors.Is(err, controlplane.ErrConflict):
			code = connect.CodeAlreadyExists
		case errors.Is(err, controlplane.ErrInvalidTransition):
			code = connect.CodeFailedPrecondition
		case errors.Is(err, controlplane.ErrDigestMismatch), errors.As(err, &validation):
			code = connect.CodeInvalidArgument
		}
		return nil, connect.NewError(code, "Plan approval refused")
	}
	summary, err := planSummaryMessage(controlplane.PlanSummaryFor(record))
	if err != nil {
		return nil, recordedApprovalError(connect.CodeInternal, "Approval recorded; result unavailable", record.ID, false)
	}
	response := &plansv1.ApprovePlanResponse{Plan: summary, ChangeRequestId: nativeIndexString(record.ChangeRequestID)}
	for _, approval := range record.Approvals {
		created := timestamppb.New(approval.CreatedAt)
		if created.CheckValid() != nil {
			return nil, recordedApprovalError(connect.CodeInternal, "Approval recorded; result unavailable", record.ID, false)
		}
		response.Approvals = append(response.Approvals, &plansv1.PlanApproval{Subject: nativeIndexString(approval.Subject), Rationale: nativeIndexString(approval.Rationale), CreatedAt: created})
	}
	return response, nil
}

func recordedApprovalError(code connect.Code, message, planID string, handoffFailed bool) *connect.Error {
	failure := connect.NewError(code, message)
	detail, err := connectproto.NewErrorDetail(&plansv1.PlanApprovalOutcome{PlanId: nativeIndexString(planID), ApprovalRecorded: true, HandoffFailed: handoffFailed})
	if err != nil {
		return failure
	}
	return failure.WithDetail(detail)
}
