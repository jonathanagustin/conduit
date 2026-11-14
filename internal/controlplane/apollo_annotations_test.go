package controlplane

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

// TestApolloEntityIDGrammar holds the documented identifier format and its
// bound. A truncated identifier still looks like an Entity ID, so the bound is
// enforced by refusing rather than by cutting.
func TestApolloEntityIDGrammar(t *testing.T) {
	id, err := ApolloHelmChartEntityID("prod-eu", "catalog")
	if err != nil {
		t.Fatalf("entity ID: %v", err)
	}
	if id != "aeid:prod-eu:helm-chart:catalog" {
		t.Fatalf("entity ID = %q", id)
	}

	if _, err := ApolloEntityID("", "helm-chart", "catalog"); err == nil {
		t.Fatal("an empty Environment must be refused")
	}
	// A colon in a segment would make the identifier reparse into different
	// fields than it was built from.
	if _, err := ApolloEntityID("prod:eu", "helm-chart", "catalog"); err == nil {
		t.Fatal("a colon in a segment must be refused")
	}
	if _, err := ApolloHelmChartEntityID("prod-eu", strings.Repeat("n", ApolloEntityIDMaximumLength)); err == nil {
		t.Fatalf("an identifier longer than %d characters must be refused", ApolloEntityIDMaximumLength)
	}
}

// TestApolloWorkloadMetadata holds the full documented label and annotation set.
func TestApolloWorkloadMetadata(t *testing.T) {
	metadata, err := ApolloWorkloadMetadataFor("prod-eu", "catalog", "com.palantir:catalog", "1.2.3")
	if err != nil {
		t.Fatalf("workload metadata: %v", err)
	}
	// Presence is the signal, so the value must stay empty for a selector to
	// match it.
	value, exists := metadata.Labels[ApolloLivenessReadinessLabel]
	if !exists || value != "" {
		t.Fatalf("liveness/readiness label = %q, exists=%v", value, exists)
	}
	for key, want := range map[string]string{
		ApolloEnvironmentIDAnnotation:  "prod-eu",
		ApolloEntityIDAnnotation:       "aeid:prod-eu:helm-chart:catalog",
		ApolloProductGroupAnnotation:   "com.palantir",
		ApolloProductNameAnnotation:    "catalog",
		ApolloProductVersionAnnotation: "1.2.3",
	} {
		if got := metadata.Annotations[key]; got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}

	if _, err := ApolloWorkloadMetadataFor("prod-eu", "catalog", "not-a-coordinate", "1.2.3"); err == nil {
		t.Fatal("a product that is not a group:name coordinate must be refused")
	}
	if _, err := ApolloWorkloadMetadataFor("prod-eu", "catalog", "com.palantir:catalog", ""); err == nil {
		t.Fatal("a missing version must be refused")
	}
}

// TestApolloAnnotationKeysAreNamespaced guards the one property every consumer
// of these keys depends on: they are all in the documented namespace, spelled
// once here.
func TestApolloAnnotationKeysAreNamespaced(t *testing.T) {
	for _, key := range []string{
		ApolloLivenessReadinessLabel, ApolloHealthProbeAnnotation,
		ApolloEnvironmentIDAnnotation, ApolloEntityIDAnnotation,
		ApolloProductNameAnnotation, ApolloProductGroupAnnotation, ApolloProductVersionAnnotation,
	} {
		if !strings.HasPrefix(key, "apollo.palantir.com/") {
			t.Fatalf("key %q is outside the documented namespace", key)
		}
	}
}

// A YAML mapping node holds key and value as ALTERNATING children, so the loop must stop with a
// whole pair left. A malformed node with an odd Content length -- a key whose value is missing --
// reads one past the end on the last step. A mutation sweep found the boundary unguarded: nothing
// exercised an odd-length mapping.
func TestApolloMapValueStopsOnAnIncompletePair(t *testing.T) {
	t.Parallel()
	scalar := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: v} }

	t.Run("a trailing key with no value is ignored, not read past", func(t *testing.T) {
		mapping := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			scalar("name"), scalar("api"),
			scalar("orphan"), // odd: no value follows
		}}
		if got := apolloMapValue(mapping, "name"); got == nil || got.Value != "api" {
			t.Fatalf("complete pair not found: %#v", got)
		}
		if got := apolloMapValue(mapping, "orphan"); got != nil {
			t.Errorf("an incomplete pair returned a value: %#v", got)
		}
	})

	t.Run("a non-mapping or nil node yields nothing", func(t *testing.T) {
		if apolloMapValue(nil, "name") != nil {
			t.Error("nil mapping returned a value")
		}
		if apolloMapValue(&yaml.Node{Kind: yaml.SequenceNode}, "name") != nil {
			t.Error("a sequence was treated as a mapping")
		}
	})
}
