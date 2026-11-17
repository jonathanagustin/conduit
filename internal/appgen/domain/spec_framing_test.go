package domain

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseSpecRequiresTheEndOfTheResponse(t *testing.T) {
	raw := mustJSON(t, recordedSpec(t))
	for _, suffix := range []struct{ name, text string }{
		{"closing object", "}"}, {"closing array", "]"},
		{"object", `{"private":"private-spec-canary"}`}, {"array", "[]"},
		{"null", "null"}, {"boolean", "true"}, {"number", "42"},
		{"string", `"private-spec-canary"`}, {"garbage", "private-spec-canary"},
		{"comment", "// private-spec-canary"}, {"nul", "\x00"},
	} {
		t.Run(suffix.name, func(t *testing.T) {
			_, err := ParseSpec(append(append([]byte(nil), raw...), []byte(" \t\r\n"+suffix.text)...))
			if !errors.Is(err, ErrSpecInvalid) {
				t.Fatalf("trailing response data was accepted or lost ErrSpecInvalid: %v", err)
			}
			if strings.Contains(err.Error(), "private-spec-canary") {
				t.Fatal("framing error disclosed response contents")
			}
		})
	}
}

func TestParseSpecKeepsWhitespaceAndRecordedSource(t *testing.T) {
	want := recordedSpec(t)
	body := string(mustJSON(t, want))
	for _, padding := range []string{"", " ", "\t\r\n "} {
		got, err := ParseSpec([]byte(padding + body + padding))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("single spec changed under whitespace padding: got=%#v want=%#v error=%v", got, want, err)
		}
	}
}
