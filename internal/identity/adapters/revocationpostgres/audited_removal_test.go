package revocationpostgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/governance/adapters/auditpostgres"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

func auditedStore(t *testing.T) (*Store, string, domain.AgentRevocation) {
	t.Helper()
	dsn := freshDSN(t)
	store, err := New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := auditpostgres.ApplyMigrations(t.Context(), store.db); err != nil {
		t.Fatal(err)
	}
	held, err := domain.NewAgentRevocation(domain.AgentRevocation{
		Scope: domain.RevocationEnvironment, Environment: "prod", Reason: "incident",
	}, "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutRevocation(t.Context(), held); err != nil {
		t.Fatal(err)
	}
	return store, dsn, held
}

func removalAudit(removed domain.AgentRevocation) (governance.AuthorizationAuditEvent, error) {
	return governance.NewAuthorizationCheckEvent(time.Now(), "alice", "AGENT_REVOCATION_REMOVE",
		"environment/"+removed.Environment, governance.DecisionAllow, "Agent revocation changed",
		map[string]any{"revocationId": removed.ID}), nil
}

func recordedRemovalAudits(t *testing.T, store *Store) []governance.AuthorizationAuditEvent {
	t.Helper()
	tx, err := store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	events, err := auditpostgres.New(tx).ListAuthorizationAudit(t.Context(), "environment/prod")
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func assertRevocationHeld(t *testing.T, store *Store, held domain.AgentRevocation) {
	t.Helper()
	got, err := store.Revocations(t.Context(), held.Environment)
	if err != nil || len(got) != 1 || got[0] != held {
		t.Fatalf("deny-list = %#v, %v; want original revocation %#v", got, err, held)
	}
}

func TestAuditedRemovalRollsBackOnCancellationAfterDelete(t *testing.T) {
	store, _, held := auditedStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	built := false
	_, err := store.DeleteRevocationAndAudit(ctx, held.Environment, held.ID,
		func(removed domain.AgentRevocation) (governance.AuthorizationAuditEvent, error) {
			built = true
			cancel() // The SQL DELETE has run; its transaction must not commit.
			return removalAudit(removed)
		})
	if !built || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation at write boundary: built=%v, err=%v", built, err)
	}
	assertRevocationHeld(t, store, held)
	if events := recordedRemovalAudits(t, store); len(events) != 0 {
		t.Fatalf("canceled removal left audit events: %#v", events)
	}
}

func TestAuditedRemovalRollsBackEachAuditWriteBoundary(t *testing.T) {
	for _, target := range []struct{ name, table, operation string }{
		{"event insert", "conduit_authorization_audit_events", "INSERT"},
		{"head update", "conduit_authorization_audit_head", "UPDATE"},
	} {
		t.Run(target.name, func(t *testing.T) {
			store, _, held := auditedStore(t)
			_, err := store.db.ExecContext(t.Context(), `
CREATE FUNCTION refuse_removal_audit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'injected removal audit failure'; END $$;
CREATE TRIGGER refuse_removal_audit BEFORE `+target.operation+` ON `+target.table+`
FOR EACH ROW EXECUTE FUNCTION refuse_removal_audit();`)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.DeleteRevocationAndAudit(t.Context(), held.Environment, held.ID, removalAudit); err == nil || !strings.Contains(err.Error(), "injected removal audit failure") {
				t.Fatalf("audit boundary did not fail as injected: %v", err)
			}
			assertRevocationHeld(t, store, held)
			if events := recordedRemovalAudits(t, store); len(events) != 0 {
				t.Fatalf("failed removal left audit events: %#v", events)
			}
		})
	}
}

func TestAuditedRemovalCommitsBothAndRetryCannotAddAnotherEvent(t *testing.T) {
	store, _, held := auditedStore(t)
	removed, err := store.DeleteRevocationAndAudit(t.Context(), held.Environment, held.ID, removalAudit)
	if err != nil || removed != held {
		t.Fatalf("audited removal: %#v, %v", removed, err)
	}
	if got, err := store.Revocations(t.Context(), held.Environment); err != nil || len(got) != 0 {
		t.Fatalf("committed removal retained deny-list record: %#v, %v", got, err)
	}
	if events := recordedRemovalAudits(t, store); len(events) != 1 || events[0].Action != "AGENT_REVOCATION_REMOVE" || events[0].Subject != "alice" || events[0].Context["revocationId"] != held.ID {
		t.Fatalf("committed removal audit = %#v", events)
	}
	if _, err := store.DeleteRevocationAndAudit(t.Context(), held.Environment, held.ID, removalAudit); !errors.Is(err, domain.ErrRevocationNotFound) {
		t.Fatalf("repeated removal = %v; want not found", err)
	}
	if events := recordedRemovalAudits(t, store); len(events) != 1 {
		t.Fatalf("retry appended another event: %#v", events)
	}
}

func TestPendingRemovalStillRefusesCredentialsAndPreservesANewerRevocation(t *testing.T) {
	first, dsn, held := auditedStore(t)
	second, err := New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	second.db.SetMaxOpenConns(1)
	var writerPID int
	if err := second.db.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&writerPID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	finished := make(chan error, 1)
	auditFailure := errors.New("audit event construction refused")
	go func() {
		_, err := first.DeleteRevocationAndAudit(ctx, held.Environment, held.ID,
			func(domain.AgentRevocation) (governance.AuthorizationAuditEvent, error) {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
				}
				return governance.AuthorizationAuditEvent{}, auditFailure
			})
		finished <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("removal did not reach the audit boundary")
	}
	// This is what another Hub's Agent guard reads during the pending removal.
	assertRevocationHeld(t, second, held)
	credential := domain.AgentCredential{
		Identity:  domain.AgentIdentity{Environment: held.Environment, AgentID: "primary"},
		NotBefore: held.NotBefore.Add(-time.Hour),
	}
	if _, refused := domain.RefusingRevocation([]domain.AgentRevocation{held}, credential); !refused {
		t.Fatal("the still-visible record did not refuse the credential")
	}
	newer := held
	newer.Reason, newer.CreatedBy = "newer incident", "bob"
	newer.CreatedAt = held.CreatedAt.Add(time.Minute)
	newer.NotBefore = held.NotBefore.Add(time.Minute)
	written := make(chan error, 1)
	go func() { _, err := second.PutRevocation(ctx, newer); written <- err }()
	// Observe the actual writer waiting on the shared advisory lock before
	// releasing the failed removal; no scheduler-dependent sleep is needed.
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for waiting := false; !waiting; {
		if err := first.db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid = $1 AND locktype = 'advisory' AND NOT granted)`, writerPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if !waiting {
			select {
			case <-ticker.C:
			case err := <-written:
				t.Fatalf("newer write passed the pending removal: %v", err)
			case <-ctx.Done():
				t.Fatal("newer write did not wait for the pending removal")
			}
		}
	}
	close(release)
	if err := <-finished; !errors.Is(err, auditFailure) {
		t.Fatalf("pending removal result = %v", err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	assertRevocationHeld(t, first, newer)
	if events := recordedRemovalAudits(t, first); len(events) != 0 {
		t.Fatalf("refused removal appended audit events: %#v", events)
	}
}
