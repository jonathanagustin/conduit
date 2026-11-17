// Package modelcli retains the retired command adapter as an explicit refusal.
package modelcli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jonathanagustin/conduit/internal/assistant/domain"
	"github.com/jonathanagustin/conduit/internal/localcmd"
)

// Runner is a compatibility shape; model methods refuse without calling tools or processes.
type Runner struct {
	ModelName string
	Argv      []string
	Timeout   time.Duration
	// Exec is retained for source compatibility and is never invoked.
	Exec localcmd.ExecFunc
}

func (runner Runner) Complete(ctx context.Context, prompt string) (string, domain.Provenance, error) {
	return "", domain.Provenance{}, modelError(localcmd.ErrLocalCommandUnsupported)
}

func modelError(err error) error {
	switch {
	case errors.Is(err, localcmd.ErrLocalCommandUnsupported):
		return fmt.Errorf("%w: %w", domain.ErrModelUnavailable, err)
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
