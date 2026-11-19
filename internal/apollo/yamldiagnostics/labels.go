// Package yamldiagnostics preserves compatibility error labels during type extraction.
package yamldiagnostics

import (
	"errors"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Rewrite copies structured YAML load errors and rewrites only terminal type
// labels. Positions, context, formatter and underlying causes remain intact.
func Rewrite(err error, fromPackage, toPackage string, typeNames map[string]string) error {
	var typed *yaml.LoadErrors
	if !errors.As(err, &typed) {
		return err
	}
	result := &yaml.LoadErrors{Errors: make([]*yaml.LoadError, len(typed.Errors))}
	for i, detail := range typed.Errors {
		copy := *detail
		result.Errors[i] = &copy
		message := detail.Message
		for current, legacy := range typeNames {
			for _, diagnostic := range []string{" in type ", " into "} {
				for _, wrapper := range []string{"", "*", "[]", "map[string]"} {
					prefix := diagnostic + wrapper
					suffix := prefix + fromPackage + "." + current
					if strings.HasSuffix(message, suffix) {
						copy.Message = strings.TrimSuffix(message, suffix) + prefix + toPackage + "." + legacy
					}
				}
			}
		}
	}
	return result
}
