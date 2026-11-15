// Command generate writes the native workspace Plan-input editor schema.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func main() {
	schema, err := jsonschema.For[controlplane.DesiredState](nil)
	if err != nil {
		panic(err)
	}
	schema.Schema = "https://json-schema.org/draft/2020-12/schema"
	schema.Title = "Conduit EnvironmentDeployment (Plan input)"
	schema.Description = "Structural editor validation only. Hub semantic validation and approval remain authoritative."
	for key, value := range map[string]string{"apiVersion": "platform.delivery/v1alpha1", "kind": "EnvironmentDeployment"} {
		var constant any = value
		schema.Properties[key].Const = &constant
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		panic(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		panic(err)
	}
	// JSON output tags do not define input presence: Hub permits omitted zero values.
	relaxPresence(document)
	document["required"] = []string{"apiVersion", "kind", "environment"}
	environment := document["properties"].(map[string]any)["environment"].(map[string]any)
	environment["required"] = []string{"id", "generation"}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		panic(err)
	}
	if len(os.Args) != 2 {
		panic("usage: generate OUTPUT")
	}
	if err := os.WriteFile(os.Args[1], append(data, '\n'), 0644); err != nil {
		panic(err)
	}
	fmt.Println("generated", os.Args[1])
}

// relaxPresence keeps inferred field types without confusing Go output encoding
// with the Hub's input-presence contract. Semantic checks remain on the Hub.
func relaxPresence(value any) {
	switch node := value.(type) {
	case map[string]any:
		delete(node, "required")
		for _, child := range node {
			relaxPresence(child)
		}
	case []any:
		for _, child := range node {
			relaxPresence(child)
		}
	}
}
