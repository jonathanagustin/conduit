package a2ui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

const textCatalog = `{"catalogId":"conduit.test:text-only","protocolVersion":"1.0","$defs":{"anyComponent":{"$ref":"#/components/Text"},"anyFunction":false},"components":{"Text":{"type":"object","properties":{"component":{"const":"Text"},"text":{"type":"string","maxLength":4096}},"required":["component","text"]}}}`

func TestPinnedProtocolAndApplicationCatalog(t *testing.T) {
	v, err := NewValidator([]byte(textCatalog))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"version":"1.0","createSurface":{"surfaceId":"form","catalogId":"conduit.test:text-only","components":[{"id":"root","component":"Text","text":"review <script> as inert text"}],"dataModel":{}}}`,
		`{"version":"1.0","updateDataModel":{"surfaceId":"form","path":"/intake/ref","value":"main"}}`,
		`{"version":"1.0","deleteSurface":{"surfaceId":"form"}}`,
	} {
		if _, err := v.Agent([]byte(raw)); err != nil {
			t.Fatalf("valid envelope: %v", err)
		}
	}
	for _, raw := range []string{
		`{"version":"0.9","deleteSurface":{"surfaceId":"form"}}`,
		`{"version":"1.0","createSurface":{"surfaceId":"form","components":[{"id":"root","component":"Script","text":"execute"}]}}`,
		`{"version":"1.0","createSurface":{"surfaceId":"form","components":[{"id":"root","component":"Text","text":"review","html":"<script>"}]}}`,
		`{"version":"1.0","updateDataModel":{"surfaceId":"form","value":null,"unknown":true}}`,
		`{"version":"1.0","deleteSurface":{"surfaceId":"form"},"deleteSurface":{"surfaceId":"other"}}`,
	} {
		if _, err := v.Agent([]byte(raw)); err == nil {
			t.Fatalf("invalid envelope accepted: %s", raw)
		}
	}
	if _, err := v.Renderer([]byte(`{"version":"1.0","action":{"name":"submit","surfaceId":"form","sourceComponentId":"submit","timestamp":"2026-11-15T08:00:00Z","context":{"port":"9090"}}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Renderer([]byte(`{"version":"1.0","action":{"name":"submit","surfaceId":"form","context":{}}}`)); err == nil {
		t.Fatal("incomplete action accepted")
	}
}
func TestRemoteCatalogReferencesAreRefused(t *testing.T) {
	catalog := `{"catalogId":"conduit.test:remote","$defs":{"anyComponent":{"$ref":"https://untrusted.invalid/component.json"},"anyFunction":false}}`
	if _, err := NewValidator([]byte(catalog)); err == nil {
		t.Fatal("external catalog reference was accepted")
	}
}
func TestPinnedSourceHashes(t *testing.T) {
	raw, err := os.ReadFile("upstream/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Files []struct{ File, SHA256 string }
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 9 {
		t.Fatal("source manifest incomplete")
	}
	for _, entry := range manifest.Files {
		data, err := os.ReadFile("upstream/" + entry.File)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != entry.SHA256 {
			t.Fatalf("source hash changed: %s", entry.File)
		}
	}
}

func TestNativeExtensionIdentifiersAreAnExplicitASCIISubset(t *testing.T) {
	v, err := NewValidator([]byte(textCatalog))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		key   string
		valid bool
	}{{"conduit_test", true}, {"_native2", true}, {"2bad", false}, {"bad-key", false}, {"évidence", false}} {
		raw, _ := json.Marshal(map[string]any{"version": "1.0", "createSurface": map[string]any{"surfaceId": "form", "metadata": map[string]any{"extensions": map[string]any{test.key: true}}}})
		_, err := v.Agent(raw)
		if (err == nil) != test.valid {
			t.Fatalf("extension key %q: %v", test.key, err)
		}
	}
}
