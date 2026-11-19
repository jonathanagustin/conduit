package environmentsettings

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	validationdomain "github.com/jonathanagustin/conduit/internal/validation/domain"
)

func TestCodecPreservesExplicitValuesAndUnknownFields(t *testing.T) {
	document := beforeDocument + "override-product-maintenance-windows: false\n"
	settings, err := Decode(document)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Encode(settings)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(DiffPaths(settings, again)) != 0 || again.OverrideProductMaintenanceWindows == nil || *again.OverrideProductMaintenanceWindows || len(again.Other) != 1 || len(again.ManagedHelmCharts[0].EntityLocator.Other) != 1 {
		t.Fatalf("codec lost presence or unknown fields: %#v", again)
	}
}

func TestCodecRefusesInvalidDocumentsWithExistingPaths(t *testing.T) {
	chart := func(fields string) string { return "managed-helm-charts:\n  - entity-locator: {name: api}\n" + fields }
	for _, tc := range []struct{ name, document, path, message string }{
		{"empty", "", "settings", "document is empty"},
		{"malformed", "managed-helm-charts: [", "settings", "decode settings document:"},
		{"channel", "release-channel: stable", "release-channel", "uppercase Release Channel"},
		{"blank locator", "managed-helm-charts:\n  - entity-locator: {name: '  '}\n", "managed-helm-charts[0].entity-locator.name", "must not be empty"},
		{"duplicate trimmed locator", chart("") + "  - entity-locator: {name: ' api '}\n", "managed-helm-charts[1]", "duplicate installation api"},
		{"product", chart("    product: missing-group\n"), "managed-helm-charts[0].product", "group:name coordinate"},
		{"namespace", chart("    k8s-namespace: Bad\n"), "managed-helm-charts[0].k8s-namespace", "DNS label"},
		{"chart channel", chart("    release-channel: stable\n"), "managed-helm-charts[0].release-channel", "uppercase Release Channel"},
		{"helm name length", chart("    helm-release-name: " + strings.Repeat("a", 54) + "\n"), "managed-helm-charts[0].helm-release-name", "no longer than 53"},
		{"stack", chart("    stack: /escape\n"), "managed-helm-charts[0].stack", "stack identifier"},
		{"override coordinate", chart("    dependency-stack-overrides: {bad: good}\n"), "managed-helm-charts[0].dependency-stack-overrides", "Product coordinates"},
		{"override stack", chart("    dependency-stack-overrides: {'group:db': '/escape'}\n"), "managed-helm-charts[0].dependency-stack-overrides", "stack identifiers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings, err := Decode(tc.document)
			var problems validationdomain.Problems
			if !errors.As(err, &problems) || len(problems) != 1 || problems[0].Path != tc.path || !strings.Contains(problems[0].Message, tc.message) {
				t.Fatalf("diagnostic %#v error %v", problems, err)
			}
			if !reflect.DeepEqual(settings, Settings{}) {
				t.Fatalf("invalid document leaked partial settings: %#v", settings)
			}
		})
	}
}

func TestCodecRetainsDiagnosticOrdering(t *testing.T) {
	_, err := Decode("release-channel: lowercase\nmanaged-helm-charts:\n  - entity-locator: {name: api}\n    product: invalid\n    k8s-namespace: Bad\n")
	var problems validationdomain.Problems
	if !errors.As(err, &problems) {
		t.Fatal(err)
	}
	paths := make([]string, len(problems))
	for i, p := range problems {
		paths[i] = p.Path
	}
	if !reflect.DeepEqual(paths, []string{"release-channel", "managed-helm-charts[0].product", "managed-helm-charts[0].k8s-namespace"}) {
		t.Fatalf("diagnostic order changed: %v", paths)
	}
}
