package domain

import (
	"bytes"
	"encoding/json"
	"io"
)

// Decode accepts one bounded typed document and rejects duplicate/unknown fields.
func Decode(raw []byte) (Attestation, error) {
	var a Attestation
	if len(raw) == 0 || len(raw) > MaximumDocumentBytes {
		return a, ErrInvalid
	}
	tokens := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueValue(tokens, 0); err != nil {
		return a, err
	}
	if _, err := tokens.Token(); err != io.EOF {
		return a, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&a) != nil || decoder.Decode(new(any)) != io.EOF || a.validate() != nil {
		return Attestation{}, ErrInvalid
	}
	if len(a.Signature) != 86 {
		return Attestation{}, ErrInvalid
	}
	return a, nil
}
func Encode(a Attestation) ([]byte, error) {
	if a.validate() != nil || len(a.Signature) != 86 {
		return nil, ErrInvalid
	}
	raw, err := json.Marshal(a.normalized())
	if err != nil || len(raw) > MaximumDocumentBytes {
		return nil, ErrInvalid
	}
	return raw, nil
}
func uniqueValue(d *json.Decoder, depth int) error {
	if depth > 12 {
		return ErrInvalid
	}
	token, err := d.Token()
	if err != nil {
		return ErrInvalid
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return ErrInvalid
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return ErrInvalid
			}
			seen[name] = true
			if err := uniqueValue(d, depth+1); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return ErrInvalid
		}
	case '[':
		for d.More() {
			if err := uniqueValue(d, depth+1); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
