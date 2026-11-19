//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
//
// Source: conduit/fleet/v1/fleet.proto

package fleetv1connect

import (
	connect "connectrpc.com/connect/v2"
	context "context"
	v1 "github.com/jonathanagustin/conduit/gen/go/conduit/fleet/v1"
	sync "sync"
)

const (
	// FleetReadServiceName is the fully-qualified name of the FleetReadService service.
	FleetReadServiceName = "conduit.fleet.v1.FleetReadService"
)

// These constants are the procedure names of the RPCs defined in this package. They're exposed at
// runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// FleetReadServiceListFleetInstallationsProcedure is the procedure name of the FleetReadService's
	// ListFleetInstallations RPC.
	FleetReadServiceListFleetInstallationsProcedure = "/conduit.fleet.v1.FleetReadService/ListFleetInstallations"
)

var (
	fleetReadServiceListFleetInstallationsSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType:       connect.StreamTypeUnary,
			Schema:           v1.File_conduit_fleet_v1_fleet_proto.Services().ByName("FleetReadService").Methods().ByName("ListFleetInstallations"),
			Procedure:        FleetReadServiceListFleetInstallationsProcedure,
			IdempotencyLevel: connect.IdempotencyNoSideEffects,
		}
	})
)

// FleetReadServiceClient is a client for the conduit.fleet.v1.FleetReadService service.
type FleetReadServiceClient interface {
	// ListFleetInstallations is GET /v1/fleet/installations, not a Fleet action.
	ListFleetInstallations(context.Context, *v1.ListFleetInstallationsRequest) (*v1.ListFleetInstallationsResponse, error)
}

// NewFleetReadServiceClient constructs a client for the conduit.fleet.v1.FleetReadService service.
// Multiple service clients may share a single connect.Client.
func NewFleetReadServiceClient(client *connect.Client) FleetReadServiceClient {
	return &fleetReadServiceClient{client: client}
}

// FleetReadServiceHandler is an implementation of the conduit.fleet.v1.FleetReadService service.
type FleetReadServiceHandler interface {
	// ListFleetInstallations is GET /v1/fleet/installations, not a Fleet action.
	ListFleetInstallations(context.Context, *v1.ListFleetInstallationsRequest) (*v1.ListFleetInstallationsResponse, error)
}

// RegisterFleetReadServiceHandler registers svc as the conduit.fleet.v1.FleetReadService
// implementation on server.
func RegisterFleetReadServiceHandler(server *connect.Server, svc FleetReadServiceHandler) {
	adapter := fleetReadServiceHandler{svc: svc}
	server.Register(
		connect.Method{Spec: fleetReadServiceListFleetInstallationsSpec(), Handler: adapter.listFleetInstallations},
	)
}

// UnimplementedFleetReadServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedFleetReadServiceHandler struct{}

func (UnimplementedFleetReadServiceHandler) ListFleetInstallations(context.Context, *v1.ListFleetInstallationsRequest) (*v1.ListFleetInstallationsResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.fleet.v1.FleetReadService.ListFleetInstallations is not implemented")
}

type fleetReadServiceClient struct {
	client *connect.Client
}

func (c *fleetReadServiceClient) ListFleetInstallations(ctx context.Context, req *v1.ListFleetInstallationsRequest) (*v1.ListFleetInstallationsResponse, error) {
	var res v1.ListFleetInstallationsResponse
	if err := c.client.CallUnary(ctx, fleetReadServiceListFleetInstallationsSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type fleetReadServiceHandler struct{ svc FleetReadServiceHandler }

func (h fleetReadServiceHandler) listFleetInstallations(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.ListFleetInstallationsRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.ListFleetInstallations(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}
