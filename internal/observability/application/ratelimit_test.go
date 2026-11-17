package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/observability/application"
	"github.com/jonathanagustin/conduit/internal/observability/domain"
)

func gathered(read application.ReadRateLimits) func(context.Context) (*domain.Registry, error) {
	return application.WithRateLimits(
		application.Gather("1.4.2", func(context.Context) (application.ReconciliationStats, error) { return stats(), nil }), read,
	)
}

func TestRateLimitDecisionsAreExportedByClassAndOutcome(t *testing.T) {
	t.Parallel()
	registry, err := gathered(func() []application.RateLimitClass {
		return []application.RateLimitClass{
			{Class: "operator", Allowed: 40, Limited: 2, Callers: 3},
			{Class: "agent", Allowed: 9},
			{Class: "anonymous", Limited: 11, Callers: 1},
		}
	})(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Validate(); err != nil {
		t.Fatalf("the limiter metrics would be refused at publish time: %v", err)
	}
	text := registry.Render()
	for _, required := range []string{
		`conduit_rate_limit_decisions_total{class="operator",outcome="allowed"} 40`,
		`conduit_rate_limit_decisions_total{class="operator",outcome="limited"} 2`,
		`conduit_rate_limit_decisions_total{class="agent",outcome="limited"} 0`,
		`conduit_rate_limit_decisions_total{class="anonymous",outcome="limited"} 11`,
		`conduit_rate_limit_tracked_callers{class="operator"} 3`,
		"# TYPE conduit_rate_limit_decisions_total counter",
		"# TYPE conduit_rate_limit_tracked_callers gauge",
		// The reconciliation metrics are still there.
		`conduit_reconciliation_pending{queue="reconciliation"} 3`,
	} {
		if !strings.Contains(text, required) {
			t.Errorf("missing %q in:\n%s", required, text)
		}
	}
}

// The `class` label is on the allowlist because its values are a closed set.
// The exporter holds that set itself: a principal, an Agent or an address
// handed to it as a "class" is refused, not published.
func TestARateLimitClassOutsideTheClosedSetIsRefused(t *testing.T) {
	t.Parallel()
	for _, class := range []string{"alice@example.test", "prod/agent-1", "203.0.113.5", "environment", ""} {
		_, err := gathered(func() []application.RateLimitClass {
			return []application.RateLimitClass{{Class: class, Allowed: 1}}
		})(t.Context())
		if err == nil {
			t.Errorf("a rate-limit class of %q was exported", class)
		}
	}
}

func TestRateLimitMetricsAreOptionalAndDoNotMaskAGatherFailure(t *testing.T) {
	t.Parallel()
	registry, err := gathered(nil)(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(registry.Render(), "conduit_rate_limit") {
		t.Fatal("limiter metrics were exported with no limiter to read")
	}
	failure := errors.New("database is down")
	_, err = application.WithRateLimits(
		func(context.Context) (*domain.Registry, error) { return nil, failure },
		func() []application.RateLimitClass { return []application.RateLimitClass{{Class: "operator"}} },
	)(t.Context())
	if !errors.Is(err, failure) {
		t.Fatalf("a gather failure was reported as %v", err)
	}
}
