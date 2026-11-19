package composition

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	spacememory "github.com/jonathanagustin/conduit/internal/spaces/adapters/memory"
	"google.golang.org/protobuf/encoding/protojson"
)

const scopedPlanSummaryProcedure = "/conduit.plans.v1.PlanScopedReadService/ListPlanSummariesInSpace"

func postScopedPlanSummaries(handler http.Handler, actor, represented, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, scopedPlanSummaryProcedure, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Connect-Protocol-Version", "1")
	if actor != "" {
		r.Header.Set("X-Platform-Subject", actor)
	}
	if represented != "" {
		r.Header.Set("X-Platform-Service-Account", "true")
		r.Header.Set("X-Platform-On-Behalf-Of", represented)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestScopedPlanSummaryConnectMatchesNativeVisibilitySpacesAndPages(t *testing.T) {
	f := newSpaceIndexFixture(t, controlplane.NewMemoryRepository(), spacememory.New())
	cursorBoundSeen := false
	for _, actor := range []string{"admin", "reader", "reviewer", "outsider"} {
		for _, space := range []string{f.first, f.second} {
			query := url.Values{"view": {"summary"}, "spaceId": {space}, "limit": {"1"}, "search": {" plan "}}
			exhausted := false
			for pageNumber := 0; pageNumber < 10; pageNumber++ {
				native := spaceRequest(f.handler, actor, http.MethodGet, "/v1/plans?"+query.Encode(), "")
				body, _ := json.Marshal(map[string]any{"spaceId": space, "limit": 1, "cursor": query.Get("cursor"), "search": query.Get("search")})
				wire := postScopedPlanSummaries(f.handler, actor, "", string(body))
				if native.Code != wire.Code {
					t.Fatalf("%s/%s REST%d %s RPC%d %s", actor, space, native.Code, native.Body, wire.Code, wire.Body)
				}
				if wire.Code != 200 {
					exhausted = true
					break
				}
				var expected controlplane.PlanSummaryPage
				actual := &plansv1.ListPlanSummariesInSpaceResponse{}
				if err := json.Unmarshal(native.Body.Bytes(), &expected); err != nil {
					t.Fatal(err)
				}
				if err := protojson.Unmarshal(wire.Body.Bytes(), actual); err != nil {
					t.Fatal(err)
				}
				page := actual.Page
				if actual.SpaceId != space || page == nil || page.Total != int64(expected.Total) || page.PageSize != int32(expected.PageSize) || page.Limit != int32(expected.Limit) || page.NextCursor != expected.NextCursor || page.PreviousCursor != expected.PreviousCursor || page.HasPrevious != expected.HasPrevious || len(page.Plans) != len(expected.Plans) {
					t.Fatalf("page mismatch: native%+v RPC%v", expected, actual)
				}
				for i, row := range expected.Plans {
					value := page.Plans[i]
					if value.Id != row.ID || value.Environment != row.Environment || value.Status != string(row.Status) || value.StepCount != int64(row.StepCount) || value.ApprovalCount != int64(row.ApprovalCount) || value.RequiredApprovals != int64(row.RequiredApprovals) || value.Revision != row.Revision || !value.CreatedAt.AsTime().Equal(row.CreatedAt) || !value.UpdatedAt.AsTime().Equal(row.UpdatedAt) {
						t.Fatalf("summary mismatch: %+v %v", row, value)
					}
				}
				if strings.Contains(wire.Body.String(), "lease") || strings.Contains(wire.Body.String(), "desiredState") {
					t.Fatal("private full Plan fields escaped")
				}
				if page.NextCursor == "" {
					exhausted = true
					break
				}
				other := f.first
				if space == f.first {
					other = f.second
				}
				changed, _ := json.Marshal(map[string]any{"spaceId": other, "limit": 1, "cursor": page.NextCursor, "search": query.Get("search")})
				nativeChangedQuery, _ := url.ParseQuery(query.Encode())
				nativeChangedQuery.Set("spaceId", other)
				nativeChangedQuery.Set("cursor", page.NextCursor)
				nativeChanged := spaceRequest(f.handler, actor, http.MethodGet, "/v1/plans?"+nativeChangedQuery.Encode(), "")
				wantStatus := nativeChanged.Code
				switch wantStatus {
				case http.StatusUnprocessableEntity:
					wantStatus = http.StatusBadRequest
					cursorBoundSeen = true
				case http.StatusForbidden, http.StatusNotFound:
					// Selection authorization must precede cursor validation, as in REST.
				default:
					t.Fatalf("unexpected native changed-Space result: %d %s", nativeChanged.Code, nativeChanged.Body)
				}
				if refused := postScopedPlanSummaries(f.handler, actor, "", string(changed)); refused.Code != wantStatus {
					t.Fatalf("changed Space refusal order: REST%d %s RPC%d %s", nativeChanged.Code, nativeChanged.Body, refused.Code, refused.Body)
				}
				changed, _ = json.Marshal(map[string]any{"spaceId": space, "limit": 1, "cursor": page.NextCursor, "search": "different"})
				if refused := postScopedPlanSummaries(f.handler, actor, "", string(changed)); refused.Code != 400 {
					t.Fatalf("changed filters accepted: %d %s", refused.Code, refused.Body)
				}
				query.Set("cursor", page.NextCursor)
			}
			if !exhausted {
				t.Fatal("pagination did not terminate")
			}
		}
	}
	if !cursorBoundSeen {
		t.Fatal("no readable-Space cursor binding witnessed")
	}
}

func TestScopedPlanSummaryConnectRefusesBeforePrivateReads(t *testing.T) {
	repository := &planSummaryCountingRepository{Repository: controlplane.NewMemoryRepository()}
	secure := NewHubHandler(repository, controlplane.HandlerOptions{})
	if response := postScopedPlanSummaries(secure, "", "", "{"); response.Code != 401 || repository.reads.Load() != 0 {
		t.Fatalf("anonymous decoded/read: %d %s", response.Code, response.Body)
	}
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}})
	for _, body := range []string{`{}`, `{"spaceId":""}`, `{"spaceId":"invalid"}`} {
		before := repository.reads.Load()
		response := postScopedPlanSummaries(handler, "admin", "", body)
		if response.Code != 400 || repository.reads.Load() != before {
			t.Fatalf("input reached private Plans: %d %s", response.Code, response.Body)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPut} {
		r := httptest.NewRequest(method, scopedPlanSummaryProcedure, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 405 {
			t.Fatalf("method%s served: %d %s", method, w.Code, w.Body)
		}
	}
	if repository.reads.Load() != 0 {
		t.Fatal("refused request read Plans")
	}
}

func TestScopedPlanSummaryConnectRetainsOriginalDelegatingCarrier(t *testing.T) {
	const space = "space_0123456789abcdef0123456789abcdef"
	_, repository, _ := delegationHub(t)
	if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: "secret-plan", DesiredStateDigest: "secret", Plan: controlplane.Plan{Environment: "secret-env"}, Status: controlplane.PlanRunning}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		actor, represented string
		count              int64
		status             int
	}{
		{"alice", "", 1, 200}, {"alice", "bob", 1, 200}, {"robot", "alice", 0, 200}, {"bob", "alice", 0, 403}, {"bob", "", 0, 200},
	} {
		handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}, RequireSpaceSelection: func(_ context.Context, p controlplane.Principal, id string) error {
			if p.Username != tc.actor || id != space {
				t.Errorf("Space carrier changed: %+v %s", p, id)
			}
			return nil
		}, SpaceIDLookup: func(context.Context, controlplane.Principal, string, string) (string, error) { return space, nil }, LoadSpaceIndexMembership: func(context.Context, controlplane.Principal, string) (controlplane.SpaceIndexMembership, error) {
			return controlplane.SpaceIndexMembership{EnvironmentIDs: []string{"secret-env"}}, nil
		}})
		native := as(t, handler, "/v1/plans?view=summary&spaceId="+space, tc.actor, tc.represented)
		wire := postScopedPlanSummaries(handler, tc.actor, tc.represented, `{"spaceId":"`+space+`"}`)
		if native.Code != tc.status || wire.Code != tc.status {
			t.Fatalf("%s as %s: REST%d %s RPC%d %s", tc.actor, tc.represented, native.Code, native.Body, wire.Code, wire.Body)
		}
		if wire.Code == 200 {
			page := &plansv1.ListPlanSummariesInSpaceResponse{}
			if err := protojson.Unmarshal(wire.Body.Bytes(), page); err != nil || page.Page == nil || page.Page.Total != tc.count || page.SpaceId != space {
				t.Fatalf("carrier authority changed: %s err%v", wire.Body, err)
			}
		}
	}
}
