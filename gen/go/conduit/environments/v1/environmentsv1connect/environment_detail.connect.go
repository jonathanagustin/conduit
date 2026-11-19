//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
//
// Source: conduit/environments/v1/environment_detail.proto

package environmentsv1connect

import (
	connect "connectrpc.com/connect/v2"
	context "context"
	v1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	sync "sync"
)

const (
	// EnvironmentDetailServiceName is the fully-qualified name of the EnvironmentDetailService service.
	EnvironmentDetailServiceName = "conduit.environments.v1.EnvironmentDetailService"
)

// These constants are the procedure names of the RPCs defined in this package. They're exposed at
// runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// EnvironmentDetailServiceGetEnvironmentProcedure is the procedure name of the
	// EnvironmentDetailService's GetEnvironment RPC.
	EnvironmentDetailServiceGetEnvironmentProcedure = "/conduit.environments.v1.EnvironmentDetailService/GetEnvironment"
)

var (
	environmentDetailServiceGetEnvironmentSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType:       connect.StreamTypeUnary,
			Schema:           v1.File_conduit_environments_v1_environment_detail_proto.Services().ByName("EnvironmentDetailService").Methods().ByName("GetEnvironment"),
			Procedure:        EnvironmentDetailServiceGetEnvironmentProcedure,
			IdempotencyLevel: connect.IdempotencyNoSideEffects,
		}
	})
)

// EnvironmentDetailServiceClient is a client for the
// conduit.environments.v1.EnvironmentDetailService service.
type EnvironmentDetailServiceClient interface {
	// GetEnvironment corresponds to GET /v1/environments/{environmentID}.
	// Preserve original-carrier resource authority before Space membership reads.
	GetEnvironment(context.Context, *v1.GetEnvironmentRequest) (*v1.GetEnvironmentResponse, error)
}

// NewEnvironmentDetailServiceClient constructs a client for the
// conduit.environments.v1.EnvironmentDetailService service. Multiple service clients may share a
// single connect.Client.
func NewEnvironmentDetailServiceClient(client *connect.Client) EnvironmentDetailServiceClient {
	return &environmentDetailServiceClient{client: client}
}

// EnvironmentDetailServiceHandler is an implementation of the
// conduit.environments.v1.EnvironmentDetailService service.
type EnvironmentDetailServiceHandler interface {
	// GetEnvironment corresponds to GET /v1/environments/{environmentID}.
	// Preserve original-carrier resource authority before Space membership reads.
	GetEnvironment(context.Context, *v1.GetEnvironmentRequest) (*v1.GetEnvironmentResponse, error)
}

// RegisterEnvironmentDetailServiceHandler registers svc as the
// conduit.environments.v1.EnvironmentDetailService implementation on server.
func RegisterEnvironmentDetailServiceHandler(server *connect.Server, svc EnvironmentDetailServiceHandler) {
	adapter := environmentDetailServiceHandler{svc: svc}
	server.Register(
		connect.Method{Spec: environmentDetailServiceGetEnvironmentSpec(), Handler: adapter.getEnvironment},
	)
}

// UnimplementedEnvironmentDetailServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedEnvironmentDetailServiceHandler struct{}

func (UnimplementedEnvironmentDetailServiceHandler) GetEnvironment(context.Context, *v1.GetEnvironmentRequest) (*v1.GetEnvironmentResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.environments.v1.EnvironmentDetailService.GetEnvironment is not implemented")
}

type environmentDetailServiceClient struct {
	client *connect.Client
}

func (c *environmentDetailServiceClient) GetEnvironment(ctx context.Context, req *v1.GetEnvironmentRequest) (*v1.GetEnvironmentResponse, error) {
	var res v1.GetEnvironmentResponse
	if err := c.client.CallUnary(ctx, environmentDetailServiceGetEnvironmentSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type environmentDetailServiceHandler struct {
	svc EnvironmentDetailServiceHandler
}

func (h environmentDetailServiceHandler) getEnvironment(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.GetEnvironmentRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.GetEnvironment(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}
