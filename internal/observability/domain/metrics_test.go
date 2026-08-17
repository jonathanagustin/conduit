package domain_test

import (
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/observability/domain"
)

func gauge(name string, value float64, labels map[string]string) domain.Metric {
	return domain.Metric{Name: name, Kind: domain.KindGauge, Help: "a described series", Labels: labels, Value: value}
}

// The decision this package exists to enforce, and the one most likely to be
// undone by someone adding the obviously useful thing.
//
// A gauge labelled by environment publishes the tenant's topology to anyone who
// can reach the scrape endpoint, and grows the series count with the fleet --
// which is how a metrics system takes down what it was installed to watch. The
// allowlist is checked on the publishing path so a label added anywhere is
// caught here rather than in production.
func TestALabelNamingATenantResourceIsRefused(t *testing.T) {
	t.Parallel()
	for _, label := range []string{"environment", "product", "entity", "installation", "tenant", "namespace"} {
		registry := domain.NewRegistry(gauge("conduit_work_pending", 3, map[string]string{label: "prod"}))
		err := registry.Validate()
		if err == nil {
			t.Errorf("a metric labelled by %q was accepted", label)
			continue
		}
		// The message has to say why, because the person who hits this is
		// adding a label they believe is useful and will otherwise just
		// allowlist it.
		if !strings.Contains(err.Error(), "topology") {
			t.Errorf("the refusal for %q does not explain the risk: %v", label, err)
		}
	}
}

// And the other half: the bounded dimensions must still work, or the rule is
// unusable and gets removed rather than followed.
func TestTheAllowlistedLabelsAreAccepted(t *testing.T) {
	t.Parallel()
	registry := domain.NewRegistry(
		gauge("conduit_build_info", 1, map[string]string{"version": "1.4.2"}),
		gauge("conduit_work_pending", 7, map[string]string{"queue": "reconciliation"}),
		gauge("conduit_work_finished", 2, map[string]string{"outcome": "failed"}),
	)
	if err := registry.Validate(); err != nil {
		t.Fatalf("a metric with only allowlisted labels was refused: %v", err)
	}
}

// Two scrapes of one state must be byte-identical. Map iteration is the usual
// way this rots, and a scrape that reshuffles makes every diff meaningless and
// every "did it change?" unanswerable.
func TestRenderingIsDeterministic(t *testing.T) {
	t.Parallel()
	build := func() *domain.Registry {
		return domain.NewRegistry(
			gauge("conduit_work_pending", 7, map[string]string{"queue": "outbox"}),
			gauge("conduit_build_info", 1, map[string]string{"version": "1.4.2"}),
			gauge("conduit_work_pending", 3, map[string]string{"queue": "reconciliation"}),
		)
	}
	first, second := build().Render(), build().Render()
	if first != second {
		t.Fatalf("two renders of one state differ:\n%s\n---\n%s", first, second)
	}
	// Sorted by name, then by labels, so outbox precedes reconciliation.
	if strings.Index(first, `queue="outbox"`) > strings.Index(first, `queue="reconciliation"`) {
		t.Errorf("label ordering is not stable within a name:\n%s", first)
	}
	if strings.Index(first, "conduit_build_info") > strings.Index(first, "conduit_work_pending") {
		t.Errorf("metrics are not ordered by name:\n%s", first)
	}
}

// HELP and TYPE are per name, not per series. A loop that emits them per metric
// produces a second TYPE line for the same name, which is a parse error for the
// scraper and silently drops the whole family.
func TestHelpAndTypeAreEmittedOncePerName(t *testing.T) {
	t.Parallel()
	rendered := domain.NewRegistry(
		gauge("conduit_work_pending", 3, map[string]string{"queue": "reconciliation"}),
		gauge("conduit_work_pending", 7, map[string]string{"queue": "outbox"}),
	).Render()
	if got := strings.Count(rendered, "# TYPE conduit_work_pending"); got != 1 {
		t.Errorf("TYPE emitted %d times for one name:\n%s", got, rendered)
	}
	if got := strings.Count(rendered, "# HELP conduit_work_pending"); got != 1 {
		t.Errorf("HELP emitted %d times for one name:\n%s", got, rendered)
	}
	if got := strings.Count(rendered, "conduit_work_pending{"); got != 2 {
		t.Errorf("expected two series under one name, got %d:\n%s", got, rendered)
	}
}

// One name cannot be both kinds. A rate over a gauge is meaningless, so a
// scraper that is told a series is a counter will compute nonsense from it, and
// the mistake is invisible until someone builds an alert on the result.
func TestOneNameCannotBeBothACounterAndAGauge(t *testing.T) {
	t.Parallel()
	registry := domain.NewRegistry(
		domain.Metric{Name: "conduit_thing", Kind: domain.KindGauge, Help: "h", Value: 1},
		domain.Metric{Name: "conduit_thing", Kind: domain.KindCounter, Help: "h", Value: 1},
	)
	if err := registry.Validate(); err == nil {
		t.Fatal("one name was accepted as both a counter and a gauge")
	}
}

// A series nobody can interpret is one an operator has to guess at during the
// incident it was added for.
func TestAMetricWithoutHelpIsRefused(t *testing.T) {
	t.Parallel()
	registry := domain.NewRegistry(domain.Metric{Name: "conduit_thing", Kind: domain.KindGauge, Value: 1})
	if err := registry.Validate(); err == nil {
		t.Fatal("a metric with no help text was accepted")
	}
}

// Text that cannot be parsed is worse than no text: the scraper drops the whole
// payload, so one bad series hides every good one behind it.
func TestValuesThatWouldCorruptTheOutputAreRefused(t *testing.T) {
	t.Parallel()
	for name, metric := range map[string]domain.Metric{
		"invalid name":  {Name: "conduit-work pending", Kind: domain.KindGauge, Help: "h", Value: 1},
		"leading digit": {Name: "1conduit", Kind: domain.KindGauge, Help: "h", Value: 1},
		"unknown kind":  {Name: "conduit_thing", Kind: "summary", Help: "h", Value: 1},
		"newline in a label": {
			Name: "conduit_thing", Kind: domain.KindGauge, Help: "h", Value: 1,
			Labels: map[string]string{"version": "1.0\n# TYPE evil counter"},
		},
	} {
		if err := domain.NewRegistry(metric).Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// A newline in help would be read as the start of the next line and corrupt
// everything after it. Escaped rather than refused, because help text is written
// by whoever adds the metric and wrapping it is a reasonable thing to do.
func TestHelpTextIsEscapedRatherThanBreakingTheOutput(t *testing.T) {
	t.Parallel()
	rendered := domain.NewRegistry(domain.Metric{
		Name: "conduit_thing", Kind: domain.KindGauge, Value: 1,
		Help: "first line\nsecond line",
	}).Render()
	if strings.Count(rendered, "\n") != 3 {
		t.Errorf("help text broke the line structure:\n%q", rendered)
	}
	if !strings.Contains(rendered, `first line\nsecond line`) {
		t.Errorf("help text was not escaped:\n%s", rendered)
	}
}

// Whole numbers render as whole numbers. A queue depth of 1234567 shown as
// 1.234567e+06 is technically parseable and unreadable to the person running
// curl during an incident.
func TestWholeNumbersRenderWithoutAnExponent(t *testing.T) {
	t.Parallel()
	rendered := domain.NewRegistry(gauge("conduit_thing", 1234567, nil)).Render()
	if !strings.Contains(rendered, "conduit_thing 1234567\n") {
		t.Errorf("a whole number did not render plainly:\n%s", rendered)
	}
	fractional := domain.NewRegistry(gauge("conduit_seconds", 1.5, nil)).Render()
	if !strings.Contains(fractional, "conduit_seconds 1.5\n") {
		t.Errorf("a fractional value did not render plainly:\n%s", fractional)
	}
}
