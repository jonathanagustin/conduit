package workflow

import (
	"os"
	"testing"

	historypb "go.temporal.io/api/history/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// The checked-in history came from the pinned real-server restart test, not
// from WorkflowTestEnvironment or a hand-written sequence of matching events.
func TestRecordedCancellationHistoryReplays(t *testing.T) {
	body, err := os.ReadFile("testdata/planning-cancellation-v1-history.json")
	if err != nil {
		t.Fatal(err)
	}
	var history historypb.History
	if err := protojson.Unmarshal(body, &history); err != nil {
		t.Fatal(err)
	}
	assertCancellationHistory(t, &history)
	replayCancellationHistory(t, &history)
}
