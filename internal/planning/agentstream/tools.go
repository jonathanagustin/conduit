// Package agentstream adapts governed planning sessions to agent-run streams.
package agentstream

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/jonathanagustin/conduit/api/agui"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var (
	ErrToolNotOffered       = errors.New("the form tool was not offered for this run")
	ErrInvalidToolSchema    = errors.New("the offered form-tool schema is invalid or unsupported")
	ErrInvalidToolArguments = errors.New("the form-tool arguments were rejected")
	ErrFieldUnavailable     = errors.New("the form field is unavailable for this operation")
)

//go:embed form-tools.json
var formSchemas []byte

// ToolOffer is the bounded frontend-tool declaration supplied for one run.
type ToolOffer struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// Field is server-owned form metadata. Client state cannot add fields or change
// their types, choices, or mutability. Type is string, boolean or number.
type Field struct {
	Type     string
	Choices  []string
	ReadOnly bool
}

type noSchemaLoader struct{}

func (noSchemaLoader) Load(string) (any, error) { return nil, ErrInvalidToolSchema }

func compileArguments(name string, value any) (*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(noSchemaLoader{})
	location := "urn:conduit:agent-form:" + name
	if err := compiler.AddResource(location, value); err != nil {
		return nil, ErrInvalidToolSchema
	}
	schema, err := compiler.Compile(location)
	if err != nil {
		return nil, ErrInvalidToolSchema
	}
	return schema, nil
}

var canonicalTools = sync.OnceValues(func() (map[string]*jsonschema.Schema, error) {
	value, err := agui.DecodeJSON(formSchemas)
	if err != nil {
		return nil, err
	}
	out := map[string]*jsonschema.Schema{}
	for name, value := range value.(map[string]any) {
		schema, err := compileArguments(name, value)
		if err != nil {
			return nil, err
		}
		out[name] = schema
	}
	return out, nil
})

// ToolPolicy intersects a run's offered schemas with the Hub's own argument
// schemas and field permissions. It never executes a tool.
type ToolPolicy struct {
	canonical map[string]*jsonschema.Schema
	offered   map[string]*jsonschema.Schema
	fields    map[string]Field
}

// NewToolPolicy freezes caller declarations and server field metadata for a run.
func NewToolPolicy(offers []ToolOffer, fields map[string]Field) (*ToolPolicy, error) {
	if len(offers) > 4 {
		return nil, ErrInvalidToolSchema
	}
	canonical, err := canonicalTools()
	if err != nil {
		return nil, err
	}
	policy := &ToolPolicy{canonical: canonical, offered: map[string]*jsonschema.Schema{}, fields: map[string]Field{}}
	for name, field := range fields {
		if name == "" || len(name) > 128 || (field.Type != "string" && field.Type != "boolean" && field.Type != "number") {
			return nil, ErrFieldUnavailable
		}
		field.Choices = append([]string(nil), field.Choices...)
		policy.fields[name] = field
	}
	for _, offer := range offers {
		if canonical[offer.Name] == nil || policy.offered[offer.Name] != nil || len(offer.Description) > 1024 || len(offer.Parameters) > 16<<10 {
			return nil, ErrInvalidToolSchema
		}
		value := any(map[string]any{})
		if len(offer.Parameters) > 0 {
			value, err = agui.DecodeJSON(offer.Parameters)
			if err != nil {
				return nil, ErrInvalidToolSchema
			}
		}
		if err := containedReferences(value); err != nil {
			return nil, err
		}
		schema, err := compileArguments(offer.Name, value)
		if err != nil {
			return nil, err
		}
		policy.offered[offer.Name] = schema
	}
	return policy, nil
}

func containedReferences(value any) error {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			if key == "$ref" || key == "$dynamicRef" || key == "$recursiveRef" {
				reference, ok := child.(string)
				if !ok || len(reference) == 0 || reference[0] != '#' {
					return ErrInvalidToolSchema
				}
			}
			if err := containedReferences(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range node {
			if err := containedReferences(child); err != nil {
				return err
			}
		}
	}
	return nil
}

// Validate checks the offered declaration, canonical schema, and field scope.
// Errors deliberately omit raw argument values and schema diagnostics.
func (policy *ToolPolicy) Validate(name string, raw json.RawMessage) (map[string]any, error) {
	offered := policy.offered[name]
	if offered == nil {
		return nil, ErrToolNotOffered
	}
	if len(raw) > 8<<10 {
		return nil, ErrInvalidToolArguments
	}
	value, err := agui.DecodeJSON(raw)
	if err != nil {
		return nil, ErrInvalidToolArguments
	}
	if err := policy.canonical[name].Validate(value); err != nil {
		return nil, ErrInvalidToolArguments
	}
	if err := offered.Validate(value); err != nil {
		return nil, ErrInvalidToolArguments
	}
	arguments := value.(map[string]any)
	if name == "request_confirmation" {
		return arguments, nil
	}
	field, exists := policy.fields[arguments["field"].(string)]
	if !exists {
		return nil, ErrFieldUnavailable
	}
	if name == "focus_field" {
		return arguments, nil
	}
	if field.ReadOnly {
		return nil, ErrFieldUnavailable
	}
	if name == "clear_field" || arguments["value"] == nil {
		return arguments, nil
	}
	switch field.Type {
	case "string":
		text, ok := arguments["value"].(string)
		if !ok {
			return nil, ErrInvalidToolArguments
		}
		if len(field.Choices) > 0 {
			matched := false
			for _, choice := range field.Choices {
				if choice == text {
					matched = true
				}
			}
			if !matched {
				return nil, ErrInvalidToolArguments
			}
		}
	case "boolean":
		if _, ok := arguments["value"].(bool); !ok {
			return nil, ErrInvalidToolArguments
		}
	case "number":
		if _, ok := arguments["value"].(json.Number); !ok {
			return nil, ErrInvalidToolArguments
		}
	default:
		return nil, ErrFieldUnavailable
	}
	return arguments, nil
}

// Definitions returns a detached canonical parameter-schema set for owned
// clients. A custom client's offered schema may narrow these rules, not widen.
func Definitions() (map[string]json.RawMessage, error) {
	var result map[string]json.RawMessage
	if err := json.NewDecoder(bytes.NewReader(formSchemas)).Decode(&result); err != nil {
		return nil, fmt.Errorf("form-tool definitions: %w", err)
	}
	return result, nil
}
