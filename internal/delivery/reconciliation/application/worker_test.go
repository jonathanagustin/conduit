package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/domain"
	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/ports"
)

func TestWorkerConvergesOneInstallationAndRecoversOutboxCrash(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	store := newFakeStore()
	planner := newFakePlanner(clock)
	worker := newTestWorker(t, store, planner, clock)
	key := domain.Key{Environment: "dev", Installation: "catalog"}
	desired := desiredSnapshot(key, 1, "target-a", now)
	store.upsertDesired(desired, true)
	store.upsertDesired(desired, true) // duplicate event coalesces on the stable key
	store.observations[key.String()] = observation(key, 1, "target-b", now, true)

	processed, err := worker.ProcessOne(ctx)
	if err != nil || !processed || len(store.decisions) != 1 || len(store.outbox) != 1 || len(store.audits) != 1 {
		t.Fatalf("decision: processed=%v err=%v decisions=%d outbox=%d audits=%d", processed, err, len(store.decisions), len(store.outbox), len(store.audits))
	}
	store.failCompleteProposal = true
	if processed, err := worker.DispatchOne(ctx); err == nil || !processed {
		t.Fatalf("crash before outbox acknowledgement = (%v, %v)", processed, err)
	}
	if planner.calls != 1 || len(planner.plans) != 1 || len(store.outbox) != 1 {
		t.Fatalf("first dispatch calls=%d plans=%d outbox=%d", planner.calls, len(planner.plans), len(store.outbox))
	}
	store.failCompleteProposal = false
	clock.now = clock.now.Add(2 * time.Minute)
	if processed, err := worker.DispatchOne(ctx); err != nil || !processed {
		t.Fatalf("recovered dispatch = (%v, %v)", processed, err)
	}
	if planner.calls != 2 || len(planner.plans) != 1 || len(store.outbox) != 0 || len(store.receipts) != 1 {
		t.Fatalf("idempotent recovery calls=%d plans=%d outbox=%d receipts=%d", planner.calls, len(planner.plans), len(store.outbox), len(store.receipts))
	}

	store.observations[key.String()] = observation(key, 1, "target-a", clock.now, true)
	store.enqueue(key, clock.now)
	if processed, err := worker.ProcessOne(ctx); err != nil || !processed {
		t.Fatalf("converged decision = (%v, %v)", processed, err)
	}
	latest := store.latestDecision(key)
	if latest.Kind != domain.DecisionConverged || len(store.outbox) != 0 {
		t.Fatalf("latest decision=%#v outbox=%d", latest, len(store.outbox))
	}
}

func TestWorkerRepairsDroppedEventAndUsesLatestDesiredGeneration(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 14, 13, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	store := newFakeStore()
	worker := newTestWorker(t, store, newFakePlanner(clock), clock)
	key := domain.Key{Environment: "dev", Installation: "catalog"}
	store.upsertDesired(desiredSnapshot(key, 1, "target-a", now), false) // notification deliberately dropped
	if repaired, err := worker.Repair(ctx); err != nil || repaired != 1 {
		t.Fatalf("repair = %d, %v", repaired, err)
	}
	store.upsertDesired(desiredSnapshot(key, 2, "target-b", now.Add(time.Second)), true)
	if processed, err := worker.ProcessOne(ctx); err != nil || !processed {
		t.Fatalf("process superseding desired = (%v, %v)", processed, err)
	}
	for _, queued := range store.outbox {
		if queued.proposal.Desired.Generation != "2" || queued.proposal.Desired.TargetDigest != digest("target-b") {
			t.Fatalf("stale desired escaped queue: %#v", queued.proposal.Desired)
		}
	}
	if repaired, err := worker.Repair(ctx); err != nil || repaired != 0 {
		t.Fatalf("unchanged input was repaired again = %d, %v", repaired, err)
	}
}

func TestWorkerBlocksStaleObservationWithoutProposing(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 14, 14, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	store := newFakeStore()
	worker := newTestWorker(t, store, newFakePlanner(clock), clock)
	key := domain.Key{Environment: "dev", Installation: "catalog"}
	store.upsertDesired(desiredSnapshot(key, 1, "target-a", now), true)
	store.observations[key.String()] = observation(key, 1, "target-b", now.Add(-11*time.Minute), true)
	if _, err := worker.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	decision := store.latestDecision(key)
	if decision.Kind != domain.DecisionBlocked || decision.Reason != "STALE_OBSERVATION" || len(store.outbox) != 0 {
		t.Fatalf("stale observation decision=%#v outbox=%d", decision, len(store.outbox))
	}
}

func TestWorkerKeepsQueueWhenDecisionOrAuditCommitFails(t *testing.T) {
	for name, configure := range map[string]func(*fakeStore){
		"decision write": func(store *fakeStore) { store.failCommitDecision = true },
		"audit append":   func(store *fakeStore) { store.failAudit = true },
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 8, 14, 15, 0, 0, 0, time.UTC)
			clock := &fakeClock{now: now}
			store := newFakeStore()
			worker := newTestWorker(t, store, newFakePlanner(clock), clock)
			key := domain.Key{Environment: "dev", Installation: "catalog"}
			store.upsertDesired(desiredSnapshot(key, 1, "target-a", now), true)
			configure(store)
			if _, err := worker.ProcessOne(ctx); err == nil {
				t.Fatal("failed atomic commit was reported as successful")
			}
			if len(store.decisions) != 0 || len(store.audits) != 0 || len(store.outbox) != 0 || len(store.queue) != 1 {
				t.Fatalf("partial commit: decisions=%d audits=%d outbox=%d queue=%d", len(store.decisions), len(store.audits), len(store.outbox), len(store.queue))
			}
		})
	}
}

func TestWorkerExhaustsBoundedRetry(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 14, 16, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	store := newFakeStore()
	worker := newTestWorker(t, store, newFakePlanner(clock), clock)
	key := domain.Key{Environment: "dev", Installation: "catalog"}
	store.upsertDesired(desiredSnapshot(key, 1, "target-a", now), true)
	store.loadError = errors.New("injected read failure")
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := worker.ProcessOne(ctx)
		if attempt < 3 && err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		if attempt == 3 && !errors.Is(err, ErrRetryExhausted) {
			t.Fatalf("final attempt error = %v", err)
		}
		clock.now = clock.now.Add(time.Duration(1<<attempt) * time.Second)
	}
	if len(store.queue) != 0 || store.exhaustedFailures != 1 {
		t.Fatalf("queue=%d exhausted=%d", len(store.queue), store.exhaustedFailures)
	}
}

func TestWorkerTerminallyRetiresSupersededProposal(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 14, 17, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	store := newFakeStore()
	planner := newFakePlanner(clock)
	planner.err = ports.ErrProposalSuperseded
	worker := newTestWorker(t, store, planner, clock)
	key := domain.Key{Environment: "dev", Installation: "catalog"}
	store.upsertDesired(desiredSnapshot(key, 1, "target-a", now), true)
	if _, err := worker.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	if processed, err := worker.DispatchOne(ctx); err != nil || !processed {
		t.Fatalf("superseded dispatch = (%v, %v)", processed, err)
	}
	if len(store.outbox) != 0 || store.exhaustedFailures != 1 {
		t.Fatalf("outbox=%d exhausted=%d", len(store.outbox), store.exhaustedFailures)
	}
}

// The signal this whole escalation path exists for. A proposal that has spent
// its retry budget is never leased again -- the store filters exhausted rows out
// of the lease query -- so if nothing is told at this moment, the Environment
// stays unconverged and the only evidence is a row nobody queries.
func TestWorkerEscalatesAProposalItHasStoppedRetrying(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	store := newFakeStore()
	planner := newFakePlanner(clock)
	escalations := &fakeEscalations{}
	worker := newEscalatingWorker(t, store, planner, clock, escalations)
	key := domain.Key{Environment: "dev", Installation: "catalog"}
	store.upsertDesired(desiredSnapshot(key, 1, "target-a", now), true)
	if _, err := worker.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	planner.err = errors.New("registry unavailable")
	for attempt := 1; attempt <= 3; attempt++ {
		if processed, err := worker.DispatchOne(ctx); err != nil || !processed {
			t.Fatalf("attempt %d: processed=%v err=%v", attempt, processed, err)
		}
		if attempt < 3 && len(escalations.proposals) != 0 {
			t.Fatalf("attempt %d escalated while retries remained: %#v", attempt, escalations.proposals)
		}
		clock.now = clock.now.Add(time.Hour)
	}
	if len(escalations.proposals) != 1 {
		t.Fatalf("an exhausted proposal escalated %d times", len(escalations.proposals))
	}
	if escalations.proposals[0].Key != key {
		t.Errorf("the escalation names the wrong installation: %#v", escalations.proposals[0].Key)
	}
	// The planner's own words, not a generic failure. An operator reading
	// "registry unavailable" acts differently from one reading "gave up".
	if escalations.reasons[0] != "registry unavailable" {
		t.Errorf("the escalation lost the reason: %q", escalations.reasons[0])
	}
	if escalations.attempts[0] != 3 || !escalations.moments[0].Equal(now.Add(2*time.Hour)) {
		t.Errorf("the escalation misreports when it gave up: attempt=%d at=%s", escalations.attempts[0], escalations.moments[0])
	}
	if store.exhaustedFailures != 1 {
		t.Errorf("the proposal was not marked exhausted after escalating: %d", store.exhaustedFailures)
	}
}

// The distinction the durable row cannot make. A superseded proposal is marked
// exhausted with the same boolean, and it is not an operator's problem: newer
// work is already queued. Escalating it would page somebody for the system
// working, which is how a notification channel gets muted.
func TestWorkerEscalatesNothingWhenAProposalIsMerelySuperseded(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 18, 11, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	store := newFakeStore()
	planner := newFakePlanner(clock)
	planner.err = ports.ErrProposalSuperseded
	escalations := &fakeEscalations{}
	worker := newEscalatingWorker(t, store, planner, clock, escalations)
	key := domain.Key{Environment: "dev", Installation: "catalog"}
	store.upsertDesired(desiredSnapshot(key, 1, "target-a", now), true)
	if _, err := worker.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	if processed, err := worker.DispatchOne(ctx); err != nil || !processed {
		t.Fatalf("superseded dispatch = (%v, %v)", processed, err)
	}
	if store.exhaustedFailures != 1 {
		t.Fatalf("a superseded proposal was not retired: %d", store.exhaustedFailures)
	}
	if len(escalations.proposals) != 0 {
		t.Errorf("a superseded proposal escalated to a person: %#v", escalations.proposals)
	}
}

// Escalation happens before the row is marked, so a failure to escalate must
// leave the proposal retriable. Marking it first and then failing to tell
// anybody is the exact state that cannot be recovered from: the lease query
// never returns an exhausted row again.
func TestAFailedEscalationLeavesTheProposalRetriable(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	store := newFakeStore()
	planner := newFakePlanner(clock)
	escalations := &fakeEscalations{err: errors.New("notification store unreachable")}
	worker := newEscalatingWorker(t, store, planner, clock, escalations)
	key := domain.Key{Environment: "dev", Installation: "catalog"}
	store.upsertDesired(desiredSnapshot(key, 1, "target-a", now), true)
	if _, err := worker.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	planner.err = errors.New("registry unavailable")
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		if _, err = worker.DispatchOne(ctx); attempt < 3 && err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		clock.now = clock.now.Add(time.Hour)
	}
	if err == nil || !strings.Contains(err.Error(), "notification store unreachable") {
		t.Fatalf("a failed escalation was swallowed: %v", err)
	}
	if store.exhaustedFailures != 0 || len(store.outbox) != 1 {
		t.Errorf("the proposal was retired without anybody being told: exhausted=%d outbox=%d",
			store.exhaustedFailures, len(store.outbox))
	}
}

func newTestWorker(t *testing.T, store ports.Store, planner ports.Planner, clock ports.Clock) *Worker {
	t.Helper()
	return newEscalatingWorker(t, store, planner, clock, &fakeEscalations{})
}

func newEscalatingWorker(
	t *testing.T, store ports.Store, planner ports.Planner, clock ports.Clock, escalations ports.Escalations,
) *Worker {
	t.Helper()
	worker, err := NewWorker(store, planner, clock, escalations, Config{
		LeaseDuration: time.Minute, MaximumObservationAge: 10 * time.Minute,
		MaximumAttempts: 3, BaseRetryDelay: time.Second, RepairBatchSize: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

// fakeEscalations records what the worker said had stopped being retried.
type fakeEscalations struct {
	proposals []domain.Proposal
	reasons   []string
	attempts  []uint32
	moments   []time.Time
	err       error
}

func (escalations *fakeEscalations) ProposalExhausted(
	_ context.Context, proposal domain.Proposal, reason string, attempt uint32, at time.Time,
) error {
	if escalations.err != nil {
		return escalations.err
	}
	escalations.proposals = append(escalations.proposals, proposal)
	escalations.reasons = append(escalations.reasons, reason)
	escalations.attempts = append(escalations.attempts, attempt)
	escalations.moments = append(escalations.moments, at)
	return nil
}

type fakeClock struct{ now time.Time }

func (clock *fakeClock) Now() time.Time { return clock.now }

type fakeWork struct {
	key       domain.Key
	available time.Time
	token     string
	until     time.Time
	revision  uint64
	attempt   uint32
}

type fakeOutbox struct {
	proposal  domain.Proposal
	available time.Time
	token     string
	until     time.Time
	attempt   uint32
}

type fakeStore struct {
	mu                   sync.Mutex
	desired              map[string]domain.DesiredSnapshot
	observations         map[string]domain.Observation
	queue                map[string]*fakeWork
	decisions            map[string]domain.Decision
	audits               map[string]domain.Decision
	outbox               map[string]*fakeOutbox
	receipts             map[string]domain.PlanReceipt
	loadError            error
	failCommitDecision   bool
	failAudit            bool
	failCompleteProposal bool
	exhaustedFailures    int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		desired: map[string]domain.DesiredSnapshot{}, observations: map[string]domain.Observation{},
		queue: map[string]*fakeWork{}, decisions: map[string]domain.Decision{}, audits: map[string]domain.Decision{},
		outbox: map[string]*fakeOutbox{}, receipts: map[string]domain.PlanReceipt{},
	}
}

func (store *fakeStore) upsertDesired(desired domain.DesiredSnapshot, enqueue bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := desired.Key.String()
	if current, found := store.desired[key]; !found || desired.Generation >= current.Generation {
		store.desired[key] = desired
	}
	if enqueue {
		store.enqueueLocked(desired.Key, desired.RecordedAt)
	}
}

func (store *fakeStore) enqueue(key domain.Key, available time.Time) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.enqueueLocked(key, available)
}

func (store *fakeStore) enqueueLocked(key domain.Key, available time.Time) {
	id := key.String()
	if existing := store.queue[id]; existing != nil {
		if available.Before(existing.available) {
			existing.available = available
		}
		existing.revision++
		return
	}
	store.queue[id] = &fakeWork{key: key, available: available, revision: 1}
}

func (store *fakeStore) LeaseWork(_ context.Context, now time.Time, duration time.Duration) (ports.WorkLease, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	keys := sortedMapKeys(store.queue)
	for _, id := range keys {
		item := store.queue[id]
		if item.available.After(now) || (item.token != "" && item.until.After(now)) {
			continue
		}
		item.attempt++
		item.token = "work-token-" + id + "-" + time.Duration(item.attempt).String()
		item.until = now.Add(duration)
		return ports.WorkLease{Key: item.key, Token: item.token, Revision: item.revision, Attempt: item.attempt, ExpiresAt: item.until}, true, nil
	}
	return ports.WorkLease{}, false, nil
}

func (store *fakeStore) LoadInput(_ context.Context, key domain.Key) (domain.Input, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.loadError != nil {
		return domain.Input{}, store.loadError
	}
	input := domain.Input{}
	if desired, found := store.desired[key.String()]; found {
		copy := desired
		input.Desired = &copy
	}
	if observed, found := store.observations[key.String()]; found {
		copy := observed
		input.Observation = &copy
	}
	return input, nil
}

func (store *fakeStore) CommitDecision(_ context.Context, lease ports.WorkLease, decision domain.Decision, proposal *domain.Proposal) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	item := store.queue[lease.Key.String()]
	if item == nil || item.token != lease.Token {
		return errors.New("stale work lease")
	}
	if store.failCommitDecision {
		return errors.New("injected decision persistence failure")
	}
	if store.failAudit {
		return errors.New("injected reconciliation audit failure")
	}
	store.decisions[decision.ID] = decision
	store.audits[decision.ID] = decision
	if proposal != nil {
		if store.outbox[proposal.ID] == nil {
			store.outbox[proposal.ID] = &fakeOutbox{proposal: *proposal, available: decision.DecidedAt}
		}
	}
	if item.revision == lease.Revision {
		delete(store.queue, lease.Key.String())
	} else {
		item.token, item.until = "", time.Time{}
		item.available = decision.DecidedAt
	}
	return nil
}

func (store *fakeStore) FailWork(_ context.Context, lease ports.WorkLease, _ string, retryAt time.Time, exhausted bool) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	item := store.queue[lease.Key.String()]
	if item == nil || item.token != lease.Token {
		return errors.New("stale work lease")
	}
	if exhausted {
		delete(store.queue, lease.Key.String())
		store.exhaustedFailures++
		return nil
	}
	item.available, item.token, item.until = retryAt, "", time.Time{}
	return nil
}

func (store *fakeStore) RepairDroppedWork(_ context.Context, now time.Time, limit int) (int, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	keys := sortedMapKeys(store.desired)
	repaired := 0
	for _, id := range keys {
		if repaired >= limit || store.queue[id] != nil {
			continue
		}
		desired := store.desired[id]
		input := domain.Input{Desired: &desired}
		if observed, found := store.observations[id]; found {
			input.Observation = &observed
		}
		digest, err := input.Digest()
		if err != nil {
			return repaired, err
		}
		latest := store.latestDecisionLocked(desired.Key)
		if latest.InputDigest == digest {
			continue
		}
		store.enqueueLocked(desired.Key, now)
		repaired++
	}
	return repaired, nil
}

func (store *fakeStore) LeaseProposal(_ context.Context, now time.Time, duration time.Duration) (ports.ProposalLease, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	keys := sortedMapKeys(store.outbox)
	for _, id := range keys {
		item := store.outbox[id]
		if item.available.After(now) || (item.token != "" && item.until.After(now)) {
			continue
		}
		item.attempt++
		item.token = "proposal-token-" + id + "-" + time.Duration(item.attempt).String()
		item.until = now.Add(duration)
		return ports.ProposalLease{Proposal: item.proposal, Token: item.token, Attempt: item.attempt, ExpiresAt: item.until}, true, nil
	}
	return ports.ProposalLease{}, false, nil
}

func (store *fakeStore) CompleteProposal(_ context.Context, lease ports.ProposalLease, receipt domain.PlanReceipt) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	item := store.outbox[lease.Proposal.ID]
	if item == nil || item.token != lease.Token {
		return errors.New("stale proposal lease")
	}
	if store.failCompleteProposal {
		return errors.New("injected crash before outbox acknowledgement")
	}
	store.receipts[receipt.ProposalID] = receipt
	delete(store.outbox, lease.Proposal.ID)
	return nil
}

func (store *fakeStore) FailProposal(_ context.Context, lease ports.ProposalLease, _ string, retryAt time.Time, exhausted bool) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	item := store.outbox[lease.Proposal.ID]
	if item == nil || item.token != lease.Token {
		return errors.New("stale proposal lease")
	}
	if exhausted {
		delete(store.outbox, lease.Proposal.ID)
		store.exhaustedFailures++
		return nil
	}
	item.available, item.token, item.until = retryAt, "", time.Time{}
	return nil
}

func (store *fakeStore) latestDecision(key domain.Key) domain.Decision {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.latestDecisionLocked(key)
}

func (store *fakeStore) latestDecisionLocked(key domain.Key) domain.Decision {
	latest := domain.Decision{}
	for _, decision := range store.decisions {
		if decision.Key == key && (latest.DecidedAt.IsZero() || decision.DecidedAt.After(latest.DecidedAt) ||
			(decision.DecidedAt.Equal(latest.DecidedAt) && decision.ID > latest.ID)) {
			latest = decision
		}
	}
	return latest
}

type fakePlanner struct {
	mu    sync.Mutex
	clock ports.Clock
	calls int
	plans map[string]domain.PlanReceipt
	err   error
}

func newFakePlanner(clock ports.Clock) *fakePlanner {
	return &fakePlanner{clock: clock, plans: map[string]domain.PlanReceipt{}}
}

func (planner *fakePlanner) Propose(_ context.Context, proposal domain.Proposal) (domain.PlanReceipt, error) {
	planner.mu.Lock()
	defer planner.mu.Unlock()
	planner.calls++
	if planner.err != nil {
		return domain.PlanReceipt{}, planner.err
	}
	if receipt, found := planner.plans[proposal.IdempotencyKey]; found {
		return receipt, nil
	}
	receipt := domain.PlanReceipt{ProposalID: proposal.ID, PlanID: "pln_" + proposal.IdempotencyKey[4:], CreatedAt: planner.clock.Now().UTC()}
	planner.plans[proposal.IdempotencyKey] = receipt
	return receipt, nil
}

func desiredSnapshot(key domain.Key, generation uint64, target string, recordedAt time.Time) domain.DesiredSnapshot {
	payload := []byte(`{"environment":"` + key.Environment + `","installation":"` + key.Installation + `"}`)
	sum := sha256.Sum256(payload)
	return domain.DesiredSnapshot{
		Key: key, Generation: strconv.FormatUint(generation, 10), TargetDigest: digest(target), PayloadDigest: "sha256:" + hex.EncodeToString(sum[:]),
		Payload: payload, RecordedAt: recordedAt,
	}
}

func observation(key domain.Key, generation uint64, target string, observedAt time.Time, complete bool) domain.Observation {
	return domain.Observation{Key: key, Generation: strconv.FormatUint(generation, 10), TargetDigest: digest(target), Complete: complete, ObservedAt: observedAt}
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sortedMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
