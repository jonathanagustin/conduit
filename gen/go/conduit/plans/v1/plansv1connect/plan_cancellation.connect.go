//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
//
// Source: conduit/plans/v1/plan_cancellation.proto

package plansv1connect

import (
	connect "connectrpc.com/connect/v2"
	context "context"
	v1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	sync "sync"
)

const (
	// PlanCancellationServiceName is the fully-qualified name of the PlanCancellationService service.
	PlanCancellationServiceName = "conduit.plans.v1.PlanCancellationService"
)

// These constants are the procedure names of the RPCs defined in this package. They're exposed at
// runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// PlanCancellationServiceCancelPlanProcedure is the procedure name of the PlanCancellationService's
	// CancelPlan RPC.
	PlanCancellationServiceCancelPlanProcedure = "/conduit.plans.v1.PlanCancellationService/CancelPlan"
)

var (
	planCancellationServiceCancelPlanSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType: connect.StreamTypeUnary,
			Schema:     v1.File_conduit_plans_v1_plan_cancellation_proto.Services().ByName("PlanCancellationService").Methods().ByName("CancelPlan"),
			Procedure:  PlanCancellationServiceCancelPlanProcedure,
		}
	})
)

// PlanCancellationServiceClient is a client for the conduit.plans.v1.PlanCancellationService
// service.
type PlanCancellationServiceClient interface {
	// CancelPlan is POST /v1/plans/{planID}/cancel. Never retry or REST-replay.
	CancelPlan(context.Context, *v1.CancelPlanRequest) (*v1.CancelPlanResponse, error)
}

// NewPlanCancellationServiceClient constructs a client for the
// conduit.plans.v1.PlanCancellationService service. Multiple service clients may share a single
// connect.Client.
func NewPlanCancellationServiceClient(client *connect.Client) PlanCancellationServiceClient {
	return &planCancellationServiceClient{client: client}
}

// PlanCancellationServiceHandler is an implementation of the
// conduit.plans.v1.PlanCancellationService service.
type PlanCancellationServiceHandler interface {
	// CancelPlan is POST /v1/plans/{planID}/cancel. Never retry or REST-replay.
	CancelPlan(context.Context, *v1.CancelPlanRequest) (*v1.CancelPlanResponse, error)
}

// RegisterPlanCancellationServiceHandler registers svc as the
// conduit.plans.v1.PlanCancellationService implementation on server.
func RegisterPlanCancellationServiceHandler(server *connect.Server, svc PlanCancellationServiceHandler) {
	adapter := planCancellationServiceHandler{svc: svc}
	server.Register(
		connect.Method{Spec: planCancellationServiceCancelPlanSpec(), Handler: adapter.cancelPlan},
	)
}

// UnimplementedPlanCancellationServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedPlanCancellationServiceHandler struct{}

func (UnimplementedPlanCancellationServiceHandler) CancelPlan(context.Context, *v1.CancelPlanRequest) (*v1.CancelPlanResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.plans.v1.PlanCancellationService.CancelPlan is not implemented")
}

type planCancellationServiceClient struct {
	client *connect.Client
}

func (c *planCancellationServiceClient) CancelPlan(ctx context.Context, req *v1.CancelPlanRequest) (*v1.CancelPlanResponse, error) {
	var res v1.CancelPlanResponse
	if err := c.client.CallUnary(ctx, planCancellationServiceCancelPlanSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type planCancellationServiceHandler struct {
	svc PlanCancellationServiceHandler
}

func (h planCancellationServiceHandler) cancelPlan(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.CancelPlanRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.CancelPlan(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}
