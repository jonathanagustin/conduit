package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/liveevents/adapters/filejournal"
	schemavalidator "github.com/jonathanagustin/conduit/internal/liveevents/adapters/schema"
	"github.com/jonathanagustin/conduit/internal/liveevents/domain"
)

type ownerKey struct{}
type authFixture struct {
	mu         sync.Mutex
	identities map[string]domain.Identity
	allowed    map[string]map[string]bool
	errors     map[string]error
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func owner(ctx context.Context) string { value, _ := ctx.Value(ownerKey{}).(string); return value }
func asOwner(value string) context.Context {
	return context.WithValue(context.Background(), ownerKey{}, value)
}
func (auth *authFixture) Snapshot(ctx context.Context) (domain.Identity, error) {
	auth.mu.Lock()
	defer auth.mu.Unlock()
	name := owner(ctx)
	if err := auth.errors[name]; err != nil {
		return domain.Identity{}, err
	}
	identity, ok := auth.identities[name]
	if !ok {
		return domain.Identity{}, domain.ErrUnauthorized
	}
	return identity, nil
}
func (auth *authFixture) Visible(ctx context.Context, expected domain.Identity, subject domain.Subject) (bool, error) {
	auth.mu.Lock()
	defer auth.mu.Unlock()
	name := owner(ctx)
	if auth.identities[name] != expected {
		return false, domain.ErrReset
	}
	return auth.allowed[name][subject.ID], nil
}

type readyFixture struct{ err error }

func (ready *readyFixture) Check(context.Context) error { return ready.err }

type setup struct {
	service *Service
	auth    *authFixture
	ready   *readyFixture
	journal *filejournal.Journal
	path    string
	now     time.Time
}

func newSetup(t *testing.T) *setup {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.json")
	journal, err := filejournal.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := schemavalidator.New()
	if err != nil {
		t.Fatal(err)
	}
	auth := &authFixture{identities: map[string]domain.Identity{}, allowed: map[string]map[string]bool{}, errors: map[string]error{}}
	for _, name := range []string{"alice", "bob"} {
		auth.identities[name] = domain.Identity{Owner: digest(name), Represented: digest(""), Authorization: digest(name + "-grants-1")}
		auth.allowed[name] = map[string]bool{}
	}
	fixture := &setup{auth: auth, ready: &readyFixture{}, journal: journal, path: path, now: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)}
	fixture.service, err = New(Config{Journal: journal, Authorizer: auth, Validator: validator, Readiness: fixture.ready, Now: func() time.Time { return fixture.now }})
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}
func record(id string, position, revision uint64) domain.Record {
	return domain.Record{Position: position, Subject: domain.Subject{Type: "plan", ID: id}, Content: domain.Content{OccurredAt: time.Date(2026, 11, 15, 11, 59, 0, 0, time.UTC), Kind: "plan.changed", Resource: domain.Resource{Type: "plan", ID: id, Revision: revision}, Data: json.RawMessage(`{"status":"RUNNING"}`)}}
}
func open(t *testing.T, fixture *setup, name, cursor string) *Subscription {
	t.Helper()
	subscription, err := fixture.service.Open(asOwner(name), cursor)
	if err != nil {
		t.Fatal(err)
	}
	return subscription
}
func publish(t *testing.T, fixture *setup, records ...domain.Record) {
	t.Helper()
	if err := fixture.service.PublishCommittedBatch(context.Background(), records); err != nil {
		t.Fatal(err)
	}
}
func next(t *testing.T, subscription *Subscription) *domain.Envelope {
	t.Helper()
	value, err := subscription.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if value == nil {
		t.Fatal("missing expected event")
	}
	return value
}
func delivered(t *testing.T, subscription *Subscription, envelope *domain.Envelope) {
	t.Helper()
	if err := subscription.Delivered(envelope.Cursor); err != nil {
		t.Fatal(err)
	}
}

func TestVisibilityPrecedesContiguousSequenceAndQueueAllocation(t *testing.T) {
	fixture := newSetup(t)
	fixture.auth.allowed["alice"]["visible"] = true
	fixture.auth.allowed["bob"]["hidden"] = true
	alice := open(t, fixture, "alice", "")
	defer alice.Close()
	bob := open(t, fixture, "bob", "")
	defer bob.Close()
	if alice.Ready.Sequence != 0 || alice.Ready.Cursor == "" || alice.Ready.StreamID == bob.Ready.StreamID {
		t.Fatal("invalid initial watermarks")
	}
	publish(t, fixture, record("hidden", 1, 1), record("visible", 1, 1), record("hidden", 2, 2), record("visible", 2, 2))
	for _, pair := range []struct {
		subscription *Subscription
		id           string
	}{{alice, "visible"}, {bob, "hidden"}} {
		for sequence := uint64(1); sequence <= 2; sequence++ {
			value := next(t, pair.subscription)
			if value.Sequence != sequence || value.Resource.ID != pair.id {
				t.Fatalf("visibility/count leak: %+v", value)
			}
			delivered(t, pair.subscription, value)
		}
		if value, err := pair.subscription.Next(context.Background()); err != nil || value != nil {
			t.Fatalf("unexpected event: %+v %v", value, err)
		}
	}
	before, _ := fixture.journal.Snapshot(context.Background())
	fixture.auth.errors["denied"] = domain.ErrForbidden
	if _, err := fixture.service.Open(asOwner("denied"), ""); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal(err)
	}
	if _, err := fixture.service.Open(context.Background(), ""); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal(err)
	}
	after, _ := fixture.journal.Snapshot(context.Background())
	if len(before.Streams) != len(after.Streams) || len(fixture.service.sessions) != 2 {
		t.Fatal("refused caller allocated state")
	}
}

func TestDurableReplayAndOfflineCatchupKeepStablePositions(t *testing.T) {
	fixture := newSetup(t)
	fixture.auth.allowed["alice"]["visible"] = true
	subscription := open(t, fixture, "alice", "")
	root := subscription.Ready.Cursor
	publish(t, fixture, record("visible", 1, 1))
	first := next(t, subscription)
	delivered(t, subscription, first)
	subscription.Close()
	// A new process-side service and journal retain source and replay positions.
	journal, err := filejournal.Open(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	validator, _ := schemavalidator.New()
	fixture.journal = journal
	fixture.service, err = New(Config{Journal: journal, Authorizer: fixture.auth, Validator: validator, Readiness: fixture.ready, Now: func() time.Time { return fixture.now }})
	if err != nil {
		t.Fatal(err)
	}
	publish(t, fixture, record("hidden", 1, 1), record("visible", 2, 2))
	resumed := open(t, fixture, "alice", root)
	defer resumed.Close()
	replay := next(t, resumed)
	if replay.Cursor != first.Cursor || replay.Sequence != first.Sequence || replay.StreamID != first.StreamID {
		t.Fatal("replay changed identity")
	}
	delivered(t, resumed, replay)
	second := next(t, resumed)
	if second.Sequence != 2 || second.Resource.Revision != 2 {
		t.Fatalf("offline catchup: %+v", second)
	}
	delivered(t, resumed, second)
	resumed.Close()
	again := open(t, fixture, "alice", first.Cursor)
	defer again.Close()
	if again.Ready.Sequence != 1 || again.Ready.StreamID != first.StreamID {
		t.Fatal("resume watermark changed")
	}
	replaySecond := next(t, again)
	if replaySecond.Cursor != second.Cursor || replaySecond.Sequence != 2 {
		t.Fatal("same event got a new replay position")
	}
	if _, err := fixture.service.Open(asOwner("bob"), first.Cursor); !errors.Is(err, domain.ErrReset) {
		t.Fatalf("foreign cursor: %v", err)
	}
	if _, err := fixture.service.Open(asOwner("alice"), "unknown_cursor"); !errors.Is(err, domain.ErrReset) {
		t.Fatalf("unknown cursor: %v", err)
	}
}

func TestAuthorizationChangesAndReplayVisibilityRequireReset(t *testing.T) {
	fixture := newSetup(t)
	fixture.auth.allowed["alice"]["visible"] = true
	subscription := open(t, fixture, "alice", "")
	root := subscription.Ready.Cursor
	publish(t, fixture, record("visible", 1, 1))
	fixture.auth.allowed["alice"]["visible"] = false
	if _, err := subscription.Next(context.Background()); !errors.Is(err, domain.ErrReset) {
		t.Fatalf("revoked row: %v", err)
	}
	subscription.Close()
	if _, err := fixture.service.Open(asOwner("alice"), root); !errors.Is(err, domain.ErrReset) {
		t.Fatalf("replay ignored current row permission: %v", err)
	}
	fixture.auth.allowed["alice"]["visible"] = true
	identity := fixture.auth.identities["alice"]
	identity.Authorization = digest("grants-2")
	fixture.auth.identities["alice"] = identity
	if _, err := fixture.service.Open(asOwner("alice"), root); !errors.Is(err, domain.ErrReset) {
		t.Fatalf("authorization revision cursor reuse: %v", err)
	}
	fresh := open(t, fixture, "alice", "")
	defer fresh.Close()
	fixture.auth.errors["alice"] = domain.ErrUnauthorized
	if _, err := fresh.Next(context.Background()); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("identity revocation: %v", err)
	}
}

func TestOverflowTerminatesWithoutDroppingDurableReplay(t *testing.T) {
	fixture := newSetup(t)
	fixture.auth.allowed["alice"]["visible"] = true
	subscription := open(t, fixture, "alice", "")
	root := subscription.Ready.Cursor
	batch := make([]domain.Record, domain.MaxQueueEvents)
	for index := range batch {
		batch[index] = record("visible", uint64(index+1), uint64(index+1))
	}
	publish(t, fixture, batch...)
	first := next(t, subscription)
	if _, err := subscription.Next(context.Background()); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("unbounded in-flight read: %v", err)
	}
	// One in-flight event still counts; a 257th envelope closes this connection.
	publish(t, fixture, record("visible", 257, 257))
	if _, err := subscription.Next(context.Background()); !errors.Is(err, domain.ErrReset) {
		t.Fatalf("overflow did not terminate: %v", err)
	}
	subscription.Close()
	resumed := open(t, fixture, "alice", root)
	defer resumed.Close()
	for sequence := uint64(1); sequence <= 257; sequence++ {
		value := next(t, resumed)
		if value.Sequence != sequence {
			t.Fatalf("lost durable event at %d", sequence)
		}
		if sequence == 1 && value.Cursor != first.Cursor {
			t.Fatal("overflow rewrote old cursor")
		}
		delivered(t, resumed, value)
	}
}

func TestRetentionLifetimeProducerDedupAndContractFailures(t *testing.T) {
	fixture := newSetup(t)
	fixture.auth.allowed["alice"]["visible"] = true
	subscription := open(t, fixture, "alice", "")
	root := subscription.Ready.Cursor
	original := record("visible", 1, 1)
	publish(t, fixture, original)
	publish(t, fixture, original)
	first := next(t, subscription)
	delivered(t, subscription, first)
	if value, err := subscription.Next(context.Background()); err != nil || value != nil {
		t.Fatal("duplicate producer event was reassigned")
	}
	changed := original
	changed.Content.Data = json.RawMessage(`{"status":"FAILED"}`)
	if err := fixture.service.PublishCommittedBatch(context.Background(), []domain.Record{changed}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("source retcon: %v", err)
	}
	subscription.Close()
	if err := fixture.service.PublishCommittedBatch(context.Background(), []domain.Record{record("visible", 3, 3)}); !errors.Is(err, domain.ErrReset) {
		t.Fatalf("source gap accepted: %v", err)
	}
	bad := record("visible", 2, 2)
	bad.Content.Data = json.RawMessage(`{"status":"RUNNING","prompt":"secret"}`)
	if err := fixture.service.PublishCommittedBatch(context.Background(), []domain.Record{bad}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("arbitrary payload accepted: %v", err)
	}
	bad = record("visible", 2, 2)
	bad.Content.Resource.ID = "foreign"
	if err := fixture.service.PublishCommittedBatch(context.Background(), []domain.Record{bad}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("subject binding accepted: %v", err)
	}
	if _, err := fixture.service.Open(asOwner("alice"), ""); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("failed producer left stream ready: %v", err)
	}
	publish(t, fixture, record("visible", 2, 2))
	fixture.now = fixture.now.Add(domain.ReplayRetention)
	if _, err := fixture.service.Open(asOwner("alice"), root); !errors.Is(err, domain.ErrReset) {
		t.Fatalf("expired cursor accepted: %v", err)
	}
	fresh := open(t, fixture, "alice", "")
	defer fresh.Close()
	fixture.now = fixture.now.Add(domain.ConnectionLifetime)
	if _, err := fresh.Next(context.Background()); !errors.Is(err, domain.ErrClosed) {
		t.Fatalf("connection lifetime: %v", err)
	}
}

func TestReadinessAndTokenCollisionsCannotInventAStream(t *testing.T) {
	fixture := newSetup(t)
	fixture.ready.err = errors.New("producer unavailable")
	if _, err := fixture.service.Open(asOwner("alice"), ""); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal(err)
	}
	state, _ := fixture.journal.Snapshot(context.Background())
	if len(state.Streams) != 0 {
		t.Fatal("unready source allocated a stream")
	}
	fixture.ready.err = nil
	fixture.service.cfg.Token = func(string) (string, error) { return "same_token", nil }
	if _, err := fixture.service.Open(asOwner("alice"), ""); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("token collision: %v", err)
	}
	state, _ = fixture.journal.Snapshot(context.Background())
	if len(state.Streams) != 0 {
		t.Fatal("collision persisted partial state")
	}
}

func TestProducerHighWatermarkSurvivesRetention(t *testing.T) {
	fixture := newSetup(t)
	first := record("visible", 1, 1)
	publish(t, fixture, first)
	fixture.now = fixture.now.Add(domain.ReplayRetention + time.Second)
	publish(t, fixture, record("visible", 2, 2))
	before, _ := fixture.journal.Snapshot(context.Background())
	publish(t, fixture, first)
	after, _ := fixture.journal.Snapshot(context.Background())
	if after.SourcePosition != before.SourcePosition {
		t.Fatal("expired duplicate was appended again")
	}
}

func TestDerivedIndexResourceIsBoundToNativeSubject(t *testing.T) {
	fixture := newSetup(t)
	fixture.auth.allowed["alice"]["change-1"] = true
	subscription := open(t, fixture, "alice", "")
	defer subscription.Close()
	subject := domain.Subject{Type: "change-request", ID: "change-1"}
	value := domain.Record{Position: 1, Subject: subject, Content: domain.Content{Kind: "index.invalidated", Resource: domain.Resource{Type: "index", ID: domain.IndexID(subject), Revision: 3}, OccurredAt: fixture.now, Data: json.RawMessage(`{"areas":["approvals","overview"]}`)}}
	publish(t, fixture, value)
	event := next(t, subscription)
	if event.Resource.ID != domain.IndexID(subject) || event.Resource.Revision != 3 {
		t.Fatal("index lost native revision binding")
	}
}

func TestCursorBindingIncludesRepresentedIdentity(t *testing.T) {
	fixture := newSetup(t)
	subscription := open(t, fixture, "alice", "")
	cursor := subscription.Ready.Cursor
	subscription.Close()
	identity := fixture.auth.identities["alice"]
	identity.Represented = digest("represented-bob")
	fixture.auth.identities["alice"] = identity
	if _, err := fixture.service.Open(asOwner("alice"), cursor); !errors.Is(err, domain.ErrReset) {
		t.Fatalf("represented identity reused cursor: %v", err)
	}
}

func TestReplayCountBoundaryRequiresResetInsteadOfSkippingPrefix(t *testing.T) {
	fixture := newSetup(t)
	fixture.auth.allowed["alice"]["visible"] = true
	subscription := open(t, fixture, "alice", "")
	root := subscription.Ready
	subscription.Close()
	// Store valid replay fixtures in one transaction instead of 10,001 fsyncs.
	if err := fixture.journal.Update(context.Background(), func(state *domain.State) error {
		stream := state.Streams[root.StreamID]
		for sequence := uint64(1); sequence <= domain.MaxReplayEvents+1; sequence++ {
			stream.Frames = append(stream.Frames, domain.Frame{Envelope: domain.Envelope{Version: 1, StreamID: stream.ID, Sequence: sequence, Cursor: fmt.Sprintf("cursor_%d", sequence), Content: record("visible", sequence, sequence).Content}, Subject: domain.Subject{Type: "plan", ID: "visible"}, StoredAt: fixture.now})
		}
		stream.LastSequence = domain.MaxReplayEvents + 1
		state.Streams[stream.ID] = stream
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Open(asOwner("alice"), root.Cursor); !errors.Is(err, domain.ErrReset) {
		t.Fatalf("trimmed replay skipped first event: %v", err)
	}
	resumed := open(t, fixture, "alice", "cursor_2")
	defer resumed.Close()
	if value := next(t, resumed); value.Sequence != 3 || value.Cursor != "cursor_3" {
		t.Fatal("first retained replay position changed")
	}
	state, _ := fixture.journal.Snapshot(context.Background())
	if len(state.Streams[root.StreamID].Frames) != domain.MaxReplayEvents {
		t.Fatal("replay retention is not bounded")
	}
}

func TestConcurrentNativePublishAndReadersRemainRaceSafe(t *testing.T) {
	fixture := newSetup(t)
	fixture.auth.allowed["alice"]["a"] = true
	fixture.auth.allowed["bob"]["b"] = true
	alice := open(t, fixture, "alice", "")
	defer alice.Close()
	bob := open(t, fixture, "bob", "")
	defer bob.Close()
	var group sync.WaitGroup
	failures := make(chan error, 2)
	for _, id := range []string{"a", "b"} {
		group.Add(1)
		go func(id string) {
			defer group.Done()
			for position := uint64(1); position <= 20; position++ {
				if err := fixture.service.PublishCommittedBatch(context.Background(), []domain.Record{record(id, position, position)}); err != nil {
					failures <- err
					return
				}
			}
		}(id)
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	for _, subscription := range []*Subscription{alice, bob} {
		for sequence := uint64(1); sequence <= 20; sequence++ {
			value := next(t, subscription)
			if value.Sequence != sequence {
				t.Fatal("concurrent fanout changed visible ordering")
			}
			delivered(t, subscription, value)
		}
	}
}
