package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/planlifecycle"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/fixtures"
)

func TestHappyPathSignalsOnlyAfterConduitApproval(t *testing.T) {
	acts, id, repository := prepare(t)
	env := boot(acts)
	message := approve(t, acts, id)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalReview, message) }, 0)
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Hour})
	result := mustComplete(t, env)
	if result.Outcome != domain.OutcomeHandoffRecorded || result.PlanID == "" || result.HandoffReconciled {
		t.Fatalf("result = %#v", result)
	}
	if result.ArtifactDigest == "" || result.ProposalRevision == "" || result.SourceDigest != id.SourceDigest {
		t.Fatalf("identity was not carried: %#v", result)
	}
	if mustPlans(t, repository) != 1 {
		t.Fatalf("plans = %d", mustPlans(t, repository))
	}
	encoded, _ := json.Marshal(result)
	if jsonContains(encoded, "kind: fixture") || jsonContains(encoded, "BEGIN PRIVATE KEY") {
		t.Fatalf("result copied fixture or credential bytes: %s", encoded)
	}
}

func TestUpdateCarriesTheSameDecisionAsASignal(t *testing.T) {
	acts, id, _ := prepare(t)
	message := approve(t, acts, id)
	env := boot(acts)
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(UpdateReview, "upd-1", &testsuite.TestUpdateCallback{
			OnReject: func(err error) { t.Errorf("update rejected: %v", err) },
		}, message)
	}, time.Minute)
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Hour})
	result := mustComplete(t, env)
	if result.Outcome != domain.OutcomeHandoffRecorded || result.LastReview != domain.ReviewAccepted {
		t.Fatalf("result = %#v", result)
	}
}

func TestSignalWithoutGovernanceRecordDoesNotHandoff(t *testing.T) {
	acts, id, repository := prepare(t)
	env := boot(acts)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReview, domain.ReviewMessage{
			ApprovalID: "appr-missing", RunID: id.RunID, ProposalRevision: "rev-unknown",
			ArtifactDigest: "sha256:missing", TargetPlanRef: id.TargetPlanRef,
		})
	}, 0)
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Hour})
	result := mustComplete(t, env)
	if result.Outcome != domain.OutcomeRejected || mustPlans(t, repository) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestStaleAndChangedArtifactDoNotHandoff(t *testing.T) {
	acts, id, repository := prepare(t)
	message := approve(t, acts, id)
	message.ArtifactDigest = "sha256:changed"
	env := boot(acts)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalReview, message) }, 0)
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Hour})
	result := mustComplete(t, env)
	if result.Outcome != domain.OutcomeStaleApproval || mustPlans(t, repository) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestDuplicateApprovalHandsOffOnce(t *testing.T) {
	acts, id, repository := prepare(t)
	message := approve(t, acts, id)
	env := boot(acts)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReview, message)
		env.SignalWorkflow(SignalReview, message)
	}, 0)
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Hour})
	result := mustComplete(t, env)
	if result.Outcome != domain.OutcomeHandoffRecorded || mustPlans(t, repository) != 1 {
		t.Fatalf("result = %#v plans = %d", result, mustPlans(t, repository))
	}
	if result.DuplicateReviews > 1 {
		t.Fatalf("duplicate reviews = %d", result.DuplicateReviews)
	}
}

func TestValidationGatesBlockHandoff(t *testing.T) {
	for _, tc := range []struct {
		marker string
		want   domain.Outcome
	}{
		{"lint: fail\n", domain.OutcomeFailRequired},
		{"security-evidence: unknown\n", domain.OutcomeUnknownSecurity},
		{"requirement: unsupported\n", domain.OutcomeUnsupported},
	} {
		t.Run(string(tc.want), func(t *testing.T) {
			acts, id, repository := prepare(t)
			body := append(append([]byte{}, fixtures.Source...), tc.marker...)
			if err := os.WriteFile(filepath.Join(acts.FixtureDir, "source.txt"), body, 0o644); err != nil {
				t.Fatal(err)
			}
			id.SourceDigest = domain.DigestBytes(body)
			env := boot(acts)
			env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Hour})
			result := mustComplete(t, env)
			if result.Outcome != tc.want || mustPlans(t, repository) != 0 {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestMismatchedValidationDoesNotInheritAPass(t *testing.T) {
	acts, id, repository := prepare(t)
	acts.ValidateFn = func(context.Context, domain.RunIdentity, string, domain.Receipt) (domain.Validation, error) {
		return domain.Validation{Outcome: domain.OutcomePass, ArtifactDigest: "sha256:earlier", ProposalRevision: "rev-earlier"}, nil
	}
	env := boot(acts)
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Hour})
	result := mustComplete(t, env)
	if result.Outcome != domain.OutcomeFailRequired || mustPlans(t, repository) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestTransientProposalRetryKeepsOneReceipt(t *testing.T) {
	acts, id, repository := prepare(t)
	var calls atomic.Int32
	inner := fixtures.Suite{}.Propose
	acts.ProposeFn = func(ctx context.Context, got domain.RunIdentity, dir string) (domain.Receipt, error) {
		if calls.Add(1) == 1 {
			return domain.Receipt{}, errors.New("model unavailable")
		}
		return inner(ctx, got, dir)
	}
	message := approve(t, acts, id)
	env := boot(acts)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalReview, message) }, 0)
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Hour})
	result := mustComplete(t, env)
	if calls.Load() != 2 || result.Outcome != domain.OutcomeHandoffRecorded || mustPlans(t, repository) != 1 {
		t.Fatalf("calls = %d result = %#v", calls.Load(), result)
	}
}

func TestActivityTimeoutBlocksWithoutHandoff(t *testing.T) {
	acts, id, repository := prepare(t)
	env := boot(acts)
	env.OnActivity(ActAssess, mock.Anything, mock.Anything).Return(domain.Receipt{}, temporal.NewTimeoutError(enumspb.TIMEOUT_TYPE_START_TO_CLOSE, nil))
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Hour})
	result := mustComplete(t, env)
	if result.Outcome != domain.OutcomeTimedOut || mustPlans(t, repository) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestApprovalWaitTimeout(t *testing.T) {
	acts, id, repository := prepare(t)
	env := boot(acts)
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Millisecond})
	result := mustComplete(t, env)
	if result.Outcome != domain.OutcomeTimedOut || mustPlans(t, repository) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestCancelDuringReviewDoesNotHandoff(t *testing.T) {
	acts, id, repository := prepare(t)
	env := boot(acts)
	env.RegisterDelayedCallback(func() { env.CancelWorkflow() }, time.Minute)
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Hour})
	if !env.IsWorkflowCompleted() || !temporal.IsCanceledError(env.GetWorkflowError()) {
		t.Fatalf("completed = %v err = %v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if mustPlans(t, repository) != 0 {
		t.Fatal("cancel submitted a plan")
	}
}

func TestCancelRacesAnApprovalAndDoesNotHandoff(t *testing.T) {
	acts, id, repository := prepare(t)
	message := approve(t, acts, id)
	env := boot(acts)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReview, message)
		env.CancelWorkflow()
	}, time.Minute)
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Hour})
	if mustPlans(t, repository) != 0 {
		t.Fatal("raced approval submitted a plan")
	}
}

func TestExecutionControlIsRecheckedAtHandoff(t *testing.T) {
	acts, id, repository := prepare(t)
	message := approve(t, acts, id)
	now := time.Now().UTC().Add(-time.Minute)
	if _, err := repository.CreateSuppression(context.Background(), controlplane.SuppressionRecord{
		ID: "sup-env-dev", Environment: "env-dev", Scope: controlplane.SuppressionEnvironment,
		Source: controlplane.SuppressionManual, Reason: "maintenance", CreatedBy: "operator",
		StartsAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	env := boot(acts)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalReview, message) }, 0)
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Hour})
	result := mustComplete(t, env)
	if result.Outcome == domain.OutcomeHandoffRecorded || mustPlans(t, repository) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestCrashAfterSubmitReconcilesWithoutASecondPlan(t *testing.T) {
	acts, id, repository := prepare(t)
	message := approve(t, acts, id)
	acts.Plans.(*planlifecycle.Submitter).FailAfterSubmit = true
	env := boot(acts)
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(SignalReview, message) }, 0)
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Hour})
	result := mustComplete(t, env)
	if result.Outcome != domain.OutcomeHandoffRecorded || !result.HandoffReconciled || mustPlans(t, repository) != 1 {
		t.Fatalf("result = %#v plans = %d", result, mustPlans(t, repository))
	}
}

func TestContinueAsNewCarriesTheAssessment(t *testing.T) {
	acts, id, repository := prepare(t)
	var assessments atomic.Int32
	inner := fixtures.Suite{}.Assess
	acts.AssessFn = func(ctx context.Context, got domain.RunIdentity, dir string) (domain.Receipt, error) {
		assessments.Add(1)
		return inner(ctx, got, dir)
	}
	env := boot(acts)
	env.SetCurrentHistoryLength(10000)
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Hour, ContinueAsNewAfterEvents: 1})
	var continued *workflow.ContinueAsNewError
	if !errors.As(env.GetWorkflowError(), &continued) {
		t.Fatalf("err = %v", env.GetWorkflowError())
	}
	var next Input
	if err := converter.GetDefaultDataConverter().FromPayloads(continued.Input, &next); err != nil {
		t.Fatal(err)
	}
	if next.CANCount != 1 || next.Carry == nil || next.Carry.Phase != domain.PhasePropose || next.Carry.Assessment.SourceDigest != id.SourceDigest {
		t.Fatalf("continued input = %#v", next)
	}
	if assessments.Load() != 1 {
		t.Fatalf("assessments = %d", assessments.Load())
	}
	message := approve(t, acts, id)
	followed := boot(acts)
	followed.RegisterDelayedCallback(func() { followed.SignalWorkflow(SignalReview, message) }, 0)
	next.ContinueAsNewAfterEvents = defaultHistoryEvents
	followed.ExecuteWorkflow(PackagingWorkflow, next)
	result := mustComplete(t, followed)
	if result.Outcome != domain.OutcomeHandoffRecorded || assessments.Load() != 1 || mustPlans(t, repository) != 1 {
		t.Fatalf("assessments = %d result = %#v", assessments.Load(), result)
	}
}

func TestCredentialMarkerNeverBecomesWorkflowInput(t *testing.T) {
	acts, id, repository := prepare(t)
	env := boot(acts)
	id.ToolVersions = "lint=BEGIN PRIVATE KEY"
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id})
	result := mustComplete(t, env)
	if result.Outcome != domain.OutcomeFailRequired || mustPlans(t, repository) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func prepare(t *testing.T) (*Activities, domain.RunIdentity, *controlplane.MemoryRepository) {
	t.Helper()
	dir := t.TempDir()
	if err := fixtures.Materialize(dir); err != nil {
		t.Fatal(err)
	}
	ledger, err := adapters.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repository := controlplane.NewMemoryRepository()
	id, err := fixtures.Identity("sample-app-1")
	if err != nil {
		t.Fatal(err)
	}
	return &Activities{FixtureDir: dir, Ledger: ledger, Plans: planlifecycle.New(repository, ledger)}, id, repository
}

func boot(acts *Activities) *testsuite.TestWorkflowEnvironment {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(PackagingWorkflow, workflow.RegisterOptions{Name: WorkflowName})
	registerActivities(env, *acts)
	return env
}

func approve(t *testing.T, acts *Activities, id domain.RunIdentity) domain.ReviewMessage {
	t.Helper()
	proposal, err := fixtures.Suite{}.Propose(context.Background(), id, acts.FixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	record := domain.ApprovalRecord{
		ApprovalID: "appr-1", RunID: id.RunID, ProposalRevision: proposal.ProposalRevision,
		ArtifactDigest: proposal.Artifact.Digest, TargetPlanRef: id.TargetPlanRef, Decision: domain.DecisionApproved,
	}
	if err := acts.Ledger.PutApproval(record); err != nil {
		t.Fatal(err)
	}
	return domain.ReviewMessage{
		ApprovalID: record.ApprovalID, RunID: record.RunID, ProposalRevision: record.ProposalRevision,
		ArtifactDigest: record.ArtifactDigest, TargetPlanRef: record.TargetPlanRef,
	}
}

func mustComplete(t *testing.T, env *testsuite.TestWorkflowEnvironment) domain.Result {
	t.Helper()
	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result domain.Result
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func mustPlans(t *testing.T, repository controlplane.Repository) int {
	t.Helper()
	plans, err := repository.ListPlans(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	return len(plans)
}

func jsonContains(encoded []byte, needle string) bool {
	return bytesContains(encoded, []byte(needle))
}

func bytesContains(encoded, needle []byte) bool {
	if len(needle) == 0 || len(encoded) < len(needle) {
		return false
	}
	for i := 0; i+len(needle) <= len(encoded); i++ {
		match := true
		for j := range needle {
			if encoded[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
