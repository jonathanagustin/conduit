// Package productconfiguration owns compatibility configuration document behavior.
package productconfiguration

import "fmt"

// GeneratedSecretsKey is the configuration block an override may not
// touch.
const GeneratedSecretsKey = "generated_secrets"

// MergeOverrides merges an installation's overrides onto a
// Product's configuration. Maps merge recursively; every other kind of value,
// lists included, is replaced wholesale, because merging two lists has no
// meaning the Product author could have predicted.
//
// generated_secrets is refused rather than merged. Overwriting a generated
// secret's declaration rotates the value under a running installation, which
// presents as an outage or as data encrypted under a key nobody holds.
func MergeOverrides(base, override map[string]any) (map[string]any, error) {
	if _, exists := override[GeneratedSecretsKey]; exists {
		return nil, fmt.Errorf("configuration override must not set %s", GeneratedSecretsKey)
	}
	return mergeMaps(base, override), nil
}

func mergeMaps(base, override map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(override))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range override {
		existing, exists := merged[key]
		if !exists {
			merged[key] = value
			continue
		}
		existingMap, existingIsMap := asStringMap(existing)
		valueMap, valueIsMap := asStringMap(value)
		if existingIsMap && valueIsMap {
			merged[key] = mergeMaps(existingMap, valueMap)
			continue
		}
		merged[key] = value
	}
	return merged
}

// asStringMap normalizes both map spellings a YAML decoder can produce, so
// a merge does not depend on which decoder built the tree.
func asStringMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case map[any]any:
		converted := make(map[string]any, len(typed))
		for key, child := range typed {
			converted[fmt.Sprint(key)] = child
		}
		return converted, true
	default:
		return nil, false
	}
}
