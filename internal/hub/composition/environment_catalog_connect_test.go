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
	"google.golang.org/protobuf/proto"
)

const environmentCatalogProcedure = "/conduit.environments.v1.EnvironmentCatalogService/ListEnvironments"

func catalogRequest(handler http.Handler, actor, represented, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, environmentCatalogProcedure, strings.NewReader(body))
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

func TestEnvironmentCatalogConnectMatchesFullNativePagesAndDetailRecords(t *testing.T) {
	f := newSpaceIndexFixture(t, controlplane.NewMemoryRepository(), spacememory.New())
	desired := controlplane.DesiredState{Environment: controlplane.Environment{
		ID: "e-full-config", Generation: "opaque:revision", Config: map[string]any{"literal": "<raw>", "nested": map[string]any{"enabled": false}},
		Labels: map[string]string{"owner": "native"}, AutomaticSecurityRecall: true, AutoSuppressionThreshold: 7,
		Namespaces: []controlplane.NamespaceTarget{{RID: "namespace", Name: "ns", DeletionProtected: new(false)}},
	}}
	if _, err := f.repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: "full-config-plan", DesiredState: &desired, Plan: controlplane.Plan{Environment: "e-full-config"}}); err != nil {
		t.Fatal(err)
	}

	for _, actor := range []string{"admin", "reader", "outsider"} {
		for _, space := range []string{"", f.first, f.second} {
			q := url.Values{"view": {"full"}, "limit": {"1"}, "search": {" "}}
			body := map[string]any{"limit": 1, "search": " "}
			if space != "" {
				q.Set("spaceId", space)
				body["spaceId"] = space
			}
			exhausted := false
			for pageNumber := 0; pageNumber < 20; pageNumber++ {
				native := spaceRequest(f.handler, actor, http.MethodGet, "/v1/environments?"+q.Encode(), "")
				payload, _ := json.Marshal(body)
				wire := catalogRequest(f.handler, actor, "", string(payload))
				if native.Code != wire.Code {
					t.Fatalf("%s/%s REST %d %s RPC %d %s", actor, space, native.Code, native.Body, wire.Code, wire.Body)
				}
				if wire.Code != 200 {
					exhausted = true
					break
				}
				var expected controlplane.EnvironmentCatalogPage
				actual := &environmentsv1.ListEnvironmentsResponse{}
				if err := json.Unmarshal(native.Body.Bytes(), &expected); err != nil {
					t.Fatal(err)
				}
				if err := protojson.Unmarshal(wire.Body.Bytes(), actual); err != nil {
					t.Fatal(err)
				}
				if actual.Total != int64(expected.Total) || actual.PageSize != int32(expected.PageSize) || actual.Limit != int32(expected.Limit) || actual.NextCursor != expected.NextCursor || actual.PreviousCursor != expected.PreviousCursor || actual.HasPrevious != expected.HasPrevious || len(actual.Environments) != len(expected.Environments) {
					t.Fatalf("metadata mismatch: %+v %v", expected, actual)
				}
				for i, record := range expected.Environments {
					detailBody := map[string]any{"environment": record.Environment.ID}
					if space != "" {
						detailBody["spaceId"] = space
					}
					payload, _ := json.Marshal(detailBody)
					detail := detailRequest(f.handler, actor, "", string(payload))
					value := &environmentsv1.GetEnvironmentResponse{}
					if detail.Code != 200 {
						t.Fatalf("detail %d %s", detail.Code, detail.Body)
					}
					if err := protojson.Unmarshal(detail.Body.Bytes(), value); err != nil {
						t.Fatal(err)
					}
					if !proto.Equal(actual.Environments[i], value.Record) {
						t.Fatalf("full record differs: %v %v", actual.Environments[i], value.Record)
					}
				}
				if actual.NextCursor == "" {
					exhausted = true
					break
				}
				body["cursor"] = actual.NextCursor
				q.Set("cursor", actual.NextCursor)
				changed := map[string]any{"limit": 1, "search": "different", "cursor": actual.NextCursor}
				if space != "" {
					changed["spaceId"] = space
				}
				payload, _ = json.Marshal(changed)
				if refused := catalogRequest(f.handler, actor, "", string(payload)); refused.Code != 400 {
					t.Fatalf("changed search accepted: %d %s", refused.Code, refused.Body)
				}
			}
			if !exhausted {
				t.Fatal("pagination did not terminate")
			}
		}
	}
	for _, body := range []string{`{"spaceId":""}`, `{"spaceId":"invalid"}`, `{"limit":0}`, `{"limit":21}`, `{"cursor":"invalid"}`} {
		if got := catalogRequest(f.handler, "admin", "", body); got.Code != 400 {
			t.Fatalf("invalid input %s: %d %s", body, got.Code, got.Body)
		}
	}
}

func TestEnvironmentCatalogConnectAuthenticatesBeforeDecodeAndLimitsMethod(t *testing.T) {
	store := &detailReadCounter{Repository: controlplane.NewMemoryRepository()}
	handler := NewHubHandler(store, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := catalogRequest(handler, "", "", `{`)
	if response.Code != 401 && response.Code != 403 {
		t.Fatalf("anonymous decode preceded auth: %d %s", response.Code, response.Body)
	}
	if store.reads != 0 {
		t.Fatal("anonymous request read private catalog")
	}
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		request := httptest.NewRequest(method, environmentCatalogProcedure, nil)
		request.Header.Set("X-Platform-Subject", "reader")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 405 {
			t.Fatalf("%s: %d %s", method, response.Code, response.Body)
		}
	}
	if store.reads != 0 {
		t.Fatal("unsupported method read private catalog")
	}
}
