package composition

import (
	"strings"

	"github.com/jonathanagustin/conduit/internal/modelhttp"
)

// errModelBothConfigured is the refusal to choose between two model
// transports. Startup fails rather than guessing which one was meant.
var errModelBothConfigured = modelhttp.ErrBothConfigured

// modelHTTPClient reads the HTTP model configuration. It returns nil with no
// error when CONDUIT_MODEL_BASE_URL is unset, which leaves every caller on
// its existing path. The rule is shared with the packaging-workflow binary.
func modelHTTPClient(getenv func(string) string) (*modelhttp.Client, error) {
	return modelhttp.FromEnv(getenv)
}

// configuredModelName is the operator configuration, never model-authored text.
func configuredModelName(getenv func(string) string) string {
	return strings.TrimSpace(getenv("CONDUIT_MODEL_NAME"))
}
