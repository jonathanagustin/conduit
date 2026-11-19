package operator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1/plansv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"google.golang.org/protobuf/proto"
)

// PlanCancellationReceipt is the explicit Connect output, never a full Plan or
// an Agent acknowledgement that execution stopped or effects were rolled back.
type PlanCancellationReceipt struct {
	APIVersion      string                        `json:"apiVersion"`
	Kind            string                        `json:"kind"`
	Plan            controlplane.PlanSummary      `json:"plan"`
	Cancellation    controlplane.PlanCancellation `json:"cancellation"`
	ChangeRequestID string                        `json:"changeRequestId,omitempty"`
}

// PlanCancellationFailure treats absent or unbound evidence as an unknown outcome.
// Error strings exclude raw transport diagnostics and credentials.
type PlanCancellationFailure struct {
	Code             string
	OutcomeKnown     bool
	CommandCompleted bool
	cause            error
}

func (failure *PlanCancellationFailure) Error() string {
	if failure.OutcomeKnown && failure.CommandCompleted {
		return "Plan cancellation command completed, but result evidence is unavailable; inspect native Plan state before manual recovery"
	}
	return fmt.Sprintf("Plan cancellation failed (%s); outcome is unknown; inspect native Plan state before manual recovery", failure.Code)
}
func (failure *PlanCancellationFailure) Unwrap() error { return failure.cause }

func planCancellationFailure(id string, cause error) *PlanCancellationFailure {
	failure := &PlanCancellationFailure{Code: connect.CodeOf(cause).String(), cause: cause}
	var rpc *connect.Error
	if errors.As(cause, &rpc) && len(rpc.Details()) == 1 {
		message, err := connectproto.UnmarshalErrorDetail(rpc.Details()[0])
		outcome, ok := message.(*plansv1.PlanCancellationOutcome)
		if err == nil && ok && outcome.PlanId == id && outcome.CommandCompleted {
			failure.OutcomeKnown = true
			failure.CommandCompleted = true
		}
	}
	return failure
}

// CancelPlanConnect sends one generated cancellation. It does not redirect,
// retry, read a substitute result or replay the command over REST.
func (adapter *HTTPControlPlane) CancelPlanConnect(ctx context.Context, id, reason string) (PlanCancellationReceipt, error) {
	request := &plansv1.CancelPlanRequest{PlanId: id, Reason: reason}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(reason) == "" {
		return PlanCancellationReceipt{}, fmt.Errorf("Plan ID and cancellation reason are required")
	}
	if proto.Size(request) > maxOperatorResponseBytes {
		return PlanCancellationReceipt{}, fmt.Errorf("Plan cancellation request exceeds the command size limit")
	}
	client := *adapter.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	remote := plansv1connect.NewPlanCancellationServiceClient(connect.NewClient(connecthttp.NewTransport(&client, adapter.baseURL.String(), connecthttp.WithReadMaxBytes(maxOperatorResponseBytes))))
	reply, err := remote.CancelPlan(ctx, request)
	if err != nil {
		return PlanCancellationReceipt{}, planCancellationFailure(id, err)
	}
	invalid := func() (PlanCancellationReceipt, error) {
		return PlanCancellationReceipt{}, planCancellationFailure(id, errors.New("invalid Plan cancellation receipt"))
	}
	if reply == nil || reply.Plan == nil || reply.Cancellation == nil {
		return invalid()
	}
	p, c := reply.Plan, reply.Cancellation
	if p.Id != id || p.Status != string(controlplane.PlanCancelled) || p.CreatedAt == nil || p.UpdatedAt == nil || p.CreatedAt.CheckValid() != nil || p.UpdatedAt.CheckValid() != nil || c.CreatedAt == nil || c.CreatedAt.CheckValid() != nil || c.Subject == "" || c.Reason != strings.TrimSpace(reason) {
		return invalid()
	}
	maximum := int64(^uint(0) >> 1)
	for _, count := range []int64{p.StepCount, p.ApprovalCount, p.RequiredApprovals} {
		if count < 0 || count > maximum {
			return invalid()
		}
	}
	return PlanCancellationReceipt{APIVersion: "conduit.plan-cancellation/v1", Kind: "PlanCancellationReceipt", Plan: controlplane.PlanSummary{ID: p.Id, Environment: p.Environment, Status: controlplane.PlanStatus(p.Status), StepCount: int(p.StepCount), ApprovalCount: int(p.ApprovalCount), RequiredApprovals: int(p.RequiredApprovals), CreatedAt: p.CreatedAt.AsTime(), UpdatedAt: p.UpdatedAt.AsTime(), Revision: p.Revision}, Cancellation: controlplane.PlanCancellation{Subject: c.Subject, Reason: c.Reason, CreatedAt: c.CreatedAt.AsTime()}, ChangeRequestID: reply.ChangeRequestId}, nil
}

// CancelPlanConnect is optional: existing REST/in-process ports keep their full
// document contract and cannot silently substitute for the generated command.
func (service Service) CancelPlanConnect(ctx context.Context, id, reason string) (PlanCancellationReceipt, error) {
	port, ok := service.controlPlane.(interface {
		CancelPlanConnect(context.Context, string, string) (PlanCancellationReceipt, error)
	})
	if !ok {
		return PlanCancellationReceipt{}, fmt.Errorf("Connect Plan cancellation is unavailable for this operator port")
	}
	return port.CancelPlanConnect(ctx, id, reason)
}
