package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestChangeRequestRESTReadsPreserveNativeSpaceAndCompleteIndex(t *testing.T) {
	const space = "space_0123456789abcdef0123456789abcdef"
	for _, tc := range []struct {
		name, actor, selection, environment, detail string
		denied                                      bool
		rows, listStatus, detailStatus, pages       int
	}{
		{name: "unscoped", actor: "reader", detail: "outside", rows: 202, listStatus: 200, detailStatus: 200, pages: 2},
		{name: "selected multi-page", actor: "reader", selection: space, detail: "inside-200", rows: 201, listStatus: 200, detailStatus: 200, pages: 2},
		{name: "selected nonmember", actor: "reader", selection: space, detail: "outside", rows: 201, listStatus: 200, detailStatus: 404, pages: 2},
		{name: "Environment filter", actor: "reader", selection: space, environment: "outside", detail: "outside", listStatus: 200, detailStatus: 404, pages: 1},
		{name: "membership grants no authority", actor: "outsider", selection: space, detail: "inside-200", listStatus: 200, detailStatus: 403, pages: 1},
		{name: "Space refused", actor: "reader", selection: space, detail: "inside-200", denied: true, listStatus: 403, detailStatus: 403, pages: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := controlplane.NewMemoryRepository()
			for i := range 202 {
				id, environment := fmt.Sprintf("inside-%03d", i), "inside"
				if i == 201 {
					id, environment = "outside", "outside"
				}
				if _, err := repository.CreateChangeRequest(t.Context(), controlplane.ChangeRequest{ID: id, Environment: environment, Author: controlplane.Principal{Username: "reader"}}); err != nil {
					t.Fatal(err)
				}
			}
			hub := controlplane.NewHandler(repository, controlplane.HandlerOptions{
				OperatorIdentityResolver: func(*http.Request) (controlplane.IdentityClaims, error) {
					return controlplane.IdentityClaims{Username: tc.actor}, nil
				},
				BootstrapAdministrators: []string{"reader"},
				RequireSpaceSelection: func(_ context.Context, p controlplane.Principal, id string) error {
					if p.Username != tc.actor || id != space {
						t.Errorf("selection carrier changed: %+v %s", p, id)
					}
					if tc.denied {
						return controlplane.ErrForbidden
					}
					return nil
				},
				SpaceIDLookup: func(context.Context, controlplane.Principal, string, string) (string, error) {
					t.Error("unrelated resource lookup")
					return "", nil
				},
				LoadSpaceIndexMembership: func(_ context.Context, p controlplane.Principal, id string) (controlplane.SpaceIndexMembership, error) {
					if p.Username != tc.actor || id != space {
						t.Errorf("membership carrier changed: %+v %s", p, id)
					}
					return controlplane.SpaceIndexMembership{EnvironmentIDs: []string{"inside"}, ProductIDs: []string{"outside"}}, nil
				},
			})
			pages := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("spaceId") != tc.selection {
					t.Errorf("selection dropped: %s", r.URL)
				}
				if r.URL.Path == "/v1/change-requests" {
					pages++
					if r.URL.Query().Get("limit") != "200" || r.URL.Query().Get("environment") != tc.environment {
						t.Errorf("query changed: %s", r.URL)
					}
				}
				hub.ServeHTTP(w, r)
			}))
			defer server.Close()
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if err := adapter.SetSpaceSelection(tc.selection); err != nil {
				t.Fatal(err)
			}
			rows, err := adapter.ListChangeRequests(t.Context(), tc.environment)
			if tc.listStatus == 200 {
				if err != nil || len(rows) != tc.rows {
					t.Fatalf("rows%d want%d err%v", len(rows), tc.rows, err)
				}
				seen := map[string]bool{}
				for _, row := range rows {
					if seen[row.ID] {
						t.Fatalf("duplicate %s", row.ID)
					}
					seen[row.ID] = true
					if tc.selection != "" && row.Environment != "inside" {
						t.Fatalf("outside row: %+v", row)
					}
				}
			} else if err == nil || !strings.Contains(err.Error(), strconv.Itoa(tc.listStatus)) || rows != nil {
				t.Fatalf("list refusal: %+v %v", rows, err)
			}
			if pages != tc.pages {
				t.Fatalf("pages%d want%d", pages, tc.pages)
			}
			record, err := adapter.GetChangeRequest(t.Context(), tc.detail)
			if tc.detailStatus == 200 {
				if err != nil || record.ID != tc.detail {
					t.Fatalf("detail=%+v %v", record, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), strconv.Itoa(tc.detailStatus)) || record.ID != "" {
				t.Fatalf("detail refusal: %+v %v", record, err)
			}
		})
	}
}

func TestChangeRequestRESTPagingKeepsOpaqueQueryAndRejectsPartialResults(t *testing.T) {
	const space = "space_0123456789abcdef0123456789abcdef"
	const cursor = "opaque+cursor?&next"
	for _, mode := range []string{"success", "cycle", "late refusal"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				wantCursor := ""
				if calls > 1 {
					wantCursor = cursor
				}
				q := r.URL.Query()
				if r.Method != "GET" || r.URL.Path != "/v1/change-requests" || q.Get("spaceId") != space || q.Get("environment") != "east & west" || q.Get("cursor") != wantCursor || q.Get("limit") != "200" {
					t.Errorf("query changed: %s", r.URL)
				}
				if calls == 2 && mode == "late refusal" {
					http.Error(w, "refused", 403)
					return
				}
				next := cursor
				if calls == 2 && mode == "success" {
					next = ""
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"changeRequests": []controlplane.ChangeRequest{{ID: fmt.Sprint(calls)}}, "nextCursor": next})
			}))
			defer server.Close()
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if err := adapter.SetSpaceSelection(space); err != nil {
				t.Fatal(err)
			}
			rows, err := adapter.ListChangeRequests(t.Context(), "east & west")
			if mode == "success" {
				if err != nil || len(rows) != 2 {
					t.Fatalf("rows=%+v err%v", rows, err)
				}
			} else if err == nil || rows != nil {
				t.Fatalf("partial result accepted: %+v %v", rows, err)
			}
			if calls != 2 {
				t.Fatalf("calls%d", calls)
			}
		})
	}
}
