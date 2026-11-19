package operator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

type productSpaceReadCounter struct {
	controlplane.Repository
	reads atomic.Int32
}

func (s *productSpaceReadCounter) ListPlans(ctx context.Context, environment string) ([]controlplane.PlanRecord, error) {
	s.reads.Add(1)
	return s.Repository.ListPlans(ctx, environment)
}
func (s *productSpaceReadCounter) ListProductPublications(ctx context.Context, product string) ([]controlplane.ProductPublication, error) {
	s.reads.Add(1)
	return s.Repository.ListProductPublications(ctx, product)
}

func TestFullProductRESTClientPreservesSpaceOnEveryCursorPage(t *testing.T) {
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
				if r.Method != "GET" || r.URL.Path != "/v1/products" || q.Get("spaceId") != selection || q.Get("view") != "full" || q.Get("limit") != "20" || q.Get("cursor") != wantCursor {
					t.Errorf("selection/cursor changed: %s", r.URL)
				}
				next, product := cursor, "com.example:first"
				if calls == 2 {
					next, product = "", "com.example:second"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"products": []controlplane.ProductCatalogRecord{{Product: product}}, "total": 2, "pageSize": 1, "limit": 20, "nextCursor": next})
			}))
			defer server.Close()
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if err := adapter.SetSpaceSelection(selection); err != nil {
				t.Fatal(err)
			}
			result, err := adapter.ListProducts(t.Context())
			if err != nil || calls != 2 || len(result) != 2 || result[0].Product != "com.example:first" || result[1].Product != "com.example:second" {
				t.Fatalf("catalog=%+v calls%d err%v", result, calls, err)
			}
		})
	}
}

func TestFullProductRESTClientUsesProductMembershipWithoutGrantingAuthority(t *testing.T) {
	const space = "space_0123456789abcdef0123456789abcdef"
	for _, tc := range []struct {
		name, actor, selection string
		denied                 bool
		wantRows, wantReads    int
	}{
		{name: "unscoped", actor: "reader", wantRows: 2, wantReads: 2},
		{name: "Product grouping differs from Environment grouping", actor: "reader", selection: space, wantRows: 1, wantReads: 2},
		{name: "membership grants no resource authority", actor: "outsider", selection: space, wantRows: 0, wantReads: 2},
		{name: "Space refusal precedes catalog reads", actor: "reader", selection: space, denied: true, wantReads: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := controlplane.NewMemoryRepository()
			for i, name := range []string{"inside", "outside"} {
				environment := "env-" + name
				state := controlplane.DesiredState{Environment: controlplane.Environment{ID: environment}, Releases: []controlplane.Release{{Product: "com.example:" + name, Version: "1.0.0"}}}
				if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: "plan-" + name, DesiredState: &state, Plan: controlplane.Plan{Environment: environment}, CreatedAt: time.Date(2026, 11, 19, 0, 0, i, 0, time.UTC)}); err != nil {
					t.Fatal(err)
				}
			}
			store := &productSpaceReadCounter{Repository: repository}
			var selections, memberships atomic.Int32
			hub := controlplane.NewHandler(store, controlplane.HandlerOptions{OperatorIdentityResolver: func(*http.Request) (controlplane.IdentityClaims, error) {
				return controlplane.IdentityClaims{Username: tc.actor}, nil
			}, BootstrapAdministrators: []string{"reader"}, RequireSpaceSelection: func(_ context.Context, p controlplane.Principal, id string) error {
				selections.Add(1)
				if p.Username != tc.actor || id != space {
					t.Errorf("selection carrier changed: %+v %s", p, id)
				}
				if tc.denied {
					return controlplane.ErrForbidden
				}
				return nil
			}, SpaceIDLookup: func(context.Context, controlplane.Principal, string, string) (string, error) {
				t.Error("unrelated resource lookup")
				return "", nil
			}, LoadSpaceIndexMembership: func(context.Context, controlplane.Principal, string) (controlplane.SpaceIndexMembership, error) {
				memberships.Add(1)
				return controlplane.SpaceIndexMembership{ProductIDs: []string{"com.example:inside"}, EnvironmentIDs: []string{"env-outside"}}, nil
			}})
			server := httptest.NewServer(hub)
			defer server.Close()
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if err := adapter.SetSpaceSelection(tc.selection); err != nil {
				t.Fatal(err)
			}
			rows, err := adapter.ListProducts(t.Context())
			if tc.denied {
				if err == nil || !strings.Contains(err.Error(), "403") || len(rows) != 0 {
					t.Fatalf("refusal: %+v %v", rows, err)
				}
			} else if err != nil || len(rows) != tc.wantRows {
				t.Fatalf("grouping/authority: %+v %v", rows, err)
			}
			if tc.selection != "" && tc.wantRows == 1 && rows[0].Product != "com.example:inside" {
				t.Fatalf("used Environment anchor instead of Product membership: %+v", rows)
			}
			if store.reads.Load() != int32(tc.wantReads) {
				t.Fatalf("catalog reads%d want%d", store.reads.Load(), tc.wantReads)
			}
			wantSelection, wantMembership := int32(0), int32(0)
			if tc.selection != "" {
				wantSelection = 1
				if !tc.denied {
					wantMembership = 1
				}
			}
			if selections.Load() != wantSelection || memberships.Load() != wantMembership {
				t.Fatalf("selection/membership=%d/%d", selections.Load(), memberships.Load())
			}
		})
	}
}
