//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
//
// Source: conduit/environments/v1/environment_catalog.proto

package environmentsv1connect

import (
	connect "connectrpc.com/connect/v2"
	context "context"
	v1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	sync "sync"
)

const (
	// EnvironmentCatalogServiceName is the fully-qualified name of the EnvironmentCatalogService
	// service.
	EnvironmentCatalogServiceName = "conduit.environments.v1.EnvironmentCatalogService"
)

// These constants are the procedure names of the RPCs defined in this package. They're exposed at
// runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// EnvironmentCatalogServiceListEnvironmentsProcedure is the procedure name of the
	// EnvironmentCatalogService's ListEnvironments RPC.
	EnvironmentCatalogServiceListEnvironmentsProcedure = "/conduit.environments.v1.EnvironmentCatalogService/ListEnvironments"
)

var (
	environmentCatalogServiceListEnvironmentsSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType:       connect.StreamTypeUnary,
			Schema:           v1.File_conduit_environments_v1_environment_catalog_proto.Services().ByName("EnvironmentCatalogService").Methods().ByName("ListEnvironments"),
			Procedure:        EnvironmentCatalogServiceListEnvironmentsProcedure,
			IdempotencyLevel: connect.IdempotencyNoSideEffects,
		}
	})
)

// EnvironmentCatalogServiceClient is a client for the
// conduit.environments.v1.EnvironmentCatalogService service.
type EnvironmentCatalogServiceClient interface {
	// ListEnvironments corresponds to GET /v1/environments with absent/full view.
	// Filter by original-carrier resource authority and readable Space membership
	// before search, totals and cursor pagination. Membership grants no authority.
	ListEnvironments(context.Context, *v1.ListEnvironmentsRequest) (*v1.ListEnvironmentsResponse, error)
}

// NewEnvironmentCatalogServiceClient constructs a client for the
// conduit.environments.v1.EnvironmentCatalogService service. Multiple service clients may share a
// single connect.Client.
func NewEnvironmentCatalogServiceClient(client *connect.Client) EnvironmentCatalogServiceClient {
	return &environmentCatalogServiceClient{client: client}
}

// EnvironmentCatalogServiceHandler is an implementation of the
// conduit.environments.v1.EnvironmentCatalogService service.
type EnvironmentCatalogServiceHandler interface {
	// ListEnvironments corresponds to GET /v1/environments with absent/full view.
	// Filter by original-carrier resource authority and readable Space membership
	// before search, totals and cursor pagination. Membership grants no authority.
	ListEnvironments(context.Context, *v1.ListEnvironmentsRequest) (*v1.ListEnvironmentsResponse, error)
}

// RegisterEnvironmentCatalogServiceHandler registers svc as the
// conduit.environments.v1.EnvironmentCatalogService implementation on server.
func RegisterEnvironmentCatalogServiceHandler(server *connect.Server, svc EnvironmentCatalogServiceHandler) {
	adapter := environmentCatalogServiceHandler{svc: svc}
	server.Register(
		connect.Method{Spec: environmentCatalogServiceListEnvironmentsSpec(), Handler: adapter.listEnvironments},
	)
}

// UnimplementedEnvironmentCatalogServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedEnvironmentCatalogServiceHandler struct{}

func (UnimplementedEnvironmentCatalogServiceHandler) ListEnvironments(context.Context, *v1.ListEnvironmentsRequest) (*v1.ListEnvironmentsResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.environments.v1.EnvironmentCatalogService.ListEnvironments is not implemented")
}

type environmentCatalogServiceClient struct {
	client *connect.Client
}

func (c *environmentCatalogServiceClient) ListEnvironments(ctx context.Context, req *v1.ListEnvironmentsRequest) (*v1.ListEnvironmentsResponse, error) {
	var res v1.ListEnvironmentsResponse
	if err := c.client.CallUnary(ctx, environmentCatalogServiceListEnvironmentsSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type environmentCatalogServiceHandler struct {
	svc EnvironmentCatalogServiceHandler
}

func (h environmentCatalogServiceHandler) listEnvironments(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.ListEnvironmentsRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.ListEnvironments(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}
