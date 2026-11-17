package domain

import (
	"fmt"
	"sort"
	"strings"
)

// Histogram is one distribution: how many observations fell at or under each
// bound, and their sum.
//
// It is a separate type rather than a third Kind on Metric because one
// histogram renders as many lines under one TYPE -- a `_bucket` per bound, a
// `_sum` and a `_count` -- and the bounds have to stay in numeric order, which
// the name-then-label sort Metric uses would not keep ("10" sorts before "2.5").
//
// Buckets holds per-bound counts, not cumulative ones, with one more entry than
// Bounds: the last is everything above the highest bound. Rendering does the
// accumulation, once, so a caller cannot publish a histogram whose buckets
// decrease.
type Histogram struct {
	Name    string
	Help    string
	Labels  map[string]string
	Bounds  []float64
	Buckets []uint64
	Sum     float64
}

// AddHistogram appends a distribution.
func (registry *Registry) AddHistogram(histogram Histogram) {
	registry.histograms = append(registry.histograms, histogram)
}

// histogramOwns reports whether a scalar name is one a histogram renders.
func (registry *Registry) histogramOwns(name string) bool {
	for _, histogram := range registry.histograms {
		for _, suffix := range []string{"", "_bucket", "_sum", "_count"} {
			if name == histogram.Name+suffix {
				return true
			}
		}
	}
	return false
}

func (registry *Registry) validateHistograms() error {
	shapes := map[string]string{}
	for _, histogram := range registry.histograms {
		if !validMetricName(histogram.Name) {
			return fmt.Errorf("histogram name %q is not a valid exposition name", histogram.Name)
		}
		if strings.TrimSpace(histogram.Help) == "" {
			return fmt.Errorf("histogram %s has no help text", histogram.Name)
		}
		if len(histogram.Bounds) == 0 || len(histogram.Buckets) != len(histogram.Bounds)+1 {
			return fmt.Errorf("histogram %s needs one bucket per bound and one above the last", histogram.Name)
		}
		if !sort.Float64sAreSorted(histogram.Bounds) {
			return fmt.Errorf("histogram %s has bounds that do not ascend", histogram.Name)
		}
		// One family, one set of bounds. A scraper aggregates buckets across
		// series by `le`; two series of one name with different bounds sum
		// into a quantile that describes neither.
		shape := fmt.Sprint(histogram.Bounds)
		if previous, seen := shapes[histogram.Name]; seen && previous != shape {
			return fmt.Errorf("histogram %s is exported with two different sets of bounds", histogram.Name)
		}
		shapes[histogram.Name] = shape
		if _, reserved := histogram.Labels["le"]; reserved {
			return fmt.Errorf("histogram %s sets the le label, which rendering owns", histogram.Name)
		}
		if err := validateLabels(histogram.Name, histogram.Labels); err != nil {
			return err
		}
	}
	return nil
}

// renderHistograms writes each distribution in the text exposition format:
// cumulative `_bucket` lines in ascending bound order ending at +Inf, then
// `_sum` and `_count`.
func (registry *Registry) renderHistograms(family func(string) *strings.Builder, described map[string]bool) {
	ordered := make([]Histogram, len(registry.histograms))
	copy(ordered, registry.histograms)
	sort.SliceStable(ordered, func(left, right int) bool {
		if ordered[left].Name != ordered[right].Name {
			return ordered[left].Name < ordered[right].Name
		}
		return renderLabels(ordered[left].Labels) < renderLabels(ordered[right].Labels)
	})
	for _, histogram := range ordered {
		out := family(histogram.Name)
		if !described[histogram.Name] {
			fmt.Fprintf(out, "# HELP %s %s\n", histogram.Name, escapeHelp(histogram.Help))
			fmt.Fprintf(out, "# TYPE %s histogram\n", histogram.Name)
			described[histogram.Name] = true
		}
		var cumulative uint64
		for index, count := range histogram.Buckets {
			cumulative += count
			bound := "+Inf"
			if index < len(histogram.Bounds) {
				bound = formatValue(histogram.Bounds[index])
			}
			fmt.Fprintf(out, "%s_bucket%s %d\n", histogram.Name, renderLabels(withLabel(histogram.Labels, "le", bound)), cumulative)
		}
		fmt.Fprintf(out, "%s_sum%s %s\n", histogram.Name, renderLabels(histogram.Labels), formatValue(histogram.Sum))
		fmt.Fprintf(out, "%s_count%s %d\n", histogram.Name, renderLabels(histogram.Labels), cumulative)
	}
}

func withLabel(labels map[string]string, name, value string) map[string]string {
	extended := make(map[string]string, len(labels)+1)
	for key, existing := range labels {
		extended[key] = existing
	}
	extended[name] = value
	return extended
}
