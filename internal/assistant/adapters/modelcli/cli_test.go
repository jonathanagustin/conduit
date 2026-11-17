package modelcli

import (
	"errors"
	"github.com/jonathanagustin/conduit/internal/assistant/domain"
	"github.com/jonathanagustin/conduit/internal/localcmd"
	"testing"
)

func TestLegacyRunnerRefusesHostExecution(t *testing.T) {
	_, _, err := (Runner{Argv: []string{"/bin/sh", "-c", "exit 0"}}).Complete(t.Context(), "prompt")
	if !errors.Is(err, localcmd.ErrLocalCommandUnsupported) || !errors.Is(err, domain.ErrModelUnavailable) {
		t.Fatalf("wrong refusal: %v", err)
	}
}
