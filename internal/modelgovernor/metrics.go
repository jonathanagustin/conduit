package modelgovernor

import (
	"sort"
	"sync"
)

// Metrics counts model calls in this process. Its key is capability,
// transport and outcome: three closed sets, so the series count is fixed at
// compile time. An owner is never a key, and neither is a model name: a
// provider-reported id is remote text, and per-model numbers are in the usage
// store, which is access-controlled where a scrape endpoint is not.
type Metrics struct {
	mu            sync.Mutex
	series        map[MetricKey]*MetricSample
	usageFailures uint64
}

// ProcessMetrics is the default sink: one process, one set of counters.
var ProcessMetrics = NewMetrics()

func NewMetrics() *Metrics { return &Metrics{series: map[MetricKey]*MetricSample{}} }

// MetricKey is the label set of one series.
type MetricKey struct {
	Capability string
	Transport  string
	Outcome    string
}

// MetricSample is the counters of one series. All of them only rise.
type MetricSample struct {
	MetricKey
	Calls uint64
	// Retries is attempts beyond each call's first.
	Retries          uint64
	DurationSeconds  float64
	PromptTokens     uint64
	CompletionTokens uint64
}

func (metrics *Metrics) observe(record UsageRecord) {
	key := MetricKey{Capability: record.Capability, Transport: record.Transport, Outcome: record.Outcome}
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	sample := metrics.series[key]
	if sample == nil {
		sample = &MetricSample{MetricKey: key}
		metrics.series[key] = sample
	}
	sample.Calls++
	if record.Attempts > 1 {
		sample.Retries += uint64(record.Attempts - 1)
	}
	sample.DurationSeconds += record.Latency.Seconds()
	if record.PromptTokens != nil {
		sample.PromptTokens += uint64(*record.PromptTokens)
	}
	if record.CompletionTokens != nil {
		sample.CompletionTokens += uint64(*record.CompletionTokens)
	}
}

func (metrics *Metrics) usageWriteFailed() {
	metrics.mu.Lock()
	metrics.usageFailures++
	metrics.mu.Unlock()
}

// Snapshot is every series, in a stable order.
func (metrics *Metrics) Snapshot() []MetricSample {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	out := make([]MetricSample, 0, len(metrics.series))
	for _, sample := range metrics.series {
		out = append(out, *sample)
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := out[i].MetricKey, out[j].MetricKey
		if left.Capability != right.Capability {
			return left.Capability < right.Capability
		}
		if left.Transport != right.Transport {
			return left.Transport < right.Transport
		}
		return left.Outcome < right.Outcome
	})
	return out
}

// UsageWriteFailures is how many usage records could not be persisted.
func (metrics *Metrics) UsageWriteFailures() uint64 {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	return metrics.usageFailures
}
