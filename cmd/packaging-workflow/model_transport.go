package main

import (
	"fmt"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/modelhttp"
	packagingmodelhttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/modelhttp"
	packagingapp "github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
)

// modelTransport is the one model transport this process was configured
// with: an HTTP endpoint, or the explicit no-model path.
type modelTransport struct {
	client   *modelhttp.Client
	governor *modelgovernor.Governor
}

// workerGovernor builds the governor of a worker process. A worker has no
// audit log, no usage store and no operator: it applies the concurrency
// limits and the retry policy and counts calls in process, and nothing else.
// Budgets, per-call audit events and transcripts are the Hub's. Requested
// budgets or transcript retention are refused here: clearing those settings
// would let the worker call a model without enforcing the requested policy.
func workerGovernor(getenv func(string) string) (*modelgovernor.Governor, error) {
	config, err := modelgovernor.FromEnv(getenv)
	if err != nil {
		return nil, err
	}
	if config.DailyTokenBudget > 0 || config.DailyCallBudget > 0 || config.Transcripts {
		return nil, fmt.Errorf("%w: standalone packaging worker cannot enforce daily model budgets or retain transcripts; use Hub-governed model execution for these policies", modelgovernor.ErrInvalidConfig)
	}
	return modelgovernor.New(config, modelgovernor.Dependencies{}), nil
}

func (transport modelTransport) resolver() modelgovernor.Resolver {
	return func() *modelgovernor.Governor { return transport.governor }
}

// governedClient is the HTTP client with one capability's calls governed.
func (transport modelTransport) governedClient(capability modelgovernor.Capability) *modelhttp.Client {
	return transport.client.With(modelgovernor.HTTPMiddleware(capability, transport.resolver()))
}

// refuseBothModelTransports retains the shared startup validation hook. Every
// retired command setting is refused before a subcommand reads input.
func refuseBothModelTransports(getenv func(string) string) error {
	return modelhttp.RejectLocalCommands(getenv)
}

// resolveModelTransport selects HTTP or no model; command configuration is refused.
func resolveModelTransport(getenv func(string) string) (modelTransport, error) {
	client, err := modelhttp.FromEnv(getenv)
	if err != nil {
		return modelTransport{}, err
	}
	governor, err := workerGovernor(getenv)
	if err != nil {
		return modelTransport{}, err
	}
	return modelTransport{client: client, governor: governor}, nil
}

func (transport modelTransport) configured() bool {
	return transport.client != nil
}

// planner builds the planning turn runner for this transport.
func (transport modelTransport) planner(clone planner.CloneFunc) planner.Planner {
	if transport.client != nil {
		return planner.Planner{Model: planner.HTTPModel{Client: transport.governedClient(modelgovernor.CapabilityPlanning)}, Clone: clone}
	}
	return planner.Planner{Clone: clone}
}

// packageWriter builds the post-Accept package writer for this transport.
func (transport modelTransport) packageWriter() packagingapp.PackageWriter {
	if transport.client != nil {
		return packagingmodelhttp.Adapter{Client: transport.governedClient(modelgovernor.CapabilityPackaging)}
	}
	return nil
}
