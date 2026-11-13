package main

import (
	"strings"
	"testing"
)

func TestDuplicateKeys(t *testing.T) {
	for _, s := range []string{`{"revision":"a","revision":"b"}`, `{"revision":"a","Revision":"b"}`, `{"findings":[{"basis":"unknown","Basis":"source-observed"}]}`, `{"findings":[],"findings":[]}`, `{"findings":[{"basis":"unknown","basis":"source-observed"}]}`, strings.Repeat("[", 66) + strings.Repeat("]", 66)} {
		if err := rejectDuplicateKeys([]byte(s)); err == nil {
			t.Fatal("accepted ambiguous input")
		}
	}
	if err := rejectDuplicateKeys([]byte(`{"findings":[{"basis":"unknown"},{"basis":"inferred"}]}`)); err != nil {
		t.Fatal(err)
	}
}
