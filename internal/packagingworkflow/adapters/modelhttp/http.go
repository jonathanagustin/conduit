// Package modelhttp puts an OpenAI-compatible HTTP endpoint behind the
// proposal port and the package-writer port. It does not approve a change.
package modelhttp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jonathanagustin/conduit/internal/modelhttp"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/modelcli"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	packagingskill "github.com/jonathanagustin/conduit/skills/packaging"
)

// Adapter asks for one JSON proposal in one chat completion request. The
// prompt is the same text the modelcli runner writes to stdin, including the
// inspected image facts when Tools is set.
type Adapter struct {
	Client *modelhttp.Client
	Tools  domain.Toolbox
}

func (adapter Adapter) Propose(ctx context.Context, input domain.PackagingContext) (domain.Proposal, domain.Provenance, error) {
	text, provenance, err := adapter.complete(ctx, input)
	if err != nil {
		return domain.Proposal{}, provenance, err
	}
	proposal, err := domain.ParseProposal([]byte(text))
	if err != nil {
		return domain.Proposal{}, provenance, err
	}
	return proposal, provenance, nil
}

// ProposePackage asks for the Dockerfile or package files after Accept. It
// is the package-writer port.
func (adapter Adapter) ProposePackage(ctx context.Context, input domain.PackagingContext) (domain.PackageProposal, domain.Provenance, error) {
	if strings.TrimSpace(input.Task) == "" {
		input.Task = domain.TaskPackage
	}
	text, provenance, err := adapter.complete(ctx, input)
	if err != nil {
		return domain.PackageProposal{}, provenance, err
	}
	proposal, err := domain.ParsePackageProposal([]byte(text))
	if err != nil {
		return domain.PackageProposal{}, provenance, err
	}
	return proposal, provenance, nil
}

// complete sends one prompt. The provenance names the configured model and
// the id the provider reported for this response, never the reply's claim.
func (adapter Adapter) complete(ctx context.Context, input domain.PackagingContext) (string, domain.Provenance, error) {
	if adapter.Client == nil {
		return "", domain.Provenance{}, domain.ErrModelUnavailable
	}
	input = input.WithSkill(packagingskill.Source, packagingskill.SHA256())
	calls := modelcli.InspectFacts(ctx, adapter.Tools, &input)
	prompt := domain.Prompt(input)
	provenance := domain.Provenance{
		Adapter: modelhttp.AdapterName, ConfiguredModel: adapter.Client.RequestedModel(),
		PromptSHA256: domain.DigestText([]byte(prompt)), ToolCalls: calls,
	}.WithSkillDigest(input)
	completion, err := adapter.Client.Complete(ctx, prompt)
	provenance.ResponseSHA256 = domain.DigestText([]byte(completion.Text))
	provenance.ProviderModel = completion.Model
	if err != nil {
		return "", provenance, modelError(err)
	}
	return completion.Text, provenance, nil
}

// ReportedModel is the model id from the most recent response. Each call's
// own id is in its provenance.
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
