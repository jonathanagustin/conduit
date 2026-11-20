package operator

import (
	"context"
	"fmt"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	activityv1 "github.com/jonathanagustin/conduit/gen/go/conduit/changeactivity/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/changeactivity/v1/changeactivityv1connect"
	activity "github.com/jonathanagustin/conduit/internal/changeactivity/domain"
)

// SetDiscussionReadTransport selects only discussion reads, without fallback.
func (adapter *HTTPControlPlane) SetDiscussionReadTransport(mode string) error {
	if mode != "" && mode != "rest" && mode != "connect" {
		return fmt.Errorf("discussion read transport must be rest or connect")
	}
	adapter.discussionReadTransport = mode
	return nil
}

func (adapter *HTTPControlPlane) listChangeRequestCommentsConnect(ctx context.Context, id string) ([]activity.Comment, error) {
	if !activity.ValidRequestID(id) {
		return nil, activity.ErrInvalid
	}
	client := changeactivityv1connect.NewChangeRequestDiscussionReadServiceClient(connect.NewClient(connecthttp.NewTransport(adapter.client, adapter.baseURL.String(), connecthttp.WithReadMaxBytes(maxOperatorResponseBytes))))
	request := &activityv1.ListChangeRequestCommentsRequest{ChangeRequestId: id}
	result := make([]activity.Comment, 0)
	seenCursor := map[string]bool{}
	seenID := map[string]bool{}
	invalid := func() ([]activity.Comment, error) {
		return nil, fmt.Errorf("operator control plane returned invalid discussion pages")
	}
	for {
		page, err := client.ListChangeRequestComments(ctx, request)
		if err != nil {
			return nil, err
		}
		if page == nil || page.ValueTier != activity.ValueTier || len(page.Comments) > activity.MaxPageSize || len(page.NextCursor) > 1024 || (page.NextCursor != "" && len(page.Comments) != activity.MaxPageSize) {
			return invalid()
		}
		for _, row := range page.Comments {
			if row == nil || row.ChangeRequestId != id || row.CreatedAt == nil || row.CreatedAt.CheckValid() != nil {
				return invalid()
			}
			comment := activity.Comment{ID: row.Id, RequestID: row.ChangeRequestId, Author: row.Author, Actor: row.Actor, CreatedAt: row.CreatedAt.AsTime(), Text: row.Text, ValueTier: row.ValueTier}
			if comment.Validate() != nil || seenID[comment.ID] {
				return invalid()
			}
			if len(result) > 0 {
				previous := result[len(result)-1]
				if comment.CreatedAt.Before(previous.CreatedAt) || (comment.CreatedAt.Equal(previous.CreatedAt) && comment.ID <= previous.ID) {
					return invalid()
				}
			}
			seenID[comment.ID] = true
			result = append(result, comment)
		}
		if page.NextCursor == "" {
			return result, nil
		}
		if seenCursor[page.NextCursor] {
			return invalid()
		}
		seenCursor[page.NextCursor] = true
		request.Cursor = page.NextCursor
	}
}
