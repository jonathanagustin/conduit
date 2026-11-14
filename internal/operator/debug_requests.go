package operator

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	requests "github.com/jonathanagustin/conduit/internal/debugsessions/domain"
)

// DebugRequestDraft is native non-executing metadata, never a Pod or credential.
type DebugRequestDraft struct {
	SessionID       string          `json:"sessionId"`
	Target          requests.Target `json:"target"`
	LifetimeSeconds int64           `json:"lifetimeSeconds,omitempty"`
}
type DebugRequestPage struct {
	Requests   []requests.Event `json:"requests"`
	NextCursor string           `json:"nextCursor,omitempty"`
	ValueTier  string           `json:"valueTier"`
}

// DebugRequestControlPlane is the optional native metadata lifecycle transport.
type DebugRequestControlPlane interface {
	CreateDebugRequest(context.Context, DebugRequestDraft) (requests.Event, error)
	ListDebugRequests(context.Context, requests.Scope, string, int) (DebugRequestPage, error)
	GetDebugRequest(context.Context, string) (requests.Event, error)
	DebugRequestHistory(context.Context, string) ([]requests.Event, error)
	MutateDebugRequest(context.Context, string, string) (requests.Event, error)
}

func (service Service) debugRequests() (DebugRequestControlPlane, error) {
	port, ok := service.controlPlane.(DebugRequestControlPlane)
	if !ok {
		return nil, errors.New("debug request metadata is unavailable from this transport")
	}
	return port, nil
}
func (service Service) CreateDebugRequest(ctx context.Context, d DebugRequestDraft) (requests.Event, error) {
	p, e := service.debugRequests()
	if e != nil {
		return requests.Event{}, e
	}
	return p.CreateDebugRequest(ctx, d)
}
func (service Service) ListDebugRequests(ctx context.Context, scope requests.Scope, after string, limit int) (DebugRequestPage, error) {
	p, e := service.debugRequests()
	if e != nil {
		return DebugRequestPage{}, e
	}
	return p.ListDebugRequests(ctx, scope, after, limit)
}
func (service Service) GetDebugRequest(ctx context.Context, id string) (requests.Event, error) {
	p, e := service.debugRequests()
	if e != nil {
		return requests.Event{}, e
	}
	return p.GetDebugRequest(ctx, id)
}
func (service Service) DebugRequestHistory(ctx context.Context, id string) ([]requests.Event, error) {
	p, e := service.debugRequests()
	if e != nil {
		return nil, e
	}
	return p.DebugRequestHistory(ctx, id)
}
func (service Service) MutateDebugRequest(ctx context.Context, id, state string) (requests.Event, error) {
	p, e := service.debugRequests()
	if e != nil {
		return requests.Event{}, e
	}
	return p.MutateDebugRequest(ctx, id, state)
}
func (adapter *HTTPControlPlane) debugMetadataScope() error {
	if adapter.spaceID != "" {
		return errors.New("debug request metadata requires explicit Environment/namespace scope; Space selection is not supported")
	}
	return nil
}
func (adapter *HTTPControlPlane) CreateDebugRequest(ctx context.Context, d DebugRequestDraft) (requests.Event, error) {
	if err := adapter.debugMetadataScope(); err != nil {
		return requests.Event{}, err
	}
	if !requests.ValidID(d.SessionID) || d.Target.Validate() != nil || d.LifetimeSeconds < 0 || d.LifetimeSeconds > int64(requests.MaximumLifetime/time.Second) {
		return requests.Event{}, requests.ErrInvalid
	}
	var result requests.Event
	if err := adapter.request(ctx, http.MethodPost, "/v1/debug-session-requests", d, &result); err != nil {
		return requests.Event{}, err
	}
	expectedLifetime := d.LifetimeSeconds
	if expectedLifetime == 0 {
		expectedLifetime = int64(requests.MaximumLifetime / time.Second)
	}
	if result.Validate() != nil || result.SessionID != d.SessionID || result.Target != d.Target || result.ExpiresAt.Sub(result.CreatedAt) != time.Duration(expectedLifetime)*time.Second {
		return requests.Event{}, requests.ErrCorrupt
	}
	return result, nil
}
func (adapter *HTTPControlPlane) ListDebugRequests(ctx context.Context, scope requests.Scope, after string, limit int) (DebugRequestPage, error) {
	if err := adapter.debugMetadataScope(); err != nil {
		return DebugRequestPage{}, err
	}
	if scope.Validate() != nil || (after != "" && !requests.ValidID(after)) || limit < 1 || limit > 100 {
		return DebugRequestPage{}, requests.ErrInvalid
	}
	query := url.Values{"environment": {scope.Environment}, "namespace": {scope.Namespace}, "limit": {strconv.Itoa(limit)}}
	if after != "" {
		query.Set("after", after)
	}
	var page DebugRequestPage
	if err := adapter.request(ctx, http.MethodGet, "/v1/debug-session-requests?"+query.Encode(), nil, &page); err != nil {
		return page, err
	}
	if page.ValueTier != requests.ValueTier || len(page.Requests) > limit {
		return DebugRequestPage{}, requests.ErrCorrupt
	}
	cursor := after
	for _, e := range page.Requests {
		if e.Validate() != nil || e.Target.Scope() != scope || e.SessionID <= cursor || e.State == requests.Deleted {
			return DebugRequestPage{}, requests.ErrCorrupt
		}
		cursor = e.SessionID
	}
	if page.NextCursor != "" && (len(page.Requests) == 0 || page.NextCursor != cursor) {
		return DebugRequestPage{}, requests.ErrCorrupt
	}
	return page, nil
}
func (adapter *HTTPControlPlane) GetDebugRequest(ctx context.Context, id string) (requests.Event, error) {
	return adapter.debugEvent(ctx, id, http.MethodGet, "", nil, "")
}
func (adapter *HTTPControlPlane) MutateDebugRequest(ctx context.Context, id, state string) (requests.Event, error) {
	switch state {
	case requests.Revoked:
		return adapter.debugEvent(ctx, id, http.MethodPost, "/revoke", struct{}{}, state)
	case requests.Deleted:
		return adapter.debugEvent(ctx, id, http.MethodDelete, "", nil, state)
	default:
		return requests.Event{}, requests.ErrInvalid
	}
}
func (adapter *HTTPControlPlane) debugEvent(ctx context.Context, id, method, suffix string, body any, state string) (requests.Event, error) {
	if err := adapter.debugMetadataScope(); err != nil {
		return requests.Event{}, err
	}
	if !requests.ValidID(id) {
		return requests.Event{}, requests.ErrInvalid
	}
	var result requests.Event
	if err := adapter.request(ctx, method, "/v1/debug-session-requests/"+url.PathEscape(id)+suffix, body, &result); err != nil {
		return requests.Event{}, err
	}
	if result.Validate() != nil || result.SessionID != id || (state != "" && result.State != state) {
		return requests.Event{}, requests.ErrCorrupt
	}
	return result, nil
}
func (adapter *HTTPControlPlane) DebugRequestHistory(ctx context.Context, id string) ([]requests.Event, error) {
	if err := adapter.debugMetadataScope(); err != nil {
		return nil, err
	}
	if !requests.ValidID(id) {
		return nil, requests.ErrInvalid
	}
	var result struct {
		Events    []requests.Event `json:"events"`
		ValueTier string           `json:"valueTier"`
	}
	if err := adapter.request(ctx, http.MethodGet, "/v1/debug-session-requests/"+url.PathEscape(id)+"/history", nil, &result); err != nil {
		return nil, err
	}
	last, err := requests.ValidateHistory(result.Events)
	if err != nil || last.SessionID != id || result.ValueTier != requests.ValueTier {
		return nil, requests.ErrCorrupt
	}
	return result.Events, nil
}
