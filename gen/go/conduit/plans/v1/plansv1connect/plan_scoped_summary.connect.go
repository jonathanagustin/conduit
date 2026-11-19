//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
//
// Source: conduit/plans/v1/plan_scoped_summary.proto

package plansv1connect

import (
	connect "connectrpc.com/connect/v2"
	context "context"
	v1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	sync "sync"
)

const (
	// PlanScopedReadServiceName is the fully-qualified name of the PlanScopedReadService service.
	PlanScopedReadServiceName = "conduit.plans.v1.PlanScopedReadService"
)

// These constants are the procedure names of the RPCs defined in this package. They're exposed at
// runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// PlanScopedReadServiceListPlanSummariesInSpaceProcedure is the procedure name of the
	// PlanScopedReadService's ListPlanSummariesInSpace RPC.
	PlanScopedReadServiceListPlanSummariesInSpaceProcedure = "/conduit.plans.v1.PlanScopedReadService/ListPlanSummariesInSpace"
)

var (
	planScopedReadServiceListPlanSummariesInSpaceSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType:       connect.StreamTypeUnary,
			Schema:           v1.File_conduit_plans_v1_plan_scoped_summary_proto.Services().ByName("PlanScopedReadService").Methods().ByName("ListPlanSummariesInSpace"),
			Procedure:        PlanScopedReadServiceListPlanSummariesInSpaceProcedure,
			IdempotencyLevel: connect.IdempotencyNoSideEffects,
		}
	})
)

// PlanScopedReadServiceClient is a client for the conduit.plans.v1.PlanScopedReadService service.
type PlanScopedReadServiceClient interface {
	// ListPlanSummariesInSpace reads GET /v1/plans?view=summary&spaceId=SPACE.
	// Authorize the original carrier's selected persisted Space before private
	// membership and Plan reads. Membership narrows visibility; it grants none.
	ListPlanSummariesInSpace(context.Context, *v1.ListPlanSummariesInSpaceRequest) (*v1.ListPlanSummariesInSpaceResponse, error)
}

// NewPlanScopedReadServiceClient constructs a client for the conduit.plans.v1.PlanScopedReadService
// service. Multiple service clients may share a single connect.Client.
func NewPlanScopedReadServiceClient(client *connect.Client) PlanScopedReadServiceClient {
	return &planScopedReadServiceClient{client: client}
}

// PlanScopedReadServiceHandler is an implementation of the conduit.plans.v1.PlanScopedReadService
// service.
type PlanScopedReadServiceHandler interface {
	// ListPlanSummariesInSpace reads GET /v1/plans?view=summary&spaceId=SPACE.
	// Authorize the original carrier's selected persisted Space before private
	// membership and Plan reads. Membership narrows visibility; it grants none.
	ListPlanSummariesInSpace(context.Context, *v1.ListPlanSummariesInSpaceRequest) (*v1.ListPlanSummariesInSpaceResponse, error)
}

// RegisterPlanScopedReadServiceHandler registers svc as the conduit.plans.v1.PlanScopedReadService
// implementation on server.
func RegisterPlanScopedReadServiceHandler(server *connect.Server, svc PlanScopedReadServiceHandler) {
	adapter := planScopedReadServiceHandler{svc: svc}
	server.Register(
		connect.Method{Spec: planScopedReadServiceListPlanSummariesInSpaceSpec(), Handler: adapter.listPlanSummariesInSpace},
	)
}

// UnimplementedPlanScopedReadServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedPlanScopedReadServiceHandler struct{}

func (UnimplementedPlanScopedReadServiceHandler) ListPlanSummariesInSpace(context.Context, *v1.ListPlanSummariesInSpaceRequest) (*v1.ListPlanSummariesInSpaceResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.plans.v1.PlanScopedReadService.ListPlanSummariesInSpace is not implemented")
}

type planScopedReadServiceClient struct {
	client *connect.Client
}

func (c *planScopedReadServiceClient) ListPlanSummariesInSpace(ctx context.Context, req *v1.ListPlanSummariesInSpaceRequest) (*v1.ListPlanSummariesInSpaceResponse, error) {
	var res v1.ListPlanSummariesInSpaceResponse
	if err := c.client.CallUnary(ctx, planScopedReadServiceListPlanSummariesInSpaceSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type planScopedReadServiceHandler struct{ svc PlanScopedReadServiceHandler }

func (h planScopedReadServiceHandler) listPlanSummariesInSpace(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.ListPlanSummariesInSpaceRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.ListPlanSummariesInSpace(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}
