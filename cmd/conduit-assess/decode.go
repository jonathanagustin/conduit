package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/packaging/domain"
	"io"
)

func decodeAndReview(data []byte, out io.Writer) error {
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var a domain.Assessment
	if err := dec.Decode(&a); err != nil {
		return fmt.Errorf("decode assessment: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("assessment must contain exactly one JSON object")
	}
	r, err := domain.ReviewAssessment(a)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
