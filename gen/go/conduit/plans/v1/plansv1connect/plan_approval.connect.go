//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
//
// Source: conduit/plans/v1/plan_approval.proto

package plansv1connect

import (
	connect "connectrpc.com/connect/v2"
	context "context"
	v1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	sync "sync"
)

const (
	// PlanApprovalServiceName is the fully-qualified name of the PlanApprovalService service.
	PlanApprovalServiceName = "conduit.plans.v1.PlanApprovalService"
)

// These constants are the procedure names of the RPCs defined in this package. They're exposed at
// runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// PlanApprovalServiceApprovePlanProcedure is the procedure name of the PlanApprovalService's
	// ApprovePlan RPC.
	PlanApprovalServiceApprovePlanProcedure = "/conduit.plans.v1.PlanApprovalService/ApprovePlan"
)

var (
	planApprovalServiceApprovePlanSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType: connect.StreamTypeUnary,
			Schema:     v1.File_conduit_plans_v1_plan_approval_proto.Services().ByName("PlanApprovalService").Methods().ByName("ApprovePlan"),
			Procedure:  PlanApprovalServiceApprovePlanProcedure,
		}
	})
)

// PlanApprovalServiceClient is a client for the conduit.plans.v1.PlanApprovalService service.
type PlanApprovalServiceClient interface {
	// ApprovePlan is POST /v1/plans/{planID}/approvals. Do not retry or REST-replay.
	ApprovePlan(context.Context, *v1.ApprovePlanRequest) (*v1.ApprovePlanResponse, error)
}

// NewPlanApprovalServiceClient constructs a client for the conduit.plans.v1.PlanApprovalService
// service. Multiple service clients may share a single connect.Client.
func NewPlanApprovalServiceClient(client *connect.Client) PlanApprovalServiceClient {
	return &planApprovalServiceClient{client: client}
}

// PlanApprovalServiceHandler is an implementation of the conduit.plans.v1.PlanApprovalService
// service.
type PlanApprovalServiceHandler interface {
	// ApprovePlan is POST /v1/plans/{planID}/approvals. Do not retry or REST-replay.
	ApprovePlan(context.Context, *v1.ApprovePlanRequest) (*v1.ApprovePlanResponse, error)
}

// RegisterPlanApprovalServiceHandler registers svc as the conduit.plans.v1.PlanApprovalService
// implementation on server.
func RegisterPlanApprovalServiceHandler(server *connect.Server, svc PlanApprovalServiceHandler) {
	adapter := planApprovalServiceHandler{svc: svc}
	server.Register(
		connect.Method{Spec: planApprovalServiceApprovePlanSpec(), Handler: adapter.approvePlan},
	)
}

// UnimplementedPlanApprovalServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedPlanApprovalServiceHandler struct{}

func (UnimplementedPlanApprovalServiceHandler) ApprovePlan(context.Context, *v1.ApprovePlanRequest) (*v1.ApprovePlanResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.plans.v1.PlanApprovalService.ApprovePlan is not implemented")
}

type planApprovalServiceClient struct {
	client *connect.Client
}

func (c *planApprovalServiceClient) ApprovePlan(ctx context.Context, req *v1.ApprovePlanRequest) (*v1.ApprovePlanResponse, error) {
	var res v1.ApprovePlanResponse
	if err := c.client.CallUnary(ctx, planApprovalServiceApprovePlanSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type planApprovalServiceHandler struct{ svc PlanApprovalServiceHandler }

func (h planApprovalServiceHandler) approvePlan(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.ApprovePlanRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.ApprovePlan(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}
