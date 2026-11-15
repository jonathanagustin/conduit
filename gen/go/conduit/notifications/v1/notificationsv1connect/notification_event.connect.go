//lint:file-ignore ST1003 proto names keep their spelling
// Regenerated from proto/conduit. DO NOT EDIT.
//
// Source: conduit/notifications/v1/notification_event.proto

// Package conduit.notifications.v1 describes resource-authorized native events.

package notificationsv1connect

import (
	connect "connectrpc.com/connect/v2"
	context "context"
	v1 "github.com/jonathanagustin/conduit/gen/go/conduit/notifications/v1"
	sync "sync"
)

const (
	// NotificationServiceName is the fully-qualified name of the NotificationService service.
	NotificationServiceName = "conduit.notifications.v1.NotificationService"
)

// These constants are the procedure names of the RPCs defined in this package. They're exposed at
// runtime as Spec.Procedure and as the final two segments of the HTTP route.
//
// Note that these are different from the fully-qualified method names used by
// google.golang.org/protobuf/reflect/protoreflect. To convert from these constants to
// reflection-formatted method names, remove the leading slash and convert the remaining slash to a
// period.
const (
	// NotificationServiceListNotificationEventsProcedure is the procedure name of the
	// NotificationService's ListNotificationEvents RPC.
	NotificationServiceListNotificationEventsProcedure = "/conduit.notifications.v1.NotificationService/ListNotificationEvents"
)

var (
	notificationServiceListNotificationEventsSpec = sync.OnceValue(func() connect.Spec {
		return connect.Spec{
			StreamType:       connect.StreamTypeUnary,
			Schema:           v1.File_conduit_notifications_v1_notification_event_proto.Services().ByName("NotificationService").Methods().ByName("ListNotificationEvents"),
			Procedure:        NotificationServiceListNotificationEventsProcedure,
			IdempotencyLevel: connect.IdempotencyNoSideEffects,
		}
	})
)

// NotificationServiceClient is a client for the conduit.notifications.v1.NotificationService
// service.
type NotificationServiceClient interface {
	// ListNotificationEvents maps GET /v1/notifications/events for one resource.
	ListNotificationEvents(context.Context, *v1.ListNotificationEventsRequest) (*v1.ListNotificationEventsResponse, error)
}

// NewNotificationServiceClient constructs a client for the
// conduit.notifications.v1.NotificationService service. Multiple service clients may share a single
// connect.Client.
func NewNotificationServiceClient(client *connect.Client) NotificationServiceClient {
	return &notificationServiceClient{client: client}
}

// NotificationServiceHandler is an implementation of the
// conduit.notifications.v1.NotificationService service.
type NotificationServiceHandler interface {
	// ListNotificationEvents maps GET /v1/notifications/events for one resource.
	ListNotificationEvents(context.Context, *v1.ListNotificationEventsRequest) (*v1.ListNotificationEventsResponse, error)
}

// RegisterNotificationServiceHandler registers svc as the
// conduit.notifications.v1.NotificationService implementation on server.
func RegisterNotificationServiceHandler(server *connect.Server, svc NotificationServiceHandler) {
	adapter := notificationServiceHandler{svc: svc}
	server.Register(
		connect.Method{Spec: notificationServiceListNotificationEventsSpec(), Handler: adapter.listNotificationEvents},
	)
}

// UnimplementedNotificationServiceHandler returns CodeUnimplemented from all methods.
type UnimplementedNotificationServiceHandler struct{}

func (UnimplementedNotificationServiceHandler) ListNotificationEvents(context.Context, *v1.ListNotificationEventsRequest) (*v1.ListNotificationEventsResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "conduit.notifications.v1.NotificationService.ListNotificationEvents is not implemented")
}

type notificationServiceClient struct {
	client *connect.Client
}

func (c *notificationServiceClient) ListNotificationEvents(ctx context.Context, req *v1.ListNotificationEventsRequest) (*v1.ListNotificationEventsResponse, error) {
	var res v1.ListNotificationEventsResponse
	if err := c.client.CallUnary(ctx, notificationServiceListNotificationEventsSpec(), req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

type notificationServiceHandler struct{ svc NotificationServiceHandler }

func (h notificationServiceHandler) listNotificationEvents(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
	var req v1.ListNotificationEventsRequest
	if err := stream.Receive(&req); err != nil {
		return err
	}
	res, err := h.svc.ListNotificationEvents(ctx, &req)
	if err != nil {
		return err
	}
	return stream.Send(res)
}
