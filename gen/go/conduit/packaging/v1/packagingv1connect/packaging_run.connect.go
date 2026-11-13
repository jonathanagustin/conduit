//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
//
// Source: conduit/packaging/v1/packaging_run.proto

// Package conduit.packaging.v1 is the read contract for one packaging run.
// Other Hub domains use the same proto/conduit/<domain>/v1 layout.

package packagingv1connect

import (
	connect "connectrpc.com/connect"
	context "context"
	errors "errors"
	v1 "github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1"
	http "net/http"
	strings "strings"
)

// This is a compile-time assertion to ensure that this generated file and the connect package are
// compatible. If you get a compiler error that this constant is not defined, this code was
// generated with a version of connect newer than the one compiled into your binary. You can fix the
// problem by either regenerating this code with an older version of connect or updating the connect
// version compiled into your binary.
const _ = connect.IsAtLeastVersion1_13_0

const (
	// PackagingRunServiceName is the fully-qualified name of the PackagingRunService service.
	PackagingRunServiceName = "conduit.packaging.v1.PackagingRunService"
)

// These constants are the fully-qualified names of the RPCs defined in this package. They're
// exposed at runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// PackagingRunServiceGetPackagingRunProcedure is the fully-qualified name of the
	// PackagingRunService's GetPackagingRun RPC.
	PackagingRunServiceGetPackagingRunProcedure = "/conduit.packaging.v1.PackagingRunService/GetPackagingRun"
)

// PackagingRunServiceClient is a client for the conduit.packaging.v1.PackagingRunService service.
type PackagingRunServiceClient interface {
	// GetPackagingRun is the read model of GET /v1/packaging-runs/{run_id}.
	GetPackagingRun(context.Context, *connect.Request[v1.GetPackagingRunRequest]) (*connect.Response[v1.GetPackagingRunResponse], error)
}

// NewPackagingRunServiceClient constructs a client for the conduit.packaging.v1.PackagingRunService
// service. By default, it uses the Connect protocol with the binary Protobuf Codec, asks for
// gzipped responses, and sends uncompressed requests. To use the gRPC or gRPC-Web protocols, supply
// the connect.WithGRPC() or connect.WithGRPCWeb() options.
//
// The URL supplied here should be the base URL for the Connect or gRPC server (for example,
// http://api.acme.com or https://acme.com/grpc).
func NewPackagingRunServiceClient(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) PackagingRunServiceClient {
	baseURL = strings.TrimRight(baseURL, "/")
	packagingRunServiceMethods := v1.File_conduit_packaging_v1_packaging_run_proto.Services().ByName("PackagingRunService").Methods()
	return &packagingRunServiceClient{
		getPackagingRun: connect.NewClient[v1.GetPackagingRunRequest, v1.GetPackagingRunResponse](
			httpClient,
			baseURL+PackagingRunServiceGetPackagingRunProcedure,
			connect.WithSchema(packagingRunServiceMethods.ByName("GetPackagingRun")),
			connect.WithIdempotency(connect.IdempotencyNoSideEffects),
			connect.WithClientOptions(opts...),
		),
	}
}

// packagingRunServiceClient implements PackagingRunServiceClient.
type packagingRunServiceClient struct {
	getPackagingRun *connect.Client[v1.GetPackagingRunRequest, v1.GetPackagingRunResponse]
}

// GetPackagingRun calls conduit.packaging.v1.PackagingRunService.GetPackagingRun.
func (c *packagingRunServiceClient) GetPackagingRun(ctx context.Context, req *connect.Request[v1.GetPackagingRunRequest]) (*connect.Response[v1.GetPackagingRunResponse], error) {
	return c.getPackagingRun.CallUnary(ctx, req)
}

// PackagingRunServiceHandler is an implementation of the conduit.packaging.v1.PackagingRunService
// service.
type PackagingRunServiceHandler interface {
	// GetPackagingRun is the read model of GET /v1/packaging-runs/{run_id}.
	GetPackagingRun(context.Context, *connect.Request[v1.GetPackagingRunRequest]) (*connect.Response[v1.GetPackagingRunResponse], error)
}

// NewPackagingRunServiceHandler builds an HTTP handler from the service implementation. It returns
// the path on which to mount the handler and the handler itself.
//
// By default, handlers support the Connect, gRPC, and gRPC-Web protocols with the binary Protobuf
// and JSON codecs. They also support gzip compression.
func NewPackagingRunServiceHandler(svc PackagingRunServiceHandler, opts ...connect.HandlerOption) (string, http.Handler) {
	packagingRunServiceMethods := v1.File_conduit_packaging_v1_packaging_run_proto.Services().ByName("PackagingRunService").Methods()
	packagingRunServiceGetPackagingRunHandler := connect.NewUnaryHandler(
		PackagingRunServiceGetPackagingRunProcedure,
		svc.GetPackagingRun,
		connect.WithSchema(packagingRunServiceMethods.ByName("GetPackagingRun")),
		connect.WithIdempotency(connect.IdempotencyNoSideEffects),
		connect.WithHandlerOptions(opts...),
	)
	return "/conduit.packaging.v1.PackagingRunService/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case PackagingRunServiceGetPackagingRunProcedure:
			packagingRunServiceGetPackagingRunHandler.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// UnimplementedPackagingRunServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedPackagingRunServiceHandler struct{}

func (UnimplementedPackagingRunServiceHandler) GetPackagingRun(context.Context, *connect.Request[v1.GetPackagingRunRequest]) (*connect.Response[v1.GetPackagingRunResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("conduit.packaging.v1.PackagingRunService.GetPackagingRun is not implemented"))
}
