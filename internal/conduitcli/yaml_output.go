package conduitcli

import (
	"encoding/json"
	"strings"

	"go.yaml.in/yaml/v4"
)

// JSON normalization keeps the existing field-name and custom MarshalJSON
// contract. Tag each number's original literal instead of rounding through a
// machine float or letting the YAML encoder mistake json.Number for a string.
type exactYAMLNumber string

func (number exactYAMLNumber) MarshalYAML() (any, error) {
	tag := "!!int"
	if strings.ContainsAny(string(number), ".eE") || number == "-0" {
		tag = "!!float"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: string(number)}, nil
}

func exactYAMLNumbers(value any) any {
	switch typed := value.(type) {
	case json.Number:
		return exactYAMLNumber(typed.String())
	case map[string]any:
		for key, child := range typed {
			typed[key] = exactYAMLNumbers(child)
		}
	case []any:
		for index, child := range typed {
			typed[index] = exactYAMLNumbers(child)
		}
	}
	return value
}
