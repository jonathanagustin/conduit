// Package liveevents holds contract fixtures, not a mounted stream implementation.
package liveevents

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func decode(t *testing.T, name string) any {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func compile(t *testing.T) *jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	const location = "https://conduit.invalid/contracts/live-event-v1"
	if err := compiler.AddResource(location, decode(t, "event.schema.json")); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(location)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestResumeContractFixtures(t *testing.T) {
	schema := compile(t)
	fixtures := decode(t, "resume-fixtures.json").([]any)
	for _, raw := range fixtures {
		fixture := raw.(map[string]any)
		t.Run(fixture["name"].(string), func(t *testing.T) {
			stream, cursor := "", ""
			sequence, revision := float64(0), float64(0)
			revisions := map[string]float64{}
			resync := false
			events := fixture["events"].([]any)
			actions := fixture["actions"].([]any)
			if len(events) != len(actions) {
				t.Fatal("missing expectation")
			}
			for index, raw := range events {
				if err := schema.Validate(raw); err != nil {
					t.Fatal(err)
				}
				event := raw.(map[string]any)
				next := event["sequence"].(float64)
				nextStream := event["streamID"].(string)
				resource := event["resource"].(map[string]any)
				nextRevision := resource["revision"].(float64)
				key := resource["type"].(string) + ":" + resource["id"].(string)
				action := "apply"
				switch {
				case resync || (stream != "" && stream != nextStream):
					resync, action = true, "resync"
				case next <= sequence:
					action = "ignore"
				case next != sequence+1:
					resync, action = true, "resync"
				default:
					stream, sequence, cursor = nextStream, next, event["cursor"].(string)
					if nextRevision <= revisions[key] {
						action = "ignore-resource"
					} else {
						revisions[key] = nextRevision
					}
				}
				if action != actions[index] {
					t.Fatalf("event %d: got %s want %s", index, action, actions[index])
				}
			}
			revision = revisions["plan:plan-1"]
			if sequence != fixture["sequence"] || revision != fixture["revision"] {
				t.Fatalf("wrong final state: sequence=%v revision=%v", sequence, revision)
			}
			if sequence > 0 && cursor == "" {
				t.Fatal("applied sequence without cursor")
			}
		})
	}
}

func freshEvent(t *testing.T) map[string]any {
	return decode(t, "resume-fixtures.json").([]any)[0].(map[string]any)["events"].([]any)[0].(map[string]any)
}

func TestEventSchemaRejectsUnsafeAndIncompatibleData(t *testing.T) {
	schema := compile(t)
	tests := map[string]func(map[string]any){
		"unknown version":       func(e map[string]any) { e["version"] = float64(2) },
		"missing cursor":        func(e map[string]any) { delete(e, "cursor") },
		"zero sequence":         func(e map[string]any) { e["sequence"] = float64(0) },
		"fractional sequence":   func(e map[string]any) { e["sequence"] = 1.5 },
		"unsafe integer":        func(e map[string]any) { e["sequence"] = float64(9007199254740992) },
		"unknown event":         func(e map[string]any) { e["kind"] = "run.arbitrary" },
		"invalid date":          func(e map[string]any) { e["occurredAt"] = "yesterday" },
		"credential URL cursor": func(e map[string]any) { e["cursor"] = "https://host/?token=secret" },
		"missing resource":      func(e map[string]any) { delete(e, "resource") },
		"wrong resource kind":   func(e map[string]any) { e["resource"].(map[string]any)["type"] = "environment" },
		"body in resource":      func(e map[string]any) { e["resource"].(map[string]any)["body"] = "file" },
		"unknown status":        func(e map[string]any) { e["data"].(map[string]any)["status"] = "secret log text" },
		"raw output":            func(e map[string]any) { e["data"].(map[string]any)["output"] = "secret" },
		"token":                 func(e map[string]any) { e["token"] = "secret" },
		"payload type":          func(e map[string]any) { e["data"] = "raw log" },
		"empty payload":         func(e map[string]any) { e["data"] = map[string]any{} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			event := freshEvent(t)
			mutate(event)
			if schema.Validate(event) == nil {
				t.Fatal("invalid event accepted")
			}
		})
	}
}

func TestAllEventKindsHaveBoundedPayloads(t *testing.T) {
	schema := compile(t)
	cases := []struct {
		kind, resource string
		data           map[string]any
	}{
		{"packaging.step", "packaging-run", map[string]any{"phase": "assess", "outcome": "PASS"}},
		{"plan.changed", "plan", map[string]any{"status": "AWAITING_APPROVAL"}},
		{"approval.changed", "plan", map[string]any{"status": "APPROVED"}},
		{"spoke.reported", "environment", map[string]any{"state": "live"}},
		{"assistant.activity", "assistant-run", map[string]any{"activity": "WAITING_INPUT"}},
		{"index.invalidated", "index", map[string]any{"areas": []any{"overview", "kpi"}}},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			event := freshEvent(t)
			event["kind"] = tc.kind
			event["resource"].(map[string]any)["type"] = tc.resource
			event["data"] = tc.data
			if err := schema.Validate(event); err != nil {
				t.Fatal(err)
			}
			tc.data["rawOutput"] = "must never be delivered"
			if schema.Validate(event) == nil {
				t.Fatal("unbounded payload accepted")
			}
		})
	}
}
