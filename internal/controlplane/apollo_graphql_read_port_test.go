package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The binder has neither an aggregate Repository nor any write capability.
type graphQLReadOnly struct {
	overviewReadOnly
	bundles func(context.Context, string) ([]BundleEnvelope, error)
}

func (r graphQLReadOnly) ListBundles(ctx context.Context, filter string) ([]BundleEnvelope, error) {
	return r.bundles(ctx, filter)
}

var _ graphQLProjectionReader = graphQLReadOnly{}

func TestGraphQLBinderAcceptsOnlyReadSnapshotsAndPreservesContext(t *testing.T) {
	type key struct{}
	calls := []string{}
	check := func(ctx context.Context, name, filter string) {
		t.Helper()
		if ctx.Value(key{}) != "request-context" || filter != "" {
			t.Fatalf("context/filter changed: %s %q", name, filter)
		}
		calls = append(calls, name)
	}
	reader := graphQLReadOnly{
		overviewReadOnly: overviewReadOnly{
			plans: func(ctx context.Context, f string) ([]PlanRecord, error) {
				check(ctx, "plans", f)
				return []PlanRecord{{ID: "failed", Plan: Plan{Environment: "dev"}, Status: PlanFailed}}, nil
			},
			change: func(context.Context, string) (ChangeRequest, error) {
				t.Fatal("unexpected ChangeRequest read")
				return ChangeRequest{}, nil
			},
			changes: func(context.Context, string) ([]ChangeRequest, error) {
				t.Fatal("unrequested panel read")
				return nil, nil
			},
			reports: func(context.Context) ([]EnvironmentReport, error) {
				t.Fatal("unrequested report read")
				return nil, nil
			},
			promotions: func(context.Context, string) ([]PromotionRecord, error) {
				t.Fatal("unrequested promotion read")
				return nil, nil
			},
		},
		bundles: func(ctx context.Context, f string) ([]BundleEnvelope, error) {
			check(ctx, "bundles", f)
			return []BundleEnvelope{{Manifest: BundleManifest{ID: "native-bundle"}}}, nil
		},
	}
	governance := NewGovernanceService(NewMemoryRepository())
	options := HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}, SpaceIDLookup: func(ctx context.Context, principal Principal, kind, id string) (string, error) {
		if ctx.Value(key{}) != "request-context" || principal.Username != "root" || kind != "BUNDLE" || id != "native-bundle" {
			t.Fatal("grouping authority/context changed")
		}
		return "native-space", nil
	}}
	mux := http.NewServeMux()
	if err := registerApolloGraphQL(mux, nil, reader, governance, options); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("native-bundle"))
	rid := fmt.Sprintf("ri.conduit.main.bundle.%x", digest[:12])
	query := `{ apollo { plans(pageSize:10) { plans { rid } } bundle(rid:"` + rid + `") { rid spaceId } } }`
	encoded, _ := json.Marshal(map[string]string{"query": query})
	// In insecure development mode an absent header resolves local-developer,
	// which is authenticated but has no bootstrap grant in this fixture.
	for _, actor := range []string{"", "outsider", "root"} {
		before := len(calls)
		request := httptest.NewRequest(http.MethodPost, "/v1/apollo/graphql", strings.NewReader(string(encoded)))
		request = request.WithContext(context.WithValue(request.Context(), key{}, "request-context"))
		if actor != "" {
			request.Header.Set("X-Platform-Subject", actor)
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if actor != "root" {
			want := http.StatusForbidden
			if response.Code != want || len(calls) != before {
				t.Fatalf("refusal read snapshots: %s %d %v", actor, response.Code, calls)
			}
			continue
		}
		var body struct {
			Data struct {
				Apollo struct {
					Plans struct {
						Plans []struct {
							RID string `json:"rid"`
						}
					} `json:"plans"`
					Bundle struct{ RID, SpaceID string } `json:"bundle"`
				} `json:"apollo"`
			} `json:"data"`
			Errors []json.RawMessage `json:"errors"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if response.Code != 200 || len(body.Errors) != 0 || len(body.Data.Apollo.Plans.Plans) != 1 || body.Data.Apollo.Bundle.RID != rid || body.Data.Apollo.Bundle.SpaceID != "native-space" {
			t.Fatalf("native snapshots lost: %d %s", response.Code, response.Body)
		}
		if len(calls) != before+2 {
			t.Fatalf("snapshot calls=%v", calls)
		}
	}
	options.AllowInsecureOperatorIdentityHeaders = false
	production := http.NewServeMux()
	if err := registerApolloGraphQL(production, nil, reader, governance, options); err != nil {
		t.Fatal(err)
	}
	before := len(calls)
	response := graphqlRequest(t, production, "root", query)
	if response.Code != http.StatusUnauthorized || len(calls) != before {
		t.Fatalf("production headers gained authority: %d reads=%v", response.Code, calls)
	}

}
