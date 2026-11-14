package operator

import (
	"context"
	"errors"
	activity "github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"net/http"
	"net/url"
)

// ChangeActivityControlPlane is the optional native discussion/label transport.
type ChangeActivityControlPlane interface {
	CommentChangeRequest(context.Context, string, string) (activity.Comment, error)
	ListChangeRequestComments(context.Context, string) ([]activity.Comment, error)
	MutateChangeRequestLabel(context.Context, string, string, string, bool) (activity.LabelMutation, error)
}

func (service Service) changeActivity() (ChangeActivityControlPlane, error) {
	port, ok := service.controlPlane.(ChangeActivityControlPlane)
	if !ok {
		return nil, errors.New("change request discussion and label mutations are unavailable from this transport")
	}
	return port, nil
}
func (service Service) CommentChangeRequest(ctx context.Context, id, text string) (activity.Comment, error) {
	port, err := service.changeActivity()
	if err != nil {
		return activity.Comment{}, err
	}
	return port.CommentChangeRequest(ctx, id, text)
}
func (service Service) ListChangeRequestComments(ctx context.Context, id string) ([]activity.Comment, error) {
	port, err := service.changeActivity()
	if err != nil {
		return nil, err
	}
	return port.ListChangeRequestComments(ctx, id)
}
func (service Service) MutateChangeRequestLabel(ctx context.Context, id, name, operation string, dryRun bool) (activity.LabelMutation, error) {
	port, err := service.changeActivity()
	if err != nil {
		return activity.LabelMutation{}, err
	}
	return port.MutateChangeRequestLabel(ctx, id, name, operation, dryRun)
}
func (adapter *HTTPControlPlane) CommentChangeRequest(ctx context.Context, id, text string) (activity.Comment, error) {
	var result activity.Comment
	err := adapter.request(ctx, http.MethodPost, "/v1/change-requests/"+url.PathEscape(id)+"/comments", map[string]string{"text": text}, &result)
	return result, err
}
func (adapter *HTTPControlPlane) MutateChangeRequestLabel(ctx context.Context, id, name, operation string, dryRun bool) (activity.LabelMutation, error) {
	var result activity.LabelMutation
	err := adapter.request(ctx, http.MethodPost, "/v1/change-requests/"+url.PathEscape(id)+"/label-mutations", map[string]any{"labelName": name, "operation": operation, "dryRun": dryRun}, &result)
	return result, err
}
func (adapter *HTTPControlPlane) ListChangeRequestComments(ctx context.Context, id string) ([]activity.Comment, error) {
	result := make([]activity.Comment, 0)
	cursor := ""
	seen := map[string]bool{}
	for {
		path := "/v1/change-requests/" + url.PathEscape(id) + "/comments"
		if cursor != "" {
			path += "?cursor=" + url.QueryEscape(cursor)
		}
		var page struct {
			Comments   []activity.Comment `json:"comments"`
			NextCursor string             `json:"nextCursor"`
		}
		if err := adapter.request(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, err
		}
		result = append(result, page.Comments...)
		if page.NextCursor == "" {
			return result, nil
		}
		if seen[page.NextCursor] {
			return nil, errors.New("change request discussion repeated a cursor")
		}
		seen[page.NextCursor] = true
		cursor = page.NextCursor
	}
}
