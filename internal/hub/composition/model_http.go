package composition

import (
	"log/slog"
	"strings"

	"github.com/jonathanagustin/conduit/internal/localcmd"
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

// configuredModelName is CONDUIT_MODEL_NAME. For a model CLI it is the only
// source of the model name that Accept binds; the CLI's own reply is not one.
func configuredModelName(getenv func(string) string) string {
	return strings.TrimSpace(getenv("CONDUIT_MODEL_NAME"))
}

// warnUnverifiedPreset logs once at startup when CONDUIT_MODEL_CMD selects an
// agent-exec provider preset with no recorded conformance run.
func warnUnverifiedPreset(argv []string, getenv func(string) string) {
	preset, ok := localcmd.PresetFromArgv(argv, getenv("CONDUIT_AGENT_PROVIDER"))
	if !ok || preset.Verified {
		return
	}
	slog.Warn("CONDUIT_MODEL_CMD selects an UNVERIFIED agent-exec provider preset",
		"provider", preset.Name, "status", preset.Status(), "evidence", preset.Evidence)
}
