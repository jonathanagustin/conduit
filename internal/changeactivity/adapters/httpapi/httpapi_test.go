package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/changeactivity/adapters/memory"
	"github.com/jonathanagustin/conduit/internal/changeactivity/application"
	"github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"github.com/jonathanagustin/conduit/internal/changeactivity/ports"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type authority struct{}

func (authority) RequireRead(context.Context, string) error { return nil }
func (authority) RequireAnnotation(context.Context, string) (ports.Writer, error) {
	return ports.Writer{Author: "author", Actor: "actor"}, nil
}
func TestDiscussionPagesStayWithinOperatorResponseBoundAndRejectForeignCursors(t *testing.T) {
	store := memory.New()
	for i := range domain.MaxPageSize + 1 {
		c := domain.Comment{ID: fmt.Sprintf("cmt_%032x", i), RequestID: "crq_page", Author: strings.Repeat("<", 512), Actor: strings.Repeat("<", 512), CreatedAt: time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC), Text: strings.Repeat("<", domain.MaxTextBytes), ValueTier: domain.ValueTier}
		if err := store.Append(t.Context(), c); err != nil {
			t.Fatal(err)
		}
	}
	service, err := application.New(store, authority{})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	if err := Register(mux, Config{BindLabels: func(*http.Request) (LabelMutator, error) { return nil, errors.New("unused") }, BindDiscussion: func(*http.Request) (*application.Service, error) { return service, nil }}); err != nil {
		t.Fatal(err)
	}
	get := func(path string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response
	}
	response := get("/v1/change-requests/crq_page/comments")
	if response.Code != http.StatusOK || response.Body.Len() > 1<<20 {
		t.Fatal("maximal page exceeds operator bound", response.Code, response.Body.Len())
	}
	var page struct {
		Comments   []domain.Comment `json:"comments"`
		NextCursor string           `json:"nextCursor"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Comments) != domain.MaxPageSize || page.NextCursor == "" {
		t.Fatal("page truncation was not disclosed")
	}
	cursor := url.QueryEscape(page.NextCursor)
	response = get("/v1/change-requests/crq_page/comments?cursor=" + cursor)
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(page.Comments) != 1 || page.NextCursor != "" {
		t.Fatal("last page lost discussion", response.Code, len(page.Comments), page.NextCursor)
	}
	for _, path := range []string{"/v1/change-requests/crq_other/comments?cursor=" + cursor, "/v1/change-requests/crq_page/comments?cursor=" + cursor + "&cursor=" + cursor, "/v1/change-requests/crq_page/comments?limit=100"} {
		if response := get(path); response.Code != http.StatusBadRequest {
			t.Fatal("invalid cursor or ignored query accepted", path, response.Code)
		}
	}
}
