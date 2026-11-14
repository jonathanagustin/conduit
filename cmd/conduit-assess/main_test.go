package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	var out bytes.Buffer
	err := run(strings.NewReader(`{"revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","udsVersion":"1.15.0","findings":[]}`), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"status": "needs-evidence"`) {
		t.Fatal(out.String())
	}
}
func TestRejectBadInput(t *testing.T) {
	for _, s := range []string{`{}`, `null`, `{} {}`, `{"unexpected":true}`, strings.Repeat(" ", maxInput+1)} {
		var out bytes.Buffer
		if err := run(strings.NewReader(s), &out); err == nil {
			t.Fatalf("accepted invalid input of size %d", len(s))
		}
		if out.Len() != 0 {
			t.Fatal("wrote success output on failure")
		}
	}
}
