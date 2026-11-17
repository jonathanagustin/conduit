package domain

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func framedAssistantTools() []struct {
	name string
	body string
	want ToolCall
} {
	return []struct {
		name string
		body string
		want ToolCall
	}{
		{ToolQuery, `{"objectType":"Environment","filter":{"field":"name","value":"private-tool-canary"},"links":["installations"]}`,
			ToolCall{Query: QueryArgs{ObjectType: "Environment", Filter: &EqFilter{Field: "name", Value: "private-tool-canary"}, Links: []string{"installations"}}}},
		{ToolPropose, `{"actionId":"cancel_packaging_run","targetEntityId":"run-1","targetObjectType":"PackagingRun","environment":"dev","installationId":"install-1","productId":"web"}`,
			ToolCall{Propose: ProposeArgs{ActionID: "cancel_packaging_run", TargetEntityID: "run-1", TargetObjectType: "PackagingRun", Environment: "dev", InstallationID: "install-1", ProductID: "web"}}},
		{ToolDiff, `{"changeRequestId":"change-1"}`, ToolCall{Diff: DiffArgs{ChangeRequestID: "change-1"}}},
		{ToolWrite, `{"action":"cancel_packaging_run","targetId":"run-1","environment":"dev","installationId":"install-1","productId":"web","value":"private-tool-canary","justification":"reviewed","expectedOutcome":"stopped","confirm":true}`,
			ToolCall{Write: WriteArgs{Action: "cancel_packaging_run", TargetID: "run-1", Environment: "dev", InstallationID: "install-1", ProductID: "web", Value: "private-tool-canary", Justification: "reviewed", Expected: "stopped", Confirm: true}}},
	}
}

func TestAssistantToolArgumentsRequireTheEndOfTheObject(t *testing.T) {
	for _, tool := range framedAssistantTools() {
		t.Run(tool.name, func(t *testing.T) {
			for _, suffix := range []struct{ name, text string }{
				{"closing object", "}"}, {"closing array", "]"},
				{"object", `{"private":"private-tool-canary"}`}, {"array", "[]"},
				{"null", "null"}, {"boolean", "true"}, {"number", "42"},
				{"string", `"private-tool-canary"`}, {"garbage", "private-tool-canary"},
				{"comment", "// private-tool-canary"}, {"nul", "\x00"},
			} {
				t.Run(suffix.name, func(t *testing.T) {
					_, err := ParseToolCall(tool.name, []byte(tool.body+" \t\r\n"+suffix.text))
					if !errors.Is(err, ErrToolInvalid) {
						t.Fatalf("trailing arguments were accepted or lost ErrToolInvalid: %v", err)
					}
					if strings.Contains(err.Error(), "private-tool-canary") {
						t.Fatal("framing error disclosed arguments")
					}
				})
			}
		})
	}
}

func TestAssistantFramingKeepsWhitespaceAndExistingToolFields(t *testing.T) {
	for _, tool := range framedAssistantTools() {
		t.Run(tool.name, func(t *testing.T) {
			want := tool.want
			want.Name = tool.name
			want.Arguments = json.RawMessage(tool.body)
			for _, padding := range []string{"", " ", "\t\r\n "} {
				got, err := ParseToolCall(tool.name, []byte(padding+tool.body+padding))
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("single tool call changed under whitespace padding: got=%#v want=%#v error=%v", got, want, err)
				}
			}
		})
	}
}

func TestAssistantReplyCandidateRequiresTheEndOfTheObject(t *testing.T) {
	for _, suffix := range []string{"}", "]", `{"text":"private-tool-canary"}`, "null", "private-tool-canary"} {
		if _, _, err := parseReplyObject([]byte(queryReply + " \t\n" + suffix)); !errors.Is(err, ErrReplyInvalid) {
			t.Fatalf("trailing candidate data was accepted or lost ErrReplyInvalid: %v", err)
		}
	}
}
