package composition

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/pgtest"
	spacememory "github.com/jonathanagustin/conduit/internal/spaces/adapters/memory"
	spacepostgres "github.com/jonathanagustin/conduit/internal/spaces/adapters/postgres"
	spaces "github.com/jonathanagustin/conduit/internal/spaces/domain"
	spaceports "github.com/jonathanagustin/conduit/internal/spaces/ports"
)

type countedSpaceRoster struct {
	spaceports.Store
	reads atomic.Int32
	fail  bool
}

func (store *countedSpaceRoster) ListBindings(ctx context.Context, id string) ([]spaces.Binding, error) {
	store.reads.Add(1)
	if store.fail {
		return nil, errors.New("private-membership-diagnostic")
	}
	return store.Store.ListBindings(ctx, id)
}

type spaceIndexFixture struct {
	handler       http.Handler
	repository    controlplane.Repository
	store         *countedSpaceRoster
	first, second string
}

func newSpaceIndexFixture(t *testing.T, repository controlplane.Repository, raw spaceports.Store) spaceIndexFixture {
	t.Helper()
	ctx := t.Context()
	store := &countedSpaceRoster{Store: raw}
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}}, Options{Spaces: store})
	if err != nil {
		t.Fatal(err)
	}
	first := createNativeSpace(t, handler, "First").ID
	second := createNativeSpace(t, handler, "Second").ID
	bind := func(space, kind, id string) {
		t.Helper()
		value, err := spaces.NewBinding(space, spaces.Resource{Kind: kind, ID: id}, "admin", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = raw.BindResource(ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	// Interleaved identities and intentionally opposite Product/Environment
	// assignments expose both post-page filtering and accidental anchor inference.
	for i, environment := range []string{"a-one", "b-two", "c-three", "d-four"} {
		product := fmt.Sprintf("com.example:product-%d", i)
		environmentSpace, productSpace := first, second
		if i%2 == 1 {
			environmentSpace, productSpace = second, first
		}
		bind(environmentSpace, spaces.Environment, environment)
		bind(productSpace, spaces.Product, product)
		at := time.Date(2025, 1, 1, 0, 0, i, 0, time.UTC)
		state := controlplane.DesiredState{Environment: controlplane.Environment{ID: environment}, Releases: []controlplane.Release{{Product: product, Version: "1.0.0"}}}
		if _, err := repository.CreatePlan(ctx, controlplane.PlanRecord{ID: "plan-" + environment, ChangeRequestID: "change-" + environment, DesiredState: &state, Plan: controlplane.Plan{Environment: environment, Steps: []controlplane.PlanStep{{InstallationID: "install-" + environment, Product: product, Namespace: environment, Version: "1.0.0"}}}, Status: controlplane.PlanAwaitingApproval, CreatedAt: at, UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.CreateChangeRequest(ctx, controlplane.ChangeRequest{ID: "change-" + environment, PlanID: "plan-" + environment, Environment: environment, Author: controlplane.Principal{Username: "reviewer"}, Status: controlplane.ChangePendingApprovals, CreatedAt: at, UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.CreatePromotionUnlessStagePaused(ctx, controlplane.PromotionRecord{ID: "promotion-" + environment, Product: product, Version: "1.0.0", FromChannel: "DEV", ToChannel: "RELEASE", Status: controlplane.PromotionPassed, EvaluatedAt: at}); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.PutEnvironmentReport(ctx, controlplane.EnvironmentReport{Environment: environment, AgentID: "agent", ObservedAt: at, Entities: []controlplane.ReportedEntity{{InstallationID: "install-" + environment, Product: product, Namespace: environment, Version: "1.0.0", Health: "HEALTHY"}}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range []struct{ space, id string }{{first, "bundle-a"}, {second, "bundle-b"}} {
		bind(entry.space, spaces.Bundle, entry.id)
		if _, err := repository.CreateBundle(ctx, controlplane.BundleEnvelope{Manifest: controlplane.BundleManifest{ID: entry.id}}); err != nil {
			t.Fatal(err)
		}
	}
	governance := controlplane.NewGovernanceService(repository)
	for _, person := range []string{"reader", "reviewer"} {
		if _, err := governance.PutTeam(ctx, controlplane.Team{Name: person, DisplayName: person, AdminUsers: []string{person}, MemberUsers: []string{person}}); err != nil {
			t.Fatal(err)
		}
		if _, err := governance.PutGrant(ctx, controlplane.ResourceGrant{Team: person, ResourceRID: "product/com.example:product-1", Operations: []string{controlplane.OperationReadResource}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutGrant(ctx, controlplane.ResourceGrant{Team: "reader", ResourceRID: "environment/a-one", Operations: []string{controlplane.OperationReadResource}}); err != nil {
		t.Fatal(err)
	}
	return spaceIndexFixture{handler, repository, store, first, second}
}

func spaceIndexBody(t *testing.T, handler http.Handler, subject, path string) (int, map[string]any) {
	t.Helper()
	response := spaceRequest(handler, subject, http.MethodGet, path, "")
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: %d %s: %v", path, response.Code, response.Body, err)
	}
	return response.Code, body
}
func requireSpaceIndex(t *testing.T, handler http.Handler, subject, path string) map[string]any {
	t.Helper()
	status, body := spaceIndexBody(t, handler, subject, path)
	if status != http.StatusOK {
		t.Fatalf("%s as %s: %d %#v", path, subject, status, body)
	}
	return body
}

var selectedIndexes = []struct {
	path, key string
	expected  map[string]bool
}{
	{"/v1/environments?view=summary", "environments", map[string]bool{"a-one": true, "c-three": true}},
	{"/v1/products?view=summary", "products", map[string]bool{"com.example:product-1": true, "com.example:product-3": true}},
	{"/v1/plans?view=summary", "plans", map[string]bool{"plan-a-one": true, "plan-c-three": true}},
	{"/v1/fleet/installations?view=summary", "installations", map[string]bool{"install-a-one": true, "install-c-three": true}},
	{"/v1/change-requests?view=summary", "changeRequests", map[string]bool{"change-a-one": true, "change-c-three": true}},
	{"/v1/promotions?view=summary", "promotions", map[string]bool{"promotion-b-two": true, "promotion-d-four": true}},
}

func spaceRowID(key string, row map[string]any) string {
	switch key {
	case "environments":
		return row["environment"].(map[string]any)["id"].(string)
	case "products":
		return row["product"].(string)
	case "installations":
		return row["installationId"].(string)
	default:
		return row["id"].(string)
	}
}
func checkSelectedIndexes(t *testing.T, f spaceIndexFixture) {
	t.Helper()
	for _, index := range selectedIndexes {
		t.Run(index.key, func(t *testing.T) {
			base := index.path + "&spaceId=" + f.first + "&limit=1"
			seen := map[string]bool{}
			cursor := ""
			for page := 0; page < 2; page++ {
				before := f.store.reads.Load()
				body := requireSpaceIndex(t, f.handler, "admin", base+"&cursor="+url.QueryEscape(cursor))
				if f.store.reads.Load()-before != 1 {
					t.Fatal("membership was not loaded once")
				}
				total := body["total"]
				if index.key == "installations" {
					total = body["summary"].(map[string]any)["total"]
				}
				rows := body[index.key].([]any)
				if total != float64(2) || len(rows) != 1 {
					t.Fatalf("wrong pre-page membership count: %#v", body)
				}
				id := spaceRowID(index.key, rows[0].(map[string]any))
				if !index.expected[id] || seen[id] {
					t.Fatalf("wrong or repeated Space member %q: %#v", id, body)
				}
				seen[id] = true
				next, _ := body["nextCursor"].(string)
				if page == 0 {
					if next == "" {
						t.Fatal("missing continuation")
					}
					for _, other := range []string{strings.ReplaceAll(base, f.first, f.second), index.path + "&limit=1"} {
						status, _ := spaceIndexBody(t, f.handler, "admin", other+"&cursor="+url.QueryEscape(next))
						if status != http.StatusUnprocessableEntity {
							t.Fatalf("cross-Space cursor accepted: %d", status)
						}
					}
				} else if next != "" {
					t.Fatal("continued past complete Space roster")
				}
				cursor = next
			}
			// No out-of-Space matches may survive search or contribute to totals.
			body := requireSpaceIndex(t, f.handler, "admin", base+"&search=does-not-exist")
			if len(body[index.key].([]any)) != 0 {
				t.Fatalf("search was not intersected: %#v", body)
			}
		})
	}
	fleet := requireSpaceIndex(t, f.handler, "admin", "/v1/fleet/installations?spaceId="+f.first+"&environment=a-one")
	facets := fleet["facets"].(map[string]any)["environment"].(map[string]any)
	if len(facets) != 2 || facets["a-one"] != float64(1) || facets["c-three"] != float64(1) {
		t.Fatalf("Space facets included other tenants or lost near-misses: %#v", fleet)
	}
	body := requireSpaceIndex(t, f.handler, "admin", "/v1/bundles?spaceId="+f.first)
	rows := body["bundles"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["id"] != "bundle-a" {
		t.Fatalf("Bundle grouping inferred from other anchors: %#v", body)
	}
	for _, index := range selectedIndexes {
		body := requireSpaceIndex(t, f.handler, "admin", index.path+"&limit=1")
		cursor, _ := body["nextCursor"].(string)
		if cursor == "" {
			t.Fatal("unscoped index lost continuation")
		}
		// Pinned pre-Space fingerprints: omitted selection preserves old cursors.
		legacy := map[string]string{
			"environments":   "0f86d804632f8819e5c5f68f594e46bf78c3bef632a713743525e8d12ba351fe",
			"products":       "7d435bb517f77454e55c5400f7fa3756fe0d5316c4608bfda2e7c877eceb4750",
			"plans":          "bce34b7a7df0264567785e1caf1b69de8233860d6fd611e6512e3384c8955e54",
			"changeRequests": "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a",
			"promotions":     "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a",
			"installations":  "7318e2e4efe357767a9d73b7d566e5ee2b8c188fc5c2753c5ac6ebd5bde73e15",
		}
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			t.Fatal(err)
		}
		var stamp struct {
			Filter string `json:"f"`
		}
		if err = json.Unmarshal(raw, &stamp); err != nil || stamp.Filter != legacy[index.key] {
			t.Fatalf("unscoped cursor changed: %s %s: %v", index.key, raw, err)
		}
		status, _ := spaceIndexBody(t, f.handler, "admin", index.path+"&spaceId="+f.first+"&limit=1&cursor="+url.QueryEscape(cursor))
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("unscoped cursor entered selected Space: %d", status)
		}
	}
}
func TestSpaceIndexesFilterCompleteInputsBeforePagesAndCounts(t *testing.T) {
	checkSelectedIndexes(t, newSpaceIndexFixture(t, controlplane.NewMemoryRepository(), spacememory.New()))
}
func TestPostgresSpaceIndexesUsePersistedMembership(t *testing.T) {
	dsn := os.Getenv(pgtest.DataSourceNameEnvironment)
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is required")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	private, drop, err := pgtest.Fresh(t.Context(), database, dsn, "space_indexes")
	if err != nil {
		t.Fatal(err)
	}
	defer drop()
	repository, err := controlplane.NewPostgresRepository(t.Context(), private)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	store, err := spacepostgres.New(t.Context(), private)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	fixture := newSpaceIndexFixture(t, repository, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := spacepostgres.New(t.Context(), private)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	fixture.store.Store = reopened
	checkSelectedIndexes(t, fixture)
}
func TestSpaceIndexesPreserveRecordAuthorityAndExactSelection(t *testing.T) {
	f := newSpaceIndexFixture(t, controlplane.NewMemoryRepository(), spacememory.New())
	for _, entry := range []struct{ path, key string }{{"/v1/plans?view=summary", "plans"}, {"/v1/change-requests?view=full", "changeRequests"}} {
		body := requireSpaceIndex(t, f.handler, "reviewer", entry.path+"&spaceId="+f.first)
		if len(body[entry.key].([]any)) != 2 {
			t.Fatalf("review authority became Environment-read authority: %#v", body)
		}
		body = requireSpaceIndex(t, f.handler, "reader", entry.path+"&spaceId="+f.first)
		if len(body[entry.key].([]any)) != 1 {
			t.Fatalf("Space visibility granted record authority: %#v", body)
		}
	}
	for _, path := range []string{"/v1/environments/b-two", "/v1/environments/b-two/entities", "/v1/plans/plan-b-two", "/v1/change-requests/change-b-two", "/v1/products/com.example:product-0/releases", "/v1/products/com.example:product-0/releases/1.0.0", "/v1/promotions/promotion-a-one", "/v1/bundles/bundle-b"} {
		status, _ := spaceIndexBody(t, f.handler, "admin", path+"?spaceId="+f.first)
		if status != http.StatusNotFound {
			t.Fatalf("linked selection escaped Space: %s: %d", path, status)
		}
		requireSpaceIndex(t, f.handler, "admin", path)
	}
	for _, path := range []string{"/v1/plans/plan-a-one", "/v1/change-requests/change-a-one"} {
		requireSpaceIndex(t, f.handler, "reviewer", path+"?spaceId="+f.first)
	}
	body := requireSpaceIndex(t, f.handler, "reader", "/v1/fleet/installations?spaceId="+f.first)
	if body["summary"].(map[string]any)["total"] != float64(1) {
		t.Fatalf("Fleet facets/counts widened authority: %#v", body)
	}
	body = requireSpaceIndex(t, f.handler, "reader", "/v1/products?spaceId="+f.first)
	if body["total"] != float64(1) {
		t.Fatalf("Product grouping granted other Products: %#v", body)
	}
}
func TestSpaceIndexesFailWithoutWideningTheSelection(t *testing.T) {
	f := newSpaceIndexFixture(t, controlplane.NewMemoryRepository(), spacememory.New())
	paths := []string{"/v1/environments", "/v1/products", "/v1/plans", "/v1/fleet/installations", "/v1/change-requests", "/v1/promotions", "/v1/bundles"}
	for _, path := range paths {
		for _, probe := range []struct {
			subject, query string
			status         int
		}{{"admin", "spaceId=invalid", 400}, {"admin", "spaceId=" + f.first + "&spaceId=" + f.second, 400}, {"admin", "spaceId=space_" + strings.Repeat("f", 32), 404}, {"stranger", "spaceId=" + f.first, 403}} {
			status, _ := spaceIndexBody(t, f.handler, probe.subject, path+"?"+probe.query)
			if status != probe.status {
				t.Fatalf("%s %s: %d want %d", path, probe.query, status, probe.status)
			}
		}
		f.store.fail = true
		status, body := spaceIndexBody(t, f.handler, "admin", path+"?spaceId="+f.first)
		f.store.fail = false
		if status != 500 || strings.Contains(fmt.Sprint(body), "private-membership") {
			t.Fatalf("membership failure became empty/sensitive success: %d %#v", status, body)
		}
	}
	empty := createNativeSpace(t, f.handler, "Empty")
	body := requireSpaceIndex(t, f.handler, "admin", "/v1/plans?spaceId="+empty.ID)
	if body["total"] != float64(0) {
		t.Fatalf("empty persisted Space: %#v", body)
	}
}
