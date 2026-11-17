package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/hub/ratelimit"
	metricsapplication "github.com/jonathanagustin/conduit/internal/observability/application"
)

func limitEnvironment(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func burstOf(limiter *ratelimit.Limiter, class ratelimit.Class) int {
	admitted := 0
	for range 5000 {
		if !limiter.Allow(class, "caller").Allowed {
			break
		}
		admitted++
	}
	return admitted
}

// A Hub with no rate-limit configuration is still limited, and the Agent class
// keeps a budget of its own.
func TestRequestLimiterHasNonZeroDefaults(t *testing.T) {
	limiter, err := loadRequestLimiter(limitEnvironment(nil))
	if err != nil {
		t.Fatal(err)
	}
	defaults := ratelimit.DefaultConfig()
	for class, limit := range map[ratelimit.Class]ratelimit.Limit{
		ratelimit.ClassOperator: defaults.Operator, ratelimit.ClassAgent: defaults.Agent, ratelimit.ClassAnonymous: defaults.Anonymous,
	} {
		// The clock is real here, so a few tokens may refill during the loop.
		if got := burstOf(limiter, class); got < limit.Burst || got > limit.Burst+int(limit.PerSecond) {
			t.Errorf("%s default admitted %d at once, want its burst of %d", class, got, limit.Burst)
		}
	}
}

func TestRequestLimiterReadsEachClassFromItsOwnVariables(t *testing.T) {
	limiter, err := loadRequestLimiter(limitEnvironment(map[string]string{
		"RATE_LIMIT_OPERATOR_RPS": "0.001", "RATE_LIMIT_OPERATOR_BURST": "7",
		"RATE_LIMIT_AGENT_RPS": " 0.001 ", "RATE_LIMIT_AGENT_BURST": "11",
		"RATE_LIMIT_ANONYMOUS_RPS": "0.001", "RATE_LIMIT_ANONYMOUS_BURST": "2",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for class, want := range map[ratelimit.Class]int{
		ratelimit.ClassOperator: 7, ratelimit.ClassAgent: 11, ratelimit.ClassAnonymous: 2,
	} {
		if got := burstOf(limiter, class); got != want {
			t.Errorf("%s admitted %d at once, want the configured burst of %d", class, got, want)
		}
	}
}

// A limit that is set and unusable stops the Hub; it does not silently become
// the default, and no value turns the limiter off.
func TestRequestLimiterRefusesUnusableValues(t *testing.T) {
	for variable, values := range map[string][]string{
		"RATE_LIMIT_OPERATOR_RPS":    {"0", "-1", "fast", "NaN", "Inf", "1e9"},
		"RATE_LIMIT_AGENT_RPS":       {"0", "off"},
		"RATE_LIMIT_ANONYMOUS_RPS":   {"0", "unlimited"},
		"RATE_LIMIT_OPERATOR_BURST":  {"0", "-5", "1.5", "many", "10000000"},
		"RATE_LIMIT_AGENT_BURST":     {"0"},
		"RATE_LIMIT_ANONYMOUS_BURST": {"0", "none"},
	} {
		for _, value := range values {
			_, err := loadRequestLimiter(limitEnvironment(map[string]string{variable: value}))
			if err == nil || !strings.Contains(err.Error(), variable) {
				t.Errorf("%s=%q: error = %v, want a refusal naming the variable", variable, value, err)
			}
		}
	}
}

// Limiter decisions are exported per class, and nothing a caller chose -- a
// username, an Agent, an address -- reaches the scrape.
func TestMetricsListenerExportsLimiterDecisionsByClassOnly(t *testing.T) {
	limiter, err := loadRequestLimiter(limitEnvironment(map[string]string{
		"RATE_LIMIT_OPERATOR_RPS": "0.001", "RATE_LIMIT_OPERATOR_BURST": "2",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		limiter.Allow(ratelimit.ClassOperator, "user:alice@example.test")
	}
	limiter.Allow(ratelimit.ClassAgent, "prod-east/agent-7")
	limiter.Allow(ratelimit.ClassAnonymous, "203.0.113.5")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	stop := startMetricsListener(t.Context(), address, func(context.Context) (metricsapplication.ReconciliationStats, error) {
		return metricsapplication.ReconciliationStats{}, nil
	}, requestLimiterMetrics(limiter))
	if stop == nil {
		t.Fatal("no listener started")
	}
	defer stop()
	var body string
	for attempt := 0; attempt < 50 && body == ""; attempt++ {
		response, err := http.Get("http://" + address + "/metrics")
		if err != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		payload, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("metrics = %d: %s", response.StatusCode, payload)
		}
		body = string(payload)
	}
	for _, required := range []string{
		`conduit_rate_limit_decisions_total{class="operator",outcome="allowed"} 2`,
		`conduit_rate_limit_decisions_total{class="operator",outcome="limited"} 3`,
		`conduit_rate_limit_decisions_total{class="agent",outcome="allowed"} 1`,
		`conduit_rate_limit_decisions_total{class="anonymous",outcome="limited"} 0`,
		`conduit_rate_limit_tracked_callers{class="operator"} 1`,
		`# TYPE conduit_rate_limit_decisions_total counter`,
	} {
		if !strings.Contains(body, required) {
			t.Errorf("missing %q in:\n%s", required, body)
		}
	}
	for _, leaked := range []string{"alice", "prod-east", "agent-7", "203.0.113"} {
		if strings.Contains(body, leaked) {
			t.Errorf("the scrape names a caller (%q):\n%s", leaked, body)
		}
	}
}
