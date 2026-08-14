package consoleui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeBootstrapAcceptsSharedTenantFixtures(t *testing.T) {
	for _, name := range []string{"tenant-a.json", "tenant-b.json"} {
		name := name
		t.Run(name, func(t *testing.T) {
			file, err := os.Open(filepath.Join("..", "..", "testdata", "console-bootstrap", name))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			bootstrap, err := DecodeBootstrap(file)
			if err != nil {
				t.Fatal(err)
			}
			if bootstrap.Provenance.ClaimClass != "tenant-observation" || bootstrap.Tenant.ID == "conduit" {
				t.Fatalf("fixture decoded without tenant provenance: %#v", bootstrap)
			}
		})
	}
}

func TestDecodeBootstrapRejectsUnknownAndSecurityWeakeningFields(t *testing.T) {
	base := DefaultBootstrap()
	encoded, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]string{
		"top-level capability": strings.Replace(string(encoded), `"provenance":`, `"capabilities":["admin"],"provenance":`, 1),
		"brand script":         strings.Replace(string(encoded), `"icon":"dashboard"`, `"icon":"dashboard","script":"alert(1)"`, 1),
		"tenant API origin":    strings.Replace(string(encoded), `"displayName":"Conduit"`, `"displayName":"Conduit","apiBaseUrl":"https://example.invalid"`, 1),
		"trailing document":    string(encoded) + `{}`,
	}
	for name, document := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeBootstrap(strings.NewReader(document)); err == nil {
				t.Fatalf("DecodeBootstrap accepted %s", document)
			}
		})
	}
}

func TestBootstrapValidationRejectsExecutableOrUnprovenBranding(t *testing.T) {
	tests := map[string]func(*Bootstrap){
		"markup": func(bootstrap *Bootstrap) {
			bootstrap.Brand.Title = "<script>"
		},
		"executable URI": func(bootstrap *Bootstrap) {
			bootstrap.Brand.Subtitle = "javascript:alert(1)"
		},
		"arbitrary color": func(bootstrap *Bootstrap) {
			bootstrap.Brand.Accent = "url(https://example.invalid)"
		},
		"arbitrary icon": func(bootstrap *Bootstrap) {
			bootstrap.Brand.Icon = "unknown"
		},
		"tenant claim without evidence": func(bootstrap *Bootstrap) {
			bootstrap.Provenance.ClaimClass = "tenant-observation"
			bootstrap.Provenance.EvidenceRef = "conduit://console/default"
		},
		"invented claim class": func(bootstrap *Bootstrap) {
			bootstrap.Provenance.ClaimClass = "apollo-compatible"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			bootstrap := DefaultBootstrap()
			mutate(&bootstrap)
			if err := bootstrap.Validate(); err == nil {
				t.Fatalf("Validate accepted %#v", bootstrap)
			}
		})
	}
}

func TestLoadBootstrapFileUsesExplicitDefaultOnlyForEmptyPath(t *testing.T) {
	bootstrap, err := LoadBootstrapFile("  ")
	if err != nil {
		t.Fatal(err)
	}
	if bootstrap != DefaultBootstrap() {
		t.Fatalf("default bootstrap = %#v", bootstrap)
	}
	if _, err := LoadBootstrapFile(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("missing configured bootstrap did not fail closed")
	}
}
