//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
//
// Source: conduit/governance/v1/authorization_audit.proto

package governancev1connect

import (
	connect "connectrpc.com/connect/v2"
	context "context"
	v1 "github.com/jonathanagustin/conduit/gen/go/conduit/governance/v1"
	sync "sync"
)

const (
	// AuthorizationAuditReadServiceName is the fully-qualified name of the
	// AuthorizationAuditReadService service.
	AuthorizationAuditReadServiceName = "conduit.governance.v1.AuthorizationAuditReadService"
	// AuthorizationAuditChainReadServiceName is the fully-qualified name of the
	// AuthorizationAuditChainReadService service.
	AuthorizationAuditChainReadServiceName = "conduit.governance.v1.AuthorizationAuditChainReadService"
)

// These constants are the procedure names of the RPCs defined in this package. They're exposed at
// runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// AuthorizationAuditReadServiceListAuthorizationAuditEventsProcedure is the procedure name of the
	// AuthorizationAuditReadService's ListAuthorizationAuditEvents RPC.
	AuthorizationAuditReadServiceListAuthorizationAuditEventsProcedure = "/conduit.governance.v1.AuthorizationAuditReadService/ListAuthorizationAuditEvents"
	// AuthorizationAuditChainReadServiceListAuthorizationAuditChainProcedure is the procedure name of
	// the AuthorizationAuditChainReadService's ListAuthorizationAuditChain RPC.
	AuthorizationAuditChainReadServiceListAuthorizationAuditChainProcedure = "/conduit.governance.v1.AuthorizationAuditChainReadService/ListAuthorizationAuditChain"
)

var (
	authorizationAuditReadServiceListAuthorizationAuditEventsSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType:       connect.StreamTypeUnary,
			Schema:           v1.File_conduit_governance_v1_authorization_audit_proto.Services().ByName("AuthorizationAuditReadService").Methods().ByName("ListAuthorizationAuditEvents"),
			Procedure:        AuthorizationAuditReadServiceListAuthorizationAuditEventsProcedure,
			IdempotencyLevel: connect.IdempotencyNoSideEffects,
		}
	})
)

// AuthorizationAuditReadServiceClient is a client for the
// conduit.governance.v1.AuthorizationAuditReadService service.
type AuthorizationAuditReadServiceClient interface {
	// ListAuthorizationAuditEvents projects the events view of the native audit
	// route. REST remains supported; this method changes no caller transport.
	ListAuthorizationAuditEvents(context.Context, *v1.ListAuthorizationAuditEventsRequest) (*v1.ListAuthorizationAuditEventsResponse, error)
}

// NewAuthorizationAuditReadServiceClient constructs a client for the
// conduit.governance.v1.AuthorizationAuditReadService service. Multiple service clients may share a
// single connect.Client.
func NewAuthorizationAuditReadServiceClient(client *connect.Client) AuthorizationAuditReadServiceClient {
	return &authorizationAuditReadServiceClient{client: client}
}

// AuthorizationAuditReadServiceHandler is an implementation of the
// conduit.governance.v1.AuthorizationAuditReadService service.
type AuthorizationAuditReadServiceHandler interface {
	// ListAuthorizationAuditEvents projects the events view of the native audit
	// route. REST remains supported; this method changes no caller transport.
	ListAuthorizationAuditEvents(context.Context, *v1.ListAuthorizationAuditEventsRequest) (*v1.ListAuthorizationAuditEventsResponse, error)
}

// RegisterAuthorizationAuditReadServiceHandler registers svc as the
// conduit.governance.v1.AuthorizationAuditReadService implementation on server.
func RegisterAuthorizationAuditReadServiceHandler(server *connect.Server, svc AuthorizationAuditReadServiceHandler) {
	adapter := authorizationAuditReadServiceHandler{svc: svc}
	server.Register(
		connect.Method{Spec: authorizationAuditReadServiceListAuthorizationAuditEventsSpec(), Handler: adapter.listAuthorizationAuditEvents},
	)
}

// UnimplementedAuthorizationAuditReadServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedAuthorizationAuditReadServiceHandler struct{}

func (UnimplementedAuthorizationAuditReadServiceHandler) ListAuthorizationAuditEvents(context.Context, *v1.ListAuthorizationAuditEventsRequest) (*v1.ListAuthorizationAuditEventsResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.governance.v1.AuthorizationAuditReadService.ListAuthorizationAuditEvents is not implemented")
}

type authorizationAuditReadServiceClient struct {
	client *connect.Client
}

func (c *authorizationAuditReadServiceClient) ListAuthorizationAuditEvents(ctx context.Context, req *v1.ListAuthorizationAuditEventsRequest) (*v1.ListAuthorizationAuditEventsResponse, error) {
	var res v1.ListAuthorizationAuditEventsResponse
	if err := c.client.CallUnary(ctx, authorizationAuditReadServiceListAuthorizationAuditEventsSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type authorizationAuditReadServiceHandler struct {
	svc AuthorizationAuditReadServiceHandler
}

func (h authorizationAuditReadServiceHandler) listAuthorizationAuditEvents(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.ListAuthorizationAuditEventsRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.ListAuthorizationAuditEvents(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}

var (
	authorizationAuditChainReadServiceListAuthorizationAuditChainSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType:       connect.StreamTypeUnary,
			Schema:           v1.File_conduit_governance_v1_authorization_audit_proto.Services().ByName("AuthorizationAuditChainReadService").Methods().ByName("ListAuthorizationAuditChain"),
			Procedure:        AuthorizationAuditChainReadServiceListAuthorizationAuditChainProcedure,
			IdempotencyLevel: connect.IdempotencyNoSideEffects,
		}
	})
)

// AuthorizationAuditChainReadServiceClient is a client for the
// conduit.governance.v1.AuthorizationAuditChainReadService service.
type AuthorizationAuditChainReadServiceClient interface {
	ListAuthorizationAuditChain(context.Context, *v1.ListAuthorizationAuditChainRequest) (*v1.ListAuthorizationAuditChainResponse, error)
}

// NewAuthorizationAuditChainReadServiceClient constructs a client for the
// conduit.governance.v1.AuthorizationAuditChainReadService service. Multiple service clients may
// share a single connect.Client.
func NewAuthorizationAuditChainReadServiceClient(client *connect.Client) AuthorizationAuditChainReadServiceClient {
	return &authorizationAuditChainReadServiceClient{client: client}
}

// AuthorizationAuditChainReadServiceHandler is an implementation of the
// conduit.governance.v1.AuthorizationAuditChainReadService service.
type AuthorizationAuditChainReadServiceHandler interface {
	ListAuthorizationAuditChain(context.Context, *v1.ListAuthorizationAuditChainRequest) (*v1.ListAuthorizationAuditChainResponse, error)
}

// RegisterAuthorizationAuditChainReadServiceHandler registers svc as the
// conduit.governance.v1.AuthorizationAuditChainReadService implementation on server.
func RegisterAuthorizationAuditChainReadServiceHandler(server *connect.Server, svc AuthorizationAuditChainReadServiceHandler) {
	adapter := authorizationAuditChainReadServiceHandler{svc: svc}
	server.Register(
		connect.Method{Spec: authorizationAuditChainReadServiceListAuthorizationAuditChainSpec(), Handler: adapter.listAuthorizationAuditChain},
	)
}

// UnimplementedAuthorizationAuditChainReadServiceHandler returns CodeUnimplemented from all
// methods.
type UnimplementedAuthorizationAuditChainReadServiceHandler struct{}

func (UnimplementedAuthorizationAuditChainReadServiceHandler) ListAuthorizationAuditChain(context.Context, *v1.ListAuthorizationAuditChainRequest) (*v1.ListAuthorizationAuditChainResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.governance.v1.AuthorizationAuditChainReadService.ListAuthorizationAuditChain is not implemented")
}

type authorizationAuditChainReadServiceClient struct {
	client *connect.Client
}

func (c *authorizationAuditChainReadServiceClient) ListAuthorizationAuditChain(ctx context.Context, req *v1.ListAuthorizationAuditChainRequest) (*v1.ListAuthorizationAuditChainResponse, error) {
	var res v1.ListAuthorizationAuditChainResponse
	if err := c.client.CallUnary(ctx, authorizationAuditChainReadServiceListAuthorizationAuditChainSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type authorizationAuditChainReadServiceHandler struct {
	svc AuthorizationAuditChainReadServiceHandler
}

func (h authorizationAuditChainReadServiceHandler) listAuthorizationAuditChain(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.ListAuthorizationAuditChainRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.ListAuthorizationAuditChain(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}
