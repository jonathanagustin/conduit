package httpapi

import (
	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"context"
	"errors"
	activityv1 "github.com/jonathanagustin/conduit/gen/go/conduit/changeactivity/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/changeactivity/v1/changeactivityv1connect"
	"github.com/jonathanagustin/conduit/internal/changeactivity/application"
	"github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"google.golang.org/protobuf/types/known/timestamppb"
	"net/http"
)

const DiscussionReadConnectRoute = "POST /conduit.changeactivity.v1.ChangeRequestDiscussionReadService/ListChangeRequestComments"

type discussionServiceKey struct{}
type discussionReadService struct{}

// RegisterDiscussionReadConnect retains native authentication before decoding.
// The bound service authorizes the original request context before store reads.
func RegisterDiscussionReadConnect(mux *http.ServeMux, bind func(*http.Request) (*application.Service, error)) {
	if bind == nil {
		return
	}
	server := connect.NewServer()
	changeactivityv1connect.RegisterChangeRequestDiscussionReadServiceHandler(server, &discussionReadService{})
	rpc := http.NewServeMux()
	connecthttp.Mount(rpc, server)
	mux.Handle(DiscussionReadConnectRoute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service, err := bind(r)
		if err == nil && service == nil {
			err = errors.New("unavailable")
		}
		if err != nil {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_ = connecthttp.NewErrorWriter().Write(w, r, discussionReadError(err))
			return
		}
		rpc.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), discussionServiceKey{}, service)))
	}))
}

func discussionReadError(err error) error {
	code := connect.CodeUnavailable
	var status StatusError
	switch {
	case errors.As(err, &status):
		switch status.Status {
		case 400:
			code = connect.CodeInvalidArgument
		case 401:
			code = connect.CodeUnauthenticated
		case 403:
			code = connect.CodePermissionDenied
		case 404:
			code = connect.CodeNotFound
		case 409:
			code = connect.CodeAborted
		}
	case errors.Is(err, domain.ErrInvalid):
		code = connect.CodeInvalidArgument
	case errors.Is(err, domain.ErrForbidden):
		code = connect.CodePermissionDenied
	case errors.Is(err, domain.ErrNotFound):
		code = connect.CodeNotFound
	case errors.Is(err, domain.ErrConflict):
		code = connect.CodeAborted
	case errors.Is(err, context.Canceled):
		code = connect.CodeCanceled
	case errors.Is(err, context.DeadlineExceeded):
		code = connect.CodeDeadlineExceeded
	}
	return connect.NewError(code, code.String())
}
func (s *discussionReadService) ListChangeRequestComments(ctx context.Context, request *activityv1.ListChangeRequestCommentsRequest) (*activityv1.ListChangeRequestCommentsResponse, error) {
	service, ok := ctx.Value(discussionServiceKey{}).(*application.Service)
	if !ok || service == nil {
		return nil, discussionReadError(errors.New("unavailable"))
	}
	page, err := service.ReadPage(ctx, request.ChangeRequestId, request.Cursor)
	if err != nil {
		return nil, discussionReadError(err)
	}
	out := &activityv1.ListChangeRequestCommentsResponse{NextCursor: page.NextCursor, ValueTier: page.ValueTier}
	for _, c := range page.Comments {
		stamp := timestamppb.New(c.CreatedAt)
		if err := stamp.CheckValid(); err != nil {
			return nil, discussionReadError(err)
		}
		out.Comments = append(out.Comments, &activityv1.ChangeRequestComment{Id: c.ID, ChangeRequestId: c.RequestID, Author: c.Author, Actor: c.Actor, CreatedAt: stamp, Text: c.Text, ValueTier: c.ValueTier})
	}
	return out, nil
}
