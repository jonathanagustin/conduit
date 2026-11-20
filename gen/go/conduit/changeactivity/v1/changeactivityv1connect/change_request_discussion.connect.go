//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
//
// Source: conduit/changeactivity/v1/change_request_discussion.proto

package changeactivityv1connect

import (
	connect "connectrpc.com/connect/v2"
	context "context"
	v1 "github.com/jonathanagustin/conduit/gen/go/conduit/changeactivity/v1"
	sync "sync"
)

const (
	// ChangeRequestDiscussionReadServiceName is the fully-qualified name of the
	// ChangeRequestDiscussionReadService service.
	ChangeRequestDiscussionReadServiceName = "conduit.changeactivity.v1.ChangeRequestDiscussionReadService"
)

// These constants are the procedure names of the RPCs defined in this package. They're exposed at
// runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// ChangeRequestDiscussionReadServiceListChangeRequestCommentsProcedure is the procedure name of the
	// ChangeRequestDiscussionReadService's ListChangeRequestComments RPC.
	ChangeRequestDiscussionReadServiceListChangeRequestCommentsProcedure = "/conduit.changeactivity.v1.ChangeRequestDiscussionReadService/ListChangeRequestComments"
)

var (
	changeRequestDiscussionReadServiceListChangeRequestCommentsSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType:       connect.StreamTypeUnary,
			Schema:           v1.File_conduit_changeactivity_v1_change_request_discussion_proto.Services().ByName("ChangeRequestDiscussionReadService").Methods().ByName("ListChangeRequestComments"),
			Procedure:        ChangeRequestDiscussionReadServiceListChangeRequestCommentsProcedure,
			IdempotencyLevel: connect.IdempotencyNoSideEffects,
		}
	})
)

// ChangeRequestDiscussionReadServiceClient is a client for the
// conduit.changeactivity.v1.ChangeRequestDiscussionReadService service.
type ChangeRequestDiscussionReadServiceClient interface {
	// ListChangeRequestComments preserves the existing eight-comment page and
	// request-bound continuation. Existing callers remain REST; no fallback.
	ListChangeRequestComments(context.Context, *v1.ListChangeRequestCommentsRequest) (*v1.ListChangeRequestCommentsResponse, error)
}

// NewChangeRequestDiscussionReadServiceClient constructs a client for the
// conduit.changeactivity.v1.ChangeRequestDiscussionReadService service. Multiple service clients
// may share a single connect.Client.
func NewChangeRequestDiscussionReadServiceClient(client *connect.Client) ChangeRequestDiscussionReadServiceClient {
	return &changeRequestDiscussionReadServiceClient{client: client}
}

// ChangeRequestDiscussionReadServiceHandler is an implementation of the
// conduit.changeactivity.v1.ChangeRequestDiscussionReadService service.
type ChangeRequestDiscussionReadServiceHandler interface {
	// ListChangeRequestComments preserves the existing eight-comment page and
	// request-bound continuation. Existing callers remain REST; no fallback.
	ListChangeRequestComments(context.Context, *v1.ListChangeRequestCommentsRequest) (*v1.ListChangeRequestCommentsResponse, error)
}

// RegisterChangeRequestDiscussionReadServiceHandler registers svc as the
// conduit.changeactivity.v1.ChangeRequestDiscussionReadService implementation on server.
func RegisterChangeRequestDiscussionReadServiceHandler(server *connect.Server, svc ChangeRequestDiscussionReadServiceHandler) {
	adapter := changeRequestDiscussionReadServiceHandler{svc: svc}
	server.Register(
		connect.Method{Spec: changeRequestDiscussionReadServiceListChangeRequestCommentsSpec(), Handler: adapter.listChangeRequestComments},
	)
}

// UnimplementedChangeRequestDiscussionReadServiceHandler returns CodeUnimplemented from all
// methods.
type UnimplementedChangeRequestDiscussionReadServiceHandler struct{}

func (UnimplementedChangeRequestDiscussionReadServiceHandler) ListChangeRequestComments(context.Context, *v1.ListChangeRequestCommentsRequest) (*v1.ListChangeRequestCommentsResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.changeactivity.v1.ChangeRequestDiscussionReadService.ListChangeRequestComments is not implemented")
}

type changeRequestDiscussionReadServiceClient struct {
	client *connect.Client
}

func (c *changeRequestDiscussionReadServiceClient) ListChangeRequestComments(ctx context.Context, req *v1.ListChangeRequestCommentsRequest) (*v1.ListChangeRequestCommentsResponse, error) {
	var res v1.ListChangeRequestCommentsResponse
	if err := c.client.CallUnary(ctx, changeRequestDiscussionReadServiceListChangeRequestCommentsSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type changeRequestDiscussionReadServiceHandler struct {
	svc ChangeRequestDiscussionReadServiceHandler
}

func (h changeRequestDiscussionReadServiceHandler) listChangeRequestComments(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.ListChangeRequestCommentsRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.ListChangeRequestComments(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}
