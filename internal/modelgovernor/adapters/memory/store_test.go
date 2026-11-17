package memory

import (
	"testing"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/modelgovernor/adapters/storetest"
)

func TestMemoryStoresConform(t *testing.T) {
	storetest.RunUsage(t, func(*testing.T) modelgovernor.UsageStore { return NewUsage() })
	storetest.RunTranscripts(t, func(*testing.T) modelgovernor.TranscriptStore { return NewTranscripts() })
}
