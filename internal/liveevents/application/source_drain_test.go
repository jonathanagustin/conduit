package application

import (
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/liveevents/adapters/filejournal"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/liveevents/domain"
	"github.com/jonathanagustin/conduit/internal/liveevents/ports"
)

type sourceFunc func(context.Context, map[string]uint64, int) ([]domain.Record, error)

func (source sourceFunc) ReadCommittedEvents(ctx context.Context, positions map[string]uint64, limit int) ([]domain.Record, error) {
	return source(ctx, positions, limit)
}
func sourcesSetup(t *testing.T, fixture *setup, sources map[string]ports.CommittedSource, journal ports.Journal) {
	t.Helper()
	service, err := New(Config{Journal: journal, Authorizer: fixture.auth, Validator: fixture.service.cfg.Validator, Readiness: fixture.ready, Now: func() time.Time { return fixture.now }, Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	fixture.service = service
}
func emptySource(context.Context, map[string]uint64, int) ([]domain.Record, error) { return nil, nil }
func drain(t *testing.T, fixture *setup, name string) DrainResult {
	t.Helper()
	result, err := fixture.service.DrainCommitted(context.Background(), name, 64)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSourceReadinessRequiresEveryConfiguredSourceAndExpires(t *testing.T) {
	fixture := newSetup(t)
	sources := map[string]ports.CommittedSource{"native": sourceFunc(emptySource), "assistant": sourceFunc(emptySource)}
	sourcesSetup(t, fixture, sources, fixture.journal)
	// Mutation of the caller's configuration must not remove a required source.
	delete(sources, "assistant")
	if _, err := fixture.service.Open(asOwner("alice"), ""); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("uninitialized sources accepted: %v", err)
	}
	drain(t, fixture, "native")
	if _, err := fixture.service.Open(asOwner("alice"), ""); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("missing required source accepted: %v", err)
	}
	drain(t, fixture, "assistant")
	subscription := open(t, fixture, "alice", "")
	defer subscription.Close()
	fixture.now = fixture.now.Add(30 * time.Second)
	if _, err := subscription.Next(context.Background()); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("stale source remained ready: %v", err)
	}
	if _, err := fixture.service.Open(asOwner("alice"), ""); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("stale source opened stream: %v", err)
	}
	drain(t, fixture, "native")
	drain(t, fixture, "assistant")
	recovered := open(t, fixture, "alice", "")
	recovered.Close()
	fixture.now = fixture.now.Add(-time.Second)
	if fixture.service.readiness(context.Background()) == nil {
		t.Fatal("backward clock accepted stale health")
	}
}

func TestDrainCheckpointsCommitWithDeliveryAndBoundWork(t *testing.T) {
	fixture := newSetup(t)
	producer := domain.ProducerID(domain.Subject{Type: "plan", ID: "pln_source"})
	source := sourceFunc(func(ctx context.Context, positions map[string]uint64, limit int) ([]domain.Record, error) {
		var records []domain.Record
		for position := positions[producer] + 1; position <= 257 && len(records) < limit; position++ {
			records = append(records, record("pln_source", position, position))
		}
		return records, nil
	})
	sourcesSetup(t, fixture, map[string]ports.CommittedSource{"native": source}, fixture.journal)
	result, err := fixture.service.DrainCommitted(context.Background(), "native", 1)
	if err != nil || result.Records != 256 || !result.More {
		t.Fatalf("first bounded drain: %+v %v", result, err)
	}
	state, _ := fixture.journal.Snapshot(context.Background())
	if state.Producers[producer].Position != 256 || len(state.Sources) != 256 {
		t.Fatal("source checkpoint and delivery rows not committed together")
	}
	result = drain(t, fixture, "native")
	if result.Records != 1 || result.More {
		t.Fatalf("second drain: %+v", result)
	}
	state, _ = fixture.journal.Snapshot(context.Background())
	if state.Producers[producer].Position != 257 || len(state.Sources) != 257 {
		t.Fatal("source continuation lost or duplicated records")
	}
	if result := drain(t, fixture, "native"); result.Records != 0 {
		t.Fatal("idle retry duplicated records")
	}
}

type faultJournal struct {
	ports.Journal
	mu  sync.Mutex
	err error
}

func (journal *faultJournal) Update(ctx context.Context, update func(*domain.State) error) error {
	journal.mu.Lock()
	err := journal.err
	journal.mu.Unlock()
	if err != nil {
		return err
	}
	return journal.Journal.Update(ctx, update)
}
func (journal *faultJournal) set(err error) {
	journal.mu.Lock()
	journal.err = err
	journal.mu.Unlock()
}

func TestDrainFailureClosesSubscribersAndIdleRecoveryProvesWrite(t *testing.T) {
	fixture := newSetup(t)
	fault := &faultJournal{Journal: fixture.journal}
	sourceError := error(nil)
	source := sourceFunc(func(context.Context, map[string]uint64, int) ([]domain.Record, error) { return nil, sourceError })
	sourcesSetup(t, fixture, map[string]ports.CommittedSource{"native": source}, fault)
	drain(t, fixture, "native")
	subscription := open(t, fixture, "alice", "")
	defer subscription.Close()
	sourceError = errors.New("source unavailable")
	if _, err := fixture.service.DrainCommitted(context.Background(), "native", 1); !errors.Is(err, sourceError) {
		t.Fatal(err)
	}
	select {
	case <-subscription.Changed():
	default:
		t.Fatal("source failure did not wake subscriber")
	}
	if _, err := subscription.Next(context.Background()); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("source failure did not close subscriber")
	}
	if _, err := fixture.service.Open(asOwner("alice"), ""); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("failed source accepted new subscriber")
	}
	sourceError = nil
	diskError := errors.New("journal write unavailable")
	fault.set(diskError)
	if _, err := fixture.service.DrainCommitted(context.Background(), "native", 1); !errors.Is(err, diskError) {
		t.Fatal("idle recovery did not check durable write")
	}
	if _, err := fixture.service.Open(asOwner("alice"), ""); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("failed journal accepted new subscriber")
	}
	fault.set(nil)
	drain(t, fixture, "native")
	recovered := open(t, fixture, "alice", "")
	recovered.Close()
	state, _ := fixture.journal.Snapshot(context.Background())
	if state.SourcePosition != 0 {
		t.Fatal("idle recovery invented an event")
	}
}

func TestDrainFailedCommitRetriesFromDurableCheckpoint(t *testing.T) {
	fixture := newSetup(t)
	fault := &faultJournal{Journal: fixture.journal}
	producer := domain.ProducerID(domain.Subject{Type: "plan", ID: "pln_retry"})
	positionsSeen := []uint64{}
	source := sourceFunc(func(_ context.Context, positions map[string]uint64, _ int) ([]domain.Record, error) {
		positionsSeen = append(positionsSeen, positions[producer])
		if positions[producer] == 0 {
			return []domain.Record{record("pln_retry", 1, 1)}, nil
		}
		return nil, nil
	})
	sourcesSetup(t, fixture, map[string]ports.CommittedSource{"native": source}, fault)
	diskError := errors.New("write failed")
	fault.set(diskError)
	if _, err := fixture.service.DrainCommitted(context.Background(), "native", 1); !errors.Is(err, diskError) {
		t.Fatal(err)
	}
	state, _ := fixture.journal.Snapshot(context.Background())
	if len(state.Producers) != 0 || len(state.Sources) != 0 {
		t.Fatal("failed commit advanced source checkpoint")
	}
	fault.set(nil)
	drain(t, fixture, "native")
	if len(positionsSeen) < 2 || positionsSeen[0] != 0 || positionsSeen[1] != 0 {
		t.Fatalf("retry trusted a volatile checkpoint: %v", positionsSeen)
	}
	state, _ = fixture.journal.Snapshot(context.Background())
	if state.Producers[producer].Position != 1 || len(state.Sources) != 1 {
		t.Fatal("retry did not commit exactly once")
	}
}

func TestDrainRejectsInvalidOrNonProgressingPages(t *testing.T) {
	for _, kind := range []string{"oversize", "zero", "producer", "repeated", "gap", "checkpoint-mutation"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newSetup(t)
			producer := domain.ProducerID(domain.Subject{Type: "plan", ID: "pln_invalid"})
			publish(t, fixture, record("pln_invalid", 1, 1))
			source := sourceFunc(func(_ context.Context, positions map[string]uint64, _ int) ([]domain.Record, error) {
				switch kind {
				case "oversize":
					return make([]domain.Record, 257), nil
				case "zero":
					return []domain.Record{record("pln_new", 0, 1)}, nil
				case "producer":
					value := record("pln_invalid", 2, 2)
					value.Producer = "wrong"
					return []domain.Record{value}, nil
				case "repeated":
					return []domain.Record{record("pln_invalid", 1, 1)}, nil
				case "gap":
					return []domain.Record{record("pln_invalid", 3, 3)}, nil
				default:
					positions[producer] = 2
					return []domain.Record{record("pln_invalid", 3, 3)}, nil
				}
			})
			sourcesSetup(t, fixture, map[string]ports.CommittedSource{"native": source}, fixture.journal)
			if _, err := fixture.service.DrainCommitted(context.Background(), "native", 1); err == nil {
				t.Fatal("invalid source page accepted")
			}
			state, _ := fixture.journal.Snapshot(context.Background())
			if state.Producers[producer].Position != 1 {
				t.Fatal("invalid source page advanced checkpoint")
			}
			if fixture.service.readiness(context.Background()) == nil {
				t.Fatal("invalid source remained ready")
			}
		})
	}
}

func TestDrainSerializesConcurrentReaders(t *testing.T) {
	fixture := newSetup(t)
	producer := domain.ProducerID(domain.Subject{Type: "plan", ID: "pln_parallel"})
	source := sourceFunc(func(_ context.Context, positions map[string]uint64, _ int) ([]domain.Record, error) {
		if positions[producer] == 0 {
			return []domain.Record{record("pln_parallel", 1, 1)}, nil
		}
		return nil, nil
	})
	sourcesSetup(t, fixture, map[string]ports.CommittedSource{"native": source}, fixture.journal)
	var wait sync.WaitGroup
	failures := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := fixture.service.DrainCommitted(context.Background(), "native", 2)
			failures <- err
		}()
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	state, _ := fixture.journal.Snapshot(context.Background())
	if state.SourcePosition != 1 || state.Producers[producer].Position != 1 {
		t.Fatal("concurrent drains duplicated delivery")
	}
}

func TestDrainPartialProgressDoesNotAcknowledgeFailedPage(t *testing.T) {
	fixture := newSetup(t)
	producer := domain.ProducerID(domain.Subject{Type: "plan", ID: "pln_partial"})
	readError := errors.New("second source page unavailable")
	source := sourceFunc(func(_ context.Context, positions map[string]uint64, _ int) ([]domain.Record, error) {
		if positions[producer] == 0 {
			return []domain.Record{record("pln_partial", 1, 1)}, nil
		}
		return nil, readError
	})
	sourcesSetup(t, fixture, map[string]ports.CommittedSource{"native": source}, fixture.journal)
	result, err := fixture.service.DrainCommitted(context.Background(), "native", 2)
	if !errors.Is(err, readError) || result.Records != 1 {
		t.Fatalf("partial drain: %+v %v", result, err)
	}
	state, _ := fixture.journal.Snapshot(context.Background())
	if state.Producers[producer].Position != 1 || len(state.Sources) != 1 {
		t.Fatal("successful first page was lost")
	}
	if fixture.service.readiness(context.Background()) == nil {
		t.Fatal("failed second page stayed ready")
	}
	readError = nil
	result = drain(t, fixture, "native")
	if result.Records != 0 {
		t.Fatal("recovery duplicated first page")
	}
	recovered := open(t, fixture, "alice", "")
	recovered.Close()
}

func TestDrainRejectsMissingSourcesAndCancellation(t *testing.T) {
	fixture := newSetup(t)
	for _, sources := range []map[string]ports.CommittedSource{{"": sourceFunc(emptySource)}, {"native": nil}} {
		_, err := New(Config{Journal: fixture.journal, Authorizer: fixture.auth, Validator: fixture.service.cfg.Validator, Readiness: fixture.ready, Sources: sources})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("invalid source registry accepted")
		}
	}
	sourcesSetup(t, fixture, map[string]ports.CommittedSource{"native": sourceFunc(emptySource)}, fixture.journal)
	for _, test := range []struct {
		name   string
		budget int
	}{{"missing", 1}, {"native", 0}, {"native", 65}} {
		if _, err := fixture.service.DrainCommitted(context.Background(), test.name, test.budget); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("invalid drain invocation accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fixture.service.DrainCommitted(ctx, "native", 1); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled drain accepted")
	}
	if fixture.service.readiness(context.Background()) == nil {
		t.Fatal("canceled source stayed ready")
	}
	drain(t, fixture, "native")
	subscription := open(t, fixture, "alice", "")
	subscription.Close()
}

func TestRetentionGapAtomicallyInvalidatesAllStreamsAndRequiresFreshSourceHealth(t *testing.T) {
	fixture := newSetup(t)
	ctx := context.Background()
	first := uint64(1)
	producer := domain.ProducerID(domain.Subject{Type: "plan", ID: "pln_gap"})
	native := sourceFunc(func(_ context.Context, positions map[string]uint64, _ int) ([]domain.Record, error) {
		previous, known := positions[producer]
		if known && previous < first-1 {
			return nil, domain.ErrSourceRetentionGap
		}
		if previous >= first {
			return nil, nil
		}
		return []domain.Record{record("pln_gap", first, first)}, nil
	})
	sources := map[string]ports.CommittedSource{"native": native, "other": sourceFunc(emptySource)}
	sourcesSetup(t, fixture, sources, fixture.journal)
	drain(t, fixture, "native")
	drain(t, fixture, "other")
	alice := open(t, fixture, "alice", "")
	defer alice.Close()
	bob := open(t, fixture, "bob", "")
	defer bob.Close()
	cursor := alice.Ready.Cursor
	stream := alice.Ready.StreamID
	first = 10
	if _, err := fixture.service.DrainCommitted(ctx, "native", 4); !errors.Is(err, domain.ErrSourceRetentionGap) {
		t.Fatalf("gap=%v", err)
	}
	for _, subscription := range []*Subscription{alice, bob} {
		select {
		case <-subscription.Changed():
		default:
			t.Fatal("reset did not wake every subscriber")
		}
		if _, err := subscription.Next(ctx); !errors.Is(err, domain.ErrReset) {
			t.Fatalf("old stream remained deliverable: %v", err)
		}
	}
	state, err := fixture.journal.Snapshot(ctx)
	if err != nil || len(state.Streams) != 0 || len(state.Producers) != 0 || len(state.Sources) != 0 || state.SourcePosition != 0 {
		t.Fatalf("reset was not one complete durable transaction: %+v %v", state, err)
	}
	// Restart after the reset, before new source checkpoints: cached cursors may
	// never recover from the old process or a partially reset journal.
	reopened, err := filejournal.Open(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	sourcesSetup(t, fixture, sources, reopened)
	drain(t, fixture, "native")
	if _, err := fixture.service.Open(asOwner("alice"), ""); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("other source kept its pre-reset healthy stamp")
	}
	drain(t, fixture, "other")
	if _, err := fixture.service.Open(asOwner("alice"), cursor); !errors.Is(err, domain.ErrReset) {
		t.Fatal("retired cursor resumed after restart")
	}
	fresh := open(t, fixture, "alice", "")
	defer fresh.Close()
	if fresh.Ready.Sequence != 0 || fresh.Ready.StreamID == stream {
		t.Fatal("catch-up inherited retired delivery epoch")
	}
	fixture.auth.allowed["alice"]["pln_gap"] = true
	first = 11
	drain(t, fixture, "native")
	frame, err := fresh.Next(ctx)
	if err != nil || frame == nil || frame.Sequence != 1 || frame.Resource.Revision != 11 {
		t.Fatalf("new epoch did not resume native source: %+v %v", frame, err)
	}
}

func TestOnlyExplicitRetentionGapCanDiscardCheckpoints(t *testing.T) {
	for _, sourceErr := range []error{domain.ErrReset, domain.ErrConflict, domain.ErrInvalid, errors.New("unavailable")} {
		t.Run(sourceErr.Error(), func(t *testing.T) {
			fixture := newSetup(t)
			ctx := context.Background()
			failure := error(nil)
			producer := domain.ProducerID(domain.Subject{Type: "plan", ID: "pln_source"})
			source := sourceFunc(func(_ context.Context, positions map[string]uint64, _ int) ([]domain.Record, error) {
				if failure != nil {
					return nil, failure
				}
				if positions[producer] == 0 {
					return []domain.Record{record("pln_source", 1, 1)}, nil
				}
				return nil, nil
			})
			sourcesSetup(t, fixture, map[string]ports.CommittedSource{"native": source}, fixture.journal)
			drain(t, fixture, "native")
			old := open(t, fixture, "alice", "")
			defer old.Close()
			before, _ := fixture.journal.Snapshot(ctx)
			failure = sourceErr
			if _, err := fixture.service.DrainCommitted(ctx, "native", 1); !errors.Is(err, sourceErr) {
				t.Fatal(err)
			}
			after, _ := fixture.journal.Snapshot(ctx)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("unclassified error erased replay/checkpoint evidence")
			}
		})
	}
}

func TestFailedRetentionResetPreservesDurableEvidence(t *testing.T) {
	fixture := newSetup(t)
	ctx := context.Background()
	fault := &faultJournal{Journal: fixture.journal}
	gap := false
	producer := domain.ProducerID(domain.Subject{Type: "plan", ID: "pln_source"})
	source := sourceFunc(func(_ context.Context, positions map[string]uint64, _ int) ([]domain.Record, error) {
		if gap {
			return nil, domain.ErrSourceRetentionGap
		}
		if positions[producer] == 0 {
			return []domain.Record{record("pln_source", 1, 1)}, nil
		}
		return nil, nil
	})
	sourcesSetup(t, fixture, map[string]ports.CommittedSource{"native": source}, fault)
	drain(t, fixture, "native")
	old := open(t, fixture, "alice", "")
	defer old.Close()
	before, _ := fixture.journal.Snapshot(ctx)
	gap = true
	fault.err = errors.New("reset write failed")
	if _, err := fixture.service.DrainCommitted(ctx, "native", 1); !errors.Is(err, fault.err) {
		t.Fatal("failed reset reported complete")
	}
	after, _ := fixture.journal.Snapshot(ctx)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed reset changed durable checkpoints")
	}
	if _, err := fixture.service.Open(asOwner("alice"), ""); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("failed reset admitted a new stream")
	}
	fault.err = nil
	if _, err := fixture.service.DrainCommitted(ctx, "native", 1); !errors.Is(err, domain.ErrSourceRetentionGap) {
		t.Fatal("reset did not retry after IO recovery")
	}
	after, _ = fixture.journal.Snapshot(ctx)
	if len(after.Streams) != 0 || len(after.Producers) != 0 {
		t.Fatal("successful retry did not invalidate epoch")
	}
}
