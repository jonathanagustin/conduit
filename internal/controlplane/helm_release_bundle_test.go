package controlplane

import (
	"bytes"
	"strings"
	"testing"
)

func exactHelmBundle() HelmProductReleaseBundle {
	digest := "sha256:" + strings.Repeat("a", 64)
	return HelmProductReleaseBundle{
		Manifest: ApolloProductManifest{
			ManifestVersion: "1.0", ProductGroup: "com.example", ProductName: "application",
			ProductVersion: "2.0.0", ProductType: "helm-chart.v1",
			Extensions: ApolloProductManifestExtensions{
				HelmChart: &ApolloManifestHelmChart{Name: "application", Version: "7.4.1", RepositoryURL: "oci://registry.example/charts/application:7.4.1@" + digest},
				Artifacts: []ApolloManifestArtifact{{Type: "oci", URI: "registry.example/application:2.0.0@" + digest}},
			},
		},
		DefaultConfig: []byte("replicaCount: 3\n"),
		ConfigSchema:  []byte(`{"type":"object","properties":{"replicaCount":{"type":"integer"}}}`),
	}
}

func TestValidateHelmProductReleaseBundleBoundsAndDocuments(t *testing.T) {
	t.Parallel()
	bundle := exactHelmBundle()
	if err := ValidateHelmProductReleaseBundle(bundle); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*HelmProductReleaseBundle)
		want   string
	}{
		{"multiple YAML documents", func(bundle *HelmProductReleaseBundle) { bundle.DefaultConfig = []byte("one: 1\n---\ntwo: 2\n") }, "exactly one YAML"},
		{"invalid schema", func(bundle *HelmProductReleaseBundle) { bundle.ConfigSchema = []byte(`{"type":`) }, "decode Helm configuration schema"},
		{"multiple JSON values", func(bundle *HelmProductReleaseBundle) { bundle.ConfigSchema = []byte(`{} {}`) }, "exactly one JSON"},
		{"oversized values", func(bundle *HelmProductReleaseBundle) {
			bundle.DefaultConfig = bytes.Repeat([]byte("x"), MaxHelmDefaultConfigBytes+1)
		}, "exceeds"},
		{"oversized schema", func(bundle *HelmProductReleaseBundle) {
			bundle.ConfigSchema = bytes.Repeat([]byte("x"), MaxHelmConfigSchemaBytes+1)
		}, "exceeds"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := exactHelmBundle()
			test.mutate(&candidate)
			if err := ValidateHelmProductReleaseBundle(candidate); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v, want %q", err, test.want)
			}
		})
	}
}
