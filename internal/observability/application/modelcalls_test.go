package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/observability/application"
	"github.com/jonathanagustin/conduit/internal/observability/domain"
)

func modelCalls(read application.ReadModelCalls) func(context.Context) (*domain.Registry, error) {
	return application.WithModelCalls(func(context.Context) (*domain.Registry, error) {
		return domain.NewRegistry(), nil
	}, read)
}

func TestModelCallMetricsAreCountersWithClosedLabels(t *testing.T) {
	t.Parallel()
	registry, err := modelCalls(func() []application.ModelCallSeries {
		return []application.ModelCallSeries{
			{Capability: "assistant", Transport: "http", Outcome: "ok", Calls: 7, Retries: 2, DurationSeconds: 3.5, PromptTokens: 900, CompletionTokens: 40},
			{Capability: "planning", Transport: "cli", Outcome: "throttled", Calls: 3},
		}
	})(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// The registry's own allowlist is what a scrape is checked against.
	if err := registry.Validate(); err != nil {
		t.Fatalf("the model-call metrics do not pass the label allowlist: %v", err)
	}
	text := registry.Render()
	for _, required := range []string{
		`conduit_model_calls_total{capability="assistant",outcome="ok",transport="http"} 7`,
		`conduit_model_calls_total{capability="planning",outcome="throttled",transport="cli"} 3`,
		`conduit_model_call_retries_total{capability="assistant",outcome="ok",transport="http"} 2`,
		`conduit_model_call_duration_seconds_sum{capability="assistant",outcome="ok",transport="http"} 3.5`,
		`conduit_model_prompt_tokens_total{capability="assistant",outcome="ok",transport="http"} 900`,
		`conduit_model_completion_tokens_total{capability="assistant",outcome="ok",transport="http"} 40`,
		"# TYPE conduit_model_calls_total counter",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("missing %q in:\n%s", required, text)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "conduit_model_") && (strings.Contains(line, "owner=") || strings.Contains(line, "model=")) {
			t.Errorf("a model-call series carries an owner or model label: %s", line)
		}
	}
}

// An owner, a model name or an error message handed in as a label value is
// refused, not published.
func TestAModelCallSeriesOutsideTheClosedSetsIsRefused(t *testing.T) {
	t.Parallel()
	for _, series := range []application.ModelCallSeries{
		{Capability: "ada@tenant.example", Transport: "http", Outcome: "ok"},
		{Capability: "assistant", Transport: "gpt-large-2026", Outcome: "ok"},
		{Capability: "assistant", Transport: "http", Outcome: "model HTTP endpoint failed: status 503"},
		{Capability: "", Transport: "http", Outcome: "ok"},
	} {
		if _, err := modelCalls(func() []application.ModelCallSeries { return []application.ModelCallSeries{series} })(t.Context()); err == nil {
			t.Errorf("series %+v was exported", series)
		}
	}
}

func TestModelCallMetricsAreOptionalAndDoNotMaskAGatherFailure(t *testing.T) {
	t.Parallel()
	registry, err := modelCalls(nil)(t.Context())
	if err != nil || strings.Contains(registry.Render(), "conduit_model_") {
		t.Fatalf("with no reader: %v %q", err, registry.Render())
	}
	failure := errors.New("database is down")
	_, err = application.WithModelCalls(
		func(context.Context) (*domain.Registry, error) { return nil, failure },
		func() []application.ModelCallSeries { return nil },
	)(t.Context())
	if !errors.Is(err, failure) {
		t.Fatalf("a gather failure was reported as %v", err)
	}
}
