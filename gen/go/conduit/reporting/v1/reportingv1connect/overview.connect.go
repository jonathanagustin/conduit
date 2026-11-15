//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
//
// Source: conduit/reporting/v1/overview.proto

// Package conduit.reporting.v1 describes the existing operator-filtered overview.

package reportingv1connect

import (
	connect "connectrpc.com/connect/v2"
	context "context"
	v1 "github.com/jonathanagustin/conduit/gen/go/conduit/reporting/v1"
	sync "sync"
)

const (
	// ReportingServiceName is the fully-qualified name of the ReportingService service.
	ReportingServiceName = "conduit.reporting.v1.ReportingService"
)

// These constants are the procedure names of the RPCs defined in this package. They're exposed at
// runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// ReportingServiceGetOverviewProcedure is the procedure name of the ReportingService's GetOverview
	// RPC.
	ReportingServiceGetOverviewProcedure = "/conduit.reporting.v1.ReportingService/GetOverview"
)

var (
	reportingServiceGetOverviewSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType:       connect.StreamTypeUnary,
			Schema:           v1.File_conduit_reporting_v1_overview_proto.Services().ByName("ReportingService").Methods().ByName("GetOverview"),
			Procedure:        ReportingServiceGetOverviewProcedure,
			IdempotencyLevel: connect.IdempotencyNoSideEffects,
		}
	})
)

// ReportingServiceClient is a client for the conduit.reporting.v1.ReportingService service.
type ReportingServiceClient interface {
	// GetOverview maps GET /v1/overview. Each panel has independent availability.
	GetOverview(context.Context, *v1.GetOverviewRequest) (*v1.GetOverviewResponse, error)
}

// NewReportingServiceClient constructs a client for the conduit.reporting.v1.ReportingService
// service. Multiple service clients may share a single connect.Client.
func NewReportingServiceClient(client *connect.Client) ReportingServiceClient {
	return &reportingServiceClient{client: client}
}

// ReportingServiceHandler is an implementation of the conduit.reporting.v1.ReportingService
// service.
type ReportingServiceHandler interface {
	// GetOverview maps GET /v1/overview. Each panel has independent availability.
	GetOverview(context.Context, *v1.GetOverviewRequest) (*v1.GetOverviewResponse, error)
}

// RegisterReportingServiceHandler registers svc as the conduit.reporting.v1.ReportingService
// implementation on server.
func RegisterReportingServiceHandler(server *connect.Server, svc ReportingServiceHandler) {
	adapter := reportingServiceHandler{svc: svc}
	server.Register(
		connect.Method{Spec: reportingServiceGetOverviewSpec(), Handler: adapter.getOverview},
	)
}

// UnimplementedReportingServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedReportingServiceHandler struct{}

func (UnimplementedReportingServiceHandler) GetOverview(context.Context, *v1.GetOverviewRequest) (*v1.GetOverviewResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.reporting.v1.ReportingService.GetOverview is not implemented")
}

type reportingServiceClient struct {
	client *connect.Client
}

func (c *reportingServiceClient) GetOverview(ctx context.Context, req *v1.GetOverviewRequest) (*v1.GetOverviewResponse, error) {
	var res v1.GetOverviewResponse
	if err := c.client.CallUnary(ctx, reportingServiceGetOverviewSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type reportingServiceHandler struct{ svc ReportingServiceHandler }

func (h reportingServiceHandler) getOverview(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.GetOverviewRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.GetOverview(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}
