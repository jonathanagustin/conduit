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

// ForCapability overlays capability NAME and TIMEOUT on the shared HTTP model
// configuration. Legacy CMD values are retained only so transport validation
// can refuse them explicitly; they never select execution or hide an endpoint.
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
	return func(name string) string {
		switch name {
		case "CONDUIT_MODEL_NAME", "CONDUIT_MODEL_CMD", "CONDUIT_MODEL_TIMEOUT":
			if value := strings.TrimSpace(getenv(prefix + strings.TrimPrefix(name, "CONDUIT_MODEL_"))); value != "" {
				return value
			}
		}
		return getenv(name)
	}
}
