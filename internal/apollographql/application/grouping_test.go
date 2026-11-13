package application

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type fakeGrouping struct {
	ids         []string
	assignments map[string]string
	err         error
}

func (source *fakeGrouping) SpaceID(_ context.Context, kind, id string) (string, error) {
	return source.assignments[kind+":"+id], source.err
}
func (source *fakeGrouping) BundleIDs(context.Context) ([]string, error) {
	return source.ids, source.err
}

func TestNativeGroupingUsesEachPersistedAnchorAndDisclosesValues(t *testing.T) {
	resolver, _, _ := newFixture(t)
	resolver.sources.Grouping = &fakeGrouping{ids: []string{"bundle-1"}, assignments: map[string]string{"ENVIRONMENT:prod": "space-environment", "PRODUCT:checkout": "space-product", "BUNDLE:bundle-1": "space-bundle"}}
	response := resolver.Execute(t.Context(), Request{Query: `{ apollo { environments(pageSize: 10) { environments { spaceId entities(pageSize: 10) { entities { spaceId } } } } products(pageSize:10) { products { spaceId productReleases(pageSize:10) { productReleases { spaceId } } } } bundle(rid:"` + bundleRID("bundle-1") + `") { rid spaceId } bundles(rids:"` + bundleRID("bundle-1") + `") { spaceId } } }`})
	if len(response.Errors) != 0 {
		t.Fatalf("native grouping query failed: %v", response.Errors)
	}
	// Wire-level comparisons and independently chosen anchors are added with the
	// Hub integration; this test specifically constrains the resolver's joins.
	minted := response.Extensions["provenance"].(map[string]any)["conduitMintedValues"].([]string)
	for _, field := range []string{"ApolloEnvironment.spaceId", "ApolloEntity.spaceId", "ApolloProduct.spaceId", "ApolloProductRelease.spaceId", "ApolloBundle.spaceId", "ApolloBundle.rid"} {
		if !slices.Contains(minted, field) {
			t.Fatalf("native value lacks disclosure: %s in %v", field, minted)
		}
	}
	root := response.Data["apollo"].(map[string]any)
	bundle := root["bundle"].(map[string]any)
	if bundle["spaceId"] != "space-bundle" || bundle["rid"] != bundleRID("bundle-1") {
		t.Fatalf("Bundle source differs: %#v", bundle)
	}
	if root["bundles"].(map[string]any)["spaceId"] != "space-bundle" {
		t.Fatal("recovered scalar bundles did not resolve")
	}
	environments := root["environments"].(map[string]any)["environments"].([]any)
	environment := environments[0].(map[string]any)
	if environment["spaceId"] != "space-environment" {
		t.Fatalf("Environment join: %#v", environment)
	}
	entity := environment["entities"].(map[string]any)["entities"].([]any)[0].(map[string]any)
	if entity["spaceId"] != "space-environment" {
		t.Fatalf("Entity owning Environment join: %#v", entity)
	}
	product := root["products"].(map[string]any)["products"].([]any)[0].(map[string]any)
	if product["spaceId"] != "space-product" {
		t.Fatalf("Product join: %#v", product)
	}
	release := product["productReleases"].(map[string]any)["productReleases"].([]any)[0].(map[string]any)
	if release["spaceId"] != "space-product" {
		t.Fatalf("Release owning Product join: %#v", release)
	}
}
func TestNativeGroupingSeparatesUnassignedUnavailableAndDenied(t *testing.T) {
	resolver, _, _ := newFixture(t)
	query := Request{Query: `{ apollo { environment(rid:"ri.apollo.main.environment.a") { spaceId } } }`}
	resolver.sources.Grouping = &fakeGrouping{}
	missing := resolver.Execute(t.Context(), query)
	if len(missing.Errors) != 0 {
		t.Fatalf("observed absence failed: %v", missing.Errors)
	}

	environment := missing.Data["apollo"].(map[string]any)["environment"].(map[string]any)
	if value, present := environment["spaceId"]; !present || value != nil {
		t.Fatalf("observed no assignment must be explicit null: %#v", environment)
	}
	if !slices.Contains(missing.Extensions["provenance"].(map[string]any)["conduitMintedValues"].([]string), "ApolloEnvironment.spaceId") {
		t.Fatal("observed absence was not disclosed as a native answer")
	}
	for _, source := range []*fakeGrouping{nil, {err: errors.New("storage unavailable")}, {err: errors.New("read forbidden")}} {
		resolver.sources.Grouping = source
		// A typed nil is not an unconfigured port; supply an actual nil interface.
		if source == nil {
			resolver.sources.Grouping = nil
		}
		response := resolver.Execute(t.Context(), query)
		if len(response.Errors) == 0 {
			t.Fatal("unknown or denied grouping became observed absence")
		}
		if slices.Contains(response.Extensions["provenance"].(map[string]any)["conduitMintedValues"].([]string), "ApolloEnvironment.spaceId") {
			t.Fatal("failed lookup counted as a served native value")
		}
		bundle := resolver.Execute(t.Context(), Request{Query: `{ apollo { bundle(rid:"` + bundleRID("bundle-1") + `") { rid spaceId } } }`})
		if len(bundle.Errors) == 0 {
			t.Fatal("unavailable or denied Bundle catalogue became observed absence")
		}

	}
}
