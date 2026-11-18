// Package modelhttp puts an OpenAI-compatible HTTP endpoint behind the
// assistant model port. It does not approve a change.
package modelhttp

import (
	"context"
	"errors"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/assistant/domain"
	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/modelhttp"
)

// Adapter sends the prompt as one chat completion request.
type Adapter struct {
	Client *modelhttp.Client
}

func (adapter Adapter) Complete(ctx context.Context, prompt string) (string, domain.Provenance, error) {
	if adapter.Client == nil {
		return "", domain.Provenance{}, domain.ErrModelUnavailable
	}
	provenance := domain.Provenance{Adapter: modelhttp.AdapterName, ConfiguredModel: adapter.Client.RequestedModel(), PromptSHA256: domain.Digest([]byte(prompt))}
	completion, err := adapter.Client.Complete(ctx, prompt)
	if completion.PromptTokens != nil || completion.CompletionTokens != nil {
		provenance.Usage = (&domain.ReportedTokenUsage{PromptTokens: completion.PromptTokens, CompletionTokens: completion.CompletionTokens}).Clone()
	}
	provenance.ProviderModel = completion.Model
	provenance.ResponseSHA256 = domain.Digest([]byte(completion.Text))
	if err != nil {
		return "", provenance, modelError(err)
	}
	return completion.Text, provenance, nil
}

func modelError(err error) error {
	switch {
	case errors.Is(err, modelgovernor.ErrBusy):
		return fmt.Errorf("%w: %w", domain.ErrModelBusy, err)
	case errors.Is(err, modelgovernor.ErrBudgetExceeded):
		return fmt.Errorf("%w: %w", domain.ErrModelBudgetExceeded, err)
	case errors.Is(err, modelgovernor.ErrAuditUnavailable):
		return fmt.Errorf("%w: %w", domain.ErrAuditUnavailable, err)
	case errors.Is(err, modelhttp.ErrTimeout):
		return fmt.Errorf("%w: %w", domain.ErrModelTimeout, err)
	case errors.Is(err, modelhttp.ErrNotConfigured):
		return fmt.Errorf("%w: %w", domain.ErrModelUnavailable, err)
	default:
		return fmt.Errorf("%w: %w", domain.ErrModelFailed, err)
	}
}
