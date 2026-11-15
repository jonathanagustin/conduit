package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	notificationsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/notifications/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/notifications/v1/notificationsv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ConnectRoute is the native resource-event read, mounted only with its Store.
const ConnectRoute = "POST /conduit.notifications.v1.NotificationService/ListNotificationEvents"

type eventPrincipalKey struct{}

func (api *api) registerConnect(mux *http.ServeMux) {
	server := connect.NewServer()
	notificationsv1connect.RegisterNotificationServiceHandler(server, &eventService{api: api})
	rpcMux := http.NewServeMux()
	connecthttp.Mount(rpcMux, server)
	mux.Handle(ConnectRoute, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, err := api.resolvePrincipal(request)
		if err != nil {
			writeEventConnectError(writer, request, err)
			return
		}
		ctx := context.WithValue(request.Context(), eventPrincipalKey{}, principal)
		rpcMux.ServeHTTP(writer, request.WithContext(ctx))
	}))
}

type eventService struct{ api *api }

func (service *eventService) ListNotificationEvents(ctx context.Context, request *notificationsv1.ListNotificationEventsRequest) (*notificationsv1.ListNotificationEventsResponse, error) {
	principal, ok := ctx.Value(eventPrincipalKey{}).(Principal)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "unauthenticated")
	}
	limit := ""
	if request.Limit != nil {
		limit = strconv.FormatInt(*request.Limit, 10)
	}
	page, err := service.api.eventPage(ctx, principal, request.GetResourceRid(), limit)
	if err != nil {
		return nil, eventConnectError(err)
	}
	out := &notificationsv1.ListNotificationEventsResponse{ResourceRid: page.ResourceRID, Limit: int32(page.Limit)}
	for _, row := range page.Events {
		timestamp := timestamppb.New(row.OccurredAt)
		if timestamp.CheckValid() != nil {
			return nil, connect.NewError(connect.CodeInternal, "notification timestamp unavailable")
		}
		out.Events = append(out.Events, &notificationsv1.NotificationEvent{Id: row.ID, Type: string(row.Type), ResourceRid: row.ResourceRID, Environment: row.Environment, Summary: row.Summary, Actor: row.Actor, OccurredAt: timestamp})
	}
	return out, nil
}

func eventConnectError(err error) *connect.Error {
	status := http.StatusUnauthorized
	var failure StatusError
	if errors.As(err, &failure) && failure.Status != 0 {
		status = failure.Status
	}
	code := connect.CodeInternal
	switch status {
	case http.StatusBadRequest:
		code = connect.CodeInvalidArgument
	case http.StatusUnauthorized:
		code = connect.CodeUnauthenticated
	case http.StatusForbidden:
		code = connect.CodePermissionDenied
	case http.StatusNotFound:
		code = connect.CodeNotFound
	}
	message := err.Error()
	if code == connect.CodeInternal {
		message = "notification event read unavailable"
	}
	return connect.NewError(code, message)
}

func writeEventConnectError(writer http.ResponseWriter, request *http.Request, err error) {
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	_ = connecthttp.NewErrorWriter().Write(writer, request, eventConnectError(err))
}
