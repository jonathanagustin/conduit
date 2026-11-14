package application

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/debugsessions/adapters/memory"
	"github.com/jonathanagustin/conduit/internal/debugsessions/domain"
	"github.com/jonathanagustin/conduit/internal/debugsessions/ports"
)

type authority struct {
	mu                      sync.Mutex
	decision                ports.Decision
	denied                  bool
	calls, denyAt, changeAt int
}

func (a *authority) Require(_ context.Context, _ string, _ domain.Scope) (ports.Decision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	if a.denied || (a.denyAt > 0 && a.calls >= a.denyAt) {
		return ports.Decision{}, domain.ErrForbidden
	}
	d := a.decision
	if a.changeAt > 0 && a.calls >= a.changeAt {
		d.Identity.Owner = "changed"
	}
	return d, nil
}
func (a *authority) set(owner, actor string, manage bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.decision = ports.Decision{Identity: domain.Identity{Actor: actor, Owner: owner}, ManageOthers: manage}
}
func id(last byte) string { return "dsr_" + strings.Repeat("a", 25) + string(last) }
func fixture(t *testing.T) (*Service, *memory.Store, *authority, *time.Time, Draft) {
	t.Helper()
	journal := memory.New()
	a := &authority{}
	a.set("alice", "automation", false)
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	s, err := New(journal, a, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return s, journal, a, &now, Draft{SessionID: id('a'), Target: domain.Target{Environment: "env", Namespace: "app", Profile: "profile"}}
}
func TestCreateReplayExpiryAndDeleteRetainImmutableHistory(t *testing.T) {
	s, journal, _, now, draft := fixture(t)
	created, err := s.Create(t.Context(), draft)
	if err != nil {
		t.Fatal(err)
	}
	if created.Actor != "automation" || created.Owner != "alice" || created.ExpiresAt.Sub(created.CreatedAt) != domain.MaximumLifetime {
		t.Fatal(created)
	}
	*now = now.Add(time.Hour)
	reused, err := s.Create(t.Context(), draft)
	if err != nil || reused != created {
		t.Fatal(reused, err)
	}
	*now = created.ExpiresAt
	expired, err := s.Get(t.Context(), draft.SessionID)
	if err != nil || expired.State != domain.Expired || expired.ExpiresAt != created.ExpiresAt || expired.Actor != expiryActor {
		t.Fatal(expired, err)
	}
	deleted, err := s.Mutate(t.Context(), draft.SessionID, domain.Deleted)
	if err != nil || deleted.State != domain.Deleted || deleted.Revision != 3 {
		t.Fatal(deleted, err)
	}
	if _, err := s.Get(t.Context(), draft.SessionID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	history, err := s.History(t.Context(), draft.SessionID)
	if err != nil || len(history) != 3 || history[0] != created {
		t.Fatal(history, err)
	}
	replay, err := s.Create(t.Context(), draft)
	if err != nil || replay != deleted {
		t.Fatal("resurrected request", replay, err)
	}
	again, err := journal.History(t.Context(), draft.SessionID)
	if err != nil || len(again) != 3 {
		t.Fatal(again, err)
	}
}
func TestKnowledgeOfAnIDCannotReadOrMutateAnotherOwner(t *testing.T) {
	s, journal, a, _, draft := fixture(t)
	if _, err := s.Create(t.Context(), draft); err != nil {
		t.Fatal(err)
	}
	a.set("bob", "bob", false)
	if _, err := s.Get(t.Context(), draft.SessionID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.History(t.Context(), draft.SessionID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.Mutate(t.Context(), draft.SessionID, domain.Revoked); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.Create(t.Context(), draft); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	events, err := journal.History(t.Context(), draft.SessionID)
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	a.set("bob", "manager", true)
	revoked, err := s.Mutate(t.Context(), draft.SessionID, domain.Revoked)
	if err != nil || revoked.Owner != "alice" || revoked.Actor != "manager" {
		t.Fatal(revoked, err)
	}
}
func TestRevocationBeforeAppendAndIdentityChangesFailWithoutWrites(t *testing.T) {
	for _, kind := range []string{"revoked", "identity"} {
		t.Run(kind, func(t *testing.T) {
			s, journal, a, _, draft := fixture(t)
			if kind == "revoked" {
				a.denyAt = 2
			} else {
				a.changeAt = 2
			}
			if _, err := s.Create(t.Context(), draft); !errors.Is(err, domain.ErrForbidden) {
				t.Fatal(err)
			}
			if _, err := journal.History(t.Context(), draft.SessionID); !errors.Is(err, domain.ErrNotFound) {
				t.Fatal(err)
			}
		})
	}
	s, journal, a, _, draft := fixture(t)
	if _, err := s.Create(t.Context(), draft); err != nil {
		t.Fatal(err)
	}
	a.denied = true
	if _, err := s.Get(t.Context(), draft.SessionID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err := s.Mutate(t.Context(), draft.SessionID, domain.Deleted); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal(err)
	}
	events, err := journal.History(t.Context(), draft.SessionID)
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
}
func TestListFiltersScopeOwnerAndDeletionWithoutForeignCursors(t *testing.T) {
	s, _, a, _, draft := fixture(t)
	for _, last := range []byte{'a', 'b', 'c', 'd', 'e'} {
		a.set("alice", "alice", false)
		d := draft
		d.SessionID = id(last)
		if last == 'b' {
			a.set("bob", "bob", false)
		}
		if last == 'c' {
			d.Target.Namespace = "other"
		}
		if _, err := s.Create(t.Context(), d); err != nil {
			t.Fatal(err)
		}
		if last == 'd' {
			if _, err := s.Mutate(t.Context(), d.SessionID, domain.Deleted); err != nil {
				t.Fatal(err)
			}
		}
	}
	a.set("alice", "alice", false)
	records, next, err := s.List(t.Context(), draft.Target.Scope(), "", 1)
	if err != nil || len(records) != 1 || records[0].SessionID != id('a') || next != id('a') {
		t.Fatal(records, next, err)
	}
	records, next, err = s.List(t.Context(), draft.Target.Scope(), next, 1)
	if err != nil || len(records) != 1 || records[0].SessionID != id('e') || next != "" {
		t.Fatal(records, next, err)
	}
	a.set("alice", "manager", true)
	records, _, err = s.List(t.Context(), draft.Target.Scope(), "", 100)
	if err != nil || len(records) != 3 {
		t.Fatal(records, err)
	}
}

type failingJournal struct {
	ports.Journal
	failure error
}

func (s failingJournal) Append(context.Context, uint64, domain.Event) error { return s.failure }
func TestFailedPersistenceCannotAcknowledgeCreationOrExpiry(t *testing.T) {
	s, journal, a, now, draft := fixture(t)
	failure := errors.New("storage unavailable")
	bad, err := New(failingJournal{journal, failure}, a, s.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.Create(t.Context(), draft); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if _, err := journal.History(t.Context(), draft.SessionID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	created, err := s.Create(t.Context(), draft)
	if err != nil {
		t.Fatal(err)
	}
	*now = created.ExpiresAt
	if record, err := bad.Get(t.Context(), draft.SessionID); !errors.Is(err, failure) || record.State != "" {
		t.Fatal(record, err)
	}
	events, err := journal.History(t.Context(), draft.SessionID)
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
}
func TestConcurrentExpiryCreatesExactlyOneAuditTransition(t *testing.T) {
	s, journal, _, now, draft := fixture(t)
	created, err := s.Create(t.Context(), draft)
	if err != nil {
		t.Fatal(err)
	}
	*now = created.ExpiresAt
	var workers sync.WaitGroup
	for range 24 {
		workers.Go(func() {
			e, err := s.Get(t.Context(), draft.SessionID)
			if err != nil || e.State != domain.Expired {
				t.Error(e, err)
			}
		})
	}
	workers.Wait()
	events, err := journal.History(t.Context(), draft.SessionID)
	if err != nil || len(events) != 2 {
		t.Fatal(events, err)
	}
}
func TestIdempotencyKeyCannotChangeTargetOrLifetime(t *testing.T) {
	s, _, _, _, draft := fixture(t)
	if _, err := s.Create(t.Context(), draft); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"target", "lifetime"} {
		d := draft
		if kind == "target" {
			d.Target.Profile = "other"
		} else {
			d.LifetimeSeconds = 60
		}
		if _, err := s.Create(t.Context(), d); !errors.Is(err, domain.ErrConflict) {
			t.Fatal(kind, err)
		}
	}
	for _, seconds := range []int64{-1, 14401, 1 << 60} {
		d := draft
		d.LifetimeSeconds = seconds
		if _, err := s.Create(t.Context(), d); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := s.Mutate(t.Context(), draft.SessionID, "READY"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	if _, _, err := s.List(t.Context(), draft.Target.Scope(), "", 101); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
}

type scanJournal struct {
	ports.Journal
	events []domain.Event
}

func (s scanJournal) IDs(_ context.Context, after string, limit int) ([]string, error) {
	out := []string{}
	for _, e := range s.events {
		if e.SessionID > after {
			out = append(out, e.SessionID)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}
func (s scanJournal) History(_ context.Context, id string) ([]domain.Event, error) {
	for _, e := range s.events {
		if e.SessionID == id {
			return []domain.Event{e}, nil
		}
	}
	return nil, domain.ErrNotFound
}
func TestExhaustedScanCannotMasqueradeAsAnEmptyAuthorizedList(t *testing.T) {
	_, _, a, now, draft := fixture(t)
	events := make([]domain.Event, maximumScan+1)
	for i := range events {
		n := i
		suffix := ""
		for range 4 {
			suffix = string(byte('a'+n%26)) + suffix
			n /= 26
		}
		e, err := domain.New("dsr_"+strings.Repeat("a", 22)+suffix, draft.Target, domain.Identity{Actor: "bob", Owner: "bob"}, *now, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		events[i] = e
	}
	s, err := New(scanJournal{events: events}, a, func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	records, cursor, err := s.List(t.Context(), draft.Target.Scope(), "", 100)
	if !errors.Is(err, ErrUnavailable) || records != nil || cursor != "" {
		t.Fatal(len(records), cursor, err)
	}
}
