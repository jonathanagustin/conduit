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

// PlanApprovalReceipt is the explicit Connect output contract, not a full Plan.
// It contains no lease, execution credentials or invented execution result.
type PlanApprovalReceipt struct {
	APIVersion      string                   `json:"apiVersion"`
	Kind            string                   `json:"kind"`
	Plan            controlplane.PlanSummary `json:"plan"`
	Approvals       []controlplane.Approval  `json:"approvals"`
	ChangeRequestID string                   `json:"changeRequestId,omitempty"`
}

// PlanApprovalFailure never treats a missing outcome detail as proof of no write.
// Error text is bounded and excludes server diagnostics and transport credentials.
type PlanApprovalFailure struct {
	Code             string
	OutcomeKnown     bool
	ApprovalRecorded bool
	HandoffFailed    bool
	cause            error
}

func (failure *PlanApprovalFailure) Error() string {
	if failure.OutcomeKnown && failure.ApprovalRecorded {
		if failure.HandoffFailed {
			return "Plan approval was recorded, but sequencing handoff failed; inspect native Plan state before retrying"
		}
		return "Plan approval was recorded, but result evidence is unavailable; inspect native Plan state before retrying"
	}
	return fmt.Sprintf("Plan approval failed (%s); approval outcome is unknown; inspect native Plan state before retrying", failure.Code)
}
func (failure *PlanApprovalFailure) Unwrap() error { return failure.cause }

func planApprovalFailure(planID string, cause error) *PlanApprovalFailure {
	failure := &PlanApprovalFailure{Code: connect.CodeOf(cause).String(), cause: cause}
	var rpc *connect.Error
	if errors.As(cause, &rpc) && len(rpc.Details()) == 1 {
		message, err := connectproto.UnmarshalErrorDetail(rpc.Details()[0])
		outcome, ok := message.(*plansv1.PlanApprovalOutcome)
		if err == nil && ok && outcome.PlanId == planID && outcome.ApprovalRecorded {
			failure.OutcomeKnown = true
			failure.ApprovalRecorded = true
			failure.HandoffFailed = outcome.HandoffFailed
		}
	}
	return failure
}

// ApprovePlanConnect sends exactly one generated command. It does not follow a
// redirect, retry the command, read a replacement Plan, or replay it over REST.
func (adapter *HTTPControlPlane) ApprovePlanConnect(ctx context.Context, id, rationale string) (PlanApprovalReceipt, error) {
	request := &plansv1.ApprovePlanRequest{PlanId: id, Rationale: rationale}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(rationale) == "" {
		return PlanApprovalReceipt{}, fmt.Errorf("Plan ID and review rationale are required")
	}
	if proto.Size(request) > maxOperatorResponseBytes {
		return PlanApprovalReceipt{}, fmt.Errorf("Plan approval request exceeds the command size limit")
	}
	client := *adapter.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	remote := plansv1connect.NewPlanApprovalServiceClient(connect.NewClient(connecthttp.NewTransport(&client, adapter.baseURL.String(), connecthttp.WithReadMaxBytes(maxOperatorResponseBytes))))
	reply, err := remote.ApprovePlan(ctx, request)
	if err != nil {
		return PlanApprovalReceipt{}, planApprovalFailure(id, err)
	}
	invalid := func() (PlanApprovalReceipt, error) {
		return PlanApprovalReceipt{}, planApprovalFailure(id, errors.New("invalid Plan approval receipt"))
	}
	if reply == nil || reply.Plan == nil {
		return invalid()
	}
	p := reply.Plan
	if p.Id != id || p.Status == "" || p.CreatedAt == nil || p.UpdatedAt == nil || p.CreatedAt.CheckValid() != nil || p.UpdatedAt.CheckValid() != nil || p.ApprovalCount < 1 || p.ApprovalCount != int64(len(reply.Approvals)) {
		return invalid()
	}
	maximum := int64(^uint(0) >> 1)
	for _, count := range []int64{p.StepCount, p.ApprovalCount, p.RequiredApprovals} {
		if count < 0 || count > maximum {
			return invalid()
		}
	}
	result := PlanApprovalReceipt{APIVersion: "conduit.plan-approval/v1", Kind: "PlanApprovalReceipt", Plan: controlplane.PlanSummary{
		ID: p.Id, Environment: p.Environment, Status: controlplane.PlanStatus(p.Status), StepCount: int(p.StepCount), ApprovalCount: int(p.ApprovalCount), RequiredApprovals: int(p.RequiredApprovals), CreatedAt: p.CreatedAt.AsTime(), UpdatedAt: p.UpdatedAt.AsTime(), Revision: p.Revision,
	}, Approvals: []controlplane.Approval{}, ChangeRequestID: reply.ChangeRequestId}
	for _, approval := range reply.Approvals {
		if approval == nil || approval.Subject == "" || approval.CreatedAt == nil || approval.CreatedAt.CheckValid() != nil {
			return invalid()
		}
		result.Approvals = append(result.Approvals, controlplane.Approval{Subject: approval.Subject, Rationale: approval.Rationale, CreatedAt: approval.CreatedAt.AsTime()})
	}
	return result, nil
}

// ApprovePlanConnect is optional so existing REST/in-process ports retain their
// full-document contract and cannot silently stand in for the generated command.
func (service Service) ApprovePlanConnect(ctx context.Context, id, rationale string) (PlanApprovalReceipt, error) {
	port, ok := service.controlPlane.(interface {
		ApprovePlanConnect(context.Context, string, string) (PlanApprovalReceipt, error)
	})
	if !ok {
		return PlanApprovalReceipt{}, fmt.Errorf("Connect Plan approval is unavailable for this operator port")
	}
	return port.ApprovePlanConnect(ctx, id, rationale)
}
