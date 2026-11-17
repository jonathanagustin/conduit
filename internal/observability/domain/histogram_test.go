package domain_test

import (
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/observability/domain"
)

func histogram(labels map[string]string) domain.Histogram {
	return domain.Histogram{
		Name: "conduit_work_seconds", Help: "how long a unit of work took", Labels: labels,
		Bounds: []float64{0.5, 2.5, 10}, Buckets: []uint64{3, 0, 2, 1}, Sum: 31.5,
	}
}

// Buckets are cumulative, in ascending numeric order, and end at +Inf. A
// lexical sort would put 10 before 2.5, and a scraper computing a quantile
// from out-of-order buckets is wrong without saying so.
func TestAHistogramRendersCumulativeBucketsInNumericOrder(t *testing.T) {
	t.Parallel()
	registry := domain.NewRegistry()
	registry.AddHistogram(histogram(map[string]string{"queue": "reconciliation"}))
	if err := registry.Validate(); err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"# HELP conduit_work_seconds how long a unit of work took",
		"# TYPE conduit_work_seconds histogram",
		`conduit_work_seconds_bucket{le="0.5",queue="reconciliation"} 3`,
		`conduit_work_seconds_bucket{le="2.5",queue="reconciliation"} 3`,
		`conduit_work_seconds_bucket{le="10",queue="reconciliation"} 5`,
		`conduit_work_seconds_bucket{le="+Inf",queue="reconciliation"} 6`,
		`conduit_work_seconds_sum{queue="reconciliation"} 31.5`,
		`conduit_work_seconds_count{queue="reconciliation"} 6`,
		"",
	}, "\n")
	if got := registry.Render(); got != want {
		t.Fatalf("rendered:\n%s\nwant:\n%s", got, want)
	}
}

// The allowlist is one rule. A distribution is not a way around it.
func TestAHistogramLabelNamingATenantResourceIsRefused(t *testing.T) {
	t.Parallel()
	for _, label := range []string{"environment", "product", "tenant", "path", "principal"} {
		registry := domain.NewRegistry()
		registry.AddHistogram(histogram(map[string]string{label: "prod"}))
		err := registry.Validate()
		if err == nil || !strings.Contains(err.Error(), "topology") {
			t.Errorf("a histogram labelled by %q was accepted, or refused without the reason: %v", label, err)
		}
	}
}

func TestAMalformedHistogramIsRefused(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*domain.Histogram){
		"no help":               func(h *domain.Histogram) { h.Help = " " },
		"an invalid name":       func(h *domain.Histogram) { h.Name = "conduit work" },
		"too few buckets":       func(h *domain.Histogram) { h.Buckets = h.Buckets[:3] },
		"descending bounds":     func(h *domain.Histogram) { h.Bounds = []float64{10, 2.5, 0.5} },
		"its own le label":      func(h *domain.Histogram) { h.Labels = map[string]string{"le": "1"} },
		"an unescapable label":  func(h *domain.Histogram) { h.Labels = map[string]string{"queue": "a\nb"} },
		"no bounds whatsoever":  func(h *domain.Histogram) { h.Bounds, h.Buckets = nil, []uint64{1} },
		"a label off the list ": func(h *domain.Histogram) { h.Labels = map[string]string{"installation": "x"} },
	} {
		candidate := histogram(nil)
		mutate(&candidate)
		registry := domain.NewRegistry()
		registry.AddHistogram(candidate)
		if err := registry.Validate(); err == nil {
			t.Errorf("a histogram with %s was accepted", name)
		}
	}
}

// One family has one set of bounds, and no scalar may take a name the
// histogram renders: either would give a scraper two meanings for one series.
func TestAHistogramFamilyCannotBeAmbiguous(t *testing.T) {
	t.Parallel()
	other := histogram(map[string]string{"queue": "proposal_outbox"})
	other.Bounds = []float64{1, 2, 3}
	twoShapes := domain.NewRegistry()
	twoShapes.AddHistogram(histogram(map[string]string{"queue": "reconciliation"}))
	twoShapes.AddHistogram(other)
	if err := twoShapes.Validate(); err == nil {
		t.Error("one histogram name was accepted with two sets of bounds")
	}
	for _, name := range []string{"conduit_work_seconds", "conduit_work_seconds_count", "conduit_work_seconds_bucket"} {
		collision := domain.NewRegistry(gauge(name, 1, nil))
		collision.AddHistogram(histogram(nil))
		if err := collision.Validate(); err == nil {
			t.Errorf("a gauge named %s was accepted beside the histogram that renders it", name)
		}
	}
}

// Families stay sorted by name whatever kind they are, so two scrapes of one
// state are still byte-identical.
func TestHistogramsAndScalarsRenderInOneNameOrder(t *testing.T) {
	t.Parallel()
	build := func() string {
		registry := domain.NewRegistry(gauge("conduit_zeta", 1, nil), gauge("conduit_alpha", 1, nil))
		registry.AddHistogram(histogram(map[string]string{"queue": "b"}))
		registry.AddHistogram(histogram(map[string]string{"queue": "a"}))
		return registry.Render()
	}
	rendered := build()
	if rendered != build() {
		t.Fatal("two renders of one state differ")
	}
	alpha, work, zeta := strings.Index(rendered, "conduit_alpha"), strings.Index(rendered, "conduit_work_seconds"), strings.Index(rendered, "conduit_zeta")
	if !(alpha < work && work < zeta) {
		t.Fatalf("families are not in name order:\n%s", rendered)
	}
	if strings.Count(rendered, "# TYPE conduit_work_seconds histogram") != 1 {
		t.Fatalf("TYPE was not emitted exactly once for the family:\n%s", rendered)
	}
	if strings.Index(rendered, `queue="a"`) > strings.Index(rendered, `queue="b"`) {
		t.Fatalf("series within a family are not in label order:\n%s", rendered)
	}
}
