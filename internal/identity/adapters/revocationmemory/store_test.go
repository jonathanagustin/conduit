package revocationmemory_test

import (
	"testing"

	"github.com/jonathanagustin/conduit/internal/identity/adapters/revocationmemory"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/revocationtest"
	"github.com/jonathanagustin/conduit/internal/identity/ports"
)

func TestMemoryStoreConformsToTheAgentCredentialPort(t *testing.T) {
	revocationtest.Run(t, func(*testing.T) ports.AgentCredentialStore { return revocationmemory.New() })
}
