package productmanifest

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestReconstructedDocument(t *testing.T) {
	f, err := os.Open("../../../docs/reference-platform/cli-fixtures/reconstructed/0.814.0/deployment/manifest.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if m.ProductGroup != "com.example.conformance" || m.ProductName != "conformance-fixture" || m.Extensions.HelmChart == nil || len(m.Extensions.Artifacts) != 1 {
		t.Fatalf("manifest = %#v", m)
	}
}

func TestExtensionPresenceAndUnknownNodesRoundTrip(t *testing.T) {
	m, err := Decode(strings.NewReader(`product-name: app
extensions:
  require-stable-hostname: false
  allow-traffic-when-not-ready: false
  volumes-v2:
    present:
      volume-type:
        durable-volume: {}
    absent:
      volume-type: {}
  vendor-feature:
    nested: [one, two]
`))
	if err != nil {
		t.Fatal(err)
	}
	check := func(m Manifest) {
		t.Helper()
		e := m.Extensions
		if e.RequireStableHostname == nil || *e.RequireStableHostname || e.AllowTrafficWhenNotReady == nil || *e.AllowTrafficWhenNotReady {
			t.Fatal("explicit false lost")
		}
		if e.VolumesV2["present"].VolumeType.DurableVolume == nil || e.VolumesV2["absent"].VolumeType.DurableVolume != nil {
			t.Fatal("volume presence lost")
		}
		n, ok := e.Other["vendor-feature"]
		if !ok || n.Kind != yaml.MappingNode || len(n.Content) != 2 || n.Content[1].Kind != yaml.SequenceNode || n.Content[1].Content[1].Value != "two" {
			t.Fatal("unknown extension changed")
		}
	}
	check(m)
	b, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Decode(strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	check(again)
	j, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"productName":"app"`, `"requireStableHostname":false`, `"durableVolume":{}`} {
		if !strings.Contains(string(j), key) {
			t.Fatalf("JSON missing %s: %s", key, j)
		}
	}
	if strings.Contains(string(j), "vendor-feature") {
		t.Fatal("unknown YAML extension leaked into JSON")
	}
}

func TestStrictBoundaryAndLegacyDiagnostics(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{"unknown: true\n", "field unknown not found in type controlplane.ApolloProductManifest"},
		{"extensions:\n  helm-chart:\n    unknown: true\n", "field unknown not found in type controlplane.ApolloManifestHelmChart"},
		{"[one, two]\n", "into controlplane.ApolloProductManifest"},
		{"extensions:\n  volumes-v2:\n    data:\n      volume-type:\n        durable-volume:\n          unknown: true\n", "field unknown not found in type controlplane.ApolloDurableVolume"},
	} {
		_, err := Decode(strings.NewReader(tt.input))
		var typed *yaml.TypeError
		if err == nil || !errors.As(err, &typed) || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("input %q: %v", tt.input, err)
		}
	}
	for _, s := range []string{"product-name: first\n---\nproduct-name: second\n", "product-name: first\n---\n[unterminated"} {
		if _, err := Decode(strings.NewReader(s)); err == nil {
			t.Fatalf("accepted trailing document %q", s)
		}
	}
	if _, err := Decode(strings.NewReader("")); !errors.Is(err, io.EOF) {
		t.Fatalf("empty input: %v", err)
	}
	sentinel := errors.New("reader failed")
	if _, err := Decode(errorReader{sentinel}); !errors.Is(err, sentinel) {
		t.Fatalf("reader error lost: %v", err)
	}
	if _, err := Decode(strings.NewReader("product-name: [unterminated")); err == nil {
		t.Fatal("malformed YAML accepted")
	}
	// Retain the existing LimitReader semantics, including no newly invented
	// oversized-document policy in this structural extraction.
	if _, err := Decode(strings.NewReader("#" + strings.Repeat("x", 1<<20))); !errors.Is(err, io.EOF) {
		t.Fatalf("size boundary changed: %v", err)
	}
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func TestDiagnosticMappingDoesNotRewriteUserData(t *testing.T) {
	for current, legacy := range legacyTypeNames {
		user := "line 1: field productmanifest." + current + " not found in type productmanifest." + current
		original := &yaml.TypeError{Errors: []string{user, "unrelated productmanifest." + current}}
		got := legacyDiagnostic(original).(*yaml.TypeError)
		want := []string{"line 1: field productmanifest." + current + " not found in type controlplane." + legacy, original.Errors[1]}
		if !reflect.DeepEqual(got.Errors, want) || original.Errors[0] != user {
			t.Fatalf("mapping %s: %#v", current, got.Errors)
		}
	}
}
