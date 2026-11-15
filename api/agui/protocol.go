// Package agui validates the pinned public AG-UI wire contract without fetching
// schemas. Application authorization, tool policy and lifecycle ordering are
// separate obligations of the Hub adapter.
package agui

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Version is the protocol version implemented by this contract snapshot.
const Version = "1.0"

const schemaURI = "https://ag-ui.com/spec/1.0/schema.json"

//go:embed upstream/schema.json
var schemaBytes []byte

// Validator owns immutable compiled input and event schemas.
type Validator struct {
	input *jsonschema.Schema
	event *jsonschema.Schema
}

type denyLoader struct{}

func (denyLoader) Load(location string) (any, error) {
	return nil, fmt.Errorf("external schema loading is disabled: %s", location)
}

var compiled = sync.OnceValues(func() (*Validator, error) {
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(denyLoader{})
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		return nil, err
	}
	if err := compiler.AddResource(schemaURI, value); err != nil {
		return nil, err
	}
	input, err := compiler.Compile(schemaURI + "#/$defs/RunAgentInput")
	if err != nil {
		return nil, err
	}
	event, err := compiler.Compile(schemaURI + "#/$defs/Event")
	if err != nil {
		return nil, err
	}
	return &Validator{input: input, event: event}, nil
})

// NewValidator returns the shared, offline-compiled public contract.
func NewValidator() (*Validator, error) { return compiled() }

// Input decodes a bounded JSON object and validates its public input shape.
func (v *Validator) Input(raw []byte) (map[string]any, error) { return validate(v.input, raw) }

// Event decodes a bounded JSON object and validates its public event shape.
func (v *Validator) Event(raw []byte) (map[string]any, error) { return validate(v.event, raw) }

func validate(schema *jsonschema.Schema, raw []byte) (map[string]any, error) {
	value, err := DecodeJSON(raw)
	if err != nil {
		return nil, err
	}
	if err := schema.Validate(value); err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("AG-UI envelope must be an object")
	}
	return object, nil
}

// DecodeJSON decodes one bounded JSON value without duplicate keys or lost
// number precision. Tool-policy adapters reuse the wire parser's protections.
func DecodeJSON(raw []byte) (any, error) {
	if len(raw) > 1<<20 {
		return nil, fmt.Errorf("AG-UI JSON exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decode(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("invalid trailing JSON")
	}
	return value, nil
}

// decode rejects duplicate keys before schema validation can discard them.
func decode(decoder *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, fmt.Errorf("AG-UI JSON exceeds 64 nesting levels")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, container := token.(json.Delim)
	if !container {
		if number, ok := token.(json.Number); ok {
			text := string(number)
			if len(text) > 128 {
				return nil, fmt.Errorf("AG-UI JSON number is too long")
			}
			if position := strings.IndexAny(text, "eE"); position >= 0 {
				exponent, err := strconv.ParseInt(text[position+1:], 10, 32)
				if err != nil || exponent < -324 || exponent > 324 {
					return nil, fmt.Errorf("AG-UI JSON number exponent is outside supported bounds")
				}
			}
			value, err := strconv.ParseFloat(text, 64)
			if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
				return nil, fmt.Errorf("AG-UI JSON number is outside supported bounds")
			}
		}
		return token, nil
	}
	switch delimiter {
	case '{':
		object := map[string]any{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("invalid JSON object key")
			}
			if _, exists := object[key]; exists {
				return nil, fmt.Errorf("duplicate JSON key %q", key)
			}
			value, err := decode(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return object, nil
	case '[':
		array := []any{}
		for decoder.More() {
			value, err := decode(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return array, nil
	default:
		return nil, fmt.Errorf("unexpected JSON delimiter")
	}
}
