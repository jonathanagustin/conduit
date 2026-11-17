package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/modelhttp"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/modelcli"
	packagingmodelhttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/modelhttp"
	packagingapp "github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
)

const workerModelTimeout = 45 * time.Second

// modelTransport is the one model transport this process was configured
// with: an HTTP endpoint, a local command, or neither.
type modelTransport struct {
	client *modelhttp.Client
	argv   []string
	// name is CONDUIT_MODEL_NAME, the only source of a model CLI's name.
	name string
	// governor bounds and retries this process's model calls. See
	// workerGovernor for what it does not do here.
	governor *modelgovernor.Governor
}

// workerGovernor builds the governor of a worker process. A worker has no
// audit log, no usage store and no operator: it applies the concurrency
// limits and the retry policy and counts calls in process, and nothing else.
// Budgets, per-call audit events and transcripts are the Hub's; the settings
// for them are read and deliberately not applied, so a worker never refuses
// a call for want of a store it was never given.
func workerGovernor(getenv func(string) string) (*modelgovernor.Governor, error) {
	config, err := modelgovernor.FromEnv(getenv)
	if err != nil {
		return nil, err
	}
	config.DailyTokenBudget, config.DailyCallBudget, config.Transcripts = 0, 0, false
	return modelgovernor.New(config, modelgovernor.Dependencies{}), nil
}

func (transport modelTransport) resolver() modelgovernor.Resolver {
	return func() *modelgovernor.Governor { return transport.governor }
}

// governedClient is the HTTP client with one capability's calls governed.
func (transport modelTransport) governedClient(capability modelgovernor.Capability) *modelhttp.Client {
	return transport.client.With(modelgovernor.HTTPMiddleware(capability, transport.resolver()))
}

func (transport modelTransport) governedExec(capability modelgovernor.Capability) localcmd.ExecFunc {
	return modelgovernor.CLIExec(capability, transport.name, transport.resolver())
}

// refuseBothModelTransports is the Hub's precedence rule, applied by every
// subcommand before it does anything: both transports set is an error.
func refuseBothModelTransports(getenv func(string) string) error {
	if strings.TrimSpace(getenv("CONDUIT_MODEL_CMD")) != "" && strings.TrimSpace(getenv("CONDUIT_MODEL_BASE_URL")) != "" {
		return modelhttp.ErrBothConfigured
	}
	return nil
}

// resolveModelTransport reads the model configuration with the same rule the
// Hub uses: CONDUIT_MODEL_BASE_URL selects HTTP, CONDUIT_MODEL_CMD selects a
// local command, both set is an error, and neither leaves the caller on its
// no-model path.
func resolveModelTransport(getenv func(string) string) (modelTransport, error) {
	client, err := modelhttp.FromEnv(getenv)
	if err != nil {
		return modelTransport{}, err
	}
	governor, err := workerGovernor(getenv)
	if err != nil {
		return modelTransport{}, err
	}
	if client != nil {
		return modelTransport{client: client, governor: governor}, nil
	}
	raw := strings.TrimSpace(getenv("CONDUIT_MODEL_CMD"))
	if raw == "" {
		return modelTransport{}, nil
	}
	argv, err := localcmd.ParseArgv(raw)
	if err != nil {
		return modelTransport{}, fmt.Errorf("CONDUIT_MODEL_CMD: %w", err)
	}
	warnUnverifiedPreset(argv, getenv("CONDUIT_AGENT_PROVIDER"))
	return modelTransport{argv: argv, name: strings.TrimSpace(getenv("CONDUIT_MODEL_NAME")), governor: governor}, nil
}

func (transport modelTransport) configured() bool {
	return transport.client != nil || len(transport.argv) > 0
}

// planner builds the planning turn runner for this transport.
func (transport modelTransport) planner(clone planner.CloneFunc) planner.Planner {
	if transport.client != nil {
		return planner.Planner{Model: planner.HTTPModel{Client: transport.governedClient(modelgovernor.CapabilityPlanning)}, Clone: clone}
	}
	return planner.Planner{
		Argv: transport.argv, Timeout: workerModelTimeout, ModelName: transport.name, Clone: clone,
		Exec: transport.governedExec(modelgovernor.CapabilityPlanning),
	}
}

// packageWriter builds the post-Accept package writer for this transport.
func (transport modelTransport) packageWriter() packagingapp.PackageWriter {
	if transport.client != nil {
		return packagingmodelhttp.Adapter{Client: transport.governedClient(modelgovernor.CapabilityPackaging)}
	}
	return modelcli.Runner{
		Argv: transport.argv, Timeout: workerModelTimeout, ModelName: transport.name,
		Exec: transport.governedExec(modelgovernor.CapabilityPackaging),
	}
}

// warnUnverifiedPreset logs to stderr when the model command selects an
// agent-exec provider preset with no recorded conformance run.
func warnUnverifiedPreset(argv []string, providerEnv string) {
	preset, ok := localcmd.PresetFromArgv(argv, providerEnv)
	if !ok || preset.Verified {
		return
	}
	warnPreset(preset)
}

func warnPreset(preset localcmd.Preset) {
	slog.New(slog.NewTextHandler(os.Stderr, nil)).Warn(
		"agent-exec provider preset is UNVERIFIED",
		"provider", preset.Name, "status", preset.Status(), "evidence", preset.Evidence)
}
