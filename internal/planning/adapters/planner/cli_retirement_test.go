package planner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/localcmd"
)

func TestRetiredCLIModelRefusesExecutor(t *testing.T) {
	for _, injected := range []bool{false, true} {
		name := "nil executor"
		if injected {
			name = "side effect executor"
		}
		t.Run(name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "called")
			called := false
			var executor localcmd.ExecFunc
			if injected {
				executor = func(context.Context, []string, time.Duration, []byte) ([]byte, error) {
					called = true
					err := os.WriteFile(marker, []byte("executor was invoked"), 0600)
					return []byte("private model reply"), err
				}
			}
			model := CLIModel{Argv: []string{"legacy-model"}, Timeout: time.Second, Name: "legacy", Exec: executor}
			reply, err := model.Complete(t.Context(), "private prompt")
			_, sideEffect := os.Stat(marker)
			if called || !errors.Is(sideEffect, os.ErrNotExist) {
				t.Fatal("retired planner invoked injected executor")
			}
			if !errors.Is(err, localcmd.ErrLocalCommandUnsupported) {
				t.Fatalf("retired planner refusal = %v", err)
			}
			if len(reply.Text) != 0 || reply.ProviderModel != "" {
				t.Fatal("retired planner returned a model reply")
			}
		})
	}
}
