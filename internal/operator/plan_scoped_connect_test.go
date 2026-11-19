package operator

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1/plansv1connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestScopedPlanConnectBindsSelectionFiltersAndEveryPage(t *testing.T) {
	const space = "space_0123456789abcdef0123456789abcdef"
	const next = "opaque+next?&page"
	for _, name := range []string{"two pages", "empty page", "missing echo", "wrong echo", "missing page", "second-page mismatch", "second-page refusal", "repeated cursor", "oversized"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != plansv1connect.PlanScopedReadServiceListPlanSummariesInSpaceProcedure {
					t.Errorf("unscoped/fallback: %s %s", r.Method, r.URL.Path)
				}
				request := &plansv1.ListPlanSummariesInSpaceRequest{}
				body, _ := io.ReadAll(r.Body)
				if err := proto.Unmarshal(body, request); err != nil {
					t.Error(err)
				}
				cursor := ""
				if calls == 2 {
					cursor = next
				}
				if request.SpaceId != space || request.GetLimit() != 200 || request.Cursor != cursor || request.Environment != "east & west?" || request.Status != "FUTURE" || request.InstallationId != "app+name" || request.Search != "literal ?" {
					t.Errorf("selection/filters changed: %v", request)
				}
				if name == "second-page refusal" && calls == 2 {
					http.Error(w, "refused", http.StatusForbidden)
					return
				}
				at := timestamppb.New(time.Date(2026, 11, 19, 0, 0, 0, 123, time.UTC))
				response := &plansv1.ListPlanSummariesInSpaceResponse{SpaceId: space, Page: &plansv1.ListPlanSummariesResponse{Total: 2, PageSize: 1, Limit: 200, Plans: []*plansv1.PlanSummary{{Id: fmt.Sprintf("plan-%d", calls), Environment: "east", Status: "FUTURE", CreatedAt: at, UpdatedAt: at, StepCount: 3, ApprovalCount: 2, RequiredApprovals: 1, Revision: math.MaxUint64}}}}
				if calls == 1 {
					response.Page.NextCursor = next
				}
				switch name {
				case "empty page":
					response.Page.Plans = nil
					response.Page.Total = 0
					response.Page.PageSize = 0
					response.Page.NextCursor = ""
				case "oversized":
					response.Page.Plans[0].Id = strings.Repeat("x", 1<<20)
				case "missing echo":
					response.SpaceId = ""
				case "wrong echo":
					response.SpaceId = "space_abcdef0123456789abcdef0123456789"
				case "missing page":
					response.Page = nil
				case "second-page mismatch":
					if calls == 2 {
						response.SpaceId = "space_abcdef0123456789abcdef0123456789"
					}
				case "repeated cursor":
					response.Page.NextCursor = next
				}
				encoded, err := proto.Marshal(response)
				if err != nil {
					t.Error(err)
					return
				}
				w.Header().Set("Content-Type", "application/proto")
				_, _ = w.Write(encoded)
			}))
			defer server.Close()
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if err := adapter.SetSpaceSelection(space); err != nil {
				t.Fatal(err)
			}
			if err := adapter.SetPlanReadTransport("connect"); err != nil {
				t.Fatal(err)
			}
			result, err := adapter.ListPlans(t.Context(), PlanListQuery{Environment: "east & west?", Status: "FUTURE", InstallationID: "app+name", Search: "literal ?"})
			if name == "two pages" {
				if err != nil || calls != 2 || result.Total != 2 || len(result.Plans) != 2 || result.Plans[0].Revision != math.MaxUint64 {
					t.Fatalf("pages: %+v %v calls%d", result, err, calls)
				}
				return
			}
			if name == "empty page" {
				if err != nil || calls != 1 || result.Total != 0 || len(result.Plans) != 0 {
					t.Fatalf("empty: %+v %v calls%d", result, err, calls)
				}
				return
			}
			want := 1
			if name == "second-page mismatch" || name == "second-page refusal" || name == "repeated cursor" {
				want = 2
			}
			if err == nil || calls != want || result.Total != 0 || len(result.Plans) != 0 {
				t.Fatalf("unbound/partial/replayed: %+v %v calls%d want%d", result, err, calls, want)
			}
		})
	}
}
