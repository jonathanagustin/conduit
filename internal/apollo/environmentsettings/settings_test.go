package environmentsettings

import (
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

const beforeDocument = `release-channel: STABLE
future-setting:
  enabled: false
managed-helm-charts:
  - entity-locator:
      name: api
      zone: west
    product: group:api
    k8s-namespace: old
    future-chart-setting: unchanged
`

func decode(t *testing.T, document string) Settings {
	t.Helper()
	var settings Settings
	if err := yaml.Unmarshal([]byte(document), &settings); err != nil {
		t.Fatal(err)
	}
	return settings
}

func TestDiffRetainsUnknownPolicyPathsAndExplicitFalse(t *testing.T) {
	before := decode(t, beforeDocument)
	after := decode(t, strings.NewReplacer("enabled: false", "enabled: true", "zone: west", "zone: east", "k8s-namespace: old", "k8s-namespace: next").Replace(beforeDocument)+"override-product-maintenance-windows: false\n")
	expected := []string{"future-setting", "managed-helm-charts/api/entity-locator/zone", "managed-helm-charts/api/k8s-namespace", "override-product-maintenance-windows"}
	for i := 0; i < 25; i++ {
		if got := DiffPaths(before, after); !reflect.DeepEqual(got, expected) {
			t.Fatalf("policy paths %v, want %v", got, expected)
		}
	}
	if got := DiffPaths(before, before); len(got) != 0 {
		t.Fatalf("unchanged document differs: %v", got)
	}
	encoded, err := yaml.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip := decode(t, string(encoded))
	if got := DiffPaths(after, roundTrip); len(got) != 0 {
		t.Fatalf("round trip lost settings: %v", got)
	}
	if roundTrip.OverrideProductMaintenanceWindows == nil || *roundTrip.OverrideProductMaintenanceWindows || len(roundTrip.Other) != 1 || len(roundTrip.ManagedHelmCharts[0].Other) != 1 || len(roundTrip.ManagedHelmCharts[0].EntityLocator.Other) != 1 {
		t.Fatalf("presence or unknown fields lost: %#v", roundTrip)
	}
}

func TestInstallationAdditionRemovalAndFieldVocabulary(t *testing.T) {
	before := decode(t, beforeDocument)
	after := decode(t, "managed-helm-charts:\n  - entity-locator: {name: worker}\n")
	expected := []string{"future-setting", "managed-helm-charts/api", "managed-helm-charts/worker", "release-channel"}
	if got := DiffPaths(before, after); !reflect.DeepEqual(got, expected) {
		t.Fatalf("installation paths %v, want %v", got, expected)
	}
	encoded, err := yaml.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"release-channel:", "managed-helm-charts:", "entity-locator:", "k8s-namespace:", "future-setting:", "future-chart-setting:", "zone:"} {
		if !strings.Contains(string(encoded), key) {
			t.Fatalf("published field %q missing from %s", key, encoded)
		}
	}
}
