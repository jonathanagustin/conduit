package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/domain"
	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/ports"
)

func TestPostgresStoreMultiReplicaLeaseAndOutboxRecovery(t *testing.T) {
	ctx := t.Context()
	left, right := newStorePair(t, ctx)
	resetStore(t, ctx, left)
	now := time.Date(2026, 8, 14, 18, 0, 0, 0, time.UTC)
	key := domain.Key{Environment: "tenant-a", Installation: "catalog"}
	desired := testDesired(key, "generation-1", "target-a", now)
	observation := testObservation(key, "generation-1", "target-b", now)
	if err := left.UpsertDesired(ctx, desired); err != nil {
		t.Fatal(err)
	}
	if err := right.UpsertObservation(ctx, observation); err != nil {
		t.Fatal(err)
	}

	winner := exactlyOneWorkLease(t, ctx, now, left, right)
	input, err := winner.store.LoadInput(ctx, winner.lease.Key)
	if err != nil {
		t.Fatal(err)
	}
	decision, proposal, err := domain.Decide(input, winner.lease.Attempt, now, 10*time.Minute)
	if err != nil || proposal == nil || decision.Kind != domain.DecisionProposePlan {
		t.Fatalf("decision=%#v proposal=%#v err=%v", decision, proposal, err)
	}
	if err := winner.store.CommitDecision(ctx, winner.lease, decision, proposal); err != nil {
		t.Fatal(err)
	}
	if _, found, err := right.LeaseWork(ctx, now, time.Minute); err != nil || found {
		t.Fatalf("completed work leased again: found=%v err=%v", found, err)
	}

	firstProposal := exactlyOneProposalLease(t, ctx, now, left, right)
	crashRecoveryAt := now.Add(2 * time.Minute)
	secondProposal := exactlyOneProposalLease(t, ctx, crashRecoveryAt, left, right)
	if secondProposal.lease.Token == firstProposal.lease.Token || secondProposal.lease.Attempt != 2 {
		t.Fatalf("proposal was not re-leased after crash: first=%#v second=%#v", firstProposal.lease, secondProposal.lease)
	}
	receipt := domain.PlanReceipt{
		ProposalID: secondProposal.lease.Proposal.ID,
		PlanID:     "pln_exactly_once",
		CreatedAt:  crashRecoveryAt,
	}
	if err := firstProposal.store.CompleteProposal(ctx, firstProposal.lease, receipt); err == nil {
		t.Fatal("expired proposal lease acknowledged the outbox")
	}
	if err := secondProposal.store.CompleteProposal(ctx, secondProposal.lease, receipt); err != nil {
		t.Fatal(err)
	}
	if _, found, err := left.LeaseProposal(ctx, crashRecoveryAt, time.Minute); err != nil || found {
		t.Fatalf("completed proposal leased again: found=%v err=%v", found, err)
	}
	assertCount(t, ctx, left, "conduit_reconciliation_decisions", 1)
	assertCount(t, ctx, left, "conduit_reconciliation_audit", 1)
	assertCount(t, ctx, left, "conduit_reconciliation_proposal_outbox", 0)
	assertCount(t, ctx, left, "conduit_reconciliation_plan_receipts", 1)
	if err := left.UpsertDesired(ctx, desired); err != nil {
		t.Fatal(err)
	}
	if err := right.UpsertObservation(ctx, observation); err != nil {
		t.Fatal(err)
	}
	if _, found, err := left.LeaseWork(ctx, crashRecoveryAt, time.Minute); err != nil || found {
		t.Fatalf("unchanged sync manufactured work: found=%v err=%v", found, err)
	}
}

func TestPostgresStoreFencesSupersedingInputAndRepairsDroppedWork(t *testing.T) {
	ctx := t.Context()
	store, other := newStorePair(t, ctx)
	resetStore(t, ctx, store)
	now := time.Date(2026, 8, 14, 19, 0, 0, 0, time.UTC)
	key := domain.Key{Environment: "tenant-a", Installation: "catalog"}
	if err := store.UpsertDesired(ctx, testDesired(key, "generation-1", "target-a", now)); err != nil {
		t.Fatal(err)
	}
	lease, found, err := store.LeaseWork(ctx, now, time.Minute)
	if err != nil || !found {
		t.Fatalf("initial lease: found=%v err=%v", found, err)
	}
	oldInput, err := store.LoadInput(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.UpsertDesired(ctx, testDesired(key, "generation-2", "target-b", now.Add(time.Second))); err != nil {
		t.Fatal(err)
	}
	oldDecision, oldProposal, err := domain.Decide(oldInput, lease.Attempt, now, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitDecision(ctx, lease, oldDecision, oldProposal); err != nil {
		t.Fatal(err)
	}
	assertCount(t, ctx, store, "conduit_reconciliation_proposal_outbox", 0)
	next, found, err := other.LeaseWork(ctx, now.Add(2*time.Second), time.Minute)
	if err != nil || !found {
		t.Fatalf("superseding input was lost: found=%v err=%v", found, err)
	}
	latest, err := other.LoadInput(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Desired == nil || latest.Desired.Generation != "generation-2" || latest.Desired.TargetDigest != digest("target-b") {
		t.Fatalf("latest desired=%#v", latest.Desired)
	}
	decision, proposal, err := domain.Decide(latest, next.Attempt, now.Add(2*time.Second), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.CommitDecision(ctx, next, decision, proposal); err != nil {
		t.Fatal(err)
	}

	if err := store.UpsertDesired(ctx, testDesired(key, "generation-3", "target-c", now.Add(3*time.Second))); err != nil {
		t.Fatal(err)
	}
	if _, err := store.database.ExecContext(ctx, "DELETE FROM conduit_reconciliation_work"); err != nil {
		t.Fatal(err)
	}
	if repaired, err := other.RepairDroppedWork(ctx, now.Add(4*time.Second), 10); err != nil || repaired != 1 {
		t.Fatalf("repair=%d err=%v", repaired, err)
	}
	if repaired, err := other.RepairDroppedWork(ctx, now.Add(5*time.Second), 10); err != nil || repaired != 0 {
		t.Fatalf("duplicate repair=%d err=%v", repaired, err)
	}
}

func TestPostgresStoreRollsBackDecisionWhenAuditFails(t *testing.T) {
	ctx := t.Context()
	store, _ := newStorePair(t, ctx)
	resetStore(t, ctx, store)
	now := time.Date(2026, 8, 14, 20, 0, 0, 0, time.UTC)
	key := domain.Key{Environment: "tenant-a", Installation: "catalog"}
	if err := store.UpsertDesired(ctx, testDesired(key, "generation-1", "target-a", now)); err != nil {
		t.Fatal(err)
	}
	lease, found, err := store.LeaseWork(ctx, now, time.Minute)
	if err != nil || !found {
		t.Fatalf("lease: found=%v err=%v", found, err)
	}
	input, err := store.LoadInput(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	decision, proposal, err := domain.Decide(input, lease.Attempt, now, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.database.ExecContext(ctx, `
CREATE OR REPLACE FUNCTION conduit_test_reject_reconciliation_audit()
RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected audit failure'; END $$;
CREATE TRIGGER conduit_test_reject_reconciliation_audit
BEFORE INSERT ON conduit_reconciliation_audit
FOR EACH ROW EXECUTE FUNCTION conduit_test_reject_reconciliation_audit()`); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitDecision(ctx, lease, decision, proposal); err == nil {
		t.Fatal("audit failure was reported as a successful decision")
	}
	if _, err := store.database.ExecContext(ctx, `
DROP TRIGGER conduit_test_reject_reconciliation_audit ON conduit_reconciliation_audit;
DROP FUNCTION conduit_test_reject_reconciliation_audit()`); err != nil {
		t.Fatal(err)
	}
	assertCount(t, ctx, store, "conduit_reconciliation_decisions", 0)
	assertCount(t, ctx, store, "conduit_reconciliation_audit", 0)
	assertCount(t, ctx, store, "conduit_reconciliation_proposal_outbox", 0)
	assertCount(t, ctx, store, "conduit_reconciliation_work", 1)
	if _, found, err := store.LeaseWork(ctx, now.Add(2*time.Minute), time.Minute); err != nil || !found {
		t.Fatalf("rolled-back work was not retryable: found=%v err=%v", found, err)
	}
}

type workLeaseResult struct {
	store *Store
	lease ports.WorkLease
	found bool
	err   error
}

func exactlyOneWorkLease(t *testing.T, ctx context.Context, now time.Time, stores ...*Store) workLeaseResult {
	t.Helper()
	results := make(chan workLeaseResult, len(stores))
	var wait sync.WaitGroup
	for _, store := range stores {
		wait.Add(1)
		go func(candidate *Store) {
			defer wait.Done()
			lease, found, err := candidate.LeaseWork(ctx, now, time.Minute)
			results <- workLeaseResult{store: candidate, lease: lease, found: found, err: err}
		}(store)
	}
	wait.Wait()
	close(results)
	winners := []workLeaseResult{}
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.found {
			winners = append(winners, result)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("work lease winners=%d", len(winners))
	}
	return winners[0]
}

type proposalLeaseResult struct {
	store *Store
	lease ports.ProposalLease
	found bool
	err   error
}

func exactlyOneProposalLease(t *testing.T, ctx context.Context, now time.Time, stores ...*Store) proposalLeaseResult {
	t.Helper()
	results := make(chan proposalLeaseResult, len(stores))
	var wait sync.WaitGroup
	for _, store := range stores {
		wait.Add(1)
		go func(candidate *Store) {
			defer wait.Done()
			lease, found, err := candidate.LeaseProposal(ctx, now, time.Minute)
			results <- proposalLeaseResult{store: candidate, lease: lease, found: found, err: err}
		}(store)
	}
	wait.Wait()
	close(results)
	winners := []proposalLeaseResult{}
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.found {
			winners = append(winners, result)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("proposal lease winners=%d", len(winners))
	}
	return winners[0]
}

func newStorePair(t *testing.T, ctx context.Context) (*Store, *Store) {
	t.Helper()
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is not set")
	}
	left, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = left.Close() })
	right, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = right.Close() })
	return left, right
}

func resetStore(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()
	if _, err := store.database.ExecContext(ctx, `
DROP TRIGGER IF EXISTS conduit_test_reject_reconciliation_audit ON conduit_reconciliation_audit;
DROP FUNCTION IF EXISTS conduit_test_reject_reconciliation_audit();
TRUNCATE conduit_reconciliation_desired, conduit_reconciliation_observations,
    conduit_reconciliation_work, conduit_reconciliation_decisions,
    conduit_reconciliation_audit, conduit_reconciliation_proposal_outbox,
    conduit_reconciliation_plan_receipts`); err != nil {
		t.Fatal(err)
	}
}

func assertCount(t *testing.T, ctx context.Context, store *Store, table string, expected int) {
	t.Helper()
	allowed := map[string]bool{
		"conduit_reconciliation_decisions":       true,
		"conduit_reconciliation_audit":           true,
		"conduit_reconciliation_proposal_outbox": true,
		"conduit_reconciliation_plan_receipts":   true,
		"conduit_reconciliation_work":            true,
	}
	if !allowed[table] {
		t.Fatalf("unapproved table %q", table)
	}
	var actual int
	if err := store.database.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("%s count=%d want=%d", table, actual, expected)
	}
}

func testDesired(key domain.Key, generation, target string, recordedAt time.Time) domain.DesiredSnapshot {
	payload := []byte(`{"environment":"` + key.Environment + `","installation":"` + key.Installation + `","generation":"` + generation + `"}`)
	sum := sha256.Sum256(payload)
	return domain.DesiredSnapshot{
		Key: key, Generation: generation, TargetDigest: digest(target),
		PayloadDigest: "sha256:" + hex.EncodeToString(sum[:]), Payload: payload, RecordedAt: recordedAt,
	}
}

func testObservation(key domain.Key, generation, target string, observedAt time.Time) domain.Observation {
	return domain.Observation{
		Key: key, Generation: generation, TargetDigest: digest(target), Complete: true, ObservedAt: observedAt,
	}
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
