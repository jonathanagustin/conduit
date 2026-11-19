package operator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestPlanDetailRESTClientHonorsNativeSpaceAndResourceAuthority(t *testing.T) {
	const space = "space_0123456789abcdef0123456789abcdef"
	for _, tc := range []struct {
		name, actor, selection, environment string
		denied                              bool
		status                              int
		selectionReads, membershipReads     int32
	}{
		{name: "unscoped outside Environment", actor: "reader", environment: "outside", status: 200},
		{name: "selected member", actor: "reader", selection: space, environment: "inside", status: 200, selectionReads: 1, membershipReads: 1},
		{name: "selected nonmember", actor: "reader", selection: space, environment: "outside", status: 404, selectionReads: 1, membershipReads: 1},
		{name: "unreadable Space", actor: "reader", selection: space, environment: "inside", denied: true, status: 403, selectionReads: 1},
		{name: "membership grants no authority", actor: "outsider", selection: space, environment: "inside", status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := controlplane.NewMemoryRepository()
			id := "plan-" + tc.environment
			if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: id, Plan: controlplane.Plan{Environment: tc.environment}}); err != nil {
				t.Fatal(err)
			}
			var selections, memberships atomic.Int32
			hub := controlplane.NewHandler(repository, controlplane.HandlerOptions{
				OperatorIdentityResolver: func(*http.Request) (controlplane.IdentityClaims, error) {
					return controlplane.IdentityClaims{Username: tc.actor}, nil
				},
				BootstrapAdministrators: []string{"reader"},
				RequireSpaceSelection: func(_ context.Context, p controlplane.Principal, id string) error {
					selections.Add(1)
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
					memberships.Add(1)
					if p.Username != tc.actor || id != space {
						t.Errorf("membership carrier changed: %+v %s", p, id)
					}
					return controlplane.SpaceIndexMembership{EnvironmentIDs: []string{"inside"}, ProductIDs: []string{"outside"}}, nil
				},
			})
			server := httptest.NewServer(hub)
			defer server.Close()
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if err := adapter.SetSpaceSelection(tc.selection); err != nil {
				t.Fatal(err)
			}
			record, err := adapter.GetPlan(t.Context(), id)
			if tc.status == 200 {
				if err != nil || record.ID != id || record.Plan.Environment != tc.environment {
					t.Fatalf("record=%+v err=%v", record, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), strconv.Itoa(tc.status)) || record.ID != "" {
				t.Fatalf("refusal record=%+v err=%v want%d", record, err, tc.status)
			}
			if selections.Load() != tc.selectionReads || memberships.Load() != tc.membershipReads {
				t.Fatalf("selection/membership=%d/%d want%d/%d", selections.Load(), memberships.Load(), tc.selectionReads, tc.membershipReads)
			}
		})
	}
}
