package operator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestResourceRESTReadsPreserveDistinctNativeSpaceMembership(t *testing.T) {
	const space = "space_0123456789abcdef0123456789abcdef"
	for _, tc := range []struct {
		name, actor, selection, target string
		denied                         bool
		entityStatus, productStatus    int
	}{
		{name: "unscoped", actor: "reader", target: "inside", entityStatus: 200, productStatus: 200},
		{name: "Environment member is not Product member", actor: "reader", selection: space, target: "inside", entityStatus: 200, productStatus: 404},
		{name: "Product member is not Environment member", actor: "reader", selection: space, target: "outside", entityStatus: 404, productStatus: 200},
		{name: "Space unreadable", actor: "reader", selection: space, target: "inside", denied: true, entityStatus: 403, productStatus: 403},
		{name: "membership grants no resource authority", actor: "outsider", selection: space, target: "outside", entityStatus: 403, productStatus: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := controlplane.NewMemoryRepository()
			for _, target := range []string{"inside", "outside"} {
				state := controlplane.DesiredState{Environment: controlplane.Environment{ID: target}, Entities: []controlplane.Entity{{Name: target}}, Releases: []controlplane.Release{{Product: "com.example:" + target, Version: "1.0.0"}}}
				if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: "plan-" + target, Plan: controlplane.Plan{Environment: target}, DesiredState: &state}); err != nil {
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
					return controlplane.SpaceIndexMembership{EnvironmentIDs: []string{"inside"}, ProductIDs: []string{"com.example:outside"}}, nil
				},
			})
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Query().Get("spaceId") != tc.selection {
					t.Errorf("selection dropped: %s %s", r.Method, r.URL)
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
			entities, err := adapter.ListEntities(t.Context(), tc.target)
			if tc.entityStatus == 200 {
				if err != nil || len(entities) != 1 || entities[0].Name != tc.target {
					t.Fatalf("entities=%+v err%v", entities, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), strconv.Itoa(tc.entityStatus)) || len(entities) != 0 {
				t.Fatalf("Entity refusal=%+v %v", entities, err)
			}
			product := "com.example:" + tc.target
			releases, err := adapter.ListProductReleases(t.Context(), product)
			if tc.productStatus == 200 {
				if err != nil || len(releases) != 1 || releases[0].Product != product || releases[0].Version != "1.0.0" {
					t.Fatalf("releases=%+v err%v", releases, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), strconv.Itoa(tc.productStatus)) || len(releases) != 0 {
				t.Fatalf("release list refusal=%+v %v", releases, err)
			}
			release, err := adapter.GetProductRelease(t.Context(), product, "1.0.0")
			if tc.productStatus == 200 {
				if err != nil || release.Product != product || release.Version != "1.0.0" {
					t.Fatalf("release=%+v err%v", release, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), strconv.Itoa(tc.productStatus)) || release.Product != "" {
				t.Fatalf("release detail refusal=%+v %v", release, err)
			}
			if calls != 3 {
				t.Fatalf("calls%d want3", calls)
			}
		})
	}
}
