package composition

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	environmentsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	spacememory "github.com/jonathanagustin/conduit/internal/spaces/adapters/memory"
	"google.golang.org/protobuf/encoding/protojson"
)

const environmentSummaryProcedure = "/conduit.environments.v1.EnvironmentReadService/ListEnvironmentSummaries"

func postEnvironmentSummaries(handler http.Handler, actor, body string) *httptest.ResponseRecorder {
	return postEnvironmentSummariesFor(handler, actor, "", body)
}

func postEnvironmentSummariesFor(handler http.Handler, actor, represented, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, environmentSummaryProcedure, strings.NewReader(body))
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

func TestEnvironmentSummaryConnectMatchesNativeAuthoritySpacesAndPages(t *testing.T) {
	f := newSpaceIndexFixture(t, controlplane.NewMemoryRepository(), spacememory.New())
	state := controlplane.DesiredState{Environment: controlplane.Environment{ID: "e-no-report", Generation: "generation:opaque", Config: map[string]any{"private": "must-not-travel"}}}
	if _, err := f.repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: "plan-no-report", DesiredState: &state, Plan: controlplane.Plan{Environment: "e-no-report"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repository.PutEnvironmentReport(t.Context(), controlplane.EnvironmentReport{Environment: "f-zero-time", AgentID: "zero-agent"}); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"admin", "reader", "outsider"} {
		for _, space := range []string{"", f.first, f.second} {
			q := url.Values{"view": {"summary"}, "limit": {"1"}, "search": {" "}}
			if space != "" {
				q.Set("spaceId", space)
			}
			for pageNumber := 0; pageNumber < 10; pageNumber++ {
				native := spaceRequest(f.handler, actor, http.MethodGet, "/v1/environments?"+q.Encode(), "")
				body, _ := json.Marshal(map[string]any{"limit": 1, "cursor": q.Get("cursor"), "search": q.Get("search"), "spaceId": space})
				wire := postEnvironmentSummaries(f.handler, actor, string(body))
				if native.Code != wire.Code {
					t.Fatalf("%s/%s: REST %d RPC %d: %s", actor, space, native.Code, wire.Code, wire.Body)
				}
				if wire.Code != http.StatusOK {
					break
				}
				if strings.Contains(wire.Body.String(), "must-not-travel") || strings.Contains(wire.Body.String(), `"config"`) || strings.Contains(wire.Body.String(), `"entities"`) {
					t.Fatal("RPC leaked configuration or Entity bodies")
				}
				var expected controlplane.EnvironmentCatalogSummaryPage
				actual := &environmentsv1.ListEnvironmentSummariesResponse{}
				if json.Unmarshal(native.Body.Bytes(), &expected) != nil || protojson.Unmarshal(wire.Body.Bytes(), actual) != nil {
					t.Fatal("invalid native/RPC page")
				}
				if actual.Total != int64(expected.Total) || actual.PageSize != int32(expected.PageSize) || actual.Limit != int32(expected.Limit) || actual.NextCursor != expected.NextCursor || actual.PreviousCursor != expected.PreviousCursor || actual.HasPrevious != expected.HasPrevious || len(actual.Environments) != len(expected.Environments) {
					t.Fatalf("page mismatch: REST %+v RPC %v", expected, actual)
				}
				wantTotal := int64(0)
				if actor == "admin" {
					wantTotal = 6
					if space != "" {
						wantTotal = 2
					}
				}
				if actor == "reader" {
					wantTotal = 1
				}
				if actual.Total != wantTotal {
					t.Fatalf("authority/grouping total: %s/%s got %d want %d", actor, space, actual.Total, wantTotal)
				}
				for i, record := range expected.Environments {
					item := actual.Environments[i]
					if item.Environment.Id != record.Environment.ID || item.Environment.Generation != record.Environment.Generation || item.Spoke.State != record.Spoke.State || item.Spoke.AgentId != record.Spoke.AgentID || (item.Reported == nil) != (record.Reported == nil) {
						t.Fatalf("projection mismatch: %+v %v", record, item)
					}
					if record.Reported != nil && (item.Reported.Environment != record.Reported.Environment || item.Reported.AgentId != record.Reported.AgentID || item.Reported.EntityCount != int64(record.Reported.EntityCount) || (item.Reported.ObservedAt == nil) != record.Reported.ObservedAt.IsZero() || (!record.Reported.ObservedAt.IsZero() && !item.Reported.ObservedAt.AsTime().Equal(record.Reported.ObservedAt))) {
						t.Fatal("reported evidence changed")
					}
					if (item.Spoke.ObservedAt == nil) != record.Spoke.ObservedAt.IsZero() {
						t.Fatal("missing Spoke time replaced")
					}
				}
				if actual.NextCursor == "" {
					break
				}
				if actor == "admin" && space == f.first {
					changed, _ := json.Marshal(map[string]any{"limit": 1, "cursor": actual.NextCursor, "spaceId": f.second})
					if response := postEnvironmentSummaries(f.handler, actor, string(changed)); response.Code != http.StatusBadRequest {
						t.Fatalf("cursor changed Space: %d %s", response.Code, response.Body)
					}
				}
				bad, _ := json.Marshal(map[string]any{"limit": 1, "cursor": actual.NextCursor, "search": "different", "spaceId": space})
				if response := postEnvironmentSummaries(f.handler, actor, string(bad)); response.Code != http.StatusBadRequest {
					t.Fatalf("cursor filter changed: %d %s", response.Code, response.Body)
				}
				q.Set("cursor", actual.NextCursor)
			}
		}
	}
	for _, body := range []string{`{"limit":0}`, `{"limit":201}`, `{"cursor":"bad"}`, `{"spaceId":"not-a-space"}`} {
		if response := postEnvironmentSummaries(f.handler, "admin", body); response.Code != http.StatusBadRequest {
			t.Fatalf("invalid query admitted: %s: %d %s", body, response.Code, response.Body)
		}
	}
	secure := NewHubHandler(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{})
	if response := postEnvironmentSummaries(secure, "", "{"); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous decoded before authentication: %d %s", response.Code, response.Body)
	}
	f.store.fail = true
	response := postEnvironmentSummaries(f.handler, "admin", `{"spaceId":"`+f.first+`"}`)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "private-membership-diagnostic") {
		t.Fatalf("membership failure leaked or widened: %d %s", response.Code, response.Body)
	}
}

func TestEnvironmentSummaryConnectPreservesDelegatingCarrier(t *testing.T) {
	_, repository, _ := delegationHub(t)
	if _, err := repository.PutEnvironmentReport(t.Context(), controlplane.EnvironmentReport{Environment: "private-environment"}); err != nil {
		t.Fatal(err)
	}
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}})
	for _, tc := range []struct {
		actor, represented string
		total              int64
		status             int
	}{{"alice", "", 1, 200}, {"alice", "bob", 1, 200}, {"robot", "alice", 0, 200}, {"bob", "alice", 0, 403}, {"bob", "", 0, 200}} {
		native := as(t, handler, "/v1/environments?view=summary", tc.actor, tc.represented)
		wire := postEnvironmentSummariesFor(handler, tc.actor, tc.represented, `{}`)
		if native.Code != tc.status || wire.Code != tc.status {
			t.Fatalf("%s as %s: REST %d RPC %d %s", tc.actor, tc.represented, native.Code, wire.Code, wire.Body)
		}
		if wire.Code == http.StatusOK {
			page := &environmentsv1.ListEnvironmentSummariesResponse{}
			if protojson.Unmarshal(wire.Body.Bytes(), page) != nil || page.Total != tc.total {
				t.Fatalf("carrier authority changed: %s", wire.Body)
			}
		}
	}
}
