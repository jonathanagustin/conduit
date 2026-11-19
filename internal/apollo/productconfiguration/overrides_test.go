package productconfiguration

import (
	"reflect"
	"testing"
)

func TestRecursiveMapsReplaceListsAndPreserveInputs(t *testing.T) {
	base := map[string]any{
		"server":            map[string]any{"port": 8443, "tls": map[string]any{"enabled": true, "issuer": "kept"}},
		"hosts":             []any{"a", "b"},
		GeneratedSecretsKey: map[string]any{"token": "unchanged"},
	}
	override := map[string]any{"server": map[string]any{"tls": map[string]any{"enabled": false}}, "hosts": []any{"c"}}
	got, err := MergeOverrides(base, override)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"server": map[string]any{"port": 8443, "tls": map[string]any{"enabled": false, "issuer": "kept"}}, "hosts": []any{"c"}, GeneratedSecretsKey: map[string]any{"token": "unchanged"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged = %#v", got)
	}
	expectedBase := map[string]any{"server": map[string]any{"port": 8443, "tls": map[string]any{"enabled": true, "issuer": "kept"}}, "hosts": []any{"a", "b"}, GeneratedSecretsKey: map[string]any{"token": "unchanged"}}
	if !reflect.DeepEqual(base, expectedBase) {
		t.Fatalf("base mutated: %#v", base)
	}
	expectedOverride := map[string]any{"server": map[string]any{"tls": map[string]any{"enabled": false}}, "hosts": []any{"c"}}
	if !reflect.DeepEqual(override, expectedOverride) {
		t.Fatalf("override mutated: %#v", override)
	}
}

func TestMixedYAMLMapsAndWholeValueReplacement(t *testing.T) {
	base := map[string]any{"mixed": map[any]any{"kept": 1, "nested": map[string]any{"left": 1}}, "scalar": "old", "map": map[string]any{"old": true}, "nil": nil}
	override := map[string]any{"mixed": map[string]any{"nested": map[any]any{"right": 2}}, "scalar": map[string]any{"new": true}, "map": nil, "nil": []any{}, "added": false}
	got, err := MergeOverrides(base, override)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"mixed": map[string]any{"kept": 1, "nested": map[string]any{"left": 1, "right": 2}}, "scalar": map[string]any{"new": true}, "map": nil, "nil": []any{}, "added": false}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mixed-map merge: %#v", got)
	}
	// Preserve the original decoder normalization, without adding a policy for
	// colliding string representations of non-string YAML keys in this refactor.
	got, err = MergeOverrides(map[string]any{"mixed": map[any]any{7: "seven"}}, map[string]any{"mixed": map[any]any{"other": 8}})
	if err != nil || !reflect.DeepEqual(got, map[string]any{"mixed": map[string]any{"7": "seven", "other": 8}}) {
		t.Fatalf("key normalization: %#v %v", got, err)
	}
}

func TestGeneratedSecretsRefusedByPresence(t *testing.T) {
	if GeneratedSecretsKey != "generated_secrets" {
		t.Fatal("document key changed")
	}
	for _, value := range []any{nil, false, map[string]any{}, map[string]any{"token": "changed"}} {
		got, err := MergeOverrides(map[string]any{"unchanged": true}, map[string]any{GeneratedSecretsKey: value})
		if got != nil || err == nil || err.Error() != "configuration override must not set generated_secrets" {
			t.Fatalf("protected override %#v: %#v %v", value, got, err)
		}
	}
}

func TestNilEmptyInputsAndNestedNames(t *testing.T) {
	got, err := MergeOverrides(nil, nil)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty result: %#v %v", got, err)
	}
	got["owned"] = true
	base := map[string]any{"value": 1}
	got, err = MergeOverrides(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	got["value"] = 2
	if base["value"] != 1 {
		t.Fatal("result root aliases base")
	}
	// Only the top-level declaration block is protected. An arbitrary conf key
	// with the same spelling does not become a secret declaration.
	got, err = MergeOverrides(nil, map[string]any{"conf": map[string]any{GeneratedSecretsKey: "application data"}})
	if err != nil || !reflect.DeepEqual(got, map[string]any{"conf": map[string]any{GeneratedSecretsKey: "application data"}}) {
		t.Fatalf("nested name refused: %#v %v", got, err)
	}
}
