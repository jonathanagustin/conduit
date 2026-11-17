package domain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseToolCallRejectsExtraFieldsAndUnknownTools(t *testing.T) {
	ok, err := ParseToolCall(ToolQuery, []byte(`{"objectType":"ChangeRequest","filter":{"field":"status","value":"PENDING_APPROVALS"}}`))
	if err != nil || ok.Query.ObjectType != "ChangeRequest" || ok.Query.Filter.Field != "status" {
		t.Fatalf("query = %#v %v", ok, err)
	}
	for _, raw := range []struct{ name, body string }{
		{ToolQuery, `{"objectType":"ChangeRequest","extra":1}`},
		{ToolQuery, `{"objectType":""}`},
		{ToolPropose, `{"actionId":"cancel_packaging_run"}`},
		{ToolDiff, `not-json`},
		{"shell", `{}`},
	} {
		if _, err := ParseToolCall(raw.name, []byte(raw.body)); err == nil {
			t.Fatalf("accepted %s %s", raw.name, raw.body)
		}
	}
}

func TestParseReplyCapsToolCalls(t *testing.T) {
	reply, err := ParseReply([]byte(`{"text":"reading","toolCalls":[{"name":"query_ontology_objects","arguments":{"objectType":"Environment"}}]}`))
	if err != nil || reply.Text != "reading" || len(reply.ToolCalls) != 1 {
		t.Fatalf("reply = %#v %v", reply, err)
	}
	if _, err := ParseReply([]byte(`{"text":"no","toolCalls":[],"extra":1}`)); err == nil {
		t.Fatal("accepted an extra field")
	}
	body := `{"text":"too many","toolCalls":[` +
		`{"name":"query_ontology_objects","arguments":{"objectType":"Environment"}},` +
		`{"name":"query_ontology_objects","arguments":{"objectType":"Plan"}},` +
		`{"name":"query_ontology_objects","arguments":{"objectType":"Artifact"}},` +
		`{"name":"query_ontology_objects","arguments":{"objectType":"Spoke"}}]}`
	if _, err := ParseReply([]byte(body)); err == nil {
		t.Fatal("accepted four tool calls")
	}
}

func TestModeLimitsTheToolSubset(t *testing.T) {
	if !ModeAllows(ModeExplore, ToolQuery) || ModeAllows(ModeExplore, ToolPropose) || ModeAllows(ModeConfigDiff, ToolPropose) {
		t.Fatal("mode subset is wrong")
	}
	if !ModeAllows(ModeOntology, ToolDiff) || !ModeAllows(ModeReleasePlan, ToolPropose) {
		t.Fatal("ontology and release modes should allow their tools")
	}
}

func TestDomainDoesNotImportProcesses(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		for _, banned := range []string{`"os/exec"`, `"os"`, `"net/http"`} {
			if strings.Contains(text, banned) {
				t.Fatalf("%s imports %s", name, banned)
			}
		}
	}
}

func TestChangeRequestActionsAskForTheirTarget(t *testing.T) {
	for _, action := range []string{"promote", "recall", "cancel_packaging_run", "propose_memory", "select_course"} {
		names := map[string]bool{}
		for _, field := range MissingFields(WriteArgs{Action: action}) {
			names[field.Name] = true
		}
		for _, want := range []string{"environment", "installationId", "productId"} {
			if !names[want] {
				t.Fatalf("%s does not ask for %s", action, want)
			}
		}
		full := WriteArgs{
			Action: action, TargetID: "t", Value: "512Mi", Justification: "j",
			Environment: "prod", InstallationID: "billing-api", ProductID: "com.acme:billing",
		}
		if left := MissingFields(full); len(left) != 0 {
			t.Fatalf("%s still asks for %#v", action, left)
		}
	}
}

func TestPromptDescribesGovernedWriteAndEveryAction(t *testing.T) {
	prompt := Prompt(ModeOntology, "cancel it", nil)
	for _, want := range append(WriteActions(), `"installationId"`, `"productId"`, "Approving it does not run the named action") {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not mention %s", want)
		}
	}
	for _, action := range WriteActions() {
		if _, err := ParseToolCall(ToolWrite, []byte(`{"action":"`+action+`","installationId":"a","productId":"b"}`)); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
}
