package domain

import (
	"bytes"
	"encoding/json"
	"io"
)

// DecodePrepared refuses unknown storage fields, invalid content hashes and
// mismatched index keys. A broken record is never an observed absence.
func DecodePrepared(coordinate string, document []byte) (Prepared, error) {
	if len(document) > MaxDocumentBytes {
		return Prepared{}, ErrCorrupt
	}
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	var record Prepared
	if err := decoder.Decode(&record); err != nil {
		return Prepared{}, ErrCorrupt
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Prepared{}, ErrCorrupt
	}
	if err := record.Validate(); err != nil || record.Manifest.Coordinate() != coordinate {
		return Prepared{}, ErrCorrupt
	}
	return record, nil
}
