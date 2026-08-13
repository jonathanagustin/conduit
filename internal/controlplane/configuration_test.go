package controlplane

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestApolloAlignedConfigSelectsMostSpecificRangePreprocessesAndInjectsValues(t *testing.T) {
	state := foundation()
	state.Environment.Config = map[string]any{
		"domainName": "dev.example.test", "numConnectionsAllowed": float64(5),
		"nested": map[string]any{"enabled": true},
	}
	for index := range state.Entities {
		if state.Entities[index].Name != "example-api" {
			continue
		}
		state.Entities[index].HelmValues = json.RawMessage(`{"existing":{"keep":true},"apollo":{"spoofed":true}}`)
		state.Entities[index].ConfigOverrides = `
0.0.0:
  overrides:
    feature: old
0.1.0:
  overrides:
    feature: current
    replicas: {{ preprocess .Values.apollo.environment.numConnectionsAllowed }}
    nestedEnabled: {{ preprocess .Values.apollo.environment.nested.enabled }}
`
	}
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	for _, step := range plan.Steps {
		if step.Entity == "example-api" {
			if err := json.Unmarshal(step.Values, &values); err != nil {
				t.Fatal(err)
			}
		}
	}
	if values["feature"] != "current" || values["replicas"] != float64(5) || values["nestedEnabled"] != true {
		t.Fatalf("resolved overrides = %#v", values)
	}
	apollo, ok := values["apollo"].(map[string]any)
	if !ok || apollo["spoofed"] != nil {
		t.Fatalf("authoritative Apollo block = %#v", values["apollo"])
	}
	product := apollo["product"].(map[string]any)
	environment := apollo["environment"].(map[string]any)
	if product["group"] != "app.example" || product["name"] != "example-api" || product["version"] != "0.1.0" ||
		environment["id"] != "dev-use2" || environment["domainName"] != "dev.example.test" {
		t.Fatalf("injected values = %#v", apollo)
	}
}

func TestApolloConfigRangeIsMajorBoundedAndRequiredWhenConfigured(t *testing.T) {
	blocks, err := decodeVersionedConfigOverrides("1.2.0:\n  overrides: {}\n2.0.0:\n  overrides:\n    enabled: true\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, found := selectVersionedConfigOverride(blocks, "1.1.9"); found {
		t.Fatal("selected a range below its starting version")
	}
	if selected, found := selectVersionedConfigOverride(blocks, "2.7.1"); !found || selected["enabled"] != true {
		t.Fatalf("selected=%#v found=%v", selected, found)
	}
	if _, found := selectVersionedConfigOverride(blocks, "3.0.0"); found {
		t.Fatal("crossed a major-version boundary")
	}

	state := foundation()
	state.Entities[0].ConfigOverrides = "1.0.0:\n  overrides: {}\n"
	if _, err := BuildPlan(state); err == nil || !strings.Contains(err.Error(), "no config override declares support") {
		t.Fatalf("missing support error = %v", err)
	}
}

func TestApolloConfigValidationRejectsInvalidKeysAndDocuments(t *testing.T) {
	state := foundation()
	state.Environment.Config = map[string]any{"invalid-key": true}
	state.Entities[0].ConfigOverrides = "0.1.0:\n  unknown: {}\n---\n0.2.0:\n  overrides: {}\n"
	errs := Validate(state)
	if !hasValidationPath(errs, "environment.config") || !hasValidationPath(errs, "entities[0].configOverrides") {
		t.Fatalf("validation errors = %#v", errs)
	}
}

func hasValidationPath(errs []ValidationError, path string) bool {
	for _, err := range errs {
		if err.Path == path {
			return true
		}
	}
	return false
}

// A string can carry more than one hostname template, and each has to be
// resolved. Finding only the first leaves a literal "{{ default_hostname }}" in
// a value that reaches a chart.
func TestResolveTemplateStringResolvesEveryTemplateInAValue(t *testing.T) {
	t.Parallel()
	namespace := NamespaceTarget{
		RID: "ri.namespace.main.namespace.one",
		Organizations: []Organization{
			{DisplayName: "Alpha", PrimaryHostname: "alpha.example"},
			{DisplayName: "Beta", PrimaryHostname: "beta.example"},
		},
	}
	resolved, err := resolveTemplateString(
		`https://{{ default_hostname(organizationName: "Alpha") }}/from/{{ default_hostname(organizationName: "Beta") }}`,
		&namespace,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "https://alpha.example/from/beta.example" {
		t.Errorf("resolved to %q", resolved)
	}
	if strings.Contains(resolved, "{{") {
		t.Errorf("a template survived resolution: %q", resolved)
	}

	// Nothing to resolve is not an error, and needs no namespace at all.
	if got, err := resolveTemplateString("https://literal.example", nil); err != nil || got != "https://literal.example" {
		t.Errorf("a value with no template = %q, %v", got, err)
	}
	// A template with no namespace to resolve against is refused rather than
	// emitted literally.
	if _, err := resolveTemplateString("https://{{ default_hostname }}", nil); err == nil {
		t.Error("a template with no namespace metadata resolved anyway")
	}
}

// Which versioned override block applies to a target version. The blocks are
// keyed by the version they start at, so the answer is the highest start that is
// not above the target and shares its major.
func TestVersionedConfigOverrideSelectsTheHighestApplicableStart(t *testing.T) {
	t.Parallel()
	blocks := map[string]versionedConfigBlock{
		"1.0.0": {Overrides: map[string]any{"which": "1.0.0"}},
		"1.2.0": {Overrides: map[string]any{"which": "1.2.0"}},
		"1.5.0": {Overrides: map[string]any{"which": "1.5.0"}},
		"2.0.0": {Overrides: map[string]any{"which": "2.0.0"}},
	}
	for _, testCase := range []struct {
		target string
		want   any
		found  bool
	}{
		{"1.4.0", "1.2.0", true},
		{"1.5.0", "1.5.0", true},
		{"1.9.9", "1.5.0", true},
		{"1.0.0", "1.0.0", true},
		{"2.1.0", "2.0.0", true},
		// Below every start in its major, and a major with no blocks at all.
		{"0.9.0", nil, false},
		{"3.0.0", nil, false},
	} {
		// The blocks are a map, so the loop that picks between them sees them in a
		// different order every run. Repeating each case is what turns "picked the
		// right one" into "picks the right one regardless of order" -- a selection
		// that depends on iteration order is wrong even when it happens to be right.
		for attempt := 0; attempt < 32; attempt++ {
			overrides, found := selectVersionedConfigOverride(blocks, testCase.target)
			if found != testCase.found {
				t.Errorf("target %s found = %v, wanted %v", testCase.target, found, testCase.found)
				break
			}
			if found && overrides["which"] != testCase.want {
				t.Errorf("target %s selected %v, wanted %v", testCase.target, overrides["which"], testCase.want)
				break
			}
		}
	}
}

// The compatibility values are the Product chart's public boundary. An empty
// list is not the same as an absent key there: emitting "imagePullSecrets: []"
// or "secrets: {}" hands the chart an explicit empty value to act on, where
// omitting the key leaves the chart's own default in place.
func TestDeploymentValuesOmitEmptySecretCollectionsRatherThanEmittingThem(t *testing.T) {
	t.Parallel()
	decode := func(t *testing.T, raw json.RawMessage) map[string]any {
		t.Helper()
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		compatibility, ok := decoded["apollo"].(map[string]any)
		if !ok {
			t.Fatalf("no apollo compatibility block in %s", raw)
		}
		return compatibility
	}

	// A registry with no pull secret and no configured secrets: neither key
	// should appear, but the block itself still must because the registry does.
	bare, err := resolveDeploymentHelmValues(Environment{
		ID: "dev-use2", ArtifactRegistries: []ArtifactRegistryAssignment{{ID: "primary", URL: "registry.example"}},
	}, Entity{Name: "api", Product: "app.example:api", Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	compatibility := decode(t, bare)
	if _, present := compatibility["imagePullSecrets"]; present {
		t.Errorf("an empty pull-secret list was emitted: %#v", compatibility)
	}
	if _, present := compatibility["secrets"]; present {
		t.Errorf("an empty secret map was emitted: %#v", compatibility)
	}
	if compatibility["defaultArtifactStoreURI"] != "registry.example" {
		t.Errorf("the registry URI was not emitted: %#v", compatibility)
	}

	// And when there is something to say, it is said.
	populated, err := resolveDeploymentHelmValues(Environment{
		ID: "dev-use2",
		ArtifactRegistries: []ArtifactRegistryAssignment{
			{ID: "primary", URL: "registry.example", ImagePullSecretName: "registry-pull"},
		},
		Secrets: []ExternalSecretAssignment{{Name: "database", KubernetesSecretName: "database-secret"}},
	}, Entity{Name: "api", Product: "app.example:api", Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	compatibility = decode(t, populated)
	pullSecrets, ok := compatibility["imagePullSecrets"].([]any)
	if !ok || len(pullSecrets) != 1 {
		t.Errorf("pull secrets = %#v", compatibility["imagePullSecrets"])
	}
	secrets, ok := compatibility["secrets"].(map[string]any)
	if !ok || len(secrets) != 1 {
		t.Errorf("secrets = %#v", compatibility["secrets"])
	}
}
