// Package domain owns what Conduit can say about its own health, and the exact
// text a scraper reads.
//
// Conduit exports nothing today. `docs/operations/PRODUCTION-READINESS.md`
// records that as a Tier 1 blocker, and the reason it is Tier 1 rather than a
// nice-to-have is narrow: the Hub runs two replicas over a leased PostgreSQL
// store, and there is no way to tell a working one from a wedged one. Both
// answer their Kubernetes probes. Both hold their lease. A backlog that stops
// draining looks exactly like a fleet that has converged.
//
// Two decisions are worth stating before the code, because both are easy to
// undo by accident.
//
// # No client library
//
// The Prometheus text exposition format is small, stable and specified, and
// this repository keeps eight direct dependencies, each of which earned its
// place. A metrics client would bring a protobuf and procfs tree in to render
// text that fits in this file. The cost of owning it is one format to keep
// correct; the cost of the alternative is a supply-chain surface for a
// diagnostic endpoint.
//
// # No tenant identifiers in labels
//
// This is the load-bearing one. A gauge labelled by environment or product
// would be the obvious design and it is refused twice over: it publishes the
// tenant's topology to anyone who can reach the scrape endpoint, and it makes
// series count grow with the fleet, which is how a metrics system takes down
// the thing it was installed to watch. Everything here is an aggregate. Labels
// exist for bounded, operator-chosen dimensions only, and `Registry.Validate`
// refuses the rest rather than trusting a convention.
package domain

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Kind distinguishes the two shapes a scraper reads differently: a counter only
// ever rises and a rate over it is meaningful, while a gauge moves both ways and
// a rate over it is nonsense.
type Kind string

const (
	KindCounter Kind = "counter"
	KindGauge   Kind = "gauge"
)

// Metric is one exported series.
//
// Value is a float64 because that is what the exposition format carries, not
// because Conduit measures anything fractional; the conversion happens once,
// here, rather than at each call site guessing at a format.
type Metric struct {
	Name   string
	Kind   Kind
	Help   string
	Labels map[string]string
	Value  float64
}

// labelNames are the only labels any metric may carry.
//
// An allowlist rather than a denylist of tenant fields, because the failure mode
// is someone adding a label nobody thought to forbid. A new dimension has to be
// added here deliberately, which is the review this needs.
var labelNames = map[string]struct{}{
	// The build a replica is running, so a rollout that half-succeeded is
	// visible as two versions serving at once. Bounded by releases, not by
	// fleet size.
	"version": {},
	// Which of the reconciliation queues a depth refers to. A closed set, in
	// this package, not derived from data.
	"queue": {},
	// Terminal outcome of a unit of work. Closed set.
	"outcome": {},
}

// Registry is a snapshot of metrics ready to render.
//
// Deliberately not a live collector with registered callbacks. The Hub's numbers
// come from one SQL round trip, and a registry that invoked callbacks during
// rendering would run that query inside the scrape handler, where a slow
// database turns a monitoring request into a stuck goroutine. The caller
// gathers, then builds this.
type Registry struct {
	metrics []Metric
}

// NewRegistry builds a registry from a gathered snapshot.
func NewRegistry(metrics ...Metric) *Registry {
	return &Registry{metrics: metrics}
}

// Add appends a metric.
func (registry *Registry) Add(metric Metric) { registry.metrics = append(registry.metrics, metric) }

// Validate refuses a registry that would leak tenant data, blow up cardinality,
// or render text a scraper cannot parse.
//
// Called by the handler before rendering rather than by the caller, so a metric
// added anywhere is checked on the path that publishes it.
func (registry *Registry) Validate() error {
	seen := map[string]Kind{}
	for _, metric := range registry.metrics {
		if !validMetricName(metric.Name) {
			return fmt.Errorf("metric name %q is not a valid exposition name", metric.Name)
		}
		if metric.Kind != KindCounter && metric.Kind != KindGauge {
			return fmt.Errorf("metric %s has kind %q, which is neither a counter nor a gauge", metric.Name, metric.Kind)
		}
		if strings.TrimSpace(metric.Help) == "" {
			// An unexplained series is one an operator has to guess at during
			// the incident it was added for.
			return fmt.Errorf("metric %s has no help text", metric.Name)
		}
		if kind, already := seen[metric.Name]; already && kind != metric.Kind {
			return fmt.Errorf("metric %s is exported as both a %s and a %s", metric.Name, kind, metric.Kind)
		}
		seen[metric.Name] = metric.Kind
		for label, value := range metric.Labels {
			if _, allowed := labelNames[label]; !allowed {
				return fmt.Errorf(
					"metric %s carries label %q, which is not on the allowlist. Labels naming a tenant's "+
						"environments, products or entities publish its topology to anyone who can scrape and "+
						"grow the series count with the fleet; export an aggregate instead",
					metric.Name, label,
				)
			}
			if strings.ContainsAny(value, "\n\"\\") {
				return fmt.Errorf("metric %s label %q has a value that cannot be escaped safely", metric.Name, label)
			}
		}
	}
	return nil
}

// Render writes the Prometheus text exposition format.
//
// Sorted by name and then by rendered labels, so two scrapes of the same state
// are byte-identical. That is not cosmetic: it makes the output diffable in a
// test, and it stops a map iteration turning into a spurious change.
//
// HELP and TYPE are emitted once per name, which the format requires and which a
// naive per-metric loop gets wrong the moment one name carries two label sets.
func (registry *Registry) Render() string {
	ordered := make([]Metric, len(registry.metrics))
	copy(ordered, registry.metrics)
	sort.SliceStable(ordered, func(left, right int) bool {
		if ordered[left].Name != ordered[right].Name {
			return ordered[left].Name < ordered[right].Name
		}
		return renderLabels(ordered[left].Labels) < renderLabels(ordered[right].Labels)
	})

	var out strings.Builder
	described := map[string]bool{}
	for _, metric := range ordered {
		if !described[metric.Name] {
			fmt.Fprintf(&out, "# HELP %s %s\n", metric.Name, escapeHelp(metric.Help))
			fmt.Fprintf(&out, "# TYPE %s %s\n", metric.Name, metric.Kind)
			described[metric.Name] = true
		}
		fmt.Fprintf(&out, "%s%s %s\n", metric.Name, renderLabels(metric.Labels), formatValue(metric.Value))
	}
	return out.String()
}

func renderLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	sort.Strings(names)
	pairs := make([]string, 0, len(names))
	for _, name := range names {
		pairs = append(pairs, fmt.Sprintf("%s=%q", name, labels[name]))
	}
	return "{" + strings.Join(pairs, ",") + "}"
}

// formatValue renders without an exponent where possible, because a scraper
// reads either but a human reading a curl does not read 1.234e+06 as a queue
// depth. Integers render as integers for the same reason.
func formatValue(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// escapeHelp keeps HELP on one line. A newline in help text would be read as the
// start of a new metric line and corrupt everything after it.
func escapeHelp(help string) string {
	return strings.NewReplacer("\\", `\\`, "\n", `\n`).Replace(help)
}

func validMetricName(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		switch {
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z', character == '_':
		case character >= '0' && character <= '9' && index > 0:
		default:
			return false
		}
	}
	return true
}
