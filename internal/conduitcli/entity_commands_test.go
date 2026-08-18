package conduitcli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// The aggregate used to sort candidate versions with sort.Strings, which is a
// byte comparison: "1.10.0" < "1.9.0" because '1' precedes '9'. Every Product
// that reached a double-digit minor therefore aggregated to the older release.
// The fixture below is deliberately the smallest case that exposes it, and it
// pins the "available" list too, since --generate-versions publishes the same
// ordering the selection is read from.
func TestEntityAggregateOrdersVersionsNumericallyNotLexicographically(t *testing.T) {
	t.Parallel()
	fixture := []controlplane.ProductCatalogRecord{{
		Product: "com.example:application",
		Releases: []controlplane.Release{
			{Product: "com.example:application", Version: "1.9.0", Channels: []string{"RELEASE"}},
			{Product: "com.example:application", Version: "1.10.0", Channels: []string{"RELEASE"}},
			{Product: "com.example:application", Version: "1.2.0", Channels: []string{"RELEASE"}},
		},
	}}
	for name, testCase := range map[string]struct {
		arguments []string
		selected  string
	}{
		"highest": {arguments: nil, selected: "1.10.0"},
		"lowest":  {arguments: []string{"--generate-lowest"}, selected: "1.2.0"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			root := NewRoot(testRuntime(&output, &fakeService{products: fixture}))
			root.SetArgs(append([]string{
				"entity", "aggregate", "--generate-versions",
				"--server", "http://127.0.0.1:8081", "--development-subject", "developer",
			}, testCase.arguments...))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			var aggregate struct {
				Products map[string]struct {
					Selected  string   `json:"selected"`
					Available []string `json:"available"`
				} `json:"products"`
			}
			if err := json.Unmarshal(output.Bytes(), &aggregate); err != nil {
				t.Fatalf("decode %s: %v", output.String(), err)
			}
			product, ok := aggregate.Products["com.example:application"]
			if !ok {
				t.Fatalf("output=%s", output.String())
			}
			if product.Selected != testCase.selected {
				t.Fatalf("selected=%q want %q (output=%s)", product.Selected, testCase.selected, output.String())
			}
			want := []string{"1.2.0", "1.9.0", "1.10.0"}
			if len(product.Available) != len(want) {
				t.Fatalf("available=%v want %v", product.Available, want)
			}
			for index, version := range want {
				if product.Available[index] != version {
					t.Fatalf("available=%v want %v", product.Available, want)
				}
			}
		})
	}
}
