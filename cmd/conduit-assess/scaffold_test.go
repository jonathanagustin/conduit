package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestScaffoldCLIRejectsUnsupportedFields(t *testing.T) {
	for _, input := range []string{`{"persistence":true}`, `{"env":{"PASSWORD":"not-allowed"}}`, `{"profile":"internal-stateless/v1","Profile":"other"}`, `{"image":"a","image":"b"}`, `{} {}`, `null`} {
		var out bytes.Buffer
		if err := scaffold(strings.NewReader(input), &out); err == nil || out.Len() != 0 {
			t.Fatal("accepted unsupported/partial output")
		}
	}
}
