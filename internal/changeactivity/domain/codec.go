package domain

import (
	"bytes"
	"encoding/json"
	"io"
)

func DecodeComment(requestID, id string, document []byte) (Comment, error) {
	if len(document) > MaxDocumentBytes {
		return Comment{}, ErrCorrupt
	}
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	var c Comment
	if err := decoder.Decode(&c); err != nil {
		return Comment{}, ErrCorrupt
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Comment{}, ErrCorrupt
	}
	if c.Validate() != nil || c.RequestID != requestID || c.ID != id {
		return Comment{}, ErrCorrupt
	}
	return c, nil
}
