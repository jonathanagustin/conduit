//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
//
// Source: conduit/catalog/v1/product_summary.proto

package catalogv1connect

import (
	connect "connectrpc.com/connect/v2"
	context "context"
	v1 "github.com/jonathanagustin/conduit/gen/go/conduit/catalog/v1"
	sync "sync"
)

const (
	// ProductReadServiceName is the fully-qualified name of the ProductReadService service.
	ProductReadServiceName = "conduit.catalog.v1.ProductReadService"
)

// These constants are the procedure names of the RPCs defined in this package. They're exposed at
// runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// ProductReadServiceListProductSummariesProcedure is the procedure name of the ProductReadService's
	// ListProductSummaries RPC.
	ProductReadServiceListProductSummariesProcedure = "/conduit.catalog.v1.ProductReadService/ListProductSummaries"
)

var (
	productReadServiceListProductSummariesSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType:       connect.StreamTypeUnary,
			Schema:           v1.File_conduit_catalog_v1_product_summary_proto.Services().ByName("ProductReadService").Methods().ByName("ListProductSummaries"),
			Procedure:        ProductReadServiceListProductSummariesProcedure,
			IdempotencyLevel: connect.IdempotencyNoSideEffects,
		}
	})
)

// ProductReadServiceClient is a client for the conduit.catalog.v1.ProductReadService service.
type ProductReadServiceClient interface {
	// ListProductSummaries reads GET /v1/products?view=summary, never publications.
	ListProductSummaries(context.Context, *v1.ListProductSummariesRequest) (*v1.ListProductSummariesResponse, error)
}

// NewProductReadServiceClient constructs a client for the conduit.catalog.v1.ProductReadService
// service. Multiple service clients may share a single connect.Client.
func NewProductReadServiceClient(client *connect.Client) ProductReadServiceClient {
	return &productReadServiceClient{client: client}
}

// ProductReadServiceHandler is an implementation of the conduit.catalog.v1.ProductReadService
// service.
type ProductReadServiceHandler interface {
	// ListProductSummaries reads GET /v1/products?view=summary, never publications.
	ListProductSummaries(context.Context, *v1.ListProductSummariesRequest) (*v1.ListProductSummariesResponse, error)
}

// RegisterProductReadServiceHandler registers svc as the conduit.catalog.v1.ProductReadService
// implementation on server.
func RegisterProductReadServiceHandler(server *connect.Server, svc ProductReadServiceHandler) {
	adapter := productReadServiceHandler{svc: svc}
	server.Register(
		connect.Method{Spec: productReadServiceListProductSummariesSpec(), Handler: adapter.listProductSummaries},
	)
}

// UnimplementedProductReadServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedProductReadServiceHandler struct{}

func (UnimplementedProductReadServiceHandler) ListProductSummaries(context.Context, *v1.ListProductSummariesRequest) (*v1.ListProductSummariesResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.catalog.v1.ProductReadService.ListProductSummaries is not implemented")
}

type productReadServiceClient struct {
	client *connect.Client
}

func (c *productReadServiceClient) ListProductSummaries(ctx context.Context, req *v1.ListProductSummariesRequest) (*v1.ListProductSummariesResponse, error) {
	var res v1.ListProductSummariesResponse
	if err := c.client.CallUnary(ctx, productReadServiceListProductSummariesSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type productReadServiceHandler struct{ svc ProductReadServiceHandler }

func (h productReadServiceHandler) listProductSummaries(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.ListProductSummariesRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.ListProductSummaries(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}
