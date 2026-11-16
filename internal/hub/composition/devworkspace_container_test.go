package composition

import (
	"errors"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	devdomain "github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

func TestContainerRuntimeWithoutGitHostDoesNotStartHostProcess(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	getenv := func(key string) string {
		if key == "DEVWORKSPACE_RUNTIME" {
			return "container"
		}
		return ""
	}
	spaces := assembleDevWorkspace(repository, controlplane.HandlerOptions{}, getenv)
	workspace, err := spaces.Create(t.Context(), devdomain.Caller{Username: "alice"}, "profile", "dev", "isolated")
	if err != nil {
		t.Fatal(err)
	}
	_, err = spaces.StartRuntime(t.Context(), devdomain.Caller{Username: "alice"}, workspace.ID)
	if !errors.Is(err, devdomain.ErrDisabled) {
		t.Fatalf("start without egress: %v", err)
	}
}
