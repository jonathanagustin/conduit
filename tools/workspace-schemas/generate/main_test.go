package main

import "testing"

func TestPresenceRelaxationPreservesPropertyNamedRequired(t *testing.T) {
	property := map[string]any{"type": "boolean"}
	properties := map[string]any{"required": property}
	schema := map[string]any{"type": "object", "required": []any{"required"}, "properties": properties}
	relaxPresence(schema)
	if _, exists := schema["required"]; exists {
		t.Fatal("inferred output presence remained mandatory")
	}
	if properties["required"] == nil {
		t.Fatal("an input property named required was erased")
	}
	if property["type"] != "boolean" {
		t.Fatal("property type constraint was lost")
	}
}
