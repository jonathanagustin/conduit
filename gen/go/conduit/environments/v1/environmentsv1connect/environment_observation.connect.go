//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
//
// Source: conduit/environments/v1/environment_observation.proto

package environmentsv1connect

import (
	connect "connectrpc.com/connect/v2"
	context "context"
	v1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	sync "sync"
)

const (
	// EnvironmentObservationServiceName is the fully-qualified name of the
	// EnvironmentObservationService service.
	EnvironmentObservationServiceName = "conduit.environments.v1.EnvironmentObservationService"
)

// These constants are the procedure names of the RPCs defined in this package. They're exposed at
// runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// EnvironmentObservationServiceGetEnvironmentReportedStateProcedure is the procedure name of the
	// EnvironmentObservationService's GetEnvironmentReportedState RPC.
	EnvironmentObservationServiceGetEnvironmentReportedStateProcedure = "/conduit.environments.v1.EnvironmentObservationService/GetEnvironmentReportedState"
)

var (
	environmentObservationServiceGetEnvironmentReportedStateSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType:       connect.StreamTypeUnary,
			Schema:           v1.File_conduit_environments_v1_environment_observation_proto.Services().ByName("EnvironmentObservationService").Methods().ByName("GetEnvironmentReportedState"),
			Procedure:        EnvironmentObservationServiceGetEnvironmentReportedStateProcedure,
			IdempotencyLevel: connect.IdempotencyNoSideEffects,
		}
	})
)

// EnvironmentObservationServiceClient is a client for the
// conduit.environments.v1.EnvironmentObservationService service.
type EnvironmentObservationServiceClient interface {
	// GetEnvironmentReportedState is GET /v1/environments/{environmentID}/reported-state.
	// It requires ReadResource on that Environment, including secret references.
	GetEnvironmentReportedState(context.Context, *v1.GetEnvironmentReportedStateRequest) (*v1.GetEnvironmentReportedStateResponse, error)
}

// NewEnvironmentObservationServiceClient constructs a client for the
// conduit.environments.v1.EnvironmentObservationService service. Multiple service clients may share
// a single connect.Client.
func NewEnvironmentObservationServiceClient(client *connect.Client) EnvironmentObservationServiceClient {
	return &environmentObservationServiceClient{client: client}
}

// EnvironmentObservationServiceHandler is an implementation of the
// conduit.environments.v1.EnvironmentObservationService service.
type EnvironmentObservationServiceHandler interface {
	// GetEnvironmentReportedState is GET /v1/environments/{environmentID}/reported-state.
	// It requires ReadResource on that Environment, including secret references.
	GetEnvironmentReportedState(context.Context, *v1.GetEnvironmentReportedStateRequest) (*v1.GetEnvironmentReportedStateResponse, error)
}

// RegisterEnvironmentObservationServiceHandler registers svc as the
// conduit.environments.v1.EnvironmentObservationService implementation on server.
func RegisterEnvironmentObservationServiceHandler(server *connect.Server, svc EnvironmentObservationServiceHandler) {
	adapter := environmentObservationServiceHandler{svc: svc}
	server.Register(
		connect.Method{Spec: environmentObservationServiceGetEnvironmentReportedStateSpec(), Handler: adapter.getEnvironmentReportedState},
	)
}

// UnimplementedEnvironmentObservationServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedEnvironmentObservationServiceHandler struct{}

func (UnimplementedEnvironmentObservationServiceHandler) GetEnvironmentReportedState(context.Context, *v1.GetEnvironmentReportedStateRequest) (*v1.GetEnvironmentReportedStateResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.environments.v1.EnvironmentObservationService.GetEnvironmentReportedState is not implemented")
}

type environmentObservationServiceClient struct {
	client *connect.Client
}

func (c *environmentObservationServiceClient) GetEnvironmentReportedState(ctx context.Context, req *v1.GetEnvironmentReportedStateRequest) (*v1.GetEnvironmentReportedStateResponse, error) {
	var res v1.GetEnvironmentReportedStateResponse
	if err := c.client.CallUnary(ctx, environmentObservationServiceGetEnvironmentReportedStateSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type environmentObservationServiceHandler struct {
	svc EnvironmentObservationServiceHandler
}

func (h environmentObservationServiceHandler) getEnvironmentReportedState(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.GetEnvironmentReportedStateRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.GetEnvironmentReportedState(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}
