package httpapi

import (
	"context"
	"encoding/base64"
	"sort"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
	api "github.com/jonathanagustin/conduit/internal/planning/transport"
)

type summarySessions interface {
	List(context.Context) ([]domain.SessionSummary, error)
}

type summaryPage struct {
	ValueTier      string                  `json:"valueTier"`
	Items          []domain.SessionSummary `json:"items"`
	Total          int                     `json:"total"`
	PageSize       int                     `json:"pageSize"`
	Limit          int                     `json:"limit"`
	NextCursor     string                  `json:"nextCursor,omitempty"`
	PreviousCursor string                  `json:"previousCursor,omitempty"`
	HasPrevious    bool                    `json:"hasPrevious"`
}

func (strictServer) ListPlanningSessions(ctx context.Context, r api.ListPlanningSessionsRequestObject) (api.ListPlanningSessionsResponseObject, error) {
	limit := 100
	if r.Params.Limit != nil {
		limit = *r.Params.Limit
	}
	if limit < 1 || limit > 200 {
		return nil, wireError{400, "", "limit must be between 1 and 200"}
	}
	cursor := ""
	if r.Params.Cursor != nil {
		cursor = *r.Params.Cursor
	}
	seek := ""
	if cursor != "" {
		if len(cursor) > 2048 {
			return nil, wireError{400, "", "malformed session cursor"}
		}
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || len(decoded) == 0 {
			return nil, wireError{400, "", "malformed session cursor"}
		}
		seek = string(decoded)
	}
	lister, ok := service(ctx).(summarySessions)
	if !ok {
		return nil, wireError{501, "", "planning session listing is not configured"}
	}
	items, err := lister.List(ctx)
	if err != nil {
		return nil, err
	}
	if len(items) > domain.MaxIndexedExecutions {
		return nil, domain.ErrIndexTooLarge
	}
	// Sort after the owner-scoped port, before counts and continuation metadata.
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for i, item := range items {
		if item.ID == "" || (i > 0 && items[i-1].ID == item.ID) {
			return nil, wireError{502, "", "invalid planning session inventory"}
		}
	}
	start := 0
	if seek != "" {
		index := sort.Search(len(items), func(i int) bool { return items[i].ID >= seek })
		if index == len(items) || items[index].ID != seek {
			return nil, wireError{400, "", "cursor is not in the available session inventory"}
		}
		start = index + 1
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	page := summaryPage{ValueTier: "CONDUIT_NATIVE", Items: append([]domain.SessionSummary{}, items[start:end]...), Total: len(items), PageSize: end - start, Limit: limit, HasPrevious: start > 0}
	encode := func(id string) string { return base64.RawURLEncoding.EncodeToString([]byte(id)) }
	if end < len(items) {
		page.NextCursor = encode(items[end-1].ID)
	}
	if start > limit {
		page.PreviousCursor = encode(items[start-limit-1].ID)
	}
	return dto[api.ListPlanningSessions200JSONResponse](page)
}
