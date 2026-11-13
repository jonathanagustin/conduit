package composition

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	spacememory "github.com/jonathanagustin/conduit/internal/spaces/adapters/memory"
	spaces "github.com/jonathanagustin/conduit/internal/spaces/domain"
	spaceports "github.com/jonathanagustin/conduit/internal/spaces/ports"
)

type failedSpaceLookup struct{ spaceports.Store }

func (failedSpaceLookup) GetBinding(context.Context, spaces.Resource) (spaces.Binding, error) {
	return spaces.Binding{}, errors.New("private-space-provider-diagnostic")
}

func TestSpaceGraphQLReadsActualAssignmentsWithCanonicalAuthority(t *testing.T) {
	ctx := t.Context()
	repository := controlplane.NewMemoryRepository()
	store := spacememory.New()
	options := controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}}
	handler, err := NewHub(repository, options, Options{Spaces: store})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../../examples/example-foundation.json")
	if err != nil {
		t.Fatal(err)
	}
	var desired controlplane.DesiredState
	if err := json.Unmarshal(raw, &desired); err != nil {
		t.Fatal(err)
	}
	if _, err := controlplane.NewLifecycle(repository).CreatePlan(ctx, desired); err != nil {
		t.Fatal(err)
	}
	published := spaceRequest(handler, "admin", http.MethodPost, "/api/v0/products/service/com.example:app:1.0.0/publish", `{"extensions":{}}`)
	if published.Code != http.StatusOK {
		t.Fatalf("fixture publication: %d %s", published.Code, published.Body)
	}
	const bundleID = "bnd_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	// Only identity is read; the test does not construct or claim a signed delivery artifact.
	if _, err := repository.CreateBundle(ctx, controlplane.BundleEnvelope{Manifest: controlplane.BundleManifest{ID: bundleID}}); err != nil {
		t.Fatal(err)
	}
	environmentSpace := createNativeSpace(t, handler, "Environment grouping")
	productSpace := createNativeSpace(t, handler, "Product grouping")
	bundleSpace := createNativeSpace(t, handler, "Bundle grouping")
	for _, binding := range []struct{ space, kind, id string }{{environmentSpace.ID, spaces.Environment, desired.Environment.ID}, {productSpace.ID, spaces.Product, "com.example:app"}, {bundleSpace.ID, spaces.Bundle, bundleID}} {
		body, _ := json.Marshal(spaces.Resource{Kind: binding.kind, ID: binding.id})
		response := spaceRequest(handler, "admin", http.MethodPut, "/v1/spaces/"+binding.space+"/bindings", string(body))
		if response.Code != http.StatusOK {
			t.Fatalf("bind fixture: %d %s", response.Code, response.Body)
		}
	}
	query := `{ apollo { environments(pageSize:10) { environments { name spaceId entities(pageSize:10) { entities { spaceId } } } } products(pageSize:10) { products { productId spaceId productReleases(pageSize:10) { productReleases { spaceId } } } } bundle(rid:"ri.conduit.main.bundle.d5eb52c46a98717e4496e515") { rid spaceId } } }`
	body, _ := json.Marshal(map[string]string{"query": query})
	response := spaceRequest(handler, "admin", http.MethodPost, "/v1/apollo/graphql", string(body))
	var result struct {
		Data struct {
			Apollo struct {
				Environments struct {
					Environments []struct {
						Name, SpaceID string
						Entities      struct{ Entities []struct{ SpaceID string } }
					}
				}
				Products struct {
					Products []struct {
						ProductID, SpaceID string
						ProductReleases    struct{ ProductReleases []struct{ SpaceID string } }
					}
				}
				Bundle struct{ RID, SpaceID string }
			}
		}
		Errors     []any
		Extensions struct {
			Provenance struct{ ConduitMintedValues []string }
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(result.Errors) != 0 {
		t.Fatalf("native grouping query: %d %s", response.Code, response.Body)
	}
	environments := result.Data.Apollo.Environments.Environments
	if len(environments) != 1 || environments[0].Name != desired.Environment.ID || environments[0].SpaceID != environmentSpace.ID || len(environments[0].Entities.Entities) == 0 {
		t.Fatalf("Environment grouping: %s", response.Body)
	}
	for _, entity := range environments[0].Entities.Entities {
		if entity.SpaceID != environmentSpace.ID {
			t.Fatalf("Entity grouping: %s", response.Body)
		}
	}
	found := false
	for _, product := range result.Data.Apollo.Products.Products {
		if product.ProductID == "com.example:app" {
			found = true
			if product.SpaceID != productSpace.ID || len(product.ProductReleases.ProductReleases) != 1 || product.ProductReleases.ProductReleases[0].SpaceID != productSpace.ID {
				t.Fatalf("Product/Release grouping: %s", response.Body)
			}
		}
	}
	if !found || result.Data.Apollo.Bundle.SpaceID != bundleSpace.ID {
		t.Fatalf("Product or Bundle missing: %s", response.Body)
	}
	for _, field := range []string{"ApolloEnvironment.spaceId", "ApolloEntity.spaceId", "ApolloProduct.spaceId", "ApolloProductRelease.spaceId", "ApolloBundle.spaceId", "ApolloBundle.rid"} {
		if !strings.Contains(strings.Join(result.Extensions.Provenance.ConduitMintedValues, ","), field) {
			t.Fatalf("missing disclosure %s: %s", field, response.Body)
		}
	}
	governance := controlplane.NewGovernanceService(repository)
	if _, err := governance.PutTeam(ctx, controlplane.Team{Name: "catalogue", DisplayName: "Catalogue", AdminUsers: []string{"reader"}, MemberUsers: []string{"reader"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(ctx, controlplane.ResourceGrant{ResourceRID: "global/product", Team: "catalogue", Operations: []string{controlplane.OperationReadResource}}); err != nil {
		t.Fatal(err)
	}
	response = spaceRequest(handler, "reader", http.MethodPost, "/v1/apollo/graphql", string(body))
	if !strings.Contains(response.Body.String(), productSpace.ID) || strings.Contains(response.Body.String(), environmentSpace.ID) || strings.Contains(response.Body.String(), bundleSpace.ID) {
		t.Fatalf("GraphQL crossed resource authority: %s", response.Body)
	}
	broken, err := NewHub(repository, options, Options{Spaces: failedSpaceLookup{store}})
	if err != nil {
		t.Fatal(err)
	}
	response = spaceRequest(broken, "admin", http.MethodPost, "/v1/apollo/graphql", string(body))
	if !strings.Contains(response.Body.String(), `"errors"`) || strings.Contains(response.Body.String(), "private-space-provider-diagnostic") {
		t.Fatalf("storage failure became absence or disclosed diagnostics: %s", response.Body)
	}
}
