package application

import (
	"context"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/liveevents/domain"
)

// sourceReadiness tracks the immutable source set configured at construction.
// Draining every source is required within 30 seconds, including idle sources.
type sourceReadiness struct {
	mu        sync.Mutex
	required  map[string]sourceHealth
	freshness time.Duration
	now       func() time.Time
}
type sourceHealth struct {
	checked time.Time
	healthy bool
}

func newSourceReadiness(required []string, now func() time.Time) *sourceReadiness {
	health := &sourceReadiness{required: map[string]sourceHealth{}, freshness: 30 * time.Second, now: now}
	for _, name := range required {
		health.required[name] = sourceHealth{}
	}
	return health
}

func (health *sourceReadiness) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	health.mu.Lock()
	defer health.mu.Unlock()
	now := health.now()
	for _, source := range health.required {
		if !source.healthy || source.checked.IsZero() || now.Before(source.checked) || now.Sub(source.checked) >= health.freshness {
			return domain.ErrUnavailable
		}
	}
	return nil
}
func (health *sourceReadiness) mark(name string, healthy bool) {
	health.mu.Lock()
	defer health.mu.Unlock()
	health.required[name] = sourceHealth{checked: health.now(), healthy: healthy}
}

// DrainResult counts source records committed to delivery, never operator-visible
// frames. More means the batch budget was exhausted; the caller should poll
// again promptly. No cross-resource ordering or subscriber authority is implied.
type DrainResult struct {
	Records int
	More    bool
}

// DrainCommitted transfers bounded pages from one trusted native source. It
// derives checkpoints from the same journal PublishCommittedBatch commits, so a
// crash cannot acknowledge a source record without durable delivery state.
// Source reads happen outside the delivery lock, allowing repository-backed
// authorizers to read the native state while publication filters subscribers.
func (service *Service) DrainCommitted(ctx context.Context, name string, maxBatches int) (DrainResult, error) {
	source, registered := service.cfg.Sources[name]
	health := service.sourceHealth
	if !registered || health == nil || maxBatches < 1 || maxBatches > 64 {
		return DrainResult{}, domain.ErrInvalid
	}
	service.sourceMu.Lock()
	defer service.sourceMu.Unlock()
	result := DrainResult{}
	fail := func(err error) (DrainResult, error) {
		health.mark(name, false)
		service.publicationFailed()
		return result, err
	}
	for batch := 0; batch < maxBatches; batch++ {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		snapshot, err := service.cfg.Journal.Snapshot(ctx)
		if err != nil {
			return fail(err)
		}
		if !validState(snapshot) {
			return fail(domain.ErrUnavailable)
		}
		positions := make(map[string]uint64, len(snapshot.Producers))
		for producer, checkpoint := range snapshot.Producers {
			positions[producer] = checkpoint.Position
		}
		next := make(map[string]uint64, len(positions))
		for producer, position := range positions {
			next[producer] = position
		}
		records, err := source.ReadCommittedEvents(ctx, positions, domain.MaxQueueEvents)
		if err != nil {
			return fail(err)
		}
		if len(records) > domain.MaxQueueEvents {
			return fail(domain.ErrInvalid)
		}
		// Validate against our own checkpoint copy, never a map exposed to a source.
		for _, record := range records {
			producer := domain.ProducerID(record.Subject)
			if record.Producer != "" && record.Producer != producer {
				return fail(domain.ErrInvalid)
			}
			if previous, known := next[producer]; known && (record.Position <= previous || record.Position-previous != 1) {
				return fail(domain.ErrReset)
			}
			if record.Position == 0 {
				return fail(domain.ErrInvalid)
			}
			next[producer] = record.Position
		}
		if len(records) == 0 {
			// Validate a durable write even on an idle recovery: an empty publisher
			// call deliberately performs no journal IO and cannot prove recovery.
			service.mu.Lock()
			err = service.cfg.Journal.Update(ctx, func(state *domain.State) error {
				if !validState(*state) {
					return domain.ErrUnavailable
				}
				return nil
			})
			if err == nil {
				health.mark(name, true)
				if service.readiness(ctx) == nil {
					service.failed = false
				}
			}
			service.mu.Unlock()
			if err != nil {
				return fail(err)
			}
			return result, nil
		}
		if err := service.PublishCommittedBatch(ctx, records); err != nil {
			return fail(err)
		}
		result.Records += len(records)
		health.mark(name, true)
	}
	result.More = true
	return result, nil
}

func (service *Service) readiness(ctx context.Context) error {
	if err := service.cfg.Readiness.Check(ctx); err != nil {
		return err
	}
	if service.sourceHealth != nil {
		return service.sourceHealth.Check(ctx)
	}
	return nil
}
