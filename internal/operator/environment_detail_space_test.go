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

type environmentDetailSpaceCatalogCounter struct {
	controlplane.Repository
	reads atomic.Int32
}

func (s *environmentDetailSpaceCatalogCounter) ListPlans(ctx context.Context, id string) ([]controlplane.PlanRecord, error) {
	s.reads.Add(1)
	return s.Repository.ListPlans(ctx, id)
}
func (s *environmentDetailSpaceCatalogCounter) ListEnvironmentReports(ctx context.Context) ([]controlplane.EnvironmentReport, error) {
	s.reads.Add(1)
	return s.Repository.ListEnvironmentReports(ctx)
}

func TestEnvironmentDetailRESTClientHonorsSelectedSpaceAtRealHub(t *testing.T) {
	const space = "space_0123456789abcdef0123456789abcdef"
	for _, tc := range []struct {
		name, actor, selection, environment string
		denySelection                       bool
		status                              int
		selectionReads, membershipReads     int32
	}{
		{name: "unscoped authorized outside Environment", actor: "reader", environment: "outside", status: 200},
		{name: "selected member", actor: "reader", selection: space, environment: "inside", status: 200, selectionReads: 1, membershipReads: 1},
		{name: "selected nonmember hidden before catalog", actor: "reader", selection: space, environment: "outside", status: 404, selectionReads: 1, membershipReads: 1},
		{name: "unreadable selected Space refused", actor: "reader", selection: space, environment: "inside", denySelection: true, status: 403, selectionReads: 1},
		{name: "Space cannot grant resource authority", actor: "outsider", selection: space, environment: "inside", status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := controlplane.NewMemoryRepository()
			for _, id := range []string{"inside", "outside"} {
				if _, err := repository.PutEnvironmentReport(t.Context(), controlplane.EnvironmentReport{Environment: id}); err != nil {
					t.Fatal(err)
				}
			}
			store := &environmentDetailSpaceCatalogCounter{Repository: repository}
			var selections, memberships atomic.Int32
			hub := controlplane.NewHandler(store, controlplane.HandlerOptions{
				OperatorIdentityResolver: func(*http.Request) (controlplane.IdentityClaims, error) {
					return controlplane.IdentityClaims{Username: tc.actor}, nil
				},
				BootstrapAdministrators: []string{"reader"},
				RequireSpaceSelection: func(_ context.Context, p controlplane.Principal, id string) error {
					selections.Add(1)
					if p.Username != tc.actor || id != space {
						t.Errorf("selection lost carrier or identity: %+v %s", p, id)
					}
					if tc.denySelection {
						return controlplane.ErrForbidden
					}
					return nil
				},
				SpaceIDLookup: func(context.Context, controlplane.Principal, string, string) (string, error) {
					t.Error("detail used unrelated resource lookup")
					return "", nil
				},
				LoadSpaceIndexMembership: func(_ context.Context, p controlplane.Principal, id string) (controlplane.SpaceIndexMembership, error) {
					memberships.Add(1)
					if p.Username != tc.actor || id != space {
						t.Errorf("membership lost carrier or selection: %+v %s", p, id)
					}
					return controlplane.SpaceIndexMembership{EnvironmentIDs: []string{"inside"}}, nil
				},
			})
			server := httptest.NewServer(hub)
			defer server.Close()
			client, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if err := client.SetSpaceSelection(tc.selection); err != nil {
				t.Fatal(err)
			}
			record, err := client.GetEnvironment(t.Context(), tc.environment)
			if tc.status == 200 {
				if err != nil || record.Environment.ID != tc.environment || record.Reported == nil || store.reads.Load() == 0 {
					t.Fatalf("authorized read: %+v %v reads=%d", record, err, store.reads.Load())
				}
			} else if err == nil || !strings.HasPrefix(err.Error(), "operator control plane returned "+strconv.Itoa(tc.status)+":") || record.Environment.ID != "" || store.reads.Load() != 0 {
				t.Fatalf("refusal leaked/read private catalog: record=%+v err=%v reads=%d", record, err, store.reads.Load())
			}
			if selections.Load() != tc.selectionReads || memberships.Load() != tc.membershipReads {
				t.Fatalf("selection/membership ordering changed: %d/%d", selections.Load(), memberships.Load())
			}
		})
	}
}
