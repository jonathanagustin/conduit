package controlplane

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

func TestPostgresRepositoryMultipleReplicaTransactions(t *testing.T) {
	dataSourceName := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dataSourceName == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is required for the PostgreSQL integration gate")
	}
	ctx := context.Background()
	first, err := NewPostgresRepository(ctx, dataSourceName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	if _, err := first.database.ExecContext(ctx, `
TRUNCATE conduit_repository_blobs;
UPDATE conduit_repository_state SET generation = 0, state = '{}'::jsonb WHERE singleton = TRUE`); err != nil {
		t.Fatal(err)
	}
	second, err := NewPostgresRepository(ctx, dataSourceName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })

	firstGovernance := NewGovernanceService(first)
	if _, err := firstGovernance.PutTeam(ctx, Team{
		Name: "release-managers", DisplayName: "Release managers", AdminUsers: []string{"admin@example.test"},
		MemberGroups: []string{"release-reviewers"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := firstGovernance.PutPolicy(ctx, ApprovalPolicy{
		ID: "release-review", DisplayName: "Release review", Enabled: true,
		Expression: `type == "CREATE_ENTITY" ? require_team("release-managers", "release review") : no_op()`,
	}); err != nil {
		t.Fatal(err)
	}
	author, err := firstGovernance.ResolvePrincipal(ctx, IdentityClaims{Username: "author@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	_, request, err := firstGovernance.ProposePlan(ctx, foundation(), author, "Transactional rollout", "")
	if err != nil {
		t.Fatal(err)
	}

	services := []*GovernanceService{firstGovernance, NewGovernanceService(second)}
	start := make(chan struct{})
	approvalErrors := make([]error, len(services))
	var wait sync.WaitGroup
	for index, service := range services {
		wait.Add(1)
		go func(index int, service *GovernanceService) {
			defer wait.Done()
			<-start
			reviewer, resolveErr := service.ResolvePrincipal(ctx, IdentityClaims{
				Username: "reviewer@example.test", Groups: []string{"release-reviewers"},
			})
			if resolveErr != nil {
				approvalErrors[index] = resolveErr
				return
			}
			_, _, approvalErrors[index] = service.Approve(ctx, request.ID, reviewer, "verified rollout")
		}(index, service)
	}
	close(start)
	wait.Wait()
	if successes := nilErrors(approvalErrors); successes != 1 {
		t.Fatalf("concurrent approvals succeeded %d times; errors=%v", successes, approvalErrors)
	}
	storedRequest, err := second.GetChangeRequest(ctx, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedRequest.Status != ChangeApproved || len(storedRequest.Reviews) != 1 {
		t.Fatalf("stored request after concurrent approval = %#v", storedRequest)
	}

	plan, err := second.GetPlan(ctx, storedRequest.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	lifecycles := []*Lifecycle{NewLifecycle(first), NewLifecycle(second)}
	start = make(chan struct{})
	leaseErrors := make([]error, len(lifecycles))
	wait = sync.WaitGroup{}
	for index, lifecycle := range lifecycles {
		wait.Add(1)
		go func(index int, lifecycle *Lifecycle) {
			defer wait.Done()
			<-start
			_, leaseErrors[index] = lifecycle.LeaseNextPlan(ctx, plan.Plan.Environment, "agent-"+string(rune('a'+index)), time.Minute)
		}(index, lifecycle)
	}
	close(start)
	wait.Wait()
	if successes := nilErrors(leaseErrors); successes != 1 {
		t.Fatalf("concurrent leases succeeded %d times; errors=%v", successes, leaseErrors)
	}
	leased, err := first.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if leased.Status != PlanLeased || leased.Lease == nil || leased.Lease.Attempt != 1 {
		t.Fatalf("leased Plan = %#v", leased)
	}

	idempotent := ChangeRequest{
		ID: "chg_idempotent", PlanID: "pln_same", Status: ChangePendingApprovals, Revision: 1,
	}
	if _, err := first.CreateChangeRequest(ctx, idempotent); err != nil {
		t.Fatal(err)
	}
	if stored, err := second.CreateChangeRequest(ctx, idempotent); err != nil || stored.ID != idempotent.ID {
		t.Fatalf("idempotent create = %#v, %v", stored, err)
	}

	rollback := errors.New("simulated process loss before commit")
	if err := first.Transaction(ctx, func(transaction Repository) error {
		if _, err := transaction.UpdatePlan(ctx, leased.ID, func(record *PlanRecord) error {
			record.Status = PlanFailed
			record.Failure = "must not become visible"
			return nil
		}); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatalf("aborted transaction error = %v", err)
	}
	afterRollback, err := second.GetPlan(ctx, leased.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterRollback.Status != PlanLeased || afterRollback.Failure != "" {
		t.Fatalf("aborted transaction became visible: %#v", afterRollback)
	}
}

func TestPostgresApprovalAuditFailureRollsBackMutation(t *testing.T) {
	dataSourceName := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dataSourceName == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is required for the PostgreSQL integration gate")
	}
	ctx := context.Background()
	repository, err := NewPostgresRepository(ctx, dataSourceName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if _, err := repository.database.ExecContext(ctx, `
TRUNCATE conduit_repository_blobs;
UPDATE conduit_repository_state SET generation = 0, state = '{}'::jsonb WHERE singleton = TRUE`); err != nil {
		t.Fatal(err)
	}
	service := NewGovernanceService(repository)
	if _, err := service.PutTeam(ctx, Team{
		Name: "release-managers", DisplayName: "Release managers", AdminUsers: []string{"admin@example.test"},
		MemberGroups: []string{"release-reviewers"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutPolicy(ctx, ApprovalPolicy{
		ID: "release-review", DisplayName: "Release review", Enabled: true,
		Expression: `type == "CREATE_ENTITY" ? require_team("release-managers", "release review") : no_op()`,
	}); err != nil {
		t.Fatal(err)
	}
	author, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author@example.test"})
	_, request, err := service.ProposePlan(ctx, foundation(), author, "Audit failure rollback", "")
	if err != nil {
		t.Fatal(err)
	}
	repository.transactionView = func(base Repository) Repository {
		return auditFailureRepository{Repository: base}
	}
	reviewer, _ := service.ResolvePrincipal(ctx, IdentityClaims{
		Username: "reviewer@example.test", Groups: []string{"release-reviewers"},
	})
	if _, _, err := service.Approve(ctx, request.ID, reviewer, "verified rollout"); err == nil {
		t.Fatal("approval succeeded despite audit persistence failure")
	}
	repository.transactionView = nil
	stored, err := repository.GetChangeRequest(ctx, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != ChangePendingApprovals || len(stored.Reviews) != 0 {
		t.Fatalf("approval escaped failed transaction: %#v", stored)
	}
	plan, err := repository.GetPlan(ctx, stored.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != PlanAwaitingApproval || len(plan.Approvals) != 0 {
		t.Fatalf("Plan escaped failed transaction: %#v", plan)
	}
}

type auditFailureRepository struct {
	Repository
}

func (auditFailureRepository) AppendAuthorizationAudit(
	context.Context, AuthorizationAuditEvent,
) (AuthorizationAuditEvent, error) {
	return AuthorizationAuditEvent{}, errors.New("injected audit persistence failure")
}

func nilErrors(values []error) int {
	count := 0
	for _, value := range values {
		if value == nil {
			count++
		}
	}
	return count
}
