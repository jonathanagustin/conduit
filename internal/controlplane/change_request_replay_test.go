package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A Change Request id is derived from its target alone (governance.go, the
// sha256 over TargetKind and TargetID), so a second proposal against the same
// target collides with the first, and CreateChangeRequest returns the record
// already stored rather than the one this call just evaluated.
//
// For an APPROVED record that makes it a pre-approval token: it outlives the
// policy that approved it, and every handler that auto-applies on
// Status == APPROVED will apply it again, however the policy has changed since.
// A rejected proposal and a replayed one are then indistinguishable from the
// outside -- both answer 201 -- while only one of them has been decided by the
// policy in force.
//
// The fix is not to stop returning the stored record; callers legitimately ask
// for it. It is that only a record this call created may be auto-applied.

// archiveState drives the export-pipeline lifecycle routes, whose TargetID is
// the pipeline id with no revision or content digest folded in -- the live
// instance of the collision.
func archivePipeline(t *testing.T, handler http.Handler, pipelineID, action string) map[string]any {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/v1/export-pipelines/"+pipelineID+"/"+action, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("%s %s = %d %s", action, pipelineID, response.Code, response.Body.String())
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode %s response: %v", action, err)
	}
	return decoded
}

func putPipeline(t *testing.T, handler http.Handler, pipelineID string) {
	t.Helper()
	body, err := json.Marshal(ExportPipeline{
		DisplayName: pipelineID, BuildStrategy: BundleStrategySnapshot,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectEnvironment, IDs: []string{"dev-use2"},
			ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/v1/export-pipelines/"+pipelineID, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("put pipeline %s = %d %s", pipelineID, response.Code, response.Body.String())
	}
}

func changeRequestField(t *testing.T, result map[string]any, field string) string {
	t.Helper()
	changeRequest, ok := result["changeRequest"].(map[string]any)
	if !ok {
		t.Fatalf("no changeRequest in %#v", result)
	}
	value, _ := changeRequest[field].(string)
	return value
}

func changeRequestStatus(t *testing.T, result map[string]any) string {
	t.Helper()
	return changeRequestField(t, result, "status")
}

// TestATightenedPolicyIsNotBypassedByAStaleApprovedChangeRequest is the defect.
// It asserts on the pipeline's own archived flag rather than on the response
// body, because the question is whether the action was carried out, not what
// the handler said about it.
func TestATightenedPolicyIsNotBypassedByAStaleApprovedChangeRequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, assessedFoundation()); err != nil {
		t.Fatal(err)
	}
	governance := NewGovernanceService(repository)
	permissive := ApprovalPolicy{
		ID: "lifecycle", DisplayName: "Pipeline lifecycle", Enabled: true,
		Expression: `approve("archiving is unrestricted")`,
	}
	permissive, err := governance.PutPolicy(ctx, permissive)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	putPipeline(t, handler, "wedged")

	// Under the permissive policy the action is approved and applied, which is
	// what leaves an APPROVED record behind on this target.
	if status := changeRequestStatus(t, archivePipeline(t, handler, "wedged", "archive")); status != ChangeApproved {
		t.Fatalf("the permissive policy did not approve the archive: %s", status)
	}
	stored, err := repository.GetExportPipeline(ctx, "wedged")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != ExportPipelineArchived {
		t.Fatal("the approved archive was not applied, so there is no stale record to replay")
	}
	// Put it back, still under the permissive policy, so that a replayed archive
	// has something to change.
	archivePipeline(t, handler, "wedged", "unarchive")
	if stored, err = repository.GetExportPipeline(ctx, "wedged"); err != nil || stored.Status == ExportPipelineArchived {
		t.Fatalf("unarchive did not take effect: %#v %v", stored, err)
	}

	// The policy now refuses this action outright.
	permissive.Expression = `reject("archiving is frozen")`
	if _, err := governance.PutPolicy(ctx, permissive); err != nil {
		t.Fatal(err)
	}

	// Control: on a target carrying no prior record, the tightened policy is
	// obeyed. Without this the assertion below could pass against a Hub that had
	// simply stopped archiving anything.
	putPipeline(t, handler, "fresh")
	if status := changeRequestStatus(t, archivePipeline(t, handler, "fresh", "archive")); status != ChangeRejected {
		t.Fatalf("the tightened policy did not reject a fresh archive: %s", status)
	}
	if fresh, freshErr := repository.GetExportPipeline(ctx, "fresh"); freshErr != nil || fresh.Status == ExportPipelineArchived {
		t.Fatalf("a rejected archive was applied to the fresh pipeline: %#v %v", fresh, freshErr)
	}

	// The replay. Same call, same principal, same policy as the control -- the
	// only difference is that this target carries an APPROVED record minted
	// before the policy was tightened.
	archivePipeline(t, handler, "wedged", "archive")
	replayed, err := repository.GetExportPipeline(ctx, "wedged")
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Status == ExportPipelineArchived {
		t.Fatal("a stale APPROVED Change Request applied an action the policy in force rejects")
	}
}

// The proposal route is not the only way back to an approved record's side
// effect. Five review routes call finalizeApprovedChangeRequest (http.go), which
// re-runs the export-pipeline lifecycle action and the fleet action whenever the
// stored record reads APPROVED -- and one of those routes, PUT
// /v1/change-requests/{id}/labels, is reachable by the author, takes no
// decision, and leaves an already-satisfied record APPROVED (governance.go,
// SetLabels). Setting a label on an old approved record therefore performs its
// action again, under whatever policy is in force now.
//
// Gating the proposal route alone would leave this open, so it is asserted
// separately rather than assumed to follow.
func TestSettingLabelsDoesNotReExecuteAnAlreadyAppliedChangeRequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, assessedFoundation()); err != nil {
		t.Fatal(err)
	}
	governance := NewGovernanceService(repository)
	policy, err := governance.PutPolicy(ctx, ApprovalPolicy{
		ID: "lifecycle", DisplayName: "Pipeline lifecycle", Enabled: true,
		Expression: `approve("archiving is unrestricted")`,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	putPipeline(t, handler, "labelled")

	archived := archivePipeline(t, handler, "labelled", "archive")
	changeRequestID := changeRequestField(t, archived, "id")
	if changeRequestID == "" {
		t.Fatalf("no change request id in %#v", archived)
	}
	// Undo the applied action, still under the permissive policy, so that a
	// re-execution is visible as the pipeline going back to archived rather than
	// as a silent no-op.
	archivePipeline(t, handler, "labelled", "unarchive")

	// The policy that approved this record is gone.
	policy.Expression = `reject("archiving is frozen")`
	if _, err = governance.PutPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPut, "/v1/change-requests/"+changeRequestID+"/labels",
		bytes.NewReader([]byte(`{"labels":["reviewed"]}`)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("set labels = %d %s", response.Code, response.Body.String())
	}

	stored, err := repository.GetExportPipeline(ctx, "labelled")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status == ExportPipelineArchived {
		t.Fatal("labelling an approved Change Request performed its action again")
	}
}

// The falsifier for any fix that keys on "did this call create the record".
// Archiving, unarchiving and archiving again is an ordinary sequence, and the
// second archive collides with the first: same target kind, same pipeline id,
// same derived crq. Nothing about the policy has changed between them. If the
// guard is creation rather than the policy's current verdict, this pipeline
// silently stays active and no route can archive it again.
func TestRepeatingAnActionUnderAnUnchangedPolicyStillPerformsIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, assessedFoundation()); err != nil {
		t.Fatal(err)
	}
	if _, err := NewGovernanceService(repository).PutPolicy(ctx, ApprovalPolicy{
		ID: "lifecycle", DisplayName: "Pipeline lifecycle", Enabled: true,
		Expression: `approve("archiving is unrestricted")`,
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	putPipeline(t, handler, "repeated")

	archivePipeline(t, handler, "repeated", "archive")
	archivePipeline(t, handler, "repeated", "unarchive")
	archivePipeline(t, handler, "repeated", "archive")

	stored, err := repository.GetExportPipeline(ctx, "repeated")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != ExportPipelineArchived {
		t.Fatalf("archiving a second time under the same permissive policy did nothing: %s", stored.Status)
	}
}

// Reopen is the one review route besides approval that re-decides: it discards
// the old verdict and evaluates the change against the policy in force
// (governance.go, Reopen calls evaluateChangeRequest). An APPROVED result there
// is a decision taken now, so it must still apply -- the replay guard has to
// tell that apart from labelling, which decides nothing.
func TestReopeningUnderALoosenedPolicyPerformsTheAction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, assessedFoundation()); err != nil {
		t.Fatal(err)
	}
	governance := NewGovernanceService(repository)
	policy, err := governance.PutPolicy(ctx, ApprovalPolicy{
		ID: "lifecycle", DisplayName: "Pipeline lifecycle", Enabled: true,
		Expression: `reject("archiving is frozen")`,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	putPipeline(t, handler, "reopened")

	rejected := archivePipeline(t, handler, "reopened", "archive")
	if status := changeRequestStatus(t, rejected); status != ChangeRejected {
		t.Fatalf("the frozen policy did not reject the archive: %s", status)
	}
	changeRequestID := changeRequestField(t, rejected, "id")

	// The freeze is lifted, and the author reopens.
	policy.Expression = `approve("archiving is unrestricted again")`
	if _, err = governance.PutPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/change-requests/"+changeRequestID+"/reopen", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("reopen = %d %s", response.Code, response.Body.String())
	}

	stored, err := repository.GetExportPipeline(ctx, "reopened")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != ExportPipelineArchived {
		t.Fatalf("reopening under a policy that approves did not perform the action: %s", stored.Status)
	}
}

// The same replay on the deployment path, which is where it costs most. A Plan
// id is a digest of its desired state, so re-proposing state that has been
// proposed before collides on the derived Change Request id -- and
// supersedePendingPlans (lifecycle.go) cancels an approved Plan when a newer one
// arrives without touching its Change Request, so a CANCELLED Plan holding an
// APPROVED Change Request is a routine state rather than a partial failure.
// Re-proposing then synchronizes that stale approval back onto the Plan, clears
// the cancellation, and leaves it approved and executable.
func TestReProposingSupersededStateDoesNotResurrectItsPlan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	policy, err := service.PutPolicy(ctx, ApprovalPolicy{
		ID: "deployments", DisplayName: "Deployments", Enabled: true, Expression: `approve("unrestricted")`,
	})
	if err != nil {
		t.Fatal(err)
	}
	author, err := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author", Groups: []string{"ops"}})
	if err != nil {
		t.Fatal(err)
	}

	first := assessedFoundation()
	original, _, err := service.ProposePlan(ctx, first, author, "First", "")
	if err != nil {
		t.Fatal(err)
	}
	// A newer Plan supersedes it, which is what cancels it.
	second := assessedFoundation()
	second.Environment.Generation = "00112233445566ff"
	if _, _, err = service.ProposePlan(ctx, second, author, "Second", ""); err != nil {
		t.Fatal(err)
	}
	superseded, err := repository.GetPlan(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if superseded.Status != PlanCancelled || superseded.Cancellation == nil {
		t.Fatalf("the first Plan was not superseded, so there is nothing to resurrect: %#v", superseded)
	}

	// Deployments are frozen from here on.
	policy.Expression = `reject("deployments are frozen")`
	if _, err = service.PutPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	// Control: the freeze holds for desired state nobody has proposed before.
	third := assessedFoundation()
	third.Environment.Generation = "00112233445566aa"
	fresh, freshRequest, err := service.ProposePlan(ctx, third, author, "Third", "")
	if err != nil {
		t.Fatal(err)
	}
	if freshRequest.Status != ChangeRejected || fresh.Status == PlanApproved {
		t.Fatalf("the freeze did not hold for fresh state: request=%s plan=%s", freshRequest.Status, fresh.Status)
	}

	if _, _, err = service.ProposePlan(ctx, first, author, "First again", ""); err != nil {
		t.Fatal(err)
	}
	resurrected, err := repository.GetPlan(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resurrected.Status == PlanApproved || resurrected.Cancellation == nil {
		t.Fatalf("a cancelled Plan was approved again under a policy that rejects it: status=%s cancellation=%#v",
			resurrected.Status, resurrected.Cancellation)
	}
}
