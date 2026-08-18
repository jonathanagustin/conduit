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

// A Product Release may declare a non-orderable version, and internal/catalog
// says of that class that "nothing may order it against another version".
// CompareVersions honours it by falling back to strings.Compare when either side
// is non-orderable, which makes the comparator a PARTIAL order:
//
//	1.10.0 > 1.9.0          (numeric)
//	1.9.0  > 1.5.0-hotfix   (lexical fallback)
//	1.5.0-hotfix > 1.10.0   (lexical fallback)
//
// Fed to sort.Slice that cycle produces an input-order-dependent result, and one
// hotfix sharing a channel brought the original defect straight back: 1.9.0
// published as the latest version of a Product that has 1.10.0. The fixture is
// ordered to trip exactly that.
func TestEntityAggregateRefusesToRankNonOrderableVersions(t *testing.T) {
	t.Parallel()
	fixture := []controlplane.ProductCatalogRecord{{
		Product: "com.example:application",
		Releases: []controlplane.Release{
			{Product: "com.example:application", Version: "1.5.0-hotfix", Channels: []string{"RELEASE"}},
			{Product: "com.example:application", Version: "1.10.0", Channels: []string{"RELEASE"}},
			{Product: "com.example:application", Version: "1.9.0", Channels: []string{"RELEASE"}},
		},
	}}
	var output bytes.Buffer
	root := NewRoot(testRuntime(&output, &fakeService{products: fixture}))
	root.SetArgs([]string{
		"entity", "aggregate", "--generate-versions",
		"--server", "http://127.0.0.1:8081", "--development-subject", "developer",
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var aggregate struct {
		Products map[string]struct {
			Selected    string   `json:"selected"`
			Available   []string `json:"available"`
			Unorderable []string `json:"unorderable"`
		} `json:"products"`
	}
	if err := json.Unmarshal(output.Bytes(), &aggregate); err != nil {
		t.Fatalf("decode %s: %v", output.String(), err)
	}
	product := aggregate.Products["com.example:application"]
	if product.Selected != "1.10.0" {
		t.Fatalf("selected=%q want 1.10.0 (output=%s)", product.Selected, output.String())
	}
	// Reported, not dropped: an operator who cannot see the hotfix cannot tell a
	// Product that has none from one whose hotfix was silently discarded.
	if len(product.Unorderable) != 1 || product.Unorderable[0] != "1.5.0-hotfix" {
		t.Fatalf("unorderable=%v want [1.5.0-hotfix] (output=%s)", product.Unorderable, output.String())
	}
	for _, ranked := range product.Available {
		if ranked == "1.5.0-hotfix" {
			t.Fatalf("a non-orderable version was ranked among %v", product.Available)
		}
	}
}

// Apollo ordering deliberately ignores a snapshot's commit hash, so two
// snapshots sharing a counter compare EQUAL. sort.Slice is not stable and the
// candidate list is built from map iteration, so without a total order the
// selection alternated between runs of a command whose help promises a
// deterministic aggregate. Shuffling the input must not move the answer.
func TestEntityAggregateIsDeterministicWhenVersionsTie(t *testing.T) {
	t.Parallel()
	orderings := [][]string{
		{"2.0.0-5-gaaaaaaa", "2.0.0-5-gbbbbbbb"},
		{"2.0.0-5-gbbbbbbb", "2.0.0-5-gaaaaaaa"},
	}
	selections := make(map[string]struct{})
	for _, ordering := range orderings {
		releases := make([]controlplane.Release, 0, len(ordering))
		for _, version := range ordering {
			releases = append(releases, controlplane.Release{
				Product: "com.example:application", Version: version, Channels: []string{"RELEASE"},
			})
		}
		var output bytes.Buffer
		root := NewRoot(testRuntime(&output, &fakeService{products: []controlplane.ProductCatalogRecord{{
			Product: "com.example:application", Releases: releases,
		}}}))
		root.SetArgs([]string{
			"entity", "aggregate", "--generate-versions",
			"--server", "http://127.0.0.1:8081", "--development-subject", "developer",
		})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		var aggregate struct {
			Products map[string]struct {
				Selected string `json:"selected"`
			} `json:"products"`
		}
		if err := json.Unmarshal(output.Bytes(), &aggregate); err != nil {
			t.Fatalf("decode %s: %v", output.String(), err)
		}
		selections[aggregate.Products["com.example:application"].Selected] = struct{}{}
	}
	if len(selections) != 1 {
		t.Fatalf("input order changed the selection: %v", selections)
	}
}
