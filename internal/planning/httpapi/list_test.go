package httpapi

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
	api "github.com/jonathanagustin/conduit/internal/planning/transport"
)

func TestSessionIndexGeneratedContractPaginatesAndRejectsForeignCursors(t *testing.T) {
	a, b := application.NewService(nil), application.NewService(nil)
	for i := 0; i < 3; i++ {
		if _, err := a.Create(context.Background(), "https://github.com/org/app", "main"); err != nil {
			t.Fatal(err)
		}
	}
	foreign, err := b.Create(context.Background(), "https://github.com/private/other", "main")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Register(mux, Config{Authorize: func(*http.Request) error { return nil }, ResolveService: func(r *http.Request) (Sessions, error) {
		if r.Header.Get("X-Test-Owner") == "bob" {
			return b, nil
		}
		return a, nil
	}})
	server := httptest.NewServer(mux)
	defer server.Close()
	client, err := api.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	one := 1
	first, err := client.ListPlanningSessionsWithResponse(context.Background(), &api.ListPlanningSessionsParams{Limit: &one})
	if err != nil || first.JSON200 == nil {
		t.Fatalf("first: %#v %v", first, err)
	}
	page := first.JSON200
	if page.Total != 3 || page.PageSize != 1 || page.Limit != 1 || page.HasPrevious || page.NextCursor == nil {
		t.Fatalf("page %#v", page)
	}
	second, err := client.ListPlanningSessionsWithResponse(context.Background(), &api.ListPlanningSessionsParams{Limit: &one, Cursor: page.NextCursor})
	if err != nil || second.JSON200 == nil {
		t.Fatalf("second %#v %v", second, err)
	}
	if !second.JSON200.HasPrevious || second.JSON200.Items[0].Id <= page.Items[0].Id || second.JSON200.NextCursor == nil {
		t.Fatalf("second page %#v", second.JSON200)
	}
	third, err := client.ListPlanningSessionsWithResponse(context.Background(), &api.ListPlanningSessionsParams{Limit: &one, Cursor: second.JSON200.NextCursor})
	if err != nil || third.JSON200 == nil || third.JSON200.NextCursor != nil || third.JSON200.PreviousCursor == nil {
		t.Fatalf("third %#v %v", third, err)
	}
	back, err := client.ListPlanningSessionsWithResponse(context.Background(), &api.ListPlanningSessionsParams{Limit: &one, Cursor: third.JSON200.PreviousCursor})
	if err != nil || back.JSON200 == nil || back.JSON200.Items[0].Id != second.JSON200.Items[0].Id {
		t.Fatalf("previous %#v %v", back, err)
	}
	for _, query := range []string{"limit=0", "limit=201", "limit=bad", "cursor=%25%25%25", "cursor=" + strings.Repeat("x", 2049), "cursor=" + base64.RawURLEncoding.EncodeToString([]byte(foreign.ID))} {
		request := httptest.NewRequest(http.MethodGet, "/v1/planning-sessions?"+query, nil)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != 400 {
			t.Fatalf("query %q: %d %s", query, response.Code, response.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/planning-sessions", nil)
	request.Header.Set("X-Test-Owner", "bob")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != 200 || strings.Contains(response.Body.String(), page.Items[0].Id) || !strings.Contains(response.Body.String(), foreign.ID) || !strings.Contains(response.Body.String(), `"total":1`) {
		t.Fatalf("owner leak %d %s", response.Code, response.Body.String())
	}
}

type failingIndex struct{ *application.Service }

func (failingIndex) List(context.Context) ([]domain.SessionSummary, error) {
	return nil, domain.ErrIndexTooLarge
}
func TestSessionIndexUnavailableDoesNotReturnEmptySuccess(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Config{Service: failingIndex{application.NewService(nil)}, Authorize: func(*http.Request) error { return nil }})
	got := doJSON(t, mux, http.MethodGet, "/v1/planning-sessions", nil)
	if got.status != 502 || got.body["items"] != nil {
		t.Fatalf("unavailable %d %s", got.status, got.raw)
	}
}
