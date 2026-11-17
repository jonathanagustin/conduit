package main

import (
	"github.com/jonathanagustin/conduit/internal/modelhttp"
	packagingmodelhttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/modelhttp"
	packagingapp "github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
)

// modelTransport is the one model transport this process was configured
// with: an HTTP endpoint, or the explicit no-model path.
type modelTransport struct{ client *modelhttp.Client }

// refuseBothModelTransports retains the shared startup validation hook. Every
// retired command setting is refused before a subcommand reads input.
func refuseBothModelTransports(getenv func(string) string) error {
	return modelhttp.RejectLocalCommands(getenv)
}

// resolveModelTransport selects HTTP or no model; command configuration is refused.
func resolveModelTransport(getenv func(string) string) (modelTransport, error) {
	client, err := modelhttp.FromEnv(getenv)
	return modelTransport{client: client}, err
}

func (transport modelTransport) configured() bool {
	return transport.client != nil
}

// planner builds the planning turn runner for this transport.
func (transport modelTransport) planner(clone planner.CloneFunc) planner.Planner {
	if transport.client != nil {
		return planner.Planner{Model: planner.HTTPModel{Client: transport.client}, Clone: clone}
	}
	return planner.Planner{Clone: clone}
}

// packageWriter builds the post-Accept package writer for this transport.
func (transport modelTransport) packageWriter() packagingapp.PackageWriter {
	if transport.client != nil {
		return packagingmodelhttp.Adapter{Client: transport.client}
	}
	return nil
}
