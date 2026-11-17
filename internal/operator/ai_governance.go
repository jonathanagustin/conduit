package operator

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// AIGovernanceControlPlane is the optional model-usage and transcript
// transport. Each result is the Hub's document, unchanged.
type AIGovernanceControlPlane interface {
	// ModelUsage reads the caller's own usage, or every owner's when all is
	// set, which the Hub allows an administrator only.
	ModelUsage(ctx context.Context, all bool, owner string, days int) (map[string]any, error)
	ListAITranscripts(ctx context.Context, owner, session string) (map[string]any, error)
	ExportAITranscript(ctx context.Context, runID string) (map[string]any, error)
}

func (service Service) aiGovernance() (AIGovernanceControlPlane, error) {
	port, ok := service.controlPlane.(AIGovernanceControlPlane)
	if !ok {
		return nil, errors.New("model usage and AI transcripts are unavailable from this transport")
	}
	return port, nil
}

func (service Service) ModelUsage(ctx context.Context, all bool, owner string, days int) (map[string]any, error) {
	port, err := service.aiGovernance()
	if err != nil {
		return nil, err
	}
	return port.ModelUsage(ctx, all, owner, days)
}

func (service Service) ListAITranscripts(ctx context.Context, owner, session string) (map[string]any, error) {
	port, err := service.aiGovernance()
	if err != nil {
		return nil, err
	}
	return port.ListAITranscripts(ctx, owner, session)
}

func (service Service) ExportAITranscript(ctx context.Context, runID string) (map[string]any, error) {
	port, err := service.aiGovernance()
	if err != nil {
		return nil, err
	}
	return port.ExportAITranscript(ctx, runID)
}

func (adapter *HTTPControlPlane) ModelUsage(ctx context.Context, all bool, owner string, days int) (map[string]any, error) {
	query := url.Values{}
	if days > 0 {
		query.Set("days", strconv.Itoa(days))
	}
	path := "/v1/ai/usage"
	if all {
		path = "/v1/admin/ai/usage"
		if owner != "" {
			query.Set("owner", owner)
		}
	}
	return adapter.aiDocument(ctx, path, query)
}

func (adapter *HTTPControlPlane) ListAITranscripts(ctx context.Context, owner, session string) (map[string]any, error) {
	query := url.Values{}
	if owner != "" {
		query.Set("owner", owner)
	}
	if session != "" {
		query.Set("session", session)
	}
	return adapter.aiDocument(ctx, "/v1/admin/ai/transcripts", query)
}

func (adapter *HTTPControlPlane) ExportAITranscript(ctx context.Context, runID string) (map[string]any, error) {
	return adapter.aiDocument(ctx, "/v1/admin/ai/transcripts/"+url.PathEscape(runID), nil)
}

func (adapter *HTTPControlPlane) aiDocument(ctx context.Context, path string, query url.Values) (map[string]any, error) {
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	result := map[string]any{}
	err := adapter.request(ctx, http.MethodGet, path, nil, &result)
	return result, err
}
