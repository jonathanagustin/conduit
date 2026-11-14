package httpapi

import (
	"testing"

	"github.com/jonathanagustin/conduit/internal/intelligence/application"
)

func TestEvalRunLabelFollowsALiveColumn(t *testing.T) {
	recorded := []application.CaseResult{{Columns: []application.Column{{Label: "No model call"}}}}
	if got := evalRunLabel(recorded); got != "No model call" {
		t.Fatalf("recorded label = %q", got)
	}
	live := []application.CaseResult{{
		Columns: []application.Column{{Label: "No model call"}, {Label: "Live model call"}},
	}}
	if got := evalRunLabel(live); got != "Live model call" {
		t.Fatalf("live label = %q", got)
	}
}
