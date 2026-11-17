package composition

import (
	"errors"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"strings"

	admin "github.com/jonathanagustin/conduit/internal/adminstatus/domain"
	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/modelhttp"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/packagingtools"
)

// aiProbe observes what this Hub process can run. It reads configuration and
// looks binaries up; it calls no model and runs no tool.
type aiProbe struct {
	getenv   func(string) string
	lookPath func(string) (string, error)
}

func hostAIProbe() aiProbe {
	return aiProbe{getenv: os.Getenv, lookPath: func(name string) (string, error) {
		if name == "zarf" {
			// Packaging resolves zarf through ZARF_BIN first.
			return packagingtools.LookZarf()
		}
		return exec.LookPath(name)
	}}
}

// components is the admin status view of the AI subsystem.
func (probe aiProbe) components() []admin.Component {
	return admin.AIComponents(probe.facts())
}

// facts never carries a credential. The endpoint is reduced to scheme and
// host, a command to its base name, and a configuration error to fixed words:
// the underlying error can quote a file path or a URL.
func (probe aiProbe) facts() admin.AIFacts {
	getenv := probe.getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	facts := admin.AIFacts{
		ProviderKind:         admin.ProviderNone,
		ConfiguredModel:      configuredModelName(getenv),
		Tools:                map[string]bool{},
		ImageBuilderAdmitted: strings.TrimSpace(getenv("CONDUIT_IMAGE_BUILDER")) == "docker",
		WorkspaceRootSet:     strings.TrimSpace(getenv("DEVWORKSPACE_GIT_ROOT")) != "",
	}
	for _, tool := range admin.AITools() {
		facts.Tools[tool] = probe.found(tool)
	}
	client, err := modelHTTPClient(getenv)
	switch {
	case err != nil:
		facts.ProviderKind, facts.ConfigError = admin.ProviderInvalid, modelConfigProblem(err)
	case client != nil:
		facts.ProviderKind, facts.ProviderSummary = admin.ProviderHTTP, endpointOrigin(getenv("CONDUIT_MODEL_BASE_URL"))
	default:
		raw := strings.TrimSpace(getenv("CONDUIT_MODEL_CMD"))
		if raw == "" {
			break
		}
		argv, parseErr := localcmd.ParseArgv(raw)
		if parseErr != nil {
			facts.ProviderKind, facts.ConfigError = admin.ProviderInvalid, "CONDUIT_MODEL_CMD is not a valid command line"
			break
		}
		name := localcmd.CommandName(argv[0])
		if !probe.found(argv[0]) {
			facts.ProviderKind, facts.ConfigError = admin.ProviderInvalid, "the model command "+name+" was not found"
			break
		}
		facts.ProviderKind, facts.ProviderSummary = admin.ProviderCLI, "command "+name
		if preset, ok := localcmd.PresetFromArgv(argv, getenv("CONDUIT_AGENT_PROVIDER")); ok {
			facts.ProviderSummary += " (agent-exec preset " + preset.Name + ", " + preset.Status() + ")"
		}
	}
	return facts
}

func (probe aiProbe) found(name string) bool {
	if probe.lookPath == nil {
		return false
	}
	_, err := probe.lookPath(name)
	return err == nil
}

// modelConfigProblem maps a configuration error onto fixed words.
func modelConfigProblem(err error) string {
	switch {
	case errors.Is(err, modelhttp.ErrBothConfigured):
		return "CONDUIT_MODEL_CMD and CONDUIT_MODEL_BASE_URL are both set"
	case errors.Is(err, modelhttp.ErrInsecureEndpoint):
		return "the model endpoint uses plain http without CONDUIT_MODEL_ALLOW_INSECURE_HTTP=true"
	case errors.Is(err, modelhttp.ErrKeyFile):
		return "the model API key file is not usable"
	case errors.Is(err, modelhttp.ErrInvalidConfig):
		return "the model HTTP configuration is not valid"
	default:
		return "the model configuration could not be read"
	}
}

// endpointOrigin is the scheme and host of a validated base URL. The path is
// dropped; a validated URL has no credentials, query or fragment to drop.
func endpointOrigin(base string) string {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil || parsed.Host == "" {
		return "endpoint"
	}
	return "endpoint " + strings.ToLower(parsed.Scheme) + "://" + parsed.Host
}

// logAICapabilities writes the same view to the startup log.
func logAICapabilities(components []admin.Component) {
	for _, component := range components {
		slog.Info("AI capability", "id", component.ID, "status", component.Status, "detail", component.Detail)
	}
}
