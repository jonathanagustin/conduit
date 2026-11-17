// Package modelconfig selects operator configuration for a model capability.
// It makes no model call and accepts no model-authored configuration.
package modelconfig

import "strings"

const (
	Assistant = "ASSISTANT"
	Planning  = "PLANNING"
	Packaging = "PACKAGING"
	Appgen    = "APPGEN"
)

// ForCapability overlays CONDUIT_<CAPABILITY>_MODEL_NAME, _CMD and _TIMEOUT
// on their global counterparts. An explicit command selects CLI execution for
// this capability, so it shadows a globally configured HTTP endpoint. Names
// alone select a model on the existing HTTP endpoint; a CLI's model is selected
// by its argv, and its configured name remains the operator's assertion.
//
// Endpoint and credential overrides are deliberately absent: inheriting a key
// while replacing its endpoint would send that key to a different service.
// CONDUIT-INVENTED: modelconfig.CapabilitySelection
// Displaced-by: an adopted versioned capability-selection and model-provenance contract replacing these native configuration keys and Accept fields.
func ForCapability(getenv func(string) string, capability string) func(string) string {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	prefix := "CONDUIT_" + capability + "_MODEL_"
	command := strings.TrimSpace(getenv(prefix + "CMD"))
	return func(name string) string {
		switch name {
		case "CONDUIT_MODEL_BASE_URL":
			if command != "" {
				return ""
			}
		case "CONDUIT_MODEL_NAME", "CONDUIT_MODEL_CMD", "CONDUIT_MODEL_TIMEOUT":
			if value := strings.TrimSpace(getenv(prefix + strings.TrimPrefix(name, "CONDUIT_MODEL_"))); value != "" {
				return value
			}
		}
		return getenv(name)
	}
}
