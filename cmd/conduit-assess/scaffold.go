package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/packaging/domain"
	"io"
)

func scaffold(in io.Reader, out io.Writer) error {
	b, err := io.ReadAll(io.LimitReader(in, maxInput+1))
	if err != nil {
		return err
	}
	if len(b) > maxInput {
		return fmt.Errorf("scaffold input exceeds limit")
	}
	if err := rejectDuplicateKeys(b, "profile", "name", "version", "sourceRevision", "sourceSHA256", "image", "port", "uid", "cpuMilli", "memoryMiB", "zarfVersion", "udsCommit"); err != nil {
		return err
	}
	var input domain.ScaffoldInput
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&input); err != nil {
		return err
	}
	result, err := domain.GenerateScaffold(input)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result)
}
