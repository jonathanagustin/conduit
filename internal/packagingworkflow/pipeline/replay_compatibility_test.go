package pipeline

import (
	"os"
	"testing"

	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestVersionTwoRecordedHistoryReplaysWithCurrentWorkflow(t *testing.T) {
	body, err := os.ReadFile("testdata/packaging-v2-history.json")
	if err != nil {
		t.Fatal(err)
	}
	var history historypb.History
	if err := protojson.Unmarshal(body, &history); err != nil {
		t.Fatal(err)
	}
	// Verify the fixture's actual marker: a current-version fixture would make
	// this test green while proving nothing about the deployed v2 history.
	found := false
	for _, event := range history.Events {
		marker := event.GetMarkerRecordedEventAttributes()
		if marker == nil || marker.MarkerName != "Version" {
			continue
		}
		var id string
		var version workflow.Version
		decoder := converter.GetDefaultDataConverter()
		if err := decoder.FromPayloads(marker.Details["change-id"], &id); err != nil {
			t.Fatal(err)
		}
		if id != changeID {
			continue
		}
		if err := decoder.FromPayloads(marker.Details["version"], &version); err != nil {
			t.Fatal(err)
		}
		if version != workflow.Version(2) {
			t.Fatalf("fixture is version %d, not deployed version 2", version)
		}
		found = true
	}
	if !found {
		t.Fatal("fixture lacks the native version-2 marker")
	}
	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflowWithOptions(PackagingWorkflow, workflow.RegisterOptions{Name: WorkflowName})
	if err := replayer.ReplayWorkflowHistory(nil, &history); err != nil {
		t.Fatalf("version-2 replay changed: %v", err)
	}
}
