package domain

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type framedProposalParser struct {
	name     string
	body     string
	parse    func([]byte) (any, error)
	want     any
	required []string
}

func framedProposalParsers() []framedProposalParser {
	return []framedProposalParser{
		{
			name: "field", body: `{"field":" memory ","value":" 512Mi ","rationale":" private-response-canary "}`,
			parse:    func(raw []byte) (any, error) { return ParseProposal(raw) },
			want:     Proposal{Field: "memory", Value: "512Mi", Rationale: "private-response-canary"},
			required: []string{"field", "value", "rationale"},
		},
		{
			name: "dockerfile", body: `{"dockerfile":" FROM scratch\nUSER 65532\n","dockerignore":" .git\n","reuseExisting":true,"reuseReason":" already reviewed ","rationale":" private-response-canary "}`,
			parse:    func(raw []byte) (any, error) { return ParseDockerfileProposal(raw, false) },
			want:     DockerfileProposal{Dockerfile: "FROM scratch\nUSER 65532", Dockerignore: ".git", ReuseExisting: true, ReuseReason: "already reviewed", Rationale: "private-response-canary"},
			required: []string{"dockerfile", "rationale"},
		},
		{
			name: "package", body: `{"source":" name: web\n","zarf":" kind: ZarfPackageConfig\n","cr":" kind: Package\n","dockerfile":" FROM scratch\n","dockerignore":" .git\n","rationale":" private-response-canary "}`,
			parse:    func(raw []byte) (any, error) { return ParsePackageProposal(raw) },
			want:     PackageProposal{Source: "name: web", Zarf: "kind: ZarfPackageConfig", CR: "kind: Package", Dockerfile: "FROM scratch", Dockerignore: ".git", Rationale: "private-response-canary"},
			required: []string{"source", "zarf", "cr", "rationale"},
		},
		{
			name: "tool requests", body: `{"toolCalls":[{"name":" inspect_image ","arguments":{"ref":"private-response-canary"}}]}`,
			parse: func(raw []byte) (any, error) {
				calls, matched, err := ParseToolRequests(raw)
				if err == nil && !matched {
					return nil, errors.New("valid tool requests were not recognized")
				}
				return calls, err
			},
			want: []ToolRequest{{Name: "inspect_image", Arguments: json.RawMessage(`{"ref":"private-response-canary"}`)}},
		},
	}
}

func TestModelProposalParsersRequireTheEndOfTheResponse(t *testing.T) {
	for _, parser := range framedProposalParsers() {
		t.Run(parser.name, func(t *testing.T) {
			for _, suffix := range []struct{ name, text string }{
				{"closing object", "}"}, {"closing array", "]"},
				{"object", `{"private":"private-response-canary"}`}, {"array", "[]"},
				{"null", "null"}, {"boolean", "true"}, {"number", "42"},
				{"string", `"private-response-canary"`}, {"garbage", "private-response-canary"},
				{"comment", "// private-response-canary"}, {"nul", "\x00"},
			} {
				t.Run(suffix.name, func(t *testing.T) {
					_, err := parser.parse([]byte(parser.body + " \t\r\n" + suffix.text))
					if !errors.Is(err, ErrProposalInvalid) {
						t.Fatalf("trailing response data was accepted or lost ErrProposalInvalid: %v", err)
					}
					if strings.Contains(err.Error(), "private-response-canary") {
						t.Fatal("framing error disclosed response contents")
					}
				})
			}
		})
	}
}

func TestModelProposalParsersKeepWhitespaceAndExistingPayloadFields(t *testing.T) {
	for _, parser := range framedProposalParsers() {
		t.Run(parser.name, func(t *testing.T) {
			for _, padding := range []string{"", " ", "\t\r\n "} {
				got, err := parser.parse([]byte(padding + parser.body + padding))
				if err != nil || !reflect.DeepEqual(got, parser.want) {
					t.Fatalf("single proposal changed under whitespace padding: got=%#v want=%#v error=%v", got, parser.want, err)
				}
			}
		})
	}
}

func TestProposalFramingRetainsFieldAndToolValidation(t *testing.T) {
	for _, parser := range framedProposalParsers() {
		t.Run(parser.name+" unknown field", func(t *testing.T) {
			body := strings.TrimSuffix(parser.body, "}") + `,"unexpected":true}`
			if _, err := parser.parse([]byte(body)); err == nil {
				t.Fatal("unknown proposal field was accepted")
			}
		})
		for _, required := range parser.required {
			t.Run(parser.name+" required "+required, func(t *testing.T) {
				var body map[string]json.RawMessage
				if err := json.Unmarshal([]byte(parser.body), &body); err != nil {
					t.Fatal(err)
				}
				body[required] = json.RawMessage(`" "`)
				raw, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := parser.parse(raw); !errors.Is(err, ErrProposalInvalid) {
					t.Fatalf("empty %s no longer fails validation: %v", required, err)
				}
			})
		}
	}
	for _, body := range []string{
		`{"toolCalls":[{"name":" ","arguments":{}}]}`,
		`{"toolCalls":[{"name":"a"},{"name":"b"},{"name":"c"},{"name":"d"},{"name":"e"}]}`,
		`{"toolCalls":[{"name":"a","arguments":{invalid}}]}`,
	} {
		if calls, matched, err := ParseToolRequests([]byte(body)); err == nil || matched || len(calls) != 0 {
			t.Fatalf("invalid tool requests were accepted: matched=%v error=%v", matched, err)
		}
	}
	for _, body := range []string{`{}`, `{"toolCalls":[]}`} {
		if calls, matched, err := ParseToolRequests([]byte(body)); err != nil || matched || len(calls) != 0 {
			t.Fatalf("non-tool response changed classification: matched=%v error=%v", matched, err)
		}
	}
	if calls, matched, err := ParseToolRequests([]byte("I have no tool request.")); err == nil || matched || len(calls) != 0 {
		t.Fatalf("natural-language response changed classification: matched=%v error=%v", matched, err)
	}
}
