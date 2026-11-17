// Package modelcli runs a local command behind the assistant model port.
// It does not interpret a vendor protocol and it does not approve a change.
package modelcli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/assistant/domain"
	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/modelgovernor"
)

// Runner starts one local command. The prompt is written to stdin.
// The process working directory is temporary and is removed afterwards.
type Runner struct {
	ModelName string
	Argv      []string
	Timeout   time.Duration
	// Exec wraps the run; nil runs the command directly.
	Exec localcmd.ExecFunc
}

func (runner Runner) Complete(ctx context.Context, prompt string) (string, domain.Provenance, error) {
	if len(runner.Argv) == 0 || strings.TrimSpace(runner.Argv[0]) == "" {
		return "", domain.Provenance{}, domain.ErrModelUnavailable
	}
	body := []byte(prompt)
	provenance := domain.Provenance{Adapter: localcmd.CommandName(runner.Argv[0]), ConfiguredModel: strings.TrimSpace(runner.ModelName), PromptSHA256: domain.Digest(body)}
	stdout, err := runner.Exec.Run(ctx, runner.Argv, runner.Timeout, body)
	provenance.ResponseSHA256 = domain.Digest(stdout)
	if err != nil {
		return "", provenance, modelError(err)
	}
	return string(stdout), provenance, nil
}

func modelError(err error) error {
	switch {
	case errors.Is(err, modelgovernor.ErrBusy):
		return fmt.Errorf("%w: %w", domain.ErrModelBusy, err)
	case errors.Is(err, modelgovernor.ErrBudgetExceeded):
		return fmt.Errorf("%w: %w", domain.ErrModelBudgetExceeded, err)
	case errors.Is(err, modelgovernor.ErrAuditUnavailable):
		return fmt.Errorf("%w: %w", domain.ErrAuditUnavailable, err)
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
