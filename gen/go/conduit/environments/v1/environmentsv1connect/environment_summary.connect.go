//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
//
// Source: conduit/environments/v1/environment_summary.proto

package environmentsv1connect

import (
	connect "connectrpc.com/connect/v2"
	context "context"
	v1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	sync "sync"
)

const (
	// EnvironmentReadServiceName is the fully-qualified name of the EnvironmentReadService service.
	EnvironmentReadServiceName = "conduit.environments.v1.EnvironmentReadService"
)

// These constants are the procedure names of the RPCs defined in this package. They're exposed at
// runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// EnvironmentReadServiceListEnvironmentSummariesProcedure is the procedure name of the
	// EnvironmentReadService's ListEnvironmentSummaries RPC.
	EnvironmentReadServiceListEnvironmentSummariesProcedure = "/conduit.environments.v1.EnvironmentReadService/ListEnvironmentSummaries"
)

var (
	environmentReadServiceListEnvironmentSummariesSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType:       connect.StreamTypeUnary,
			Schema:           v1.File_conduit_environments_v1_environment_summary_proto.Services().ByName("EnvironmentReadService").Methods().ByName("ListEnvironmentSummaries"),
			Procedure:        EnvironmentReadServiceListEnvironmentSummariesProcedure,
			IdempotencyLevel: connect.IdempotencyNoSideEffects,
		}
	})
)

// EnvironmentReadServiceClient is a client for the conduit.environments.v1.EnvironmentReadService
// service.
type EnvironmentReadServiceClient interface {
	// ListEnvironmentSummaries projects GET /v1/environments?view=summary.
	// It omits Environment configuration/policy collections and Entity bodies.
	ListEnvironmentSummaries(context.Context, *v1.ListEnvironmentSummariesRequest) (*v1.ListEnvironmentSummariesResponse, error)
}

// NewEnvironmentReadServiceClient constructs a client for the
// conduit.environments.v1.EnvironmentReadService service. Multiple service clients may share a
// single connect.Client.
func NewEnvironmentReadServiceClient(client *connect.Client) EnvironmentReadServiceClient {
	return &environmentReadServiceClient{client: client}
}

// EnvironmentReadServiceHandler is an implementation of the
// conduit.environments.v1.EnvironmentReadService service.
type EnvironmentReadServiceHandler interface {
	// ListEnvironmentSummaries projects GET /v1/environments?view=summary.
	// It omits Environment configuration/policy collections and Entity bodies.
	ListEnvironmentSummaries(context.Context, *v1.ListEnvironmentSummariesRequest) (*v1.ListEnvironmentSummariesResponse, error)
}

// RegisterEnvironmentReadServiceHandler registers svc as the
// conduit.environments.v1.EnvironmentReadService implementation on server.
func RegisterEnvironmentReadServiceHandler(server *connect.Server, svc EnvironmentReadServiceHandler) {
	adapter := environmentReadServiceHandler{svc: svc}
	server.Register(
		connect.Method{Spec: environmentReadServiceListEnvironmentSummariesSpec(), Handler: adapter.listEnvironmentSummaries},
	)
}

// UnimplementedEnvironmentReadServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedEnvironmentReadServiceHandler struct{}

func (UnimplementedEnvironmentReadServiceHandler) ListEnvironmentSummaries(context.Context, *v1.ListEnvironmentSummariesRequest) (*v1.ListEnvironmentSummariesResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.environments.v1.EnvironmentReadService.ListEnvironmentSummaries is not implemented")
}

type environmentReadServiceClient struct {
	client *connect.Client
}

func (c *environmentReadServiceClient) ListEnvironmentSummaries(ctx context.Context, req *v1.ListEnvironmentSummariesRequest) (*v1.ListEnvironmentSummariesResponse, error) {
	var res v1.ListEnvironmentSummariesResponse
	if err := c.client.CallUnary(ctx, environmentReadServiceListEnvironmentSummariesSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type environmentReadServiceHandler struct{ svc EnvironmentReadServiceHandler }

func (h environmentReadServiceHandler) listEnvironmentSummaries(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.ListEnvironmentSummariesRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.ListEnvironmentSummaries(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}
