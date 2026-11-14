// Package recorded is the proposal fallback. It returns a stored proposal
// or reports that no local command is configured. It does not start a process.
package recorded

import (
	"context"
	"errors"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// Adapter is the recorded fallback. It does not start a process.
type Adapter struct {
	proposal domain.Proposal
	ready    bool
}

// Unavailable is the fallback when no local command is configured.
func Unavailable() Adapter { return Adapter{} }

// Fixed returns one recorded proposal. Tests use it in place of a process.
func Fixed(proposal domain.Proposal) Adapter {
	return Adapter{proposal: proposal, ready: true}
}

func (adapter Adapter) Propose(ctx context.Context, input domain.PackagingContext) (domain.Proposal, domain.Provenance, error) {
	if ctx.Err() != nil {
		return domain.Proposal{}, domain.Provenance{}, ctx.Err()
	}
	if !adapter.ready {
		return domain.Proposal{}, domain.Provenance{Adapter: "recorded"}, domain.ErrModelUnavailable
	}
	prompt := []byte(domain.Prompt(input))
	raw, err := canonical(adapter.proposal)
	if err != nil {
		return domain.Proposal{}, domain.Provenance{}, err
	}
	return adapter.proposal, domain.Provenance{
		Adapter: "recorded", PromptSHA256: domain.DigestText(prompt), ResponseSHA256: domain.DigestText(raw),
	}, nil
}

func canonical(proposal domain.Proposal) ([]byte, error) {
	if proposal.Field == "" {
		return nil, errors.New("recorded proposal is empty")
	}
	return []byte(proposal.Field + "\n" + proposal.Value + "\n" + proposal.Rationale), nil
}
