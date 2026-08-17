// Package application turns Conduit's internal counts into the metrics an
// operator reads.
//
// It is a separate package from the store so the store keeps returning numbers
// rather than growing opinions about how they are named, and separate from the
// handler so the naming can be tested without an HTTP round trip or a database.
package application

import (
	"context"
	"time"

	"github.com/jonathanagustin/conduit/internal/observability/domain"
)

// ReconciliationStats is the shape this package needs, declared here rather than
// imported from the postgres adapter so an adapter does not become a dependency
// of the thing describing it. The store's Stats satisfies it structurally.
type ReconciliationStats struct {
	WorkPending      int
	WorkLeased       int
	WorkExhausted    int
	OldestPendingAge time.Duration

	OutboxPending   int
	OutboxLeased    int
	OutboxExhausted int
	OldestOutboxAge time.Duration
}

// ReadStats is the store call, narrowed.
type ReadStats func(context.Context) (ReconciliationStats, error)

// Queue names, closed here rather than derived from anything. The `queue` label
// is on the allowlist precisely because this set is fixed in code and cannot
// grow with a tenant's fleet.
const (
	queueWork   = "reconciliation"
	queueOutbox = "proposal_outbox"
)

// Gather builds the registry the metrics endpoint serves.
//
// One name per question with a queue label, rather than a name per queue. Both
// queues wedge the same way and an operator wants one alert expression, not two
// that drift apart. The label set stays bounded because the queues are named
// above and not read from data.
//
// Everything is a gauge. None of these only rises: work drains, leases expire,
// and exhausted rows are cleared by an operator. Publishing any of them as a
// counter would invite a rate() that means nothing.
func Gather(version string, read ReadStats) func(context.Context) (*domain.Registry, error) {
	return func(ctx context.Context) (*domain.Registry, error) {
		stats, err := read(ctx)
		if err != nil {
			return nil, err
		}
		registry := domain.NewRegistry()

		// Which build is serving. Two versions here at once is a rollout that
		// half-succeeded, which is otherwise invisible until behaviour differs.
		registry.Add(domain.Metric{
			Name: "conduit_build_info", Kind: domain.KindGauge, Value: 1,
			Help:   "always 1; the version label names the build this replica is running",
			Labels: map[string]string{"version": version},
		})

		for queue, counts := range map[string][4]float64{
			queueWork: {
				float64(stats.WorkPending), float64(stats.WorkLeased),
				float64(stats.WorkExhausted), stats.OldestPendingAge.Seconds(),
			},
			queueOutbox: {
				float64(stats.OutboxPending), float64(stats.OutboxLeased),
				float64(stats.OutboxExhausted), stats.OldestOutboxAge.Seconds(),
			},
		} {
			labels := map[string]string{"queue": queue}
			registry.Add(domain.Metric{
				Name: "conduit_reconciliation_pending", Kind: domain.KindGauge, Value: counts[0],
				Help:   "items due now and not held by any replica",
				Labels: labels,
			})
			registry.Add(domain.Metric{
				Name: "conduit_reconciliation_leased", Kind: domain.KindGauge, Value: counts[1],
				Help:   "items a replica currently holds a lease on",
				Labels: labels,
			})
			registry.Add(domain.Metric{
				Name: "conduit_reconciliation_exhausted", Kind: domain.KindGauge, Value: counts[2],
				Help:   "items that have run out of retries; these never drain without an operator",
				Labels: labels,
			})
			// The one that separates a busy Hub from a stuck one. Depth alone
			// cannot: a fleet at steady state and a fleet whose worker died both
			// sit at some number. How long the oldest due item has waited is the
			// difference, and it is the signal to alert on.
			registry.Add(domain.Metric{
				Name: "conduit_reconciliation_oldest_pending_seconds", Kind: domain.KindGauge, Value: counts[3],
				Help:   "age of the oldest item that is due and unheld; rises only when nothing is taking work",
				Labels: labels,
			})
		}
		return registry, nil
	}
}
