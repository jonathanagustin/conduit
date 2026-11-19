package operator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestRESTPlanListPreservesSpaceFiltersAndCursorAcrossPages(t *testing.T) {
	const space = "space_0123456789abcdef0123456789abcdef"
	const cursor = "opaque+next?&page"
	for _, selection := range []string{"", space} {
		t.Run(selection, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				q := r.URL.Query()
				wantCursor := ""
				if calls == 2 {
					wantCursor = cursor
				}
				if r.Method != "GET" || r.URL.Path != "/v1/plans" || q.Get("spaceId") != selection || q.Get("view") != "summary" || q.Get("limit") != "200" || q.Get("cursor") != wantCursor || q.Get("environment") != "east & west" || q.Get("status") != "FUTURE" || q.Get("installationId") != "app+name" || q.Get("search") != "literal ?" {
					t.Errorf("query lost: %s", r.URL)
				}
				next, id := cursor, "first"
				if calls == 2 {
					next, id = "", "second"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"plans": []controlplane.PlanSummary{{ID: id}}, "total": 2, "nextCursor": next})
			}))
			defer server.Close()
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if err := adapter.SetSpaceSelection(selection); err != nil {
				t.Fatal(err)
			}
			result, err := adapter.ListPlans(t.Context(), PlanListQuery{Environment: "east & west", Status: "FUTURE", InstallationID: "app+name", Search: "literal ?"})
			if err != nil || calls != 2 || result.Total != 2 || len(result.Plans) != 2 {
				t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
			}
		})
	}
}

func TestRESTPlanListUsesNativeSpaceMembershipAndRefusals(t *testing.T) {
	const space = "space_0123456789abcdef0123456789abcdef"
	for _, tc := range []struct {
		name, actor, selection string
		deny                   bool
		wantCount, wantReads   int
	}{
		{name: "unscoped", actor: "reader", wantCount: 2, wantReads: 1},
		{name: "selected grouping", actor: "reader", selection: space, wantCount: 1, wantReads: 1},
		{name: "grouping grants no authority", actor: "outsider", selection: space, wantCount: 0, wantReads: 1},
		{name: "Space refused before private reads", actor: "reader", selection: space, deny: true, wantReads: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := controlplane.NewMemoryRepository()
			for _, id := range []string{"inside", "outside"} {
				if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: id, Plan: controlplane.Plan{Environment: id}}); err != nil {
					t.Fatal(err)
				}
			}
			store := &environmentDetailSpaceCatalogCounter{Repository: repository}
			var selected, membership atomic.Int32
			handler := controlplane.NewHandler(store, controlplane.HandlerOptions{OperatorIdentityResolver: func(*http.Request) (controlplane.IdentityClaims, error) {
				return controlplane.IdentityClaims{Username: tc.actor}, nil
			}, BootstrapAdministrators: []string{"reader"}, RequireSpaceSelection: func(_ context.Context, p controlplane.Principal, id string) error {
				selected.Add(1)
				if p.Username != tc.actor || id != space {
					t.Errorf("selection identity changed: %+v %s", p, id)
				}
				if tc.deny {
					return controlplane.ErrForbidden
				}
				return nil
			}, SpaceIDLookup: func(context.Context, controlplane.Principal, string, string) (string, error) {
				t.Error("unrelated resource lookup")
				return "", nil
			}, LoadSpaceIndexMembership: func(context.Context, controlplane.Principal, string) (controlplane.SpaceIndexMembership, error) {
				membership.Add(1)
				return controlplane.SpaceIndexMembership{EnvironmentIDs: []string{"inside"}}, nil
			}})
			server := httptest.NewServer(handler)
			defer server.Close()
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if err := adapter.SetSpaceSelection(tc.selection); err != nil {
				t.Fatal(err)
			}
			result, err := adapter.ListPlans(t.Context(), PlanListQuery{})
			if tc.deny {
				if err == nil || !strings.Contains(err.Error(), "403") || len(result.Plans) != 0 {
					t.Fatalf("refusal: %+v %v", result, err)
				}
			} else if err != nil || result.Total != tc.wantCount || len(result.Plans) != tc.wantCount {
				t.Fatalf("grouping/authority: %+v %v", result, err)
			}
			if store.reads.Load() != int32(tc.wantReads) {
				t.Fatalf("private reads=%d want%d", store.reads.Load(), tc.wantReads)
			}
			wantSelected, wantMember := int32(0), int32(0)
			if tc.selection != "" {
				wantSelected = 1
				if !tc.deny {
					wantMember = 1
				}
			}
			if selected.Load() != wantSelected || membership.Load() != wantMember {
				t.Fatalf("selection/membership=%d/%d", selected.Load(), membership.Load())
			}
		})
	}
}

func TestConnectPlanListRefusesUnsupportedScopedProcedureWithoutFallback(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/conduit.plans.v1.PlanScopedReadService/ListPlanSummariesInSpace" {
			t.Errorf("unscoped/fallback request: %s %s", r.Method, r.URL.Path)
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	adapter, err := NewHTTPControlPlane(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.SetSpaceSelection("space_0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if err := adapter.SetPlanReadTransport("connect"); err != nil {
		t.Fatal(err)
	}
	result, err := adapter.ListPlans(t.Context(), PlanListQuery{})
	if err == nil || calls.Load() != 1 || len(result.Plans) != 0 || result.Total != 0 {
		t.Fatalf("silently unscoped/replayed: %+v %v calls%d", result, err, calls.Load())
	}
}
