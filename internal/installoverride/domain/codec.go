package domain

import (
	"bytes"
	"encoding/json"
	"io"
)

func Encode(p Proposal) ([]byte, error) {
	if p.Validate() != nil {
		return nil, ErrInvalid
	}
	encoded, err := json.Marshal(p)
	if err != nil || len(encoded) > MaximumDocumentBytes {
		return nil, ErrInvalid
	}
	return encoded, nil
}

func Decode(document []byte) (Proposal, error) {
	if len(document) == 0 || len(document) > MaximumDocumentBytes {
		return Proposal{}, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	var p Proposal
	if decoder.Decode(&p) != nil || decoder.Decode(new(any)) != io.EOF || p.Validate() != nil {
		return Proposal{}, ErrInvalid
	}
	return p, nil
}
