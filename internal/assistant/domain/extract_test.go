package domain

import (
	"errors"
	"strings"
	"testing"
)

const queryReply = `{"text":"reading","toolCalls":[{"name":"query_ontology_objects","arguments":{"objectType":"Environment"}}]}`

func TestParseReplyAcceptsFencedAndProseWrappedObjects(t *testing.T) {
	for name, raw := range map[string]string{
		"bare":            queryReply,
		"json fence":      "Here you go:\n```json\n" + queryReply + "\n```\n",
		"plain fence":     "```\n" + queryReply + "\n```",
		"prose before":    "Sure, I will look that up.\n" + queryReply,
		"prose after":     queryReply + "\nLet me know if you need more.",
		"logs then reply": `{"level":"info","msg":"loading"}` + "\n" + queryReply,
		"brace in string": `noise } { ` + `{"text":"reading {braces} and \"quotes\"","toolCalls":[{"name":"query_ontology_objects","arguments":{"objectType":"Environment"}}]}`,
	} {
		reply, err := ParseReply([]byte(raw))
		if err != nil || len(reply.ToolCalls) != 1 || reply.ToolCalls[0].Name != ToolQuery || !strings.HasPrefix(reply.Text, "reading") {
			t.Fatalf("%s: %#v %v", name, reply, err)
		}
	}
	// The planner's rule: a later object wins over an earlier one.
	reply, err := ParseReply([]byte(`{"text":"draft"}` + "\nActually:\n" + `{"text":"final"}`))
	if err != nil || reply.Text != "final" {
		t.Fatalf("latest object = %#v %v", reply, err)
	}
}

func TestParseReplyStaysStrictAboutTheObjectItself(t *testing.T) {
	for name, raw := range map[string]string{
		"no object":            "I could not find anything.",
		"empty":                "",
		"unknown field fenced": "```json\n" + `{"text":"x","toolCalls":[],"extra":1}` + "\n```",
		"empty object":         "```json\n{}\n```",
		"unknown tool fenced":  "```json\n" + `{"text":"x","toolCalls":[{"name":"shell","arguments":{}}]}` + "\n```",
		"bad arguments":        `Here: {"text":"x","toolCalls":[{"name":"query_ontology_objects","arguments":{"objectType":""}}]}`,
		"unterminated":         `{"text":"x","toolCalls":[`,
	} {
		if reply, err := ParseReply([]byte(raw)); !errors.Is(err, ErrReplyInvalid) {
			t.Fatalf("%s: accepted %#v (%v)", name, reply, err)
		}
	}
	// A reply with a bad tool call is not rescued by an earlier, valid object.
	raw := `{"text":"earlier"}` + "\n" + `{"text":"x","toolCalls":[{"name":"shell","arguments":{}}]}`
	if reply, err := ParseReply([]byte(raw)); err == nil {
		t.Fatalf("fell back past an invalid tool call to %#v", reply)
	}
}
