package operator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRESTEnvironmentSummaryPreservesSpaceAndQueryAcrossPages(t *testing.T) {
	const space = "space_0123456789abcdef0123456789abcdef"
	const search = "east & west?"
	const cursor = "opaque+second?&page"
	for _, selected := range []string{"", space} {
		t.Run(selected, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				q := r.URL.Query()
				expectedCursor := ""
				if requests == 2 {
					expectedCursor = cursor
				}
				if r.URL.Path != "/v1/environments" || q.Get("spaceId") != selected || q.Get("view") != "summary" || q.Get("limit") != "200" || q.Get("search") != search || q.Get("cursor") != expectedCursor {
					t.Errorf("query lost: %s", r.URL.String())
					http.Error(w, "invalid query", http.StatusUnprocessableEntity)
					return
				}
				next, id := cursor, "east"
				if requests == 2 {
					next, id = "", "west"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"environments": []any{map[string]any{"environment": map[string]any{"id": id}, "spoke": map[string]any{"state": "unknown"}}},
					"total":        2, "limit": 200, "pageSize": 1, "nextCursor": next,
				})
			}))
			defer server.Close()
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if err := adapter.SetSpaceSelection(selected); err != nil {
				t.Fatal(err)
			}
			result, err := adapter.ListEnvironmentSummaries(context.Background(), search)
			if err != nil || requests != 2 || result.Total != 2 || len(result.Environments) != 2 {
				t.Fatalf("result=%+v requests=%d error=%v", result, requests, err)
			}
		})
	}
}

func TestSpaceSelectionPreservesBarePublicationPath(t *testing.T) {
	const space = "space_0123456789abcdef0123456789abcdef"
	adapter := &HTTPControlPlane{spaceID: space}
	const path = "/v1/product-publications/helm-chart"
	if got := adapter.withSpaceSelection(path); got != path+"?spaceId="+space {
		t.Fatalf("bare path changed: %s", got)
	}
}
