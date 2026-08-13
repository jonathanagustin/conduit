package conduitcli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readDefinitionTemplates returns nil rather than an empty slice when the
// caller has not said what was packaged, and the validator keys off that
// difference: nil skips the template check, empty reports every tmpl entry as
// missing a file. Both spellings look like "no templates" at a glance.
func TestReadDefinitionTemplatesDistinguishesUnknownFromEmpty(t *testing.T) {
	t.Parallel()

	withTemplates := func(t *testing.T, names ...string) string {
		t.Helper()
		root := t.TempDir()
		templates := filepath.Join(root, "templates")
		if err := os.MkdirAll(templates, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			if err := os.WriteFile(filepath.Join(templates, name), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return filepath.Join(root, "configuration.yml")
	}

	t.Run("nothing named at all is unknown, not empty", func(t *testing.T) {
		t.Parallel()
		got, err := readDefinitionTemplates("", "")
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("got %#v, want nil so the validator skips the template check", got)
		}
	})

	t.Run("an implicit templates directory that is absent is unknown", func(t *testing.T) {
		t.Parallel()
		// The specification packages templates/ beside configuration.yml. A
		// Product that ships none is not a Product whose templates are broken.
		configuration := filepath.Join(t.TempDir(), "configuration.yml")
		got, err := readDefinitionTemplates(configuration, "")
		if err != nil {
			t.Fatalf("an absent sibling templates directory was an error: %v", err)
		}
		if got != nil {
			t.Errorf("got %#v, want nil", got)
		}
	})

	t.Run("an explicit templates directory that is absent is an error", func(t *testing.T) {
		t.Parallel()
		// Naming a directory that is not there is a mistake worth reporting;
		// silently treating it as "no templates" hides the typo.
		_, err := readDefinitionTemplates("", filepath.Join(t.TempDir(), "absent"))
		if err == nil {
			t.Fatal("an explicitly named missing directory was accepted")
		}
		if !strings.Contains(err.Error(), "templates") {
			t.Errorf("error %q does not say what it failed to read", err)
		}
	})

	t.Run("templates beside the configuration are found", func(t *testing.T) {
		t.Parallel()
		configuration := withTemplates(t, "service.tmpl", "ingress.tmpl")
		got, err := readDefinitionTemplates(configuration, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Errorf("got %#v, want both templates", got)
		}
	})

	t.Run("an explicit directory overrides the sibling", func(t *testing.T) {
		t.Parallel()
		configuration := withTemplates(t, "sibling.tmpl")
		explicit := filepath.Join(t.TempDir(), "elsewhere")
		if err := os.MkdirAll(explicit, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(explicit, "chosen.tmpl"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := readDefinitionTemplates(configuration, explicit)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || !strings.Contains(got[0], "chosen") {
			t.Errorf("got %#v, want only the explicitly named directory's template", got)
		}
	})
}

// The media type travels with the bytes and is what the Hub records; it cannot
// infer one from the content. An extension it recognises must be declared as
// such, and one it does not must fall back rather than be sent empty.
func TestAssetPublishDeclaresAMediaTypeForEveryAsset(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	manifestPath := filepath.Join(directory, "manifest.yml")
	manifest := `manifest-version: "1.0"
product-group: com.example
product-name: network
product-version: 1.2.3
product-type: helm-chart.v1
extensions:
  helm-chart:
    helm-chart-name: network
    helm-chart-version: 1.2.3
    helm-repository-url: oci://registry.example/charts/network
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	publish := func(t *testing.T, assetName string) string {
		t.Helper()
		assetPath := filepath.Join(directory, assetName)
		if err := os.WriteFile(assetPath, []byte("asset"), 0o600); err != nil {
			t.Fatal(err)
		}
		service := &fakeService{}
		var output bytes.Buffer
		root := NewRoot(testRuntime(&output, service))
		root.SetArgs([]string{
			"--server", "http://127.0.0.1:8081", "--development-subject", "developer",
			"product-release", "asset", "publish", "--asset-path", assetPath,
			"--maven-coordinate", "com.example:network:1.2.3", "--manifest-file", manifestPath,
		})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		return service.uploadedMediaType
	}

	t.Run("a recognised extension is declared", func(t *testing.T) {
		t.Parallel()
		if got := publish(t, "asset.json"); !strings.HasPrefix(got, "application/json") {
			t.Errorf("media type = %q, want application/json", got)
		}
	})

	t.Run("an unrecognised extension falls back rather than sending nothing", func(t *testing.T) {
		t.Parallel()
		// An empty Content-Type leaves the Hub storing a blob it cannot
		// describe, and nothing downstream can recover the type afterwards.
		if got := publish(t, "asset.unknownext"); got != "application/octet-stream" {
			t.Errorf("media type = %q, want application/octet-stream", got)
		}
	})
}
