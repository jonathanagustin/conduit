// Package modelcli runs a local command behind the proposal port.
// It does not interpret a vendor protocol and it does not approve a change.
package modelcli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// DefaultArgv is the documented local command. It is not started unless
// CONDUIT_MODEL_CMD is set. The last argument reads the prompt from stdin.
func DefaultArgv() []string {
	return []string{"codex", "exec", "--skip-git-repo-check", "--sandbox", "read-only", "--ephemeral", "-"}
}

// Runner starts one local command. The prompt is written to stdin. The
// process working directory is temporary and is removed afterwards.
type Runner struct {
	Argv    []string
	Timeout time.Duration
}

func (runner Runner) Propose(ctx context.Context, input domain.PackagingContext) (domain.Proposal, domain.Provenance, error) {
	if len(runner.Argv) == 0 || strings.TrimSpace(runner.Argv[0]) == "" {
		return domain.Proposal{}, domain.Provenance{}, domain.ErrModelUnavailable
	}
	prompt := []byte(domain.Prompt(input))
	provenance := domain.Provenance{Adapter: localcmd.CommandName(runner.Argv[0]), PromptSHA256: domain.DigestText(prompt)}
	stdout, err := localcmd.Execute(ctx, runner.Argv, runner.Timeout, prompt)
	provenance.ResponseSHA256 = domain.DigestText(stdout)
	if err != nil {
		return domain.Proposal{}, provenance, modelError(err)
	}
	proposal, err := domain.ParseProposal(stdout)
	if err != nil {
		return domain.Proposal{}, provenance, err
	}
	return proposal, provenance, nil
}

func modelError(err error) error {
	switch {
	case errors.Is(err, localcmd.ErrTimeout):
		return domain.ErrModelTimeout
	case errors.Is(err, localcmd.ErrNotFound), errors.Is(err, localcmd.ErrEmpty):
		return fmt.Errorf("%w: %v", domain.ErrModelUnavailable, err)
	default:
		return fmt.Errorf("%w: %v", domain.ErrModelFailed, err)
	}
}

// ParseArgv splits a command line on spaces. Double quotes group one
// argument. There is no shell expansion.
func ParseArgv(line string) ([]string, error) {
	return localcmd.ParseArgv(line)
}
