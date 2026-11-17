// Package modelhttp puts an OpenAI-compatible HTTP endpoint behind the
// proposal port. It does not approve a change.
package modelhttp

import (
	"context"
	"errors"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/modelhttp"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	packagingskill "github.com/jonathanagustin/conduit/skills/packaging"
)

// Adapter asks for one JSON proposal in one chat completion request. Unlike
// the modelcli runner it carries no toolbox, so it adds no inspected image
// facts to the prompt.
type Adapter struct {
	Client *modelhttp.Client
}

func (adapter Adapter) Propose(ctx context.Context, input domain.PackagingContext) (domain.Proposal, domain.Provenance, error) {
	if adapter.Client == nil {
		return domain.Proposal{}, domain.Provenance{}, domain.ErrModelUnavailable
	}
	input = input.WithSkill(packagingskill.Source, packagingskill.SHA256())
	prompt := domain.Prompt(input)
	provenance := domain.Provenance{Adapter: modelhttp.AdapterName, PromptSHA256: domain.DigestText([]byte(prompt))}.WithSkillDigest(input)
	completion, err := adapter.Client.Complete(ctx, prompt)
	provenance.ResponseSHA256 = domain.DigestText([]byte(completion.Text))
	if err != nil {
		return domain.Proposal{}, provenance, modelError(err)
	}
	proposal, err := domain.ParseProposal([]byte(completion.Text))
	if err != nil {
		return domain.Proposal{}, provenance, err
	}
	return proposal, provenance, nil
}

// ReportedModel is the model id from the most recent response. The port has
// no field for it.
func (adapter Adapter) ReportedModel() string { return adapter.Client.ReportedModel() }

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
