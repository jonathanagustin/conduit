package conduitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func environmentLabelCLIInput(t *testing.T) (string, []byte, controlplane.DesiredState) {
	t.Helper()
	source, err := os.ReadFile("../../examples/example-foundation.json")
	if err != nil {
		t.Fatal(err)
	}
	var state controlplane.DesiredState
	if err := json.Unmarshal(source, &state); err != nil {
		t.Fatal(err)
	}
	state.Environment.ID = "dev-1234"
	state.Environment.Labels = map[string]string{"stage": "production", "region": "west"}
	source, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "desired.json")
	if err := os.WriteFile(file, source, 0o600); err != nil {
		t.Fatal(err)
	}
	return file, source, state
}

func TestEnvironmentLabelCLIUsesCompleteIDAndKeepsTheNativeProposalBoundary(t *testing.T) {
	for _, scenario := range []struct {
		name, action string
		preview      bool
	}{
		{name: "get", action: "get"}, {name: "list", action: "list"},
		{name: "set", action: "set"}, {name: "delete", action: "delete"},
		{name: "set preview", action: "set", preview: true}, {name: "delete preview", action: "delete", preview: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			file, source, original := environmentLabelCLIInput(t)
			service := &fakeService{}
			connections := 0
			var output bytes.Buffer
			runtime := testRuntime(&output, service)
			runtime.ServiceFactory = func(context.Context, ConnectionConfig) (Service, func(), error) {
				connections++
				return service, func() {}, nil
			}
			root := NewRoot(runtime)
			args := []string{"--server", "http://127.0.0.1:8081", "--development-subject", "developer", "label", scenario.action, "environment", "dev-1234", "--file", file, "--label-id", "stage"}
			if scenario.action == "set" {
				args = append(args, "--label-value", "development")
			}
			if scenario.preview {
				args = append(args, "--dry-run")
			}
			root.SetArgs(args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			current, err := os.ReadFile(file)
			if err != nil || !bytes.Equal(current, source) {
				t.Fatalf("label command rewrote its Git-owned input: %v", err)
			}
			mutation := scenario.action == "set" || scenario.action == "delete"
			if !mutation {
				if connections != 0 {
					t.Fatal("file-backed label read contacted the Hub")
				}
				if scenario.action == "get" {
					var result struct{ LabelID, Value string }
					if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.LabelID != "stage" || result.Value != "production" {
						t.Fatalf("exact Environment label was not read: %s %v", output.Bytes(), err)
					}
				} else {
					var result struct{ Labels map[string]string }
					if err := json.Unmarshal(output.Bytes(), &result); err != nil || !reflect.DeepEqual(result.Labels, original.Environment.Labels) {
						t.Fatalf("exact Environment labels were not listed: %s %v", output.Bytes(), err)
					}
				}
				return
			}
			proposed := service.proposed
			if scenario.preview {
				var result struct {
					DryRun       bool
					DesiredState controlplane.DesiredState
				}
				if err := json.Unmarshal(output.Bytes(), &result); err != nil || !result.DryRun || connections != 0 {
					t.Fatalf("preview submitted a proposal: connections=%d %s %v", connections, output.Bytes(), err)
				}
				proposed = result.DesiredState
			} else {
				var result controlplane.PlanRecord
				if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.Status != controlplane.PlanAwaitingApproval || connections != 1 || service.approvedPlan != "" {
					t.Fatalf("label mutation bypassed the proposal boundary: connections=%d %s %v", connections, output.Bytes(), err)
				}
			}
			want := "development"
			if scenario.action == "delete" {
				want = ""
			}
			if _, present := proposed.Environment.Labels["stage"]; scenario.action == "delete" && present {
				t.Fatal("deleted Environment label retained an empty assignment")
			}
			if proposed.Environment.ID != "dev-1234" || proposed.Environment.Labels["stage"] != want || proposed.Environment.Labels["region"] != "west" ||
				!reflect.DeepEqual(proposed.Entities, original.Entities) || !reflect.DeepEqual(proposed.Releases, original.Releases) {
				t.Fatalf("label mutation did not preserve the exact target and unrelated state: %#v", proposed)
			}
		})
	}
}

func TestEnvironmentLabelCLIRejectsAbbreviatedAndDifferentIDsBeforeConnecting(t *testing.T) {
	for _, action := range []string{"get", "list", "set", "delete"} {
		for _, resource := range []string{"dev", "dev-123", "dev-5678", "Dev-1234"} {
			t.Run(action+"/"+resource, func(t *testing.T) {
				file, source, _ := environmentLabelCLIInput(t)
				service := &fakeService{}
				connections := 0
				var output bytes.Buffer
				runtime := testRuntime(&output, service)
				runtime.ServiceFactory = func(context.Context, ConnectionConfig) (Service, func(), error) {
					connections++
					return service, func() {}, nil
				}
				root := NewRoot(runtime)
				root.SilenceUsage, root.SilenceErrors = true, true
				args := []string{"--server", "http://127.0.0.1:8081", "--development-subject", "developer", "label", action, "environment", resource, "--file", file, "--label-id", "stage"}
				if action == "set" {
					args = append(args, "--label-value", "development")
				}
				root.SetArgs(args)
				if err := root.Execute(); err == nil || connections != 0 || output.Len() != 0 {
					t.Fatalf("mismatched Environment ID was accepted: connections=%d output=%s error=%v", connections, output.Bytes(), err)
				}
				current, err := os.ReadFile(file)
				if err != nil || !bytes.Equal(current, source) {
					t.Fatalf("refused label command changed its input: %v", err)
				}
			})
		}
	}
}
