package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProductScopedCommandUsesItsFinalExecutionBudget(t *testing.T) {
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	state := foundation()
	state.Releases = state.Releases[:1]
	state.Releases[0].Dependencies = nil
	state.Entities = nil
	for index := 0; index < 171; index++ {
		state.Entities = append(state.Entities, Entity{
			Name: fmt.Sprintf("api-%03d", index), Product: state.Releases[0].Product,
			Version: state.Releases[0].Version, Namespace: fmt.Sprintf("example-%03d", index),
		})
	}
	command, err := lifecycle.IssueCommand(context.Background(), IssueCommandRequest{
		DesiredState: state, Type: CommandInstall, InstallationID: "api-000", Rationale: "recover one Entity",
	}, "operator")
	if err != nil {
		t.Fatalf("product-scoped command inherited the full Environment task budget: %v", err)
	}
	if len(command.Plan.Steps) != 1 || command.Plan.Steps[0].InstallationID != "api-000" || planExecutionTaskUpperBound(command.Plan) != 5 {
		t.Fatalf("command execution envelope = %#v", command.Plan)
	}
}

func TestRetargetedProductScopedCommandUsesItsFinalExecutionBudget(t *testing.T) {
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	state := foundation()
	state.Releases = state.Releases[:1]
	state.Releases[0].Dependencies = nil
	next := cloneJSON(state.Releases[0])
	next.Version = "0.2.0"
	next.Chart.Tag = next.Version
	for index := range next.Images {
		next.Images[index].Tag = next.Version
	}
	state.Releases = append(state.Releases, next)
	state.Entities = nil
	for index := 0; index < 171; index++ {
		state.Entities = append(state.Entities, Entity{
			Name: fmt.Sprintf("api-%03d", index), Product: state.Releases[0].Product,
			Version: state.Releases[0].Version, Namespace: fmt.Sprintf("example-%03d", index),
		})
	}
	command, err := lifecycle.IssueCommand(context.Background(), IssueCommandRequest{
		DesiredState: state, Type: CommandInstall, InstallationID: "api-000", TargetVersion: "0.2.0",
		Rationale: "recover one Entity at the corrected release",
	}, "operator")
	if err != nil {
		t.Fatalf("retargeted command inherited the full Environment task budget: %v", err)
	}
	if len(command.Plan.Steps) != 1 || command.Plan.Steps[0].Version != "0.2.0" {
		t.Fatalf("retargeted command = %#v", command.Plan)
	}
}

func TestCommandHTTPCreateAndHistory(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	state := foundation()
	_, _ = repository.PutEnvironmentReport(context.Background(), installedReport(t, state))
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"operator"},
	})
	payload, err := json.Marshal(IssueCommandRequest{
		DesiredState: state, Type: CommandEnforceConfig, InstallationID: "example-api", Rationale: "incident repair",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/commands", bytes.NewReader(payload))
	request.Header.Set("X-Platform-Subject", "operator")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create command status %d: %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/v1/environments/dev-use2/commands", nil)
	request.Header.Set("X-Platform-Subject", "operator")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("command history status %d: %s", response.Code, response.Body.String())
	}
	var history []PlanRecord
	if err := json.Unmarshal(response.Body.Bytes(), &history); err != nil || len(history) != 1 || history[0].Command == nil {
		t.Fatalf("command history = %#v, %v", history, err)
	}
}

func installedReport(t *testing.T, state DesiredState) EnvironmentReport {
	t.Helper()
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	report := EnvironmentReport{Environment: state.Environment.ID, AgentID: "agent", ObservedAt: time.Now().UTC()}
	for _, step := range plan.Steps {
		report.Entities = append(report.Entities, ReportedEntity{
			InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
			ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
			Health: HealthHealthy, RolloutStatus: RolloutUpToDate, ConfigStatus: ConfigApplied,
			Lifecycle: LifecycleInstalled, ObservedAt: report.ObservedAt,
		})
	}
	return report
}

func TestEntityCommandPriorityConstraintsAndSingleFlight(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	lifecycle.now = func() time.Time { return now }
	lifecycle.newToken = func() (string, error) { return "token", nil }
	state := foundation()
	if _, err := repository.PutEnvironmentReport(ctx, installedReport(t, state)); err != nil {
		t.Fatal(err)
	}
	normal, err := lifecycle.CreatePlan(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.ApprovePlan(ctx, normal.ID, "reviewer", "routine rollout"); err != nil {
		t.Fatal(err)
	}
	endsAt := now.Add(time.Hour)
	if _, err := lifecycle.CreateManualSuppression(ctx, state.Environment.ID, "example-api", "incident", "operator", now, &endsAt); err != nil {
		t.Fatal(err)
	}
	command, err := lifecycle.IssueCommand(ctx, IssueCommandRequest{
		DesiredState: state, Type: CommandEnforceConfig, InstallationID: "example-api", Rationale: "restore service",
	}, "incident-commander")
	if err != nil {
		t.Fatal(err)
	}
	if command.Status != PlanApproved || command.RequiredApprovals != 0 || command.Command == nil || len(command.Plan.Steps) != 1 {
		t.Fatalf("command record = %#v", command)
	}
	if _, err := lifecycle.IssueCommand(ctx, IssueCommandRequest{
		DesiredState: state, Type: CommandEnforceConfig, InstallationID: "example-api", Rationale: "duplicate",
	}, "operator"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate command error = %v", err)
	}
	leased, err := lifecycle.LeaseNextPlan(ctx, state.Environment.ID, "agent", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if leased.ID != command.ID {
		t.Fatalf("leased %s before command %s", leased.ID, command.ID)
	}
	if _, err := lifecycle.LeaseNextPlan(ctx, state.Environment.ID, "agent", time.Minute); !errors.Is(err, ErrNoPlanAvailable) {
		t.Fatalf("overlapping routine Plan leased during command: %v", err)
	}
}

func TestUninstallCommandFinishesAndPreservesOtherReportedEntities(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	lifecycle.newToken = func() (string, error) { return "token", nil }
	state := foundation()
	original := installedReport(t, state)
	if _, err := repository.PutEnvironmentReport(ctx, original); err != nil {
		t.Fatal(err)
	}
	command, err := lifecycle.IssueCommand(ctx, IssueCommandRequest{
		DesiredState: state, Type: CommandUninstall, InstallationID: "example-api", Rationale: "emergency removal",
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if command.Plan.Steps[0].Action != "uninstall" {
		t.Fatalf("uninstall action = %q", command.Plan.Steps[0].Action)
	}
	command, err = lifecycle.LeaseNextPlan(ctx, state.Environment.ID, "agent", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	command, err = lifecycle.StartPlan(ctx, command.ID, command.Lease.Token)
	if err != nil {
		t.Fatal(err)
	}
	step := command.Plan.Steps[0]
	_, err = lifecycle.FinishPlan(ctx, command.ID, command.Lease.Token, true, "", "", successfulPlanExecution(command), &EnvironmentReport{Entities: []ReportedEntity{{
		InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
		ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
		Health: HealthUnknown, RolloutStatus: RolloutUpToDate, ConfigStatus: ConfigApplied, Lifecycle: LifecycleUninstalled,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := repository.GetEnvironmentReport(ctx, state.Environment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Entities) != 2 {
		t.Fatalf("command replaced the Environment report: %#v", merged.Entities)
	}
	for _, entity := range merged.Entities {
		if entity.InstallationID == "example-api" && entity.Lifecycle != LifecycleUninstalled {
			t.Fatalf("uninstall observation = %#v", entity)
		}
	}
}

func TestRestartCommandValidatesResourceTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	state := foundation()
	_, _ = repository.PutEnvironmentReport(ctx, installedReport(t, state))
	command, err := lifecycle.IssueCommand(ctx, IssueCommandRequest{
		DesiredState: state, Type: CommandRestartResource, InstallationID: "example-api",
		ResourceKind: "Deployment", ResourceName: "example-api", Rationale: "replace unhealthy pods",
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	step := command.Plan.Steps[0]
	if step.Action != "restart" || step.ResourceKind != "Deployment" || step.ResourceName != "example-api" {
		t.Fatalf("restart step = %#v", step)
	}
}

func TestCommandWaitsForAnExecutingOverlappingPlan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	lifecycle.newToken = func() (string, error) { return "token", nil }
	state := foundation()
	_, _ = repository.PutEnvironmentReport(ctx, installedReport(t, state))
	normal, _ := lifecycle.CreatePlan(ctx, state)
	normal, _ = lifecycle.ApprovePlan(ctx, normal.ID, "reviewer", "approved")
	normal, _ = lifecycle.LeaseNextPlan(ctx, state.Environment.ID, "agent", time.Minute)
	command, err := lifecycle.IssueCommand(ctx, IssueCommandRequest{
		DesiredState: state, Type: CommandEnforceConfig, InstallationID: "example-api", Rationale: "urgent",
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.LeaseNextPlan(ctx, state.Environment.ID, "agent", time.Minute); !errors.Is(err, ErrNoPlanAvailable) {
		t.Fatalf("command overlapped leased Plan: %v", err)
	}
	if _, err := lifecycle.CancelPlan(ctx, normal.ID, "operator", "make way for command"); err != nil {
		t.Fatal(err)
	}
	leased, err := lifecycle.LeaseNextPlan(ctx, state.Environment.ID, "agent", time.Minute)
	if err != nil || leased.ID != command.ID {
		t.Fatalf("command after cancellation = %#v, %v", leased, err)
	}
}

func TestCommandTargetVersionBypassesRecallSelection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	state := foundation()
	state.Releases = append(state.Releases, Release{
		Product: "app.example:example-api", Version: "0.2.0",
		Chart:  Artifact{Repository: "registry.example/example-api", Tag: "0.2.0", Digest: digest},
		Images: []Artifact{{Repository: "registry.example/example-api", Tag: "0.2.0", Digest: digest}},
	})
	state.Recalls = []Recall{{ID: "recalled-target", Product: "app.example:example-api", StartingVersion: "0.2.0", SingleVersion: true,
		Strategy: RecallStrategy{Type: RecallRollForward}, Reason: "known incident"}}
	_, _ = repository.PutEnvironmentReport(ctx, installedReport(t, state))
	command, err := lifecycle.IssueCommand(ctx, IssueCommandRequest{
		DesiredState: state, Type: CommandEnforceConfigAndVersion, InstallationID: "example-api",
		TargetVersion: "0.2.0", Rationale: "explicit incident override",
	}, "incident-commander")
	if err != nil {
		t.Fatal(err)
	}
	if command.Plan.Steps[0].Version != "0.2.0" || command.Command.TargetVersion != "0.2.0" || len(command.DesiredState.Recalls) != 0 {
		t.Fatalf("forced recalled command = %#v", command)
	}
}

func TestCommandCannotDeploySecurityRecalledRelease(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	state := foundation()
	state.Releases = append(state.Releases, Release{
		Product: "app.example:example-api", Version: "0.2.0",
		Chart:  Artifact{Repository: "registry.example/example-api", Tag: "0.2.0", Digest: digest},
		Images: []Artifact{{Repository: "registry.example/example-api", Tag: "0.2.0", Digest: digest}},
	})
	state.Recalls = []Recall{{
		ID: "security-recalled-target", Product: "app.example:example-api", StartingVersion: "0.2.0", SingleVersion: true,
		Strategy: RecallStrategy{Type: RecallRollForward}, Reason: "vulnerable release", Source: RecallSourceSecurity,
		AssessmentRunID: "scan-incident-1",
	}}
	_, _ = repository.PutEnvironmentReport(ctx, installedReport(t, state))
	_, err := lifecycle.IssueCommand(ctx, IssueCommandRequest{
		DesiredState: state, Type: CommandEnforceConfigAndVersion, InstallationID: "example-api",
		TargetVersion: "0.2.0", Rationale: "attempt recalled deployment",
	}, "incident-commander")
	if err == nil || !strings.Contains(err.Error(), "security-recalled Product Release") {
		t.Fatalf("security-recalled command error = %v", err)
	}
}

func TestInstallCommandRequiresAnAbsentEntity(t *testing.T) {
	t.Parallel()
	lifecycle := NewLifecycle(NewMemoryRepository())
	command, err := lifecycle.IssueCommand(context.Background(), IssueCommandRequest{
		DesiredState: foundation(), Type: CommandInstall, InstallationID: "example-api", Rationale: "complete pending install",
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if command.Plan.Steps[0].Action != "reconcile" || command.Command.Type != CommandInstall {
		t.Fatalf("install command = %#v", command)
	}
}
