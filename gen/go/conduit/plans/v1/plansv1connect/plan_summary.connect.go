//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
//
// Source: conduit/plans/v1/plan_summary.proto

package plansv1connect

import (
	connect "connectrpc.com/connect/v2"
	context "context"
	v1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	sync "sync"
)

const (
	// PlanReadServiceName is the fully-qualified name of the PlanReadService service.
	PlanReadServiceName = "conduit.plans.v1.PlanReadService"
)

// These constants are the procedure names of the RPCs defined in this package. They're exposed at
// runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// PlanReadServiceListPlanSummariesProcedure is the procedure name of the PlanReadService's
	// ListPlanSummaries RPC.
	PlanReadServiceListPlanSummariesProcedure = "/conduit.plans.v1.PlanReadService/ListPlanSummaries"
)

var (
	planReadServiceListPlanSummariesSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType:       connect.StreamTypeUnary,
			Schema:           v1.File_conduit_plans_v1_plan_summary_proto.Services().ByName("PlanReadService").Methods().ByName("ListPlanSummaries"),
			Procedure:        PlanReadServiceListPlanSummariesProcedure,
			IdempotencyLevel: connect.IdempotencyNoSideEffects,
		}
	})
)

// PlanReadServiceClient is a client for the conduit.plans.v1.PlanReadService service.
type PlanReadServiceClient interface {
	// ListPlanSummaries reads GET /v1/plans?view=summary. It never returns a lease.
	ListPlanSummaries(context.Context, *v1.ListPlanSummariesRequest) (*v1.ListPlanSummariesResponse, error)
}

// NewPlanReadServiceClient constructs a client for the conduit.plans.v1.PlanReadService service.
// Multiple service clients may share a single connect.Client.
func NewPlanReadServiceClient(client *connect.Client) PlanReadServiceClient {
	return &planReadServiceClient{client: client}
}

// PlanReadServiceHandler is an implementation of the conduit.plans.v1.PlanReadService service.
type PlanReadServiceHandler interface {
	// ListPlanSummaries reads GET /v1/plans?view=summary. It never returns a lease.
	ListPlanSummaries(context.Context, *v1.ListPlanSummariesRequest) (*v1.ListPlanSummariesResponse, error)
}

// RegisterPlanReadServiceHandler registers svc as the conduit.plans.v1.PlanReadService
// implementation on server.
func RegisterPlanReadServiceHandler(server *connect.Server, svc PlanReadServiceHandler) {
	adapter := planReadServiceHandler{svc: svc}
	server.Register(
		connect.Method{Spec: planReadServiceListPlanSummariesSpec(), Handler: adapter.listPlanSummaries},
	)
}

// UnimplementedPlanReadServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedPlanReadServiceHandler struct{}

func (UnimplementedPlanReadServiceHandler) ListPlanSummaries(context.Context, *v1.ListPlanSummariesRequest) (*v1.ListPlanSummariesResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.plans.v1.PlanReadService.ListPlanSummaries is not implemented")
}

type planReadServiceClient struct {
	client *connect.Client
}

func (c *planReadServiceClient) ListPlanSummaries(ctx context.Context, req *v1.ListPlanSummariesRequest) (*v1.ListPlanSummariesResponse, error) {
	var res v1.ListPlanSummariesResponse
	if err := c.client.CallUnary(ctx, planReadServiceListPlanSummariesSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type planReadServiceHandler struct{ svc PlanReadServiceHandler }

func (h planReadServiceHandler) listPlanSummaries(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.ListPlanSummariesRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.ListPlanSummaries(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}
