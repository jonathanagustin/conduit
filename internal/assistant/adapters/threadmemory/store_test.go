package threadmemory

import (
	"testing"

	"github.com/jonathanagustin/conduit/internal/assistant/adapters/threadtest"
	"github.com/jonathanagustin/conduit/internal/assistant/application"
)

func TestMemoryStoreConformsToTheThreadsPort(t *testing.T) {
	threadtest.Run(t, func(*testing.T) application.Threads { return New() })
}
