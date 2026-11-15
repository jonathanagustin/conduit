package agentstream

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func offers(t *testing.T, names ...string) []ToolOffer {
	t.Helper()
	definitions, err := Definitions()
	if err != nil {
		t.Fatal(err)
	}
	result := []ToolOffer{}
	for _, name := range names {
		result = append(result, ToolOffer{Name: name, Description: name, Parameters: definitions[name]})
	}
	return result
}
func formFields() map[string]Field {
	return map[string]Field{"node_version": {Type: "string", Choices: []string{"20", "22"}}, "repoUrl": {Type: "string", ReadOnly: true}, "enabled": {Type: "boolean"}, "count": {Type: "number"}}
}

func TestToolPolicyRequiresAnOfferedToolAndBothSchemas(t *testing.T) {
	declared := offers(t, "fill_field")
	declared[0].Parameters = json.RawMessage(`{"type":"object","properties":{"value":{"const":"20"}}}`)
	policy, err := NewToolPolicy(declared, formFields())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.Validate("fill_field", json.RawMessage(`{"field":"node_version","value":"20"}`)); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, raw string
		want      error
	}{
		{"focus_field", `{"field":"node_version"}`, ErrToolNotOffered},
		{"fill_field", `{"field":"node_version","value":"22"}`, ErrInvalidToolArguments},
		{"fill_field", `{"field":"node_version","value":"20","execute":"command"}`, ErrInvalidToolArguments},
	} {
		if _, err := policy.Validate(test.name, json.RawMessage(test.raw)); !errors.Is(err, test.want) {
			t.Fatalf("%s: %v", test.raw, err)
		}
	}
}

func TestClientSchemaCannotWidenHubFieldPermissions(t *testing.T) {
	declared := []ToolOffer{{Name: "fill_field", Description: "any", Parameters: json.RawMessage(`true`)}, {Name: "clear_field", Description: "clear"}, {Name: "focus_field", Description: "focus"}, {Name: "request_confirmation", Description: "confirm"}}
	fields := formFields()
	policy, err := NewToolPolicy(declared, fields)
	if err != nil {
		t.Fatal(err)
	}
	// Changes to caller-owned maps/slices must not change this run's policy.
	fields["repoUrl"] = Field{Type: "string"}
	fields["node_version"].Choices[0] = "unreviewed"
	for _, test := range []struct {
		name, raw string
		want      error
	}{
		{"fill_field", `{"field":"repoUrl","value":"different"}`, ErrFieldUnavailable},
		{"clear_field", `{"field":"repoUrl"}`, ErrFieldUnavailable},
		{"fill_field", `{"field":"unknown","value":"20"}`, ErrFieldUnavailable},
		{"fill_field", `{"field":"node_version","value":"unreviewed"}`, ErrInvalidToolArguments},
		{"fill_field", `{"field":"enabled","value":"true"}`, ErrInvalidToolArguments},
		{"fill_field", `{"field":"count","value":"one"}`, ErrInvalidToolArguments},
	} {
		if _, err := policy.Validate(test.name, json.RawMessage(test.raw)); !errors.Is(err, test.want) {
			t.Fatalf("%s: %v", test.raw, err)
		}
	}
	for name, raw := range map[string]string{"focus_field": `{"field":"repoUrl"}`, "fill_field": `{"field":"enabled","value":true}`, "request_confirmation": `{"message":"Review the draft?"}`, "clear_field": `{"field":"node_version"}`} {
		if _, err := policy.Validate(name, json.RawMessage(raw)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestOfferedSchemaCannotFetchOrDeclarePrivilegedTools(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	defer server.Close()
	for _, declared := range [][]ToolOffer{
		{{Name: "execute_command", Description: "forged"}},
		{{Name: "focus_field", Description: "one"}, {Name: "focus_field", Description: "duplicate"}},
		{{Name: "focus_field", Description: "remote", Parameters: json.RawMessage(`{"$ref":"` + server.URL + `/schema"}`)}},
		{{Name: "focus_field", Description: "malformed", Parameters: json.RawMessage(`{"type":[]}`)}},
	} {
		if _, err := NewToolPolicy(declared, formFields()); !errors.Is(err, ErrInvalidToolSchema) {
			t.Fatalf("declaration accepted: %+v err=%v", declared, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("schema validation performed a network request")
	}
}

func TestInvalidArgumentsAreRejectedWithoutEchoingValues(t *testing.T) {
	policy, err := NewToolPolicy(offers(t, "fill_field"), formFields())
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"field":"node_version","value":"sentinel-secret"}`,
		`{"field":"node_version","field":"repoUrl","value":"20"}`,
		`{"field":"count","value":1e9999}`,
		`{"field":"count","value":1e-9999}`,
		`{"field":"count","value":1e309}`,
	} {
		if _, err := policy.Validate("fill_field", json.RawMessage(raw)); !errors.Is(err, ErrInvalidToolArguments) || strings.Contains(err.Error(), "sentinel-secret") {
			t.Fatalf("invalid argument diagnostics: %v", err)
		}
	}
}
