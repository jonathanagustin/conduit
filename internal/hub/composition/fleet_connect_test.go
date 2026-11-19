package composition

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	fleetv1 "github.com/jonathanagustin/conduit/gen/go/conduit/fleet/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	spacememory "github.com/jonathanagustin/conduit/internal/spaces/adapters/memory"
	"google.golang.org/protobuf/encoding/protojson"
)

const fleetReadProcedure = "/conduit.fleet.v1.FleetReadService/ListFleetInstallations"

func postFleetRead(handler http.Handler, actor, represented, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, fleetReadProcedure, strings.NewReader(body))
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

func TestFleetConnectMatchesRESTAuthoritySpacesFiltersAndPages(t *testing.T) {
	f := newSpaceIndexFixture(t, controlplane.NewMemoryRepository(), spacememory.New())
	for _, actor := range []string{"admin", "reader", "outsider"} {
		for _, space := range []string{"", f.first, f.second} {
			for _, filter := range []map[string]string{{}, {"health": "HEALTHY"}, {"search": "a-one"}, {"product": "com.example:product-1"}} {
				q := url.Values{"limit": {"1"}}
				if space != "" {
					q.Set("spaceId", space)
				}
				for k, v := range filter {
					q.Set(k, v)
				}
				for page := 0; page < 8; page++ {
					native := spaceRequest(f.handler, actor, http.MethodGet, "/v1/fleet/installations?"+q.Encode(), "")
					body := map[string]any{"limit": 1, "spaceId": space, "cursor": q.Get("cursor")}
					for k, v := range filter {
						body[k] = v
					}
					encoded, _ := json.Marshal(body)
					wire := postFleetRead(f.handler, actor, "", string(encoded))
					if native.Code != wire.Code {
						t.Fatalf("%s/%s/%v: REST %d RPC %d: %s", actor, space, filter, native.Code, wire.Code, wire.Body)
					}
					if wire.Code != http.StatusOK {
						break
					}
					var expected controlplane.FleetProjection
					actual := &fleetv1.ListFleetInstallationsResponse{}
					if err := json.Unmarshal(native.Body.Bytes(), &expected); err != nil {
						t.Fatal(err)
					}
					if err := protojson.Unmarshal(wire.Body.Bytes(), actual); err != nil {
						t.Fatal(err)
					}
					if actual.Summary.Total != int64(expected.Summary.Total) || actual.Summary.Stale != int64(expected.Summary.Stale) || len(actual.Installations) != len(expected.Installations) || actual.PageSize != int32(expected.PageSize) || actual.Limit != int32(expected.Limit) || actual.NextCursor != expected.NextCursor || actual.PreviousCursor != expected.PreviousCursor || actual.HasPrevious != expected.HasPrevious {
						t.Fatalf("page differs: REST %+v RPC %v", expected, actual)
					}
					if len(filter) == 0 {
						want := int64(0)
						if actor == "admin" {
							want = 4
							if space != "" {
								want = 2
							}
						}
						if actor == "reader" && space != f.second {
							want = 1
						}
						if actual.Summary.Total != want {
							t.Fatalf("authority leaked or lost: %s/%s got %d want %d", actor, space, actual.Summary.Total, want)
						}
					}
					for _, pair := range []struct {
						native map[string]int
						wire   map[string]int64
					}{
						{expected.Summary.Health, actual.Summary.Health}, {expected.Summary.Rollout, actual.Summary.Rollout}, {expected.Summary.Config, actual.Summary.Config}, {expected.Summary.Lifecycle, actual.Summary.Lifecycle},
						{expected.Facets.Environment, actual.Facets.Environment}, {expected.Facets.Product, actual.Facets.Product}, {expected.Facets.Health, actual.Facets.Health}, {expected.Facets.Rollout, actual.Facets.RolloutStatus}, {expected.Facets.Config, actual.Facets.ConfigStatus}, {expected.Facets.Lifecycle, actual.Facets.Lifecycle},
					} {
						counts := map[string]int64{}
						for k, v := range pair.native {
							counts[k] = int64(v)
						}
						if len(counts) != len(pair.wire) {
							t.Fatalf("facet cardinality changed: %v %v", counts, pair.wire)
						}
						for k, v := range counts {
							if pair.wire[k] != v {
								t.Fatalf("facet changed: %v %v", counts, pair.wire)
							}
						}
					}
					if actual.GeneratedAt == nil || actual.GeneratedAt.CheckValid() != nil || time.Since(actual.GeneratedAt.AsTime()) > time.Minute {
						t.Fatal("invalid Hub clock")
					}
					for i, b := range expected.Installations {
						a := actual.Installations[i]
						// Normalize only observation age: the two independent reads sample the
						// Hub clock separately. Every other visible field must remain identical.
						encoded, _ := protojson.Marshal(a)
						var row controlplane.FleetInstallation
						// Proto int64 JSON is a string; compare these two values explicitly.
						var fields map[string]json.RawMessage
						_ = json.Unmarshal(encoded, &fields)
						delete(fields, "observationAgeSeconds")
						encoded, _ = json.Marshal(fields)
						if err := json.Unmarshal(encoded, &row); err != nil {
							t.Fatal(err)
						}
						if b.ObservationAgeSeconds-a.ObservationAgeSeconds > 1 || a.ObservationAgeSeconds-b.ObservationAgeSeconds > 1 {
							t.Fatal("observation age changed")
						}
						b.ObservationAgeSeconds = 0
						if !reflect.DeepEqual(row, b) {
							t.Fatalf("row differs: REST %+v RPC %+v", b, row)
						}
					}
					if strings.Contains(wire.Body.String(), "facetContributions") {
						t.Fatal("internal facet scaffolding leaked")
					}
					if actual.NextCursor == "" {
						break
					}
					q.Set("cursor", actual.NextCursor)
					if page == 7 {
						t.Fatal("pagination did not terminate")
					}
				}
			}
		}
	}
}

func TestFleetConnectPreservesOriginalDelegatingCarrier(t *testing.T) {
	_, repository, _ := delegationHub(t)
	if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: "fleet-private", Plan: controlplane.Plan{Environment: "private-environment", Steps: []controlplane.PlanStep{{InstallationID: "app", Product: "native:app"}}}}); err != nil {
		t.Fatal(err)
	}
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}})
	for _, tc := range []struct {
		actor, represented string
		total              int64
		status             int
	}{{"alice", "", 1, 200}, {"alice", "bob", 1, 200}, {"robot", "alice", 0, 200}, {"bob", "alice", 0, 403}, {"bob", "", 0, 200}} {
		native := as(t, handler, "/v1/fleet/installations", tc.actor, tc.represented)
		wire := postFleetRead(handler, tc.actor, tc.represented, `{}`)
		if native.Code != tc.status || wire.Code != tc.status {
			t.Fatalf("%s as %s: REST %d RPC %d %s", tc.actor, tc.represented, native.Code, wire.Code, wire.Body)
		}
		if wire.Code == 200 {
			page := &fleetv1.ListFleetInstallationsResponse{}
			if protojson.Unmarshal(wire.Body.Bytes(), page) != nil || page.Summary.Total != tc.total {
				t.Fatalf("carrier authority changed: %s", wire.Body)
			}
		}
	}
}

func TestFleetConnectProductionAuthAndUnknownMethods(t *testing.T) {
	repository := &fleetSnapshotCounter{Repository: controlplane.NewMemoryRepository()}
	handler, err := NewHub(repository, controlplane.HandlerOptions{BootstrapAdministrators: []string{"root"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"", "root"} {
		response := postFleetRead(handler, actor, "", `{`)
		if response.Code != 401 || repository.reads.Load() != 0 {
			t.Fatalf("production auth=%d reads=%d", response.Code, repository.reads.Load())
		}
	}
	for _, tc := range []struct{ method, path string }{{"GET", fleetReadProcedure}, {"POST", "/conduit.fleet.v1.FleetReadService/ApprovePlan"}, {"POST", "/conduit.fleet.v1.FleetReadService/ListEverything"}} {
		response := driveAs(t, handler, tc.method, tc.path, "root", "")
		if (response.Code != 404 && response.Code != 405) || repository.reads.Load() != 0 {
			t.Fatalf("unknown method=%d reads=%d", response.Code, repository.reads.Load())
		}
	}
}

type fleetSnapshotCounter struct {
	controlplane.Repository
	reads atomic.Int64
}

func (r *fleetSnapshotCounter) ListPlans(ctx context.Context, environment string) ([]controlplane.PlanRecord, error) {
	r.reads.Add(1)
	return r.Repository.ListPlans(ctx, environment)
}
func (r *fleetSnapshotCounter) ListEnvironmentReports(ctx context.Context) ([]controlplane.EnvironmentReport, error) {
	r.reads.Add(1)
	return r.Repository.ListEnvironmentReports(ctx)
}

func TestFleetConnectCookieReadsPreserveOriginAndCSRFPolicy(t *testing.T) {
	harness := newSignInHarness(t, signInSetup{})
	session := harness.signIn()
	for _, tc := range []struct {
		origin, csrf string
		status       int
	}{{signInOrigin, "1", 200}, {signInOrigin, "", 401}, {"https://evil.example", "1", 401}, {"", "1", 401}} {
		request := httptest.NewRequest(http.MethodPost, signInOrigin+fleetReadProcedure, strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.AddCookie(&http.Cookie{Name: session.Name, Value: session.Value})
		if tc.origin != "" {
			request.Header.Set("Origin", tc.origin)
		}
		if tc.csrf != "" {
			request.Header.Set("X-Conduit-CSRF", tc.csrf)
		}
		response := harness.do(request)
		if response.Code != tc.status {
			t.Fatalf("cookie policy=%d %s want=%d", response.Code, response.Body, tc.status)
		}
	}
}
