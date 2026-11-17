package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	governanceports "github.com/jonathanagustin/conduit/internal/governance/ports"
)

// configuredIntentRepository provides a configured root and transaction-local
// ports without PostgreSQL. The source writes still use the real Memory
// transaction's rollback semantics; SQL intent durability is tested separately.
type configuredIntentRepository struct {
	Repository
	intents  Announcements
	active   bool
	before   func(Repository) error
	auditErr error
}

func (repository *configuredIntentRepository) AppendAuthorizationAudit(ctx context.Context, event AuthorizationAuditEvent) (AuthorizationAuditEvent, error) {
	if repository.auditErr != nil {
		return AuthorizationAuditEvent{}, repository.auditErr
	}
	return repository.Repository.AppendAuthorizationAudit(ctx, event)
}

func (repository *configuredIntentRepository) TransactionAnnouncements() (Announcements, bool) {
	return Announcements{}, true
}

func (repository *configuredIntentRepository) Transaction(ctx context.Context, operation func(Repository) error) error {
	return repository.Repository.(TransactionalRepository).Transaction(ctx, func(working Repository) error {
		repository.active = true
		defer func() { repository.active = false }()
		if repository.before != nil {
			if err := repository.before(working); err != nil {
				return err
			}
		}
		return operation(&configuredIntentView{Repository: working, intents: repository.intents})
	})
}

type configuredIntentView struct {
	Repository
	intents Announcements
}

func (view *configuredIntentView) TransactionAnnouncements() (Announcements, bool) {
	return view.intents, true
}

func (view *configuredIntentView) Transaction(ctx context.Context, operation func(Repository) error) error {
	return view.Repository.(TransactionalRepository).Transaction(ctx, func(working Repository) error {
		return operation(&configuredIntentView{Repository: working, intents: view.intents})
	})
}

func notificationSourceSnapshot(t *testing.T, repository *configuredIntentRepository) string {
	t.Helper()
	memory := repository.Repository.(*MemoryRepository)
	memory.mu.Lock()
	defer memory.mu.Unlock()
	document, err := json.Marshal(memory.stateLocked())
	if err != nil {
		t.Fatal(err)
	}
	return string(document)
}

func TestIntentFailureRollsBackEveryGovernanceProducer(t *testing.T) {
	for _, source := range []string{"plan", "generic", "human", "reopen", "grant"} {
		t.Run(source, func(t *testing.T) {
			repository := &configuredIntentRepository{Repository: NewMemoryRepository(), intents: (&recordedAnnouncements{}).all()}
			service, observers, author, reviewer := announcingGovernance(t, repository)
			var reopenedID string
			if source == "reopen" {
				_, request, err := service.ProposePlan(t.Context(), foundation(), author, "Initial review", "")
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := service.Reject(t.Context(), request.ID, reviewer, "review again"); err != nil {
					t.Fatal(err)
				}
				reopenedID = request.ID
			}
			failure := errors.New("private notification intent could not be recorded")
			intents := &recordedAnnouncements{err: failure}
			repository.intents = intents.all()
			before := notificationSourceSnapshot(t, repository)
			observerOpenings, observerGrants := len(observers.openings), len(observers.grants)
			var err error
			switch source {
			case "plan":
				_, _, err = service.ProposePlan(t.Context(), foundation(), author, "Deploy", "")
			case "generic":
				_, _, err = service.ProposeChanges(t.Context(), "PIPELINE", "nightly", "dev-use2", pipelineChange(), author, "Archive", "")
			case "human":
				_, err = service.ProposeHumanChange(t.Context(), "PIPELINE", "nightly", "dev-use2", pipelineChange(), author, "Archive", "")
			case "reopen":
				_, _, err = service.Reopen(t.Context(), reopenedID, author)
			case "grant":
				_, err = service.PutGrantAs(t.Context(), ResourceGrant{ResourceRID: environmentRID("dev-use2"), Team: "reviewers", Roles: []string{"environment:viewer"}}, "administrator")
			}
			if !errors.Is(err, failure) {
				t.Fatalf("%s ignored the private intent failure: %v", source, err)
			}
			if after := notificationSourceSnapshot(t, repository); after != before {
				t.Fatalf("%s committed source metadata without its intent", source)
			}
			if len(observers.openings) != observerOpenings || len(observers.grants) != observerGrants {
				t.Fatalf("rolled-back %s reached optional live observers", source)
			}
		})
	}
}

func TestConfiguredReopenPreservesOnlyItsExistingNonAuthorDenialAfterRollback(t *testing.T) {
	for _, auditUnavailable := range []bool{false, true} {
		t.Run(map[bool]string{false: "denial retained", true: "audit sink failure propagated"}[auditUnavailable], func(t *testing.T) {
			repository := &configuredIntentRepository{Repository: NewMemoryRepository(), intents: (&recordedAnnouncements{}).all()}
			service, observers, author, _ := announcingGovernance(t, repository)
			_, request, err := service.ProposePlan(t.Context(), foundation(), author, "Original review", "")
			if err != nil {
				t.Fatal(err)
			}
			beforePlans, err := repository.ListPlans(t.Context(), "")
			if err != nil {
				t.Fatal(err)
			}
			beforeRequests, err := repository.ListChangeRequests(t.Context(), "")
			if err != nil {
				t.Fatal(err)
			}
			beforeAudit, err := repository.ListAuthorizationAudit(t.Context(), "")
			if err != nil {
				t.Fatal(err)
			}
			intents := &recordedAnnouncements{}
			repository.intents = intents.all()
			observerCalls := len(observers.openings)
			auditFailure := errors.New("audit sink unavailable")
			if auditUnavailable {
				repository.auditErr = auditFailure
			}
			refused, plan, err := service.Reopen(t.Context(), request.ID, Principal{Username: "not-the-author"})
			if !errors.Is(err, ErrForbidden) || refused.ID != "" || plan.ID != "" || errors.Is(err, auditFailure) != auditUnavailable {
				t.Fatalf("refused reopen lost its authorization or audit error: %#v %#v %v", refused, plan, err)
			}
			afterPlans, err := repository.ListPlans(t.Context(), "")
			if err != nil || !reflect.DeepEqual(beforePlans, afterPlans) {
				t.Fatalf("refused reopen changed Plans: %#v %v", afterPlans, err)
			}
			afterRequests, err := repository.ListChangeRequests(t.Context(), "")
			if err != nil || !reflect.DeepEqual(beforeRequests, afterRequests) {
				t.Fatalf("refused reopen changed requests: %#v %v", afterRequests, err)
			}
			afterAudit, err := repository.ListAuthorizationAudit(t.Context(), "")
			if err != nil {
				t.Fatal(err)
			}
			oldIDs := map[string]bool{}
			for _, event := range beforeAudit {
				oldIDs[event.ID] = true
			}
			var denials []AuthorizationAuditEvent
			for _, event := range afterAudit {
				if !oldIDs[event.ID] {
					denials = append(denials, event)
				}
			}
			wantDenials := 1
			if auditUnavailable {
				wantDenials = 0
			}
			if len(denials) != wantDenials || len(afterAudit) != len(beforeAudit)+wantDenials {
				t.Fatalf("refused reopen did not retain exactly its one denied audit: %#v", denials)
			}
			if len(denials) == 1 {
				denial := denials[0]
				if denial.Action != "CHANGE_REQUEST_REOPEN" || denial.Decision != "DENY" || denial.Reason != "only the author may reopen" || denial.Subject != "not-the-author" || denial.ResourceRID != request.ID {
					t.Fatalf("refused reopen changed the existing denial vocabulary: %#v", denial)
				}
			}
			if len(intents.openings)+len(intents.grants)+len(intents.failures)+len(intents.promotions) != 0 || len(observers.openings) != observerCalls {
				t.Fatal("refused reopen emitted intent or reached a live observer")
			}
		})
	}
}

type sourceOpeningObserver func(context.Context, governanceports.ChangeRequestOpening) error

func (observer sourceOpeningObserver) AwaitingApproval(ctx context.Context, opening governanceports.ChangeRequestOpening) error {
	return observer(ctx, opening)
}

type sourceGrantObserver func(context.Context, governanceports.GrantChange) error

func (observer sourceGrantObserver) GrantChanged(ctx context.Context, change governanceports.GrantChange) error {
	return observer(ctx, change)
}

func TestConfiguredSourceCallsCustomObserversOnlyAfterItsCommit(t *testing.T) {
	intents := &recordedAnnouncements{}
	repository := &configuredIntentRepository{Repository: NewMemoryRepository(), intents: intents.all()}
	service, _, author, _ := announcingGovernance(t, repository)
	calls := 0
	service.SetAnnouncements(Announcements{ChangeRequestOpenings: sourceOpeningObserver(func(ctx context.Context, opening governanceports.ChangeRequestOpening) error {
		calls++
		if repository.active {
			t.Error("live observer ran inside the source transaction")
			return nil
		}
		stored, err := repository.GetChangeRequest(ctx, opening.ID)
		if err != nil || stored.Revision != opening.Revision {
			t.Errorf("observer did not see the final committed request: %#v %v", stored, err)
		}
		return errors.New("optional observer unavailable")
	})})
	request, _, err := service.ProposeChanges(t.Context(), "PIPELINE", "nightly", "dev-use2", pipelineChange(), author, "Archive", "")
	if err != nil || request.Status != ChangePendingApprovals || calls != 1 || len(intents.openings) != 1 {
		t.Fatalf("source intent and post-commit observer diverged: %#v %v calls=%d intents=%#v", request, err, calls, intents.openings)
	}
}

func TestConfiguredGrantObserverUsesTheActualTransactionBeforeImage(t *testing.T) {
	intents := &recordedAnnouncements{}
	repository := &configuredIntentRepository{Repository: NewMemoryRepository(), intents: intents.all()}
	service, _, _, _ := announcingGovernance(t, repository)
	initial, err := service.PutGrant(t.Context(), ResourceGrant{ResourceRID: environmentRID("dev-use2"), Team: "reviewers", Roles: []string{"environment:viewer"}})
	if err != nil {
		t.Fatal(err)
	}
	// Model another writer winning immediately before the source transaction.
	// The observer must describe revision two, rather than the old root read.
	repository.before = func(working Repository) error {
		intervening := initial
		intervening.Revision++
		intervening.Roles = []string{"environment:operator"}
		_, err := working.PutResourceGrant(t.Context(), intervening)
		return err
	}
	var observed *governanceports.GrantChange
	service.SetAnnouncements(Announcements{GrantChanges: sourceGrantObserver(func(_ context.Context, change governanceports.GrantChange) error {
		if repository.active {
			t.Error("grant observer ran before commit")
		}
		observed = &change
		return errors.New("optional observer unavailable")
	})})
	stored, err := service.PutGrantAs(t.Context(), ResourceGrant{ResourceRID: initial.ResourceRID, Team: initial.Team, Roles: []string{"environment:viewer"}, Revision: 2}, "administrator")
	if err != nil || stored.Revision != 3 || observed == nil || observed.Before == nil || observed.Before.Revision != 2 || observed.Before.Roles[0] != "environment:operator" || len(intents.grants) != 1 || !reflect.DeepEqual(intents.grants[0], *observed) {
		t.Fatalf("observer described a stale grant: stored=%#v err=%v observed=%#v intents=%#v", stored, err, observed, intents.grants)
	}
}

func TestNotificationScopePreservesServiceSettingsWithoutChangingTheRoot(t *testing.T) {
	intents := &recordedAnnouncements{}
	repository := &configuredIntentRepository{Repository: NewMemoryRepository(), intents: intents.all()}
	service := NewGovernanceService(repository)
	service.trust = PolicyTrust{ModuleService: "trusted-module", ImportService: "trusted-import"}
	serviceTime := time.Date(2026, time.November, 17, 10, 0, 0, 0, time.UTC)
	lifecycleTime := serviceTime.Add(time.Hour)
	service.now = func() time.Time { return serviceTime }
	service.lifecycle.now = func() time.Time { return lifecycleTime }
	service.lifecycle.newToken = func() (string, error) { return "caller-token-source", nil }
	originalLifecycle := service.lifecycle
	if err := service.withNotificationTransaction(t.Context(), func(working *GovernanceService) error {
		token, err := working.lifecycle.newToken()
		if err != nil || token != "caller-token-source" || !working.now().Equal(serviceTime) || !working.lifecycle.now().Equal(lifecycleTime) || !reflect.DeepEqual(working.trust, service.trust) || working.repository == repository || working.lifecycle.repository != working.repository || working.announcements.openings != intents {
			t.Fatal("transaction scope lost caller settings or its private announcement ports")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if service.repository != repository || service.lifecycle != originalLifecycle || service.lifecycle.repository != repository || service.announcements.openings != nil {
		t.Fatal("transaction scope mutated the root service")
	}
}

type sourceRecallObserver func(context.Context, string, []catalog.Recall) error

func (observer sourceRecallObserver) RecallsInForce(ctx context.Context, environment string, recalls []catalog.Recall) error {
	return observer(ctx, environment, recalls)
}

func TestAcceptedRecallIntentFailureRollsBackItsSourcePlan(t *testing.T) {
	intents := (&recordedAnnouncements{}).all()
	failure := errors.New("recall intent failed")
	repository := &configuredIntentRepository{Repository: NewMemoryRepository(), intents: intents}
	calls := 0
	repository.intents.RecallOpenings = sourceRecallObserver(func(_ context.Context, environment string, recalls []catalog.Recall) error {
		calls++
		if !repository.active || environment != "dev-use2" || len(recalls) != 1 || recalls[0].ID != "recall" {
			t.Error("recall intent did not describe its transaction's accepted state")
		}
		return failure
	})
	service, observers, author, _ := announcingGovernance(t, repository)
	state := foundation()
	state.Recalls = []Recall{{ID: "recall", Product: state.Releases[0].Product, StartingVersion: "9.0.0", SingleVersion: true, Strategy: RecallStrategy{Type: RecallRollForward}, Reason: "withdrawn"}}
	before := notificationSourceSnapshot(t, repository)
	if _, _, err := service.ProposePlan(t.Context(), state, author, "Deploy", ""); !errors.Is(err, failure) {
		t.Fatalf("recall intent failure was ignored: %v", err)
	}
	if calls != 1 || before != notificationSourceSnapshot(t, repository) || len(observers.openings) != 0 {
		t.Fatal("source plan and recall intent were not atomic")
	}
}

func TestPromotionIntentFailureRollsBackEvaluationAndSuppressions(t *testing.T) {
	failure := errors.New("promotion intent failed")
	intents := &recordedAnnouncements{err: failure}
	repository := &configuredIntentRepository{Repository: promotionRepository(t), intents: intents.all()}
	lifecycle := NewLifecycle(repository)
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	lifecycle.now = func() time.Time { return now }
	request := promotionRequest(now)
	request.Evidence[0].Health = "UNHEALTHY"
	before := notificationSourceSnapshot(t, repository)
	if _, err := lifecycle.EvaluatePromotion(t.Context(), request); !errors.Is(err, failure) {
		t.Fatalf("promotion intent failure was ignored: %v", err)
	}
	if len(intents.promotions) != 1 || before != notificationSourceSnapshot(t, repository) {
		t.Fatal("evaluation or suppressions committed without their notification intent")
	}
}
