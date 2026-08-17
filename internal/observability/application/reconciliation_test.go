package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/observability/application"
)

func stats() application.ReconciliationStats {
	return application.ReconciliationStats{
		WorkPending: 3, WorkLeased: 2, WorkExhausted: 1,
		OldestPendingAge: 90 * time.Second,
		OutboxPending:    7, OutboxLeased: 0, OutboxExhausted: 0,
		OldestOutboxAge: 0,
	}
}

func render(t *testing.T, source application.ReconciliationStats) string {
	t.Helper()
	registry, err := application.Gather("1.4.2", func(context.Context) (application.ReconciliationStats, error) {
		return source, nil
	})(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// Validate on the way through: these are the metrics that actually ship, so
	// if the naming here would be refused by the publishing path the test should
	// say so rather than asserting on text nobody could serve.
	if err := registry.Validate(); err != nil {
		t.Fatalf("the metrics this package builds would be refused at publish time: %v", err)
	}
	return registry.Render()
}

func TestBothQueuesAreReportedUnderOneNameSet(t *testing.T) {
	t.Parallel()
	rendered := render(t, stats())
	for _, required := range []string{
		`conduit_reconciliation_pending{queue="reconciliation"} 3`,
		`conduit_reconciliation_leased{queue="reconciliation"} 2`,
		`conduit_reconciliation_exhausted{queue="reconciliation"} 1`,
		`conduit_reconciliation_pending{queue="proposal_outbox"} 7`,
		`conduit_build_info{version="1.4.2"} 1`,
	} {
		if !strings.Contains(rendered, required) {
			t.Errorf("missing %q:\n%s", required, rendered)
		}
	}
}

// The signal the whole endpoint exists for. Depth alone cannot separate a busy
// Hub from a stuck one -- both sit at some number. Age can, so it has to be
// exported and it has to be in seconds, which is what an alert expression and a
// human both read without conversion.
func TestTheLagSignalIsExportedInSeconds(t *testing.T) {
	t.Parallel()
	rendered := render(t, stats())
	if !strings.Contains(rendered, `conduit_reconciliation_oldest_pending_seconds{queue="reconciliation"} 90`) {
		t.Errorf("the reconciliation lag signal is wrong or missing:\n%s", rendered)
	}
	// Nothing pending means no lag, not a stale high-water mark. A gauge that
	// stayed at its last value after the queue drained would alert forever.
	if !strings.Contains(rendered, `conduit_reconciliation_oldest_pending_seconds{queue="proposal_outbox"} 0`) {
		t.Errorf("an empty queue did not report zero lag:\n%s", rendered)
	}
}

// A wedged Hub is the case this must describe, and it is not "depth is high" --
// it is "depth is non-zero, nothing is leased, and the oldest item is old".
// Pinning it as a scenario keeps the three parts of that signal together.
func TestAWedgedHubIsDistinguishableFromABusyOne(t *testing.T) {
	t.Parallel()
	wedged := render(t, application.ReconciliationStats{
		WorkPending: 3, WorkLeased: 0, OldestPendingAge: time.Hour,
	})
	busy := render(t, application.ReconciliationStats{
		WorkPending: 200, WorkLeased: 8, OldestPendingAge: time.Second,
	})
	if !strings.Contains(wedged, `conduit_reconciliation_leased{queue="reconciliation"} 0`) ||
		!strings.Contains(wedged, `conduit_reconciliation_oldest_pending_seconds{queue="reconciliation"} 3600`) {
		t.Errorf("the wedged case is not legible:\n%s", wedged)
	}
	if !strings.Contains(busy, `conduit_reconciliation_leased{queue="reconciliation"} 8`) ||
		!strings.Contains(busy, `conduit_reconciliation_oldest_pending_seconds{queue="reconciliation"} 1`) {
		t.Errorf("the busy case is not legible:\n%s", busy)
	}
	// The depths alone would rank the busy Hub as the worse one, which is the
	// mistake this metric set exists to prevent.
	if strings.Contains(wedged, "conduit_reconciliation_pending{queue=\"reconciliation\"} 200") {
		t.Error("fixtures crossed")
	}
}

// Every series here is a gauge. Work drains, leases expire, and exhausted rows
// are cleared by an operator -- so none of them only rises, and publishing one
// as a counter would invite a rate() that means nothing.
func TestNothingIsPublishedAsACounter(t *testing.T) {
	t.Parallel()
	rendered := render(t, stats())
	if strings.Contains(rendered, " counter") {
		t.Errorf("a counter was published for a value that can fall:\n%s", rendered)
	}
	if got := strings.Count(rendered, "# TYPE"); got != 5 {
		t.Errorf("expected five distinct metric names, got %d:\n%s", got, rendered)
	}
}

// A failed read produces no registry rather than an empty one. An empty registry
// would render as a successful scrape of a Hub with nothing queued, which is
// exactly the wrong thing to tell a monitoring system about a broken database.
func TestAFailedReadIsAnErrorRatherThanAnEmptyRegistry(t *testing.T) {
	t.Parallel()
	registry, err := application.Gather("1.4.2", func(context.Context) (application.ReconciliationStats, error) {
		return application.ReconciliationStats{}, errors.New("the database is unreachable")
	})(t.Context())
	if err == nil {
		t.Fatal("a failed read produced a registry")
	}
	if registry != nil {
		t.Errorf("a failed read produced a non-nil registry: %v", registry.Render())
	}
}
