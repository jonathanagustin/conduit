package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const MaximumDocumentBytes = 8192

var ErrCorrupt = errors.New("corrupt debug-session journal")

// EncodeEvent produces the canonical private storage representation. It grants
// no authority and does not replace validation of the complete event history.
func EncodeEvent(e Event) ([]byte, error) {
	e = canonicalEvent(e)
	if e.Validate() != nil {
		return nil, ErrInvalid
	}
	b, err := json.Marshal(e)
	if err != nil || len(b) > MaximumDocumentBytes {
		return nil, ErrInvalid
	}
	return b, nil
}

// DecodeEvent rejects ambiguous JSON and verifies the external storage key.
func DecodeEvent(id string, revision uint64, b []byte) (Event, error) {
	if len(b) == 0 || len(b) > MaximumDocumentBytes {
		return Event{}, ErrCorrupt
	}
	d := json.NewDecoder(bytes.NewReader(b))
	if uniqueValue(d, 0) != nil {
		return Event{}, ErrCorrupt
	}
	if _, err := d.Token(); err != io.EOF {
		return Event{}, ErrCorrupt
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	var e Event
	if d.Decode(&e) != nil {
		return Event{}, ErrCorrupt
	}
	if d.Decode(new(any)) != io.EOF {
		return Event{}, ErrCorrupt
	}
	e = canonicalEvent(e)
	if e.Validate() != nil || e.SessionID != id || e.Revision != revision {
		return Event{}, ErrCorrupt
	}
	return e, nil
}
func canonicalEvent(e Event) Event {
	e.CreatedAt = e.CreatedAt.UTC()
	e.ExpiresAt = e.ExpiresAt.UTC()
	e.RecordedAt = e.RecordedAt.UTC()
	return e
}
func uniqueValue(d *json.Decoder, depth int) error {
	if depth > 6 {
		return ErrCorrupt
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	delimiter, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		keys := map[string]bool{}
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := k.(string)
			if !ok || keys[name] || !knownKey(depth, name) {
				return ErrCorrupt
			}
			keys[name] = true
			if err := uniqueValue(d, depth+1); err != nil {
				return err
			}
		}
		t, err = d.Token()
		if err != nil || t != json.Delim('}') {
			return ErrCorrupt
		}
	case '[':
		for d.More() {
			if err := uniqueValue(d, depth+1); err != nil {
				return err
			}
		}
		t, err = d.Token()
		if err != nil || t != json.Delim(']') {
			return ErrCorrupt
		}
	default:
		return ErrCorrupt
	}
	return nil
}

func knownKey(depth int, name string) bool {
	switch depth {
	case 0:
		switch name {
		case "sessionId", "revision", "state", "target", "owner", "actor", "createdAt", "expiresAt", "recordedAt", "valueTier":
			return true
		}
	case 1:
		switch name {
		case "environment", "namespace", "profile":
			return true
		}
	}
	return false
}
