package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestCancellationEndpointCreatesSuppressionAndSignalsAgent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	record, err := lifecycle.CreatePlan(ctx, foundation())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.ApprovePlan(ctx, record.ID, "reviewer", "approved"); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureAgentIdentityHeaders:    true,
		AllowInsecureOperatorIdentityHeaders: true,
	})

	leaseRequest := httptest.NewRequest(
		http.MethodPost, "/v1/environments/dev-use2/leases", bytes.NewBufferString(`{"durationSeconds":60}`),
	)
	leaseRequest.Header.Set("X-Platform-Environment-ID", "dev-use2")
	leaseRequest.Header.Set("X-Platform-Agent-ID", "agent-dev")
	leaseResponse := httptest.NewRecorder()
	handler.ServeHTTP(leaseResponse, leaseRequest)
	if leaseResponse.Code != http.StatusOK {
		t.Fatalf("lease status %d: %s", leaseResponse.Code, leaseResponse.Body.String())
	}
	if err := json.Unmarshal(leaseResponse.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}

	startRequest := httptest.NewRequest(http.MethodPost, "/v1/plans/"+record.ID+"/start", nil)
	setAgentLeaseHeaders(startRequest, "dev-use2", "agent-dev", record.Lease.Token)
	startResponse := httptest.NewRecorder()
	handler.ServeHTTP(startResponse, startRequest)
	if startResponse.Code != http.StatusOK {
		t.Fatalf("start status %d: %s", startResponse.Code, startResponse.Body.String())
	}

	cancelRequest := httptest.NewRequest(
		http.MethodPost, "/v1/plans/"+record.ID+"/cancel",
		bytes.NewBufferString(`{"reason":"incident containment","createSuppression":true,"installationId":"example-api"}`),
	)
	cancelRequest.Header.Set("X-Platform-Subject", "operator")
	cancelResponse := httptest.NewRecorder()
	handler.ServeHTTP(cancelResponse, cancelRequest)
	if cancelResponse.Code != http.StatusOK {
		t.Fatalf("cancel status %d: %s", cancelResponse.Code, cancelResponse.Body.String())
	}

	heartbeatRequest := httptest.NewRequest(
		http.MethodPost, "/v1/plans/"+record.ID+"/heartbeat", bytes.NewBufferString(`{"durationSeconds":60}`),
	)
	setAgentLeaseHeaders(heartbeatRequest, "dev-use2", "agent-dev", record.Lease.Token)
	heartbeatResponse := httptest.NewRecorder()
	handler.ServeHTTP(heartbeatResponse, heartbeatRequest)
	if heartbeatResponse.Code != http.StatusGone {
		t.Fatalf("cancelled heartbeat status %d: %s", heartbeatResponse.Code, heartbeatResponse.Body.String())
	}

	suppressions, err := repository.ListSuppressions(ctx, "dev-use2")
	if err != nil {
		t.Fatal(err)
	}
	if len(suppressions) != 1 || suppressions[0].Source != SuppressionManual ||
		suppressions[0].InstallationID != "example-api" || suppressions[0].EndsAt == nil {
		t.Fatalf("cancellation suppression = %#v", suppressions)
	}
}

func setAgentLeaseHeaders(request *http.Request, environment, agentID, token string) {
	request.Header.Set("X-Platform-Environment-ID", environment)
	request.Header.Set("X-Platform-Agent-ID", agentID)
	request.Header.Set("X-Platform-Lease-Token", token)
}

func TestManualSuppressionBlocksLeaseUntilRemoved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	lifecycle.now = func() time.Time { return now }

	record, err := lifecycle.CreatePlan(ctx, foundation())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lifecycle.ApprovePlan(ctx, record.ID, "reviewer", "approved"); err != nil {
		t.Fatal(err)
	}
	suppression, err := lifecycle.CreateManualSuppression(
		ctx, "dev-use2", "example-api", "incident containment", "operator", now, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.LeaseNextPlan(ctx, "dev-use2", "agent", time.Minute); !errors.Is(err, ErrNoPlanAvailable) {
		t.Fatalf("suppressed lease error = %v", err)
	}
	if _, err := lifecycle.RemoveSuppression(ctx, suppression.ID, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.LeaseNextPlan(ctx, "dev-use2", "agent", time.Minute); err != nil {
		t.Fatalf("lease after removal: %v", err)
	}
}

// A suppression window can open between the eligibility scan and the write that
// actually hands the Plan out. The scan's clock is already stale by then, so the
// suppression has to be re-evaluated against the write's clock the way the
// maintenance window is -- suppression is the operator's stop control and has
// more claim to that than either of the checks that already do it.
func TestSuppressionOpeningDuringTheLeaseWriteStillBlocksIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)

	base := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	// The eligibility scan reads the clock first; every later read is past the
	// suppression's start, which is what the atomic lease write observes.
	reads := 0
	lifecycle.now = func() time.Time {
		reads++
		if reads <= 1 {
			return base
		}
		return base.Add(2 * time.Minute)
	}

	record, err := lifecycle.CreatePlan(ctx, foundation())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lifecycle.ApprovePlan(ctx, record.ID, "reviewer", "approved"); err != nil {
		t.Fatal(err)
	}
	if _, err = lifecycle.CreateManualSuppression(
		ctx, "dev-use2", "", "freeze", "operator", base.Add(time.Minute), nil,
	); err != nil {
		t.Fatal(err)
	}

	reads = 0
	leased, leaseErr := lifecycle.LeaseNextPlan(ctx, "dev-use2", "agent", time.Minute)
	if leaseErr == nil {
		t.Fatalf("leased %s though the suppression was active when the lease was written", leased.ID)
	}
	if !errors.Is(leaseErr, ErrNoPlanAvailable) {
		t.Fatalf("lease error = %v", leaseErr)
	}
}

func TestFailedPlanCreatesAutomaticEntitySuppression(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	lifecycle.now = func() time.Time { return now }
	lifecycle.newToken = func() (string, error) { return "lease-token", nil }

	record, _ := lifecycle.CreatePlan(ctx, foundation())
	record, _ = lifecycle.ApprovePlan(ctx, record.ID, "reviewer", "approved")
	record, _ = lifecycle.LeaseNextPlan(ctx, "dev-use2", "agent", time.Minute)
	record, _ = lifecycle.StartPlan(ctx, record.ID, record.Lease.Token)
	failed, err := lifecycle.FinishPlan(
		ctx, record.ID, record.Lease.Token, false, "chart rollout failed", "example-api", failedPlanExecution(record, "example-api"), &EnvironmentReport{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != PlanFailed || failed.FailedInstallation != "example-api" {
		t.Fatalf("failed record = %#v", failed)
	}
	suppressions, err := repository.ListSuppressions(ctx, "dev-use2")
	if err != nil {
		t.Fatal(err)
	}
	if len(suppressions) != 1 || suppressions[0].Source != SuppressionPlanFailure ||
		suppressions[0].InstallationID != "example-api" {
		t.Fatalf("automatic suppressions = %#v", suppressions)
	}

	next := foundation()
	next.Environment.Generation = "next-generation"
	nextRecord, _ := lifecycle.CreatePlan(ctx, next)
	_, _ = lifecycle.ApprovePlan(ctx, nextRecord.ID, "reviewer", "approved")
	if _, err := lifecycle.LeaseNextPlan(ctx, "dev-use2", "agent", time.Minute); !errors.Is(err, ErrNoPlanAvailable) {
		t.Fatalf("automatically suppressed lease error = %v", err)
	}
}

func TestCancellationIsVisibleToLeasedAgent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	lifecycle.newToken = func() (string, error) { return "lease-token", nil }
	record, _ := lifecycle.CreatePlan(ctx, foundation())
	record, _ = lifecycle.ApprovePlan(ctx, record.ID, "reviewer", "approved")
	record, _ = lifecycle.LeaseNextPlan(ctx, "dev-use2", "agent", time.Minute)
	record, _ = lifecycle.StartPlan(ctx, record.ID, record.Lease.Token)

	cancelled, err := lifecycle.CancelPlan(ctx, record.ID, "operator", "stop rollout")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != PlanCancelled || cancelled.Cancellation == nil {
		t.Fatalf("cancelled record = %#v", cancelled)
	}
	if _, err := lifecycle.RenewLease(ctx, record.ID, record.Lease.Token, time.Minute); !errors.Is(err, ErrPlanCancelled) {
		t.Fatalf("renew cancelled Plan error = %v", err)
	}
}

func TestAutomaticEnvironmentEscalationTracksThreshold(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	lifecycle.now = func() time.Time { return now }
	plan := Plan{Environment: "dev-use2", AutoSuppressionThreshold: 1}

	if err := lifecycle.createPlanFailureSuppression(ctx, plan, "pln_one", "entity-one", "failure one"); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.createPlanFailureSuppression(ctx, plan, "pln_two", "entity-two", "failure two"); err != nil {
		t.Fatal(err)
	}
	records, err := repository.ListSuppressions(ctx, "dev-use2")
	if err != nil {
		t.Fatal(err)
	}
	var entitySuppression, escalation *SuppressionRecord
	for index := range records {
		record := records[index]
		if record.InstallationID == "entity-one" {
			copy := record
			entitySuppression = &copy
		}
		if record.Source == SuppressionEnvironmentEscalate && record.Active(now) {
			copy := record
			escalation = &copy
		}
	}
	if entitySuppression == nil || escalation == nil {
		t.Fatalf("missing entity or Environment escalation suppression: %#v", records)
	}
	if _, err := lifecycle.RemoveSuppression(ctx, entitySuppression.ID, "operator"); err != nil {
		t.Fatal(err)
	}
	updated, err := repository.GetSuppression(ctx, escalation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.RemovedAt == nil || updated.RemovedBy != "platform-delivery" {
		t.Fatalf("Environment escalation was not automatically removed: %#v", updated)
	}
}

func TestSuppressionPersistsAcrossFileRepositoryRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "delivery-state")
	repository, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := NewLifecycle(repository)
	record, err := lifecycle.CreateManualSuppression(
		ctx, "dev-use2", "example-api", "maintenance freeze", "operator", time.Time{}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := reopened.GetSuppression(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Revision != 1 || persisted.Reason != "maintenance freeze" || !persisted.Active(time.Now().UTC()) {
		t.Fatalf("persisted suppression = %#v", persisted)
	}
}
