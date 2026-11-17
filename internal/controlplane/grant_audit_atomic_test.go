package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type grantAuditFaultRepository struct {
	Repository
	before func(context.Context) error
	after  error
}

func (repository grantAuditFaultRepository) AppendAuthorizationAudit(ctx context.Context, event AuthorizationAuditEvent) (AuthorizationAuditEvent, error) {
	if event.Action == "RESOURCE_GRANT_CONFIGURE" && repository.before != nil {
		if err := repository.before(ctx); err != nil {
			return AuthorizationAuditEvent{}, err
		}
	}
	stored, err := repository.Repository.AppendAuthorizationAudit(ctx, event)
	if err == nil && event.Action == "RESOURCE_GRANT_CONFIGURE" && repository.after != nil {
		return AuthorizationAuditEvent{}, repository.after
	}
	return stored, err
}

func (repository grantAuditFaultRepository) Transaction(ctx context.Context, operation func(Repository) error) error {
	return repository.Repository.(TransactionalRepository).Transaction(ctx, func(working Repository) error {
		return operation(grantAuditFaultRepository{Repository: working, before: repository.before, after: repository.after})
	})
}

func grantAtomicRepository(t *testing.T, mode string) (Repository, func() Repository) {
	t.Helper()
	switch mode {
	case "memory":
		repository := NewMemoryRepository()
		return repository, func() Repository { return repository }
	case "file":
		root := filepath.Join(t.TempDir(), "repository")
		open := func() Repository {
			repository, err := NewFileRepository(root)
			if err != nil {
				t.Fatal(err)
			}
			return repository
		}
		return open(), open
	case "postgres":
		repository, dsn := auditRepositoryForTest(t)
		return repository, func() Repository {
			reopened, err := NewPostgresRepository(t.Context(), dsn)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reopened.Close() })
			return reopened
		}
	default:
		t.Fatalf("unknown repository %q", mode)
		return nil, nil
	}
}

func initialAtomicGrant(t *testing.T, repository Repository) ResourceGrant {
	t.Helper()
	service := NewGovernanceService(repository)
	if _, err := service.PutTeam(t.Context(), Team{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"alice"}, MemberUsers: []string{"alice"}}); err != nil {
		t.Fatal(err)
	}
	grant, err := service.PutGrant(t.Context(), ResourceGrant{ResourceRID: environmentRID("prod"), Team: "reviewers", Roles: []string{"environment:viewer"}})
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

func requireAtomicGrant(t *testing.T, repository Repository, expected ResourceGrant, auditCount int) {
	t.Helper()
	grants, err := repository.ListResourceGrants(t.Context(), expected.ResourceRID)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grant changed outside its paired commit: grants=%#v expected=%#v error=%v", grants, expected, err)
	}
	// The public record omits empty arrays; File persistence can represent
	// that same record with nil slices. Compare its complete wire value.
	actualJSON, err := json.Marshal(grants[0])
	if err != nil {
		t.Fatal(err)
	}
	expectedJSON, err := json.Marshal(expected)
	if err != nil || !bytes.Equal(actualJSON, expectedJSON) {
		t.Fatalf("grant changed outside its paired commit: actual=%s expected=%s error=%v", actualJSON, expectedJSON, err)
	}
	if events := grantAuditEvents(t, repository, "RESOURCE_GRANT_CONFIGURE"); len(events) != auditCount {
		t.Fatalf("content audit count=%d, expected=%d", len(events), auditCount)
	}
}

func TestGrantAuditFailureAndCancellationRollBackWithoutNotifications(t *testing.T) {
	for _, mode := range []string{"memory", "file", "postgres"} {
		for _, failure := range []string{"before-audit", "after-audit", "cancellation"} {
			t.Run(mode+"/"+failure, func(t *testing.T) {
				repository, reopen := grantAtomicRepository(t, mode)
				initial := initialAtomicGrant(t, repository)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				injected := errors.New("injected content audit failure")
				fault := grantAuditFaultRepository{Repository: repository}
				switch failure {
				case "before-audit":
					fault.before = func(context.Context) error { return injected }
				case "after-audit":
					fault.after = injected
				case "cancellation":
					fault.before = func(context.Context) error { cancel(); return nil }
				}
				next := initial
				next.Roles = []string{"environment:administrator"}
				_, err := NewGovernanceService(fault).PutGrantAs(ctx, next, "alice")
				if err == nil || (failure != "cancellation" && !errors.Is(err, injected)) {
					t.Fatalf("grant mutation returned %v", err)
				}
				if failure == "cancellation" && !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled mutation returned %v", err)
				}
				requireAtomicGrant(t, repository, initial, 0)
				requireAtomicGrant(t, reopen(), initial, 0)
			})
		}
	}
}

func TestCaughtGrantAuditFailureCannotCommitWithAnOuterTransaction(t *testing.T) {
	for _, mode := range []string{"memory", "file", "postgres"} {
		t.Run(mode, func(t *testing.T) {
			repository, reopen := grantAtomicRepository(t, mode)
			initial := initialAtomicGrant(t, repository)
			injected := errors.New("injected content audit failure")
			err := repository.(TransactionalRepository).Transaction(t.Context(), func(parent Repository) error {
				fault := grantAuditFaultRepository{Repository: parent, after: injected}
				next := initial
				next.Roles = []string{"environment:administrator"}
				if _, err := NewGovernanceService(fault).PutGrantAs(t.Context(), next, "alice"); !errors.Is(err, injected) {
					return errors.New("expected the child grant operation to fail")
				}
				_, err := parent.PutTeam(t.Context(), Team{Name: "unrelated", Revision: 1})
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			requireAtomicGrant(t, reopen(), initial, 0)
			if _, err := repository.GetTeam(t.Context(), "unrelated"); err != nil {
				t.Fatalf("unrelated parent change did not commit: %v", err)
			}
		})
	}
}

func TestGrantAndContentAuditCommitOnceAndKeepRevisionConflicts(t *testing.T) {
	for _, mode := range []string{"memory", "file", "postgres"} {
		t.Run(mode, func(t *testing.T) {
			repository, reopen := grantAtomicRepository(t, mode)
			initial := initialAtomicGrant(t, repository)
			service := NewGovernanceService(repository)
			next := initial
			next.Roles = []string{"environment:administrator"}
			stored, err := service.PutGrantAs(t.Context(), next, "alice")
			if err != nil || stored.Revision != initial.Revision+1 {
				t.Fatalf("paired mutation failed: %#v %v", stored, err)
			}
			requireAtomicGrant(t, reopen(), stored, 1)
			event := grantAuditEvents(t, repository, "RESOURCE_GRANT_CONFIGURE")[0]
			if event.Subject != "alice" || !reflect.DeepEqual(auditedRoles(t, event.RequestFields["grantBefore"]), initial.Roles) || !reflect.DeepEqual(auditedRoles(t, event.RequestFields["grantAfter"]), stored.Roles) {
				t.Fatalf("paired audit lost actor or exact before/after roles: %#v", event)
			}
			if _, err := service.PutGrantAs(t.Context(), next, "alice"); !errors.Is(err, ErrConflict) {
				t.Fatalf("stale revision returned %v", err)
			}
			requireAtomicGrant(t, repository, stored, 1)
		})
	}
}

// Embedding only Repository deliberately withholds Transaction from this custom
// adapter's method set. An audited write must refuse before touching its state.
type grantRepositoryWithoutTransactions struct{ Repository }

func TestAuditedGrantRefusesAnAdapterWithoutTransactions(t *testing.T) {
	repository := NewMemoryRepository()
	initial := initialAtomicGrant(t, repository)
	next := initial
	next.Roles = []string{"environment:administrator"}
	if _, err := NewGovernanceService(grantRepositoryWithoutTransactions{repository}).PutGrantAs(t.Context(), next, "alice"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("nontransactional audited mutation returned %v", err)
	}
	requireAtomicGrant(t, repository, initial, 0)
}

func TestPendingGrantDoesNotExposeNewAuthorityBeforeItsAudit(t *testing.T) {
	repository, _ := auditRepositoryForTest(t)
	initial := initialAtomicGrant(t, repository)
	paused, release := make(chan struct{}), make(chan struct{})
	fault := grantAuditFaultRepository{Repository: repository, before: func(ctx context.Context) error {
		close(paused)
		select {
		case <-release:
			return errors.New("reject pending audit")
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		next := initial
		next.Roles = []string{"environment:administrator"}
		_, err := NewGovernanceService(fault).PutGrantAs(ctx, next, "alice")
		done <- err
	}()
	select {
	case <-paused:
	case <-ctx.Done():
		t.Fatal("grant mutation did not reach the audit boundary")
	}
	// Release the goroutine even if the admission assertion fails.
	defer func() {
		close(release)
		if err := <-done; err == nil {
			t.Error("pending audit failure was ignored")
		}
		requireAtomicGrant(t, repository, initial, 0)
	}()
	requireAtomicGrant(t, repository, initial, 0)
}

func TestNestedGrantRecordsOnePrivateIntentAndNoLiveObserver(t *testing.T) {
	intents := &recordedAnnouncements{}
	repository := &configuredIntentRepository{Repository: NewMemoryRepository(), intents: intents.all()}
	initial := initialAtomicGrant(t, repository)
	err := repository.Transaction(t.Context(), func(parent Repository) error {
		service := NewGovernanceService(parent)
		service.SetAnnouncements(intents.all())
		next := initial
		next.Roles = []string{"environment:administrator"}
		_, err := service.PutGrantAs(t.Context(), next, "alice")
		return err
	})
	if err != nil || len(intents.grants) != 1 {
		t.Fatalf("nested grant duplicated or lost private intent: error=%v intents=%#v", err, intents.grants)
	}
}
