package conduitcli

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
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

// writeOutput renders in the formats Conduit has: json, yaml, csv, and table.
//
// It used to accept a name, `pretty`, in the same branch as `yaml`, and --help
// advertised "json, yaml, or pretty". The two names produced byte-identical
// bytes, so an operator who chose `pretty` for a human-oriented rendering got
// the machine format and nothing said the choice had been ignored. Naming the
// formats and meaning them beats advertising one that does not exist; `pretty`
// is now refused by the same branch that refuses any other unknown format,
// which says so.
func writeOutput(writer io.Writer, format string, value any) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	case "csv":
		marshaler, ok := value.(CSVMarshaler)
		if !ok {
			return fmt.Errorf("output format csv is not available for this command; use json or yaml")
		}
		rows, err := marshaler.MarshalCSV()
		if err != nil {
			return fmt.Errorf("encode csv output: %w", err)
		}
		csvWriter := csv.NewWriter(writer)
		if err := csvWriter.WriteAll(rows); err != nil {
			return fmt.Errorf("write csv output: %w", err)
		}
		return csvWriter.Error()
	case "table":
		marshaler, ok := value.(CSVMarshaler)
		if !ok {
			return fmt.Errorf("output format table is not available for this command; use json or yaml")
		}
		rows, err := marshaler.MarshalCSV()
		if err != nil {
			return fmt.Errorf("encode table output: %w", err)
		}
		return writeAlignedTable(writer, rows)
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
		return fmt.Errorf("unsupported output format %q; use json, yaml, csv, or table", format)
	}
}

// CSVMarshaler is implemented by a command result that can render itself as a
// table. writeOutput only offers "csv" for values that implement it: a generic
// map has no column order, so serializing one as CSV would invent a shape
// rather than report it. A command with no honest tabular form does not opt in,
// and asking for csv there is refused by the same branch that refuses any other
// format the command cannot produce.
type CSVMarshaler interface {
	MarshalCSV() ([][]string, error)
}

func writeAlignedTable(writer io.Writer, rows [][]string) error {
	if len(rows) == 0 {
		return nil
	}
	width := len(rows[0])
	widths := make([]int, width)
	sanitized := make([][]string, len(rows))
	for rowIndex, row := range rows {
		if len(row) != width {
			return errors.New("table rows must have the same number of columns")
		}
		sanitized[rowIndex] = make([]string, width)
		for column, cell := range row {
			cell = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(cell)
			sanitized[rowIndex][column] = cell
			if count := utf8.RuneCountInString(cell); count > widths[column] {
				widths[column] = count
			}
		}
	}
	for rowIndex, row := range sanitized {
		if err := writeTableRow(writer, row, widths); err != nil {
			return err
		}
		if rowIndex == 0 {
			rule := make([]string, width)
			for column, size := range widths {
				if size == 0 {
					size = 1
				}
				rule[column] = strings.Repeat("-", size)
			}
			if err := writeTableRow(writer, rule, widths); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeTableRow(writer io.Writer, row []string, widths []int) error {
	var builder strings.Builder
	for column, cell := range row {
		if column > 0 {
			builder.WriteString("  ")
		}
		builder.WriteString(cell)
		if padding := widths[column] - utf8.RuneCountInString(cell); padding > 0 {
			builder.WriteString(strings.Repeat(" ", padding))
		}
	}
	builder.WriteByte('\n')
	_, err := io.WriteString(writer, builder.String())
	return err
}
