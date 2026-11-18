package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestApolloGraphQLNativeGroupingUsesBoundIdentityAndRESTAuthority(t *testing.T) {
	repository := NewMemoryRepository()
	ctx := context.Background()
	if _, err := repository.CreateBundle(ctx, BundleEnvelope{Manifest: BundleManifest{ID: "grouping-fixture"}}); err != nil {
		t.Fatal(err)
	}
	governance := NewGovernanceService(repository)
	for _, subject := range []string{"root-reader", "product-reader"} {
		if _, err := governance.PutTeam(ctx, Team{Name: subject, DisplayName: subject, AdminUsers: []string{subject}, MemberUsers: []string{subject}}); err != nil {
			t.Fatal(err)
		}
		resource := "platform/root"
		if subject == "product-reader" {
			resource = "global/product"
		}
		if _, err := governance.PutGrant(ctx, ResourceGrant{ResourceRID: resource, Team: subject, Operations: []string{OperationReadResource}}); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, SpaceIDLookup: func(_ context.Context, principal Principal, kind, id string) (string, error) {
		calls++
		if principal.Username != "root-reader" || kind != "BUNDLE" || id != "grouping-fixture" {
			t.Fatalf("wrong native grouping binding: %s %s %s", principal.Username, kind, id)
		}
		return "space-from-native-store", nil
	}})
	digest := sha256.Sum256([]byte("grouping-fixture"))
	rid := fmt.Sprintf("ri.conduit.main.bundle.%x", digest[:12])
	query := `{ apollo { bundle(rid:"` + rid + `") { rid spaceId } } }`
	for _, subject := range []string{"root-reader", "product-reader"} {
		rest := apolloRESTGet(t, handler, subject, "/v1/bundles")
		response := graphqlRequest(t, handler, subject, query)
		if response.Code != http.StatusOK {
			t.Fatalf("GraphQL transport %s: %d %s", subject, response.Code, response.Body.String())
		}
		var got struct {
			Data struct {
				Apollo struct {
					Bundle *struct {
						RID     string `json:"rid"`
						SpaceID string `json:"spaceId"`
					} `json:"bundle"`
				} `json:"apollo"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if subject == "root-reader" {
			if rest.Code != http.StatusOK || len(got.Errors) != 0 || got.Data.Apollo.Bundle == nil || got.Data.Apollo.Bundle.RID != rid || got.Data.Apollo.Bundle.SpaceID != "space-from-native-store" || calls != 1 {
				t.Fatalf("authorized binding differs from REST: rest=%d response=%s calls=%d", rest.Code, response.Body.String(), calls)
			}
		} else if rest.Code != http.StatusForbidden || len(got.Errors) == 0 || got.Data.Apollo.Bundle != nil || calls != 1 || strings.Contains(response.Body.String(), "space-from-native-store") {
			t.Fatalf("root refusal became absence/disclosure: rest=%d response=%s calls=%d", rest.Code, response.Body.String(), calls)
		}
	}
}
