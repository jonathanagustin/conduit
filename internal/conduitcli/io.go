package conduitcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

const maxDocumentBytes = 1 << 20

func decodeDocument(path string, output any) error {
	var input io.ReadCloser
	if path == "-" {
		input = io.NopCloser(os.Stdin)
	} else {
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open input document: %w", err)
		}
		input = file
	}
	defer input.Close()

	contents, err := io.ReadAll(io.LimitReader(input, maxDocumentBytes+1))
	if err != nil {
		return fmt.Errorf("read input document: %w", err)
	}
	if len(contents) > maxDocumentBytes {
		return fmt.Errorf("input document exceeds %d bytes", maxDocumentBytes)
	}
	var generic any
	decoder := yaml.NewDecoder(strings.NewReader(string(contents)))
	if err := decoder.Decode(&generic); err != nil {
		return fmt.Errorf("decode input document: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("input must contain exactly one YAML or JSON document")
	}
	normalizedJSON, err := json.Marshal(generic)
	if err != nil {
		return fmt.Errorf("normalize input document: %w", err)
	}
	strict := json.NewDecoder(strings.NewReader(string(normalizedJSON)))
	strict.DisallowUnknownFields()
	if err := strict.Decode(output); err != nil {
		return fmt.Errorf("decode input document: %w", err)
	}
	return nil
}

// writeOutput renders in the two formats Conduit has.
//
// It used to accept a third name, `pretty`, in the same branch as `yaml`, and
// --help advertised "json, yaml, or pretty". The two names produced
// byte-identical bytes, so an operator who chose `pretty` for a human-oriented
// rendering got the machine format and nothing said the choice had been
// ignored. Naming two formats and meaning them beats advertising a third that
// does not exist; `pretty` is now refused by the same branch that refuses any
// other unknown format, which says so.
func writeOutput(writer io.Writer, format string, value any) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	case "yaml":
		jsonValue, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode output: %w", err)
		}
		var generic any
		if err := json.Unmarshal(jsonValue, &generic); err != nil {
			return fmt.Errorf("normalize output: %w", err)
		}
		encoder := yaml.NewEncoder(writer)
		encoder.SetIndent(2)
		defer encoder.Close()
		return encoder.Encode(generic)
	default:
		return fmt.Errorf("unsupported output format %q; use json or yaml", format)
	}
}
