package conduitcli

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"go.yaml.in/yaml/v4"
)

func TestYAMLOutputPreservesExactScalarKindsAndNumbers(t *testing.T) {
	value := struct {
		Values []any `json:"nativeValues"`
	}{[]any{uint64(math.MaxUint64), int64(math.MinInt64), uint64(9007199254740993), json.Number("12345678901234567890.1234567890123456789"), json.Number("1.234567890123456789e+40"), json.Number("-0"), "9007199254740993", "true", true, nil}}
	var out bytes.Buffer
	if err := writeOutput(&out, "yaml", value); err != nil {
		t.Fatal(err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	mapping := doc.Content[0]
	if len(mapping.Content) != 2 || mapping.Content[0].Value != "nativeValues" {
		t.Fatal(out.String())
	}
	nodes := mapping.Content[1].Content
	expected := []struct{ tag, value string }{
		{"!!int", "18446744073709551615"}, {"!!int", "-9223372036854775808"}, {"!!int", "9007199254740993"},
		{"!!float", "12345678901234567890.1234567890123456789"}, {"!!float", "1.234567890123456789e+40"}, {"!!float", "-0"},
		{"!!str", "9007199254740993"}, {"!!str", "true"}, {"!!bool", "true"}, {"!!null", "null"},
	}
	if len(nodes) != len(expected) {
		t.Fatal(out.String())
	}
	for i, want := range expected {
		if nodes[i].Tag != want.tag || nodes[i].Value != want.value {
			t.Fatalf("scalar %d got %s %q want %s %q\n%s", i, nodes[i].Tag, nodes[i].Value, want.tag, want.value, out.String())
		}
	}
}

func TestPlanListYAMLMatchesNativeRevisionOverRESTAndConnect(t *testing.T) {
	for _, revision := range []uint64{9007199254740993, math.MaxUint64} {
		repository := controlplane.NewMemoryRepository()
		at := time.Date(2026, time.November, 16, 10, 0, 0, 123456789, time.UTC)
		if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: "plan-exact", DesiredStateDigest: "digest", Plan: controlplane.Plan{Environment: "east", Generation: "generation", Steps: []controlplane.PlanStep{{InstallationID: "frontend", Product: "app"}}}, Status: controlplane.PlanRunning, CreatedAt: at, UpdatedAt: at, Revision: revision, Lease: &controlplane.PlanLease{Token: "PRIVATE-LEASE"}}); err != nil {
			t.Fatal(err)
		}
		handler, err := composition.NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}})
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(handler)
		for _, mode := range []string{"rest", "connect"} {
			output, err := executePlanTransport(t, server.URL, "root", mode, "--output", "yaml")
			if err != nil {
				t.Fatal(mode, err, output)
			}
			var result struct {
				Plans []struct {
					ID       string `yaml:"id"`
					Revision uint64 `yaml:"revision"`
				} `yaml:"plans"`
			}
			if err := yaml.Unmarshal([]byte(output), &result); err != nil {
				t.Fatal(mode, err, output)
			}
			if len(result.Plans) != 1 || result.Plans[0].ID != "plan-exact" || result.Plans[0].Revision != revision || strings.Contains(output, "PRIVATE-LEASE") {
				t.Fatalf("%s lost native evidence at %d: %s", mode, revision, output)
			}
		}
		server.Close()
	}
}
