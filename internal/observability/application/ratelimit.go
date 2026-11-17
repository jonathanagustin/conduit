package application

import (
	"context"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/observability/domain"
)

// RateLimitClass is what the Hub's request limiter has decided for one class
// of caller. Declared here rather than imported from the limiter, for the same
// reason as ReconciliationStats: the thing described does not become a
// dependency of the thing describing it.
type RateLimitClass struct {
	Class            string
	Allowed, Limited uint64
	// Callers is how many buckets the class currently tracks.
	Callers int
}

// ReadRateLimits is the limiter's snapshot, narrowed.
type ReadRateLimits func() []RateLimitClass

// rateLimitClasses is the closed set of values the `class` label may take.
//
// It is repeated here, not read from the snapshot, so that the label stays
// bounded by this file whatever a caller passes: a class is a kind of caller,
// never a caller. A principal, an Agent, an address or an Environment must not
// reach a label, and a value outside this set is refused rather than exported.
var rateLimitClasses = map[string]struct{}{"operator": {}, "agent": {}, "anonymous": {}}

// WithRateLimits adds the limiter's decisions to a gathered registry.
//
// Decisions are counters: they only rise, and rate() over the limited series
// is the question an operator asks ("is anybody being refused, and which kind
// of caller"). Tracked callers is a gauge: buckets are evicted when idle.
func WithRateLimits(
	gather func(context.Context) (*domain.Registry, error), read ReadRateLimits,
) func(context.Context) (*domain.Registry, error) {
	if read == nil {
		return gather
	}
	return func(ctx context.Context) (*domain.Registry, error) {
		registry, err := gather(ctx)
		if err != nil {
			return nil, err
		}
		for _, class := range read() {
			if _, known := rateLimitClasses[class.Class]; !known {
				return nil, fmt.Errorf("rate limit class %q is not one of the closed set this exporter publishes", class.Class)
			}
			for outcome, count := range map[string]uint64{"allowed": class.Allowed, "limited": class.Limited} {
				registry.Add(domain.Metric{
					Name: "conduit_rate_limit_decisions_total", Kind: domain.KindCounter, Value: float64(count),
					Help:   "requests the Hub limiter admitted or refused with 429, by class of caller",
					Labels: map[string]string{"class": class.Class, "outcome": outcome},
				})
			}
			registry.Add(domain.Metric{
				Name: "conduit_rate_limit_tracked_callers", Kind: domain.KindGauge, Value: float64(class.Callers),
				Help:   "callers the Hub limiter currently holds a bucket for; bounded per class and evicted when idle",
				Labels: map[string]string{"class": class.Class},
			})
		}
		return registry, nil
	}
}
