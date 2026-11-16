// Package workflow runs interactive planning sessions as durable Temporal
// workflows with structured logging, bounded metrics, and activity heartbeats.
package workflow

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.temporal.io/sdk/client"
	temporallog "go.temporal.io/sdk/log"

	observability "github.com/jonathanagustin/conduit/internal/observability/domain"
)

// Metric names exported by the Temporal planning workflow recorder.
// All labels use only the closed allowlist in internal/observability/domain
// ("queue", "outcome") so tenant identifiers never appear in Prometheus scrapes.
const (
	MetricPlanningTurnsTotal    = "conduit_planning_workflow_turns_total"
	MetricPlanningAcceptsTotal  = "conduit_planning_workflow_accepts_total"
	MetricPlanningActivityCalls = "conduit_planning_workflow_activity_calls_total"
	MetricPlanningActiveGauge   = "conduit_planning_workflow_active_sessions"
)

// StructuredLogger adapts a standard slog.Logger to Temporal's replay-safe
// log.Logger interface.
func StructuredLogger(logger *slog.Logger) temporallog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return temporallog.NewStructuredLogger(logger)
}

// MetricsRecorder implements Temporal's client.MetricsHandler and snapshots
// aggregate counters and gauges into Conduit's observability.Registry without
// high-cardinality tenant labels.
type MetricsRecorder struct {
	state *metricsState
	tags  map[string]string
}

type metricsState struct {
	mu       sync.RWMutex
	counters map[string]*atomic.Int64
	gauges   map[string]*atomic.Int64
	timers   map[string]*atomic.Int64 // cumulative milliseconds
}

// NewMetricsRecorder creates a thread-safe Temporal metrics handler and
// Conduit observability snapshot provider.
func NewMetricsRecorder() *MetricsRecorder {
	return &MetricsRecorder{
		state: &metricsState{
			counters: make(map[string]*atomic.Int64),
			gauges:   make(map[string]*atomic.Int64),
			timers:   make(map[string]*atomic.Int64),
		},
		tags: make(map[string]string),
	}
}

// WithTags returns a child handler carrying bounded tags.
func (m *MetricsRecorder) WithTags(tags map[string]string) client.MetricsHandler {
	merged := make(map[string]string, len(m.tags)+len(tags))
	for k, v := range m.tags {
		merged[k] = v
	}
	for k, v := range tags {
		merged[k] = v
	}
	return &MetricsRecorder{state: m.state, tags: merged}
}

// Counter returns a counter handle for the given metric name.
func (m *MetricsRecorder) Counter(name string) client.MetricsCounter {
	key := m.metricKey(name)
	m.state.mu.Lock()
	c, ok := m.state.counters[key]
	if !ok {
		c = &atomic.Int64{}
		m.state.counters[key] = c
	}
	m.state.mu.Unlock()
	return &atomicCounter{val: c}
}

// Gauge returns a gauge handle for the given metric name.
func (m *MetricsRecorder) Gauge(name string) client.MetricsGauge {
	key := m.metricKey(name)
	m.state.mu.Lock()
	g, ok := m.state.gauges[key]
	if !ok {
		g = &atomic.Int64{}
		m.state.gauges[key] = g
	}
	m.state.mu.Unlock()
	return &atomicGauge{val: g}
}

// Timer returns a timer handle for the given metric name.
func (m *MetricsRecorder) Timer(name string) client.MetricsTimer {
	key := m.metricKey(name)
	m.state.mu.Lock()
	t, ok := m.state.timers[key]
	if !ok {
		t = &atomic.Int64{}
		m.state.timers[key] = t
	}
	m.state.mu.Unlock()
	return &atomicTimer{val: t}
}

func (m *MetricsRecorder) metricKey(name string) string {
	outcome := m.tags["outcome"]
	if outcome == "" {
		return name
	}
	return name + "|outcome=" + outcome
}

// CounterValue reads the current value of a counter for testing and diagnostics.
func (m *MetricsRecorder) CounterValue(name, outcome string) int64 {
	key := name
	if outcome != "" {
		key = name + "|outcome=" + outcome
	}
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	if c, ok := m.state.counters[key]; ok {
		return c.Load()
	}
	return 0
}

// TimerMillis reads cumulative recorded timer milliseconds for a metric name.
func (m *MetricsRecorder) TimerMillis(name string) int64 {
	m.state.mu.RLock()
	defer m.state.mu.RUnlock()
	if t, ok := m.state.timers[name]; ok {
		return t.Load()
	}
	return 0
}

// Snapshot appends the bounded planning workflow metrics to an observability.Registry.
func (m *MetricsRecorder) Snapshot(reg *observability.Registry) {
	if reg == nil {
		return
	}
	for _, outcome := range []string{"pass", "fail"} {
		reg.Add(observability.Metric{
			Name:   MetricPlanningTurnsTotal,
			Kind:   observability.KindCounter,
			Help:   "Total planning workflow turns executed via Temporal.",
			Labels: map[string]string{"queue": TaskQueue, "outcome": outcome},
			Value:  float64(m.CounterValue(MetricPlanningTurnsTotal, outcome)),
		})
		reg.Add(observability.Metric{
			Name:   MetricPlanningAcceptsTotal,
			Kind:   observability.KindCounter,
			Help:   "Total planning workflow accept transitions committed via Temporal.",
			Labels: map[string]string{"queue": TaskQueue, "outcome": outcome},
			Value:  float64(m.CounterValue(MetricPlanningAcceptsTotal, outcome)),
		})
	}
}

type atomicCounter struct{ val *atomic.Int64 }

func (c *atomicCounter) Inc(delta int64) { c.val.Add(delta) }

type atomicGauge struct{ val *atomic.Int64 }

func (g *atomicGauge) Update(value float64) { g.val.Store(int64(value)) }

type atomicTimer struct{ val *atomic.Int64 }

func (t *atomicTimer) Record(duration time.Duration) {
	ms := duration.Milliseconds()
	if ms == 0 && duration > 0 {
		ms = 1
	}
	t.val.Add(ms)
}
