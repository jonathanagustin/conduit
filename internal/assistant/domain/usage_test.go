package domain

import (
	"math"
	"testing"
)

func tokenCount(value int64) *int64 { return &value }

func TestReportedUsageNeverEstimatesMissingRounds(t *testing.T) {
	cases := []struct {
		name               string
		rounds             []*ReportedTokenUsage
		prompt, completion *int64
	}{
		{"reported zero", []*ReportedTokenUsage{{tokenCount(0), tokenCount(0)}}, tokenCount(0), tokenCount(0)},
		{"sum rounds", []*ReportedTokenUsage{{tokenCount(2), tokenCount(3)}, {tokenCount(5), tokenCount(7)}}, tokenCount(7), tokenCount(10)},
		{"legacy first round", []*ReportedTokenUsage{nil, {tokenCount(5), tokenCount(7)}}, nil, nil},
		{"missing last round", []*ReportedTokenUsage{{tokenCount(2), tokenCount(3)}, nil}, nil, nil},
		{"partial first round", []*ReportedTokenUsage{{nil, tokenCount(0)}, {tokenCount(5), tokenCount(7)}}, nil, tokenCount(7)},
		{"partial last round", []*ReportedTokenUsage{{tokenCount(2), tokenCount(3)}, {tokenCount(5), nil}}, tokenCount(7), nil},
		{"negative count", []*ReportedTokenUsage{{tokenCount(-1), tokenCount(3)}, {tokenCount(5), tokenCount(7)}}, nil, tokenCount(10)},
		{"overflow", []*ReportedTokenUsage{{tokenCount(math.MaxInt64), tokenCount(3)}, {tokenCount(1), tokenCount(7)}}, nil, tokenCount(10)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var total *ReportedTokenUsage
			for _, round := range tc.rounds {
				total = IncludeReportedUsage(total, round)
			}
			check := func(name string, got, want *int64) {
				t.Helper()
				if (got == nil) != (want == nil) || (got != nil && *got != *want) {
					t.Fatalf("%s total=%v want=%v", name, got, want)
				}
			}
			check("prompt", total.PromptTokens, tc.prompt)
			check("completion", total.CompletionTokens, tc.completion)
		})
	}
}

func TestReportedUsageDoesNotAliasInputsOrTotals(t *testing.T) {
	input := &ReportedTokenUsage{tokenCount(2), tokenCount(3)}
	first := IncludeReportedUsage(nil, input)
	second := IncludeReportedUsage(first, &ReportedTokenUsage{tokenCount(5), tokenCount(7)})
	*input.PromptTokens = 100
	*second.CompletionTokens = 200
	if *first.PromptTokens != 2 || *first.CompletionTokens != 3 {
		t.Fatal("returned totals alias another round")
	}
	if (&ReportedTokenUsage{PromptTokens: tokenCount(-1)}).Valid() {
		t.Fatal("negative stored usage accepted")
	}
}
