// Package schema validates delivery envelopes against the committed JSON schema.
package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	livecontract "github.com/jonathanagustin/conduit/api/live-events"
	"github.com/jonathanagustin/conduit/internal/liveevents/domain"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"io"
)

type Validator struct{ schema *jsonschema.Schema }

func New() (*Validator, error) {
	var document any
	if err := json.Unmarshal(livecontract.EventSchema(), &document); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	const location = "https://conduit.invalid/contracts/live-event-v1"
	if err := compiler.AddResource(location, document); err != nil {
		return nil, err
	}
	compiled, err := compiler.Compile(location)
	if err != nil {
		return nil, err
	}
	return &Validator{schema: compiled}, nil
}
func (validator *Validator) Envelope(envelope domain.Envelope) error {
	body, err := json.Marshal(envelope)
	if err != nil || len(body) > domain.MaxEnvelopeBytes {
		return fmt.Errorf("%w: envelope encoding/size", domain.ErrInvalid)
	}
	if duplicateJSON(body) != nil {
		return fmt.Errorf("%w: duplicate or invalid JSON member", domain.ErrInvalid)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	var value any
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("%w: envelope JSON", domain.ErrInvalid)
	}
	if err := validator.schema.Validate(value); err != nil {
		return fmt.Errorf("%w: envelope schema", domain.ErrInvalid)
	}
	return nil
}

func duplicateJSON(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value func() error
	value = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return domain.ErrInvalid
				}
				seen[name] = true
				if err := value(); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := value(); err != nil {
					return err
				}
			}
		default:
			return domain.ErrInvalid
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		expected := json.Delim('}')
		if delimiter == '[' {
			expected = ']'
		}
		if closing != expected {
			return domain.ErrInvalid
		}
		return nil
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return domain.ErrInvalid
	}
	return nil
}
