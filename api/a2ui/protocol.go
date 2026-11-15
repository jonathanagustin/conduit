// Package a2ui validates the pinned public A2UI 1.0 envelopes against a supplied
// application catalog. Catalog admission, lifecycle, binding and action authority
// remain application policy; successful shape validation grants no operation.
package a2ui

import (
	"bytes"
	"embed"
	"fmt"
	"regexp"

	"github.com/jonathanagustin/conduit/api/agui"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const Version = "1.0"
const schemaBase = "https://a2ui.org/specification/v1_0/"

//go:embed upstream/*.json
var sources embed.FS

type Validator struct {
	agent    *jsonschema.Schema
	renderer *jsonschema.Schema
}
type denyLoader struct{}

func (denyLoader) Load(location string) (any, error) {
	return nil, fmt.Errorf("external A2UI schema loading is disabled: %s", location)
}

// NewValidator compiles the retained schemas and the application's explicitly
// supplied catalog without network access. Callers must cache the result and
// separately enforce their approved catalog ID and component/function policy.
func NewValidator(catalog []byte) (*Validator, error) {
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(denyLoader{})
	compiler.UseRegexpEngine(nativePattern)
	for _, name := range []string{"agent_to_renderer.json", "renderer_to_agent.json", "common_types.json", "catalog_definition.json", "renderer_capabilities.json", "agent_capabilities.json", "renderer_data_model.json"} {
		data, err := sources.ReadFile("upstream/" + name)
		if err != nil {
			return nil, err
		}
		value, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if err := compiler.AddResource(schemaBase+name, value); err != nil {
			return nil, err
		}
	}
	value, err := agui.DecodeJSON(catalog)
	if err != nil {
		return nil, err
	}
	catalogSchema, err := compiler.Compile(schemaBase + "catalog_definition.json")
	if err != nil {
		return nil, err
	}
	if err := catalogSchema.Validate(value); err != nil {
		return nil, err
	}
	if err := compiler.AddResource(schemaBase+"catalog.json", value); err != nil {
		return nil, err
	}
	agent, err := compiler.Compile(schemaBase + "agent_to_renderer.json")
	if err != nil {
		return nil, err
	}
	renderer, err := compiler.Compile(schemaBase + "renderer_to_agent.json")
	if err != nil {
		return nil, err
	}
	return &Validator{agent: agent, renderer: renderer}, nil
}
func (v *Validator) Agent(raw []byte) (map[string]any, error)    { return validate(v.agent, raw) }
func (v *Validator) Renderer(raw []byte) (map[string]any, error) { return validate(v.renderer, raw) }
func validate(schema *jsonschema.Schema, raw []byte) (map[string]any, error) {
	value, err := agui.DecodeJSON(raw)
	if err != nil {
		return nil, err
	}
	if err := schema.Validate(value); err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("A2UI envelope must be an object")
	}
	return object, nil
}

// The native profile limits extension metadata keys to ASCII identifiers, a
// strict subset of public UAX #31 identifiers. Do not rewrite retained schemas
// or approximate Unicode acceptance with a broader character class.
func nativePattern(pattern string) (jsonschema.Regexp, error) {
	if pattern == `^[\p{XID_Start}_][\p{XID_Continue}]*$` {
		return regexp.Compile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	}
	return regexp.Compile(pattern)
}
