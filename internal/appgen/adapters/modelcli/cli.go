// Package modelcli runs a local command behind the app generator port.
// It does not interpret a vendor protocol and it does not approve a change.
package modelcli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/appgen/domain"
	"github.com/jonathanagustin/conduit/internal/localcmd"
)

// Runner starts one local command. The prompt is written to stdin.
type Runner struct {
	ModelName string
	Argv      []string
	Timeout   time.Duration
}

func (runner Runner) Generate(ctx context.Context, request domain.Request) (domain.Spec, domain.Provenance, error) {
	if len(runner.Argv) == 0 || strings.TrimSpace(runner.Argv[0]) == "" {
		return domain.Spec{}, domain.Provenance{}, domain.ErrModelUnavailable
	}
	prompt := []byte(domain.Prompt(request))
	provenance := domain.Provenance{Adapter: localcmd.CommandName(runner.Argv[0]), ConfiguredModel: strings.TrimSpace(runner.ModelName), PromptSHA256: domain.Digest(prompt)}
	stdout, err := localcmd.Execute(ctx, runner.Argv, runner.Timeout, prompt)
	provenance.ResponseSHA256 = domain.Digest(stdout)
	if err != nil {
		return domain.Spec{}, provenance, modelError(err)
	}
	spec, err := domain.ParseSpec(stdout)
	if err != nil {
		return domain.Spec{}, provenance, err
	}
	return spec, provenance, nil
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
