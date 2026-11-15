package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	events := queryEvents(t, env)
	encoded = append(encoded, mustEventJSON(t, events)...)
	if jsonContains(encoded, "kind: fixture") || jsonContains(encoded, "BEGIN PRIVATE KEY") {
		t.Fatalf("result copied fixture or credential bytes: %s", encoded)
	}
	if !eventRecorded(events, domain.StepHandoff, string(domain.OutcomeHandoffRecorded), result.PlanID) {
		t.Fatalf("events = %#v", events)
	}
	if strings.Contains(string(mustEventJSON(t, events)), "AWAITING_APPROVAL") {
		t.Fatal("events exposed plan status")
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
			events := queryEvents(t, env)
			if eventRecorded(events, domain.StepHandoff, "", "") || !eventRecorded(events, domain.StepValidate, string(tc.want), "") {
				t.Fatalf("events = %#v", events)
			}
		})
	}
}

func TestReviewWaitHistoryDoesNotImplyHandoff(t *testing.T) {
	acts, id, repository := prepare(t)
	env := boot(acts)
	var events []domain.StepEvent
	env.RegisterDelayedCallback(func() {
		encoded, err := env.QueryWorkflow(QueryEvents)
		if err != nil {
			t.Errorf("query events: %v", err)
			return
		}
		if err := encoded.Get(&events); err != nil {
			t.Errorf("decode events: %v", err)
		}
	}, time.Second)
	env.RegisterDelayedCallback(func() { env.CancelWorkflow() }, time.Minute)
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Hour})
	if !env.IsWorkflowCompleted() || !temporal.IsCanceledError(env.GetWorkflowError()) {
		t.Fatalf("completed = %v err = %v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
	if mustPlans(t, repository) != 0 {
		t.Fatal("review wait submitted a plan")
	}
	if !eventRecorded(events, domain.StepReview, domain.EventWaiting, "") || eventRecorded(events, domain.StepHandoff, "", "") {
		t.Fatalf("events = %#v", events)
	}
	for _, event := range events {
		if event.Step == domain.StepReview && event.Outcome == string(domain.OutcomePass) {
			t.Fatalf("review wait recorded as pass: %#v", event)
		}
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
	status, err := env.QueryWorkflow(QueryStatus)
	if err != nil {
		t.Fatal(err)
	}
	var reported domain.RunStatus
	if err := status.Get(&reported); err != nil {
		t.Fatal(err)
	}
	if reported.Phase != domain.PhaseBlocked || reported.Outcome != string(domain.OutcomeCancelled) || reported.PlanID != "" {
		t.Fatalf("status after cancel = %#v", reported)
	}
	events := queryEvents(t, env)
	if !eventRecorded(events, domain.PhaseBlocked, string(domain.OutcomeCancelled), "") {
		t.Fatalf("cancellation missing from journal: %#v", events)
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
	if len(next.Carry.Events) != 1 || next.Carry.Events[0].Step != domain.StepAssess || next.Carry.Events[0].Outcome != string(domain.OutcomePass) {
		t.Fatalf("continued journal = %#v", next.Carry.Events)
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
	// Temporal test cancellation may finish before the activity goroutine exits.
	// Drain and reject late ledger operations before TempDir removes its files.
	t.Cleanup(func() {
		if err := ledger.Close(); err != nil {
			t.Error(err)
		}
	})
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

func queryEvents(t *testing.T, env *testsuite.TestWorkflowEnvironment) []domain.StepEvent {
	t.Helper()
	encoded, err := env.QueryWorkflow(QueryEvents)
	if err != nil {
		t.Fatal(err)
	}
	var events []domain.StepEvent
	if err := encoded.Get(&events); err != nil {
		t.Fatal(err)
	}
	return events
}

func eventRecorded(events []domain.StepEvent, step, outcome, planID string) bool {
	for _, event := range events {
		if event.Step != step {
			continue
		}
		if outcome != "" && event.Outcome != outcome {
			continue
		}
		if planID != "" && event.PlanID != planID {
			continue
		}
		return true
	}
	return false
}

func mustEventJSON(t *testing.T, events []domain.StepEvent) []byte {
	t.Helper()
	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
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

func TestV2CancellationPreservesExistingJournal(t *testing.T) {
	acts, id, repository := prepare(t)
	env := boot(acts)
	env.OnGetVersion(changeID, workflow.DefaultVersion, workflow.Version(4)).Return(workflow.Version(2))
	env.RegisterDelayedCallback(func() { env.CancelWorkflow() }, time.Minute)
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Hour})
	if !temporal.IsCanceledError(env.GetWorkflowError()) || mustPlans(t, repository) != 0 {
		t.Fatal("legacy cancellation changed authority")
	}
	events := queryEvents(t, env)
	if eventRecorded(events, domain.PhaseBlocked, string(domain.OutcomeCancelled), "") {
		t.Fatal("v2 history gained a new terminal journal entry")
	}
	if !eventRecorded(events, domain.StepReview, domain.EventWaiting, "") {
		t.Fatal("v2 review journal was lost")
	}
}

func TestMissingTerminalOutcomeIsJournaledWithoutHandoff(t *testing.T) {
	acts, id, repository := prepare(t)
	env := boot(acts)
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, Carry: &domain.State{Phase: domain.PhaseComplete}})
	result := mustComplete(t, env)
	if result.Outcome != domain.OutcomeFailRequired || mustPlans(t, repository) != 0 {
		t.Fatal("invalid terminal state fabricated success")
	}
	if !eventRecorded(queryEvents(t, env), domain.PhaseBlocked, string(domain.OutcomeFailRequired), "") {
		t.Fatal("terminal validation failure missing from journal")
	}
}

func TestExhaustedReviewBudgetRecordsNativeFailure(t *testing.T) {
	acts, id, repository := prepare(t)
	env := boot(acts)
	seen := make([]string, domain.MaxSeenApprovals)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReview, domain.ReviewMessage{RunID: id.RunID, ApprovalID: "overflow"})
	}, time.Minute)
	env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, ApprovalWait: time.Hour, Carry: &domain.State{Phase: domain.PhaseReview, SeenApprovalIDs: seen}})
	result := mustComplete(t, env)
	if result.Outcome != domain.OutcomeFailRequired || mustPlans(t, repository) != 0 {
		t.Fatal("exhausted review budget handed off")
	}
	if !eventRecorded(queryEvents(t, env), domain.PhaseBlocked, string(domain.OutcomeFailRequired), "") {
		t.Fatal("review budget failure missing from journal")
	}
}

func TestFullJournalCancellationPreservesV3AndRecordsV4Terminal(t *testing.T) {
	for _, version := range []workflow.Version{3, 4} {
		t.Run(fmt.Sprintf("version-%d", version), func(t *testing.T) {
			acts, id, repository := prepare(t)
			env := boot(acts)
			env.OnGetVersion(changeID, workflow.DefaultVersion, workflow.Version(4)).Return(version)
			carry := domain.State{Phase: domain.PhaseReview, TargetPlanRef: id.TargetPlanRef}
			for i := 0; i < domain.MaxStepEvents; i++ {
				carry.Events = append(carry.Events, domain.StepEvent{Sequence: i + 1, At: time.Unix(int64(i+1), 0), Step: domain.StepReview, Outcome: domain.EventWaiting})
			}
			env.RegisterDelayedCallback(func() { env.CancelWorkflow() }, time.Minute)
			env.ExecuteWorkflow(PackagingWorkflow, Input{Identity: id, Carry: &carry, ApprovalWait: time.Hour})
			if !temporal.IsCanceledError(env.GetWorkflowError()) || mustPlans(t, repository) != 0 {
				t.Fatal("cancellation changed execution authority")
			}
			events := queryEvents(t, env)
			if len(events) != domain.MaxStepEvents {
				t.Fatal("native journal is unbounded")
			}
			last := events[len(events)-1]
			if version == 3 {
				if events[0].Sequence != 1 || last.Sequence != 32 || last.Step != domain.StepReview {
					t.Fatal("v3 query projection changed")
				}
			} else {
				if events[0].Sequence != 3 || last.Sequence != 34 || last.Step != domain.PhaseBlocked || last.Outcome != string(domain.OutcomeCancelled) {
					t.Fatalf("v4 cancellation lost after full carry: %+v", events)
				}
			}
		})
	}
}
