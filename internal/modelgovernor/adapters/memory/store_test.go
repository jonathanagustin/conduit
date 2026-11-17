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

func TestMemoryTranscriptAuditRecovery(t *testing.T) {
	storetest.RunAuditRecovery(t, func(*testing.T) modelgovernor.TranscriptStore { return NewTranscripts() })
}

func TestMemoryUsageReceipts(t *testing.T) {
	storetest.RunUsageReceipts(t, func(*testing.T) modelgovernor.UsageStore { return NewUsage() })
	t.Run("contention", func(t *testing.T) { store := NewUsage(); storetest.RunUsageReceiptContention(t, store, store) })
}

func TestMemoryAttemptedCalls(t *testing.T) {
	storetest.RunAttemptedCalls(t, func(*testing.T) modelgovernor.UsageStore { return NewUsage() })
}
