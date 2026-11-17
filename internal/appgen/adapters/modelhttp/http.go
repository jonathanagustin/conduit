// Package modelhttp puts an OpenAI-compatible HTTP endpoint behind the app
// generator port. It does not approve a change.
package modelhttp

import (
	"context"
	"errors"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/appgen/domain"
	"github.com/jonathanagustin/conduit/internal/modelhttp"
)

// Adapter asks for one JSON spec in one chat completion request.
type Adapter struct {
	Client *modelhttp.Client
}

func (adapter Adapter) Generate(ctx context.Context, request domain.Request) (domain.Spec, domain.Provenance, error) {
	if adapter.Client == nil {
		return domain.Spec{}, domain.Provenance{}, domain.ErrModelUnavailable
	}
	prompt := domain.Prompt(request)
	provenance := domain.Provenance{Adapter: modelhttp.AdapterName, ConfiguredModel: adapter.Client.RequestedModel(), PromptSHA256: domain.Digest([]byte(prompt))}
	completion, err := adapter.Client.Complete(ctx, prompt)
	provenance.ProviderModel = completion.Model
	provenance.ResponseSHA256 = domain.Digest([]byte(completion.Text))
	if err != nil {
		return domain.Spec{}, provenance, modelError(err)
	}
	spec, err := domain.ParseSpec([]byte(completion.Text))
	if err != nil {
		return domain.Spec{}, provenance, err
	}
	return spec, provenance, nil
}

func modelError(err error) error {
	switch {
	case errors.Is(err, modelhttp.ErrTimeout):
		return fmt.Errorf("%w: %w", domain.ErrModelTimeout, err)
	case errors.Is(err, modelhttp.ErrNotConfigured):
		return fmt.Errorf("%w: %w", domain.ErrModelUnavailable, err)
	default:
		return fmt.Errorf("%w: %w", domain.ErrModelFailed, err)
	}
}
