package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

func TestOptimizerProposalUsesGovernanceAndFreshLastSafePointEvidence(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	if _, err := service.PutTeam(ctx, Team{
		Name: "release-managers", DisplayName: "Release managers", AdminGroups: []string{"release-admins"}, MemberGroups: []string{"release-reviewers"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutPolicy(ctx, ApprovalPolicy{
		ID: "release-review", DisplayName: "Release review", Enabled: true,
		Expression: `type == "CREATE_ENTITY" ? require_team("release-managers", "release review") : no_op()`,
	}); err != nil {
		t.Fatal(err)
	}

	proposal, requirements, baselineFacts, baseline := governedOptimizerFixture(t)
	author, err := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	plan, request, err := service.ProposeOptimizerPlan(ctx, proposal, author, "Optimizer shadow proposal", "review exact evidence")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Optimizer == nil || plan.Optimizer.ProposalID != proposal.ID || plan.Optimizer.SnapshotID != proposal.Snapshot.ID || plan.Status != PlanAwaitingApproval {
		t.Fatalf("optimizer Plan provenance/status = %#v", plan)
	}
	if request.Status != ChangePendingApprovals || len(request.Requirements) != len(plan.Plan.Steps)+1 || request.Requirements[len(request.Requirements)-1].Kind != "OPTIMIZER_REVALIDATION" {
		t.Fatalf("optimizer Change Request = %#v", request)
	}
	for _, change := range request.Changes {
		if change.Metadata["source"] != "optimizer" || change.Metadata["optimizerProposalId"] != proposal.ID {
			t.Fatalf("optimizer review metadata = %#v", change.Metadata)
		}
	}
	reviewer, err := service.ResolvePrincipal(ctx, IdentityClaims{Username: "reviewer@example.test", Groups: []string{"release-reviewers"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Approve(ctx, request.ID, reviewer, "generic approval must fail"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("generic optimizer approval error = %v", err)
	}
	if _, err := service.lifecycle.ApprovePlan(ctx, plan.ID, reviewer.Username, "direct approval must fail"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("direct optimizer approval error = %v", err)
	}

	approvalKnownAt := baseline.Add(6 * time.Second)
	drifted, driftAcquisition := authoritativeOptimizerRevalidation(t, proposal, optimization.ProposalRevalidationApproval, approvalKnownAt, true)
	service.now = func() time.Time { return approvalKnownAt.Add(time.Second) }
	if _, _, err := service.ApproveOptimizerProposal(ctx, request.ID, proposal, drifted, driftAcquisition, reviewer, "drifted evidence"); err == nil {
		t.Fatal("drifted optimizer evidence approved a Plan")
	}

	approval, approvalAcquisition := authoritativeOptimizerRevalidation(t, proposal, optimization.ProposalRevalidationApproval, approvalKnownAt, false)
	service.now = func() time.Time { return approvalKnownAt.Add(optimizerRevalidationMaximumAge + time.Second) }
	if _, _, err := service.ApproveOptimizerProposal(ctx, request.ID, proposal, approval, approvalAcquisition, reviewer, "stale evidence"); err == nil {
		t.Fatal("stale optimizer evidence approved a Plan")
	}
	approvalClockCalls := 0
	service.now = func() time.Time {
		approvalClockCalls++
		if approvalClockCalls == 1 {
			return approvalKnownAt.Add(time.Second)
		}
		return approvalKnownAt.Add(optimizerRevalidationMaximumAge + time.Second)
	}
	if _, _, err := service.ApproveOptimizerProposal(ctx, request.ID, proposal, approval, approvalAcquisition, reviewer, "expires before write"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("approval that expired at its write error = %v", err)
	}
	service.now = func() time.Time { return approvalKnownAt.Add(time.Second) }
	request, plan, err = service.ApproveOptimizerProposal(ctx, request.ID, proposal, approval, approvalAcquisition, reviewer, "verified proposal and current guards")
	if err != nil {
		t.Fatal(err)
	}
	if request.Status != ChangeApproved || plan.Status != PlanApproved || plan.Optimizer == nil || plan.Optimizer.ApprovalRevalidationID != approval.ID {
		t.Fatalf("governed optimizer approval = %#v / %#v", request, plan)
	}

	service.lifecycle.now = func() time.Time { return approvalKnownAt.Add(2 * time.Second) }
	if _, err := service.lifecycle.LeaseNextPlan(ctx, proposal.Environment, "agent-1", time.Minute); !errors.Is(err, ErrNoPlanAvailable) {
		t.Fatalf("generic lease exposed optimizer Plan: %v", err)
	}
	if _, err := service.lifecycle.LeaseNextOptimizerPlan(ctx, plan.ID, "agent-1", time.Minute, proposal, approval, approvalAcquisition); !errors.Is(err, ErrLeaseRejected) {
		t.Fatalf("approval-stage receipt was accepted at lease: %v", err)
	}

	leaseKnownAt := baseline.Add(10 * time.Second)
	lastSafePoint, lastSafePointAcquisition := authoritativeOptimizerRevalidation(t, proposal, optimization.ProposalRevalidationLastSafePoint, leaseKnownAt, false)
	leaseClockCalls := 0
	service.lifecycle.now = func() time.Time {
		leaseClockCalls++
		if leaseClockCalls == 1 {
			return leaseKnownAt.Add(time.Second)
		}
		return leaseKnownAt.Add(optimizerRevalidationMaximumAge + time.Second)
	}
	if _, err := service.lifecycle.LeaseNextOptimizerPlan(ctx, plan.ID, "agent-1", time.Minute, proposal, lastSafePoint, lastSafePointAcquisition); !errors.Is(err, ErrLeaseRejected) {
		t.Fatalf("last-safe-point evidence that expired at its lease write error = %v", err)
	}
	service.lifecycle.now = func() time.Time { return leaseKnownAt.Add(time.Second) }
	service.lifecycle.newToken = func() (string, error) { return "optimizer-lease-token", nil }
	leased, err := service.lifecycle.LeaseNextOptimizerPlan(ctx, plan.ID, "agent-1", time.Minute, proposal, lastSafePoint, lastSafePointAcquisition)
	if err != nil {
		t.Fatal(err)
	}
	if leased.Lease == nil || leased.Optimizer == nil || leased.Optimizer.LastSafePointRevalidationID != lastSafePoint.ID {
		t.Fatalf("optimizer lease provenance = %#v", leased)
	}
	service.lifecycle.now = func() time.Time { return leaseKnownAt.Add(2 * time.Second) }
	running, err := service.lifecycle.StartPlan(ctx, leased.ID, leased.Lease.Token)
	if err != nil {
		t.Fatal(err)
	}
	execution := optimizerSuccessfulExecution(running, leaseKnownAt.Add(2*time.Second))
	report := installedReport(t, foundation())
	report.ObservedAt = execution.FinishedAt
	finished, err := service.lifecycle.FinishPlan(ctx, running.ID, running.Lease.Token, true, "", "", &execution, &report)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != PlanSucceeded || finished.Optimizer == nil || finished.Optimizer.ProposalID != proposal.ID || finished.Optimizer.LastSafePointRevalidationID != lastSafePoint.ID {
		t.Fatalf("optimizer outcome lost immutable provenance = %#v", finished)
	}
	outcome, err := BuildOptimizerOutcomeRecord(finished, proposal, approval, approvalAcquisition, lastSafePoint, lastSafePointAcquisition, nil, nil, nil, execution.FinishedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Execution.PlanID != finished.ID || outcome.Execution.Revision != finished.Revision || outcome.Schedule.Status != optimization.OutcomeMeasurementUnknown || outcome.Cost.Status != optimization.OutcomeMeasurementUnknown || outcome.ValueStatus != optimization.OutcomeMeasurementUnknown {
		t.Fatalf("optimizer outcome record = %#v", outcome)
	}
	foreignRevalidation := lastSafePoint
	foreignRevalidation.ID = approval.ID
	if _, err := BuildOptimizerOutcomeRecord(finished, proposal, approval, approvalAcquisition, foreignRevalidation, lastSafePointAcquisition, nil, nil, nil, execution.FinishedAt.Add(time.Second)); err == nil {
		t.Fatal("accepted foreign last-safe-point outcome binding")
	}

	// Baseline inputs remain immutable after the complete lifecycle.
	if err := optimization.VerifyAdvisoryPlanProposal(proposal); err != nil || len(baselineFacts) != len(requirements) {
		t.Fatalf("optimizer baseline evidence changed: facts=%d requirements=%d err=%v", len(baselineFacts), len(requirements), err)
	}
}

func governedOptimizerFixture(t *testing.T) (optimization.AdvisoryPlanProposal, []optimization.TwinRequirement, []optimization.TemporalFact, time.Time) {
	t.Helper()
	baseline := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	guardKinds := []string{
		optimization.ProposalGuardCapacity,
		optimization.ProposalGuardCostOwner,
		optimization.ProposalGuardHealth,
		optimization.ProposalGuardMaintenance,
		optimization.ProposalGuardOwnership,
		optimization.ProposalGuardPolicy,
		optimization.ProposalGuardRecall,
	}
	requirements := make([]optimization.TwinRequirement, 0, len(guardKinds))
	guards := make([]optimization.AdvisoryProposalGuard, 0, len(guardKinds))
	for _, kind := range guardKinds {
		attribute := "guard." + strings.ToLower(kind)
		requirement := optimization.TwinRequirement{
			ID: attribute, EntityID: "environment/dev-use2", Attribute: attribute, Unit: "boolean",
			Plane: optimization.StateObserved, MinimumFidelity: optimization.FidelityExact, MaximumStalenessSeconds: 60,
		}
		requirements = append(requirements, requirement)
		guards = append(guards, optimization.AdvisoryProposalGuard{Kind: kind, Requirement: requirement})
	}
	knownAt := baseline.Add(time.Second)
	facts := freshOptimizerGuardFacts(t, requirements, baseline, knownAt, "r1")
	twin, err := optimization.BuildTwinSnapshot(requirements, facts, baseline, knownAt)
	if err != nil {
		t.Fatal(err)
	}
	recommendation := optimization.DiscreteRecommendationRequest{
		APIVersion: optimization.EvidenceAPIVersion, Kind: "DiscreteRecommendationRequest",
		Snapshot:  optimization.Snapshot{ID: twin.ID, Source: "optimizer-test", Revision: "r1", CapturedAt: twin.KnownAt},
		Objective: optimization.Objective{Primary: "minimize_makespan"},
		Problem: optimization.DiscreteScheduleProblem{
			Origin: baseline.Format(time.RFC3339Nano), TimeUnit: "seconds", HorizonSeconds: 10,
			CostModel: &optimization.CostModel{Currency: "USD", Source: "optimizer-test-price-list", AsOf: baseline.Format(time.RFC3339Nano)},
			Lanes:     []optimization.DiscreteLane{{ID: "lane-a", Capacity: map[string]int64{"slots": 1}}},
			Tasks: []optimization.DiscreteTask{{
				ID: "compile-plan", DurationSeconds: 1, LatestFinish: 10, EligibleLanes: []string{"lane-a"},
				Demand: map[string]int64{"slots": 1}, EstimatedCostMicros: map[string]int64{"lane-a": 1},
			}},
		},
	}
	solverRequest, err := optimization.NewDiscreteSolverRequest(
		recommendation, optimization.SolverSelection{PreferredBackend: optimization.NativeBackendName},
		optimization.SolverDeterminism{Workers: 1}, optimization.SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20},
	)
	if err != nil {
		t.Fatal(err)
	}
	ticks := []time.Time{baseline.Add(2 * time.Second), baseline.Add(3 * time.Second)}
	tick := 0
	solverReceipt, err := optimization.RunDiscreteSolverRequest(context.Background(), solverRequest, []optimization.DiscreteSolverBackend{
		optimization.NativeDiscreteSolverBackend{ExecutableDigest: digest, Architecture: runtime.GOARCH},
	}, func() time.Time {
		value := ticks[tick]
		if tick < len(ticks)-1 {
			tick++
		}
		return value
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err := json.Marshal(foundation())
	if err != nil {
		t.Fatal(err)
	}
	selection := optimization.ProposalSelectionSpec{
		CompilerRevision: optimization.DiscreteProposalCompilerRevision,
		DocumentSchema:   optimizerDesiredStateSchema,
		Base:             document,
		WorkItems: []optimization.DiscreteWorkItemSelection{{
			Task: "compile-plan",
			Variants: []optimization.DiscreteChoiceVariant{{
				Lane: "lane-a", Operations: []optimization.ProposalReplaceOperation{{Pointer: "/environment/id", Value: json.RawMessage(`"dev-use2"`)}},
			}},
		}},
	}
	proposal, err := optimization.NewAdvisoryPlanProposal(
		"dev-use2", "optimizer-compiler:test", selection,
		optimization.ProposalSolverEvidence{Discrete: &optimization.DiscreteSolverRun{Request: solverRequest, Receipt: solverReceipt}},
		twin, guards, baseline.Add(4*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	return proposal, requirements, facts, baseline
}

func freshOptimizerGuardFacts(t *testing.T, requirements []optimization.TwinRequirement, validAt, recordedAt time.Time, revision string) []optimization.TemporalFact {
	t.Helper()
	facts := make([]optimization.TemporalFact, 0, len(requirements))
	for _, requirement := range requirements {
		fact, err := optimization.NewTemporalFact(optimization.TemporalFact{
			EntityID: requirement.EntityID, Attribute: requirement.Attribute, Value: json.RawMessage(`true`), Unit: requirement.Unit,
			Plane: requirement.Plane, ValidFrom: validAt.Format(time.RFC3339Nano), RecordedAt: recordedAt.Format(time.RFC3339Nano),
			Source: "optimizer-guard-collector", SourceRevision: revision, RequestDigest: digest, ResponseDigest: digest,
			CollectorIdentity: "optimizer-test", Fidelity: optimization.FidelityExact,
		})
		if err != nil {
			t.Fatal(err)
		}
		facts = append(facts, fact)
	}
	return facts
}

type optimizerGuardTestReader struct{ observedAt time.Time }

func (reader optimizerGuardTestReader) Read(_ context.Context, request optimization.AcquisitionReadRequest) (optimization.AcquisitionReadResponse, error) {
	if request.Source != "optimizer-governance-test" || request.Method != "GET" || request.Endpoint != "/guards" {
		return optimization.AcquisitionReadResponse{}, errors.New("foreign optimizer guard read")
	}
	return optimization.AcquisitionReadResponse{
		StatusCode: 200, ObservedAt: reader.observedAt.Format(time.RFC3339Nano), SourceRevision: "guard-r2", Body: []byte(`{}`),
	}, nil
}

type optimizerGuardTestExtractor struct {
	guards []optimization.AdvisoryProposalGuard
	drift  bool
}

func (extractor optimizerGuardTestExtractor) Extract(_ optimization.AcquisitionReadRequest, response optimization.AcquisitionReadResponse) ([]optimization.ExtractedAcquisitionFact, error) {
	facts := make([]optimization.ExtractedAcquisitionFact, 0, len(extractor.guards))
	for index, guard := range extractor.guards {
		value := json.RawMessage(`true`)
		if extractor.drift && index == 0 {
			value = json.RawMessage(`false`)
		}
		facts = append(facts, optimization.ExtractedAcquisitionFact{
			RequirementID: guard.Requirement.ID,
			EntityID:      guard.Requirement.EntityID,
			Attribute:     guard.Requirement.Attribute,
			Value:         value,
			Unit:          guard.Requirement.Unit,
			Plane:         guard.Requirement.Plane,
			Fidelity:      guard.Requirement.MinimumFidelity,
			ValidFrom:     response.ObservedAt,
		})
	}
	return facts, nil
}

func authoritativeOptimizerRevalidation(t *testing.T, proposal optimization.AdvisoryPlanProposal, stage string, knownAt time.Time, drift bool) (optimization.ProposalRevalidation, optimization.AcquisitionExecutionReceipt) {
	t.Helper()
	observations := make([]optimization.FieldObservation, 0, len(proposal.Guards))
	for _, guard := range proposal.Guards {
		observations = append(observations, optimization.FieldObservation{
			Requirement:             guard.Requirement.ID,
			Plane:                   guard.Requirement.Plane,
			Fidelity:                guard.Requirement.MinimumFidelity,
			MaximumStalenessSeconds: int(guard.Requirement.MaximumStalenessSeconds),
		})
	}
	call := optimization.AcquisitionCall{
		ID: "optimizer-guards", Source: "optimizer-governance-test", Method: "GET", Endpoint: "/guards",
		Consistency: "snapshot", CostUnits: 1, Observations: observations,
	}
	store := optimization.FileProposalEvidenceStore{Directory: filepath.Join(t.TempDir(), "optimizer-evidence")}
	if err := store.SaveProposal(proposal); err != nil {
		t.Fatal(err)
	}
	observedAt := knownAt.Add(-time.Second)
	revalidator := optimization.ProposalRevalidator{
		Executor: optimization.AcquisitionExecutor{
			Reader:            optimizerGuardTestReader{observedAt: observedAt},
			Extractor:         optimizerGuardTestExtractor{guards: proposal.Guards, drift: drift},
			CollectorIdentity: "collector:optimizer-governance-test",
			Now:               func() time.Time { return knownAt },
		},
		Candidates: []optimization.AcquisitionCall{call},
		Evidence:   store,
	}
	record, err := revalidator.Execute(context.Background(), proposal, stage)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := store.GetAcquisitionReceipt(record.AcquisitionReceiptID)
	if err != nil {
		t.Fatal(err)
	}
	return record, receipt
}

func optimizerSuccessfulExecution(record PlanRecord, startedAt time.Time) PlanExecution {
	tasks := make([]PlanExecutionTask, 0, len(record.Plan.Steps))
	for index, step := range record.Plan.Steps {
		tasks = append(tasks, PlanExecutionTask{
			Sequence: index + 1, Phase: ExecutionPhaseApply, Status: ExecutionTaskSucceeded,
			InstallationID: step.InstallationID, Namespace: step.Namespace,
			StartedAt: startedAt.Add(time.Duration(index) * time.Second), FinishedAt: startedAt.Add(time.Duration(index+1) * time.Second),
		})
	}
	attempt := 0
	if record.Lease != nil {
		attempt = record.Lease.Attempt
	}
	return PlanExecution{Attempt: attempt, StartedAt: startedAt, FinishedAt: startedAt.Add(time.Duration(len(tasks)+1) * time.Second), Tasks: tasks}
}
