//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
//
// Source: conduit/packaging/v1/packaging_run.proto

// Package conduit.packaging.v1 contains packaging-run read contracts.
// Other Hub domains use the same proto/conduit/<domain>/v1 layout.

package packagingv1connect

import (
	connect "connectrpc.com/connect/v2"
	context "context"
	v1 "github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1"
	sync "sync"
)

const (
	// PackagingRunServiceName is the fully-qualified name of the PackagingRunService service.
	PackagingRunServiceName = "conduit.packaging.v1.PackagingRunService"
)

// These constants are the procedure names of the RPCs defined in this package. They're exposed at
// runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// PackagingRunServiceListPackagingRunsProcedure is the procedure name of the PackagingRunService's
	// ListPackagingRuns RPC.
	PackagingRunServiceListPackagingRunsProcedure = "/conduit.packaging.v1.PackagingRunService/ListPackagingRuns"
	// PackagingRunServiceListPackagingRunEventsProcedure is the procedure name of the
	// PackagingRunService's ListPackagingRunEvents RPC.
	PackagingRunServiceListPackagingRunEventsProcedure = "/conduit.packaging.v1.PackagingRunService/ListPackagingRunEvents"
	// PackagingRunServiceGetPackagingRunProcedure is the procedure name of the PackagingRunService's
	// GetPackagingRun RPC.
	PackagingRunServiceGetPackagingRunProcedure = "/conduit.packaging.v1.PackagingRunService/GetPackagingRun"
)

var (
	packagingRunServiceListPackagingRunsSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType:       connect.StreamTypeUnary,
			Schema:           v1.File_conduit_packaging_v1_packaging_run_proto.Services().ByName("PackagingRunService").Methods().ByName("ListPackagingRuns"),
			Procedure:        PackagingRunServiceListPackagingRunsProcedure,
			IdempotencyLevel: connect.IdempotencyNoSideEffects,
		}
	})
	packagingRunServiceListPackagingRunEventsSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType:       connect.StreamTypeUnary,
			Schema:           v1.File_conduit_packaging_v1_packaging_run_proto.Services().ByName("PackagingRunService").Methods().ByName("ListPackagingRunEvents"),
			Procedure:        PackagingRunServiceListPackagingRunEventsProcedure,
			IdempotencyLevel: connect.IdempotencyNoSideEffects,
		}
	})
	packagingRunServiceGetPackagingRunSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType:       connect.StreamTypeUnary,
			Schema:           v1.File_conduit_packaging_v1_packaging_run_proto.Services().ByName("PackagingRunService").Methods().ByName("GetPackagingRun"),
			Procedure:        PackagingRunServiceGetPackagingRunProcedure,
			IdempotencyLevel: connect.IdempotencyNoSideEffects,
		}
	})
)

// PackagingRunServiceClient is a client for the conduit.packaging.v1.PackagingRunService service.
type PackagingRunServiceClient interface {
	// ListPackagingRuns reads GET /v1/packaging-runs with the same visibility and cursors.
	ListPackagingRuns(context.Context, *v1.ListPackagingRunsRequest) (*v1.ListPackagingRunsResponse, error)
	// ListPackagingRunEvents reads the recorded, redacted history of one visible run.
	ListPackagingRunEvents(context.Context, *v1.ListPackagingRunEventsRequest) (*v1.ListPackagingRunEventsResponse, error)
	// GetPackagingRun is the read model of GET /v1/packaging-runs/{run_id}.
	GetPackagingRun(context.Context, *v1.GetPackagingRunRequest) (*v1.GetPackagingRunResponse, error)
}

// NewPackagingRunServiceClient constructs a client for the conduit.packaging.v1.PackagingRunService
// service. Multiple service clients may share a single connect.Client.
func NewPackagingRunServiceClient(client *connect.Client) PackagingRunServiceClient {
	return &packagingRunServiceClient{client: client}
}

// PackagingRunServiceHandler is an implementation of the conduit.packaging.v1.PackagingRunService
// service.
type PackagingRunServiceHandler interface {
	// ListPackagingRuns reads GET /v1/packaging-runs with the same visibility and cursors.
	ListPackagingRuns(context.Context, *v1.ListPackagingRunsRequest) (*v1.ListPackagingRunsResponse, error)
	// ListPackagingRunEvents reads the recorded, redacted history of one visible run.
	ListPackagingRunEvents(context.Context, *v1.ListPackagingRunEventsRequest) (*v1.ListPackagingRunEventsResponse, error)
	// GetPackagingRun is the read model of GET /v1/packaging-runs/{run_id}.
	GetPackagingRun(context.Context, *v1.GetPackagingRunRequest) (*v1.GetPackagingRunResponse, error)
}

// RegisterPackagingRunServiceHandler registers svc as the conduit.packaging.v1.PackagingRunService
// implementation on server.
func RegisterPackagingRunServiceHandler(server *connect.Server, svc PackagingRunServiceHandler) {
	adapter := packagingRunServiceHandler{svc: svc}
	server.Register(
		connect.Method{Spec: packagingRunServiceListPackagingRunsSpec(), Handler: adapter.listPackagingRuns},
		connect.Method{Spec: packagingRunServiceListPackagingRunEventsSpec(), Handler: adapter.listPackagingRunEvents},
		connect.Method{Spec: packagingRunServiceGetPackagingRunSpec(), Handler: adapter.getPackagingRun},
	)
}

// UnimplementedPackagingRunServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedPackagingRunServiceHandler struct{}

func (UnimplementedPackagingRunServiceHandler) ListPackagingRuns(context.Context, *v1.ListPackagingRunsRequest) (*v1.ListPackagingRunsResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.packaging.v1.PackagingRunService.ListPackagingRuns is not implemented")
}

func (UnimplementedPackagingRunServiceHandler) ListPackagingRunEvents(context.Context, *v1.ListPackagingRunEventsRequest) (*v1.ListPackagingRunEventsResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.packaging.v1.PackagingRunService.ListPackagingRunEvents is not implemented")
}

func (UnimplementedPackagingRunServiceHandler) GetPackagingRun(context.Context, *v1.GetPackagingRunRequest) (*v1.GetPackagingRunResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.packaging.v1.PackagingRunService.GetPackagingRun is not implemented")
}

type packagingRunServiceClient struct {
	client *connect.Client
}

func (c *packagingRunServiceClient) ListPackagingRuns(ctx context.Context, req *v1.ListPackagingRunsRequest) (*v1.ListPackagingRunsResponse, error) {
	var res v1.ListPackagingRunsResponse
	if err := c.client.CallUnary(ctx, packagingRunServiceListPackagingRunsSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *packagingRunServiceClient) ListPackagingRunEvents(ctx context.Context, req *v1.ListPackagingRunEventsRequest) (*v1.ListPackagingRunEventsResponse, error) {
	var res v1.ListPackagingRunEventsResponse
	if err := c.client.CallUnary(ctx, packagingRunServiceListPackagingRunEventsSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *packagingRunServiceClient) GetPackagingRun(ctx context.Context, req *v1.GetPackagingRunRequest) (*v1.GetPackagingRunResponse, error) {
	var res v1.GetPackagingRunResponse
	if err := c.client.CallUnary(ctx, packagingRunServiceGetPackagingRunSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type packagingRunServiceHandler struct{ svc PackagingRunServiceHandler }

func (h packagingRunServiceHandler) listPackagingRuns(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.ListPackagingRunsRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.ListPackagingRuns(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}

func (h packagingRunServiceHandler) listPackagingRunEvents(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.ListPackagingRunEventsRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.ListPackagingRunEvents(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}

func (h packagingRunServiceHandler) getPackagingRun(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.GetPackagingRunRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.GetPackagingRun(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}
