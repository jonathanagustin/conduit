package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestLabelDefinitionProtectsLegacyReferencesAndPersistsTombstoneHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "state")
	repository, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := NewLifecycle(repository)
	state := foundation()
	state.Environment.Labels = map[string]string{"compliance": "standard"}
	if _, err := lifecycle.CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	definition := testLabelDefinition("compliance", []string{"standard", "high"}, 1, now)
	if _, err := repository.PutLabelDefinition(ctx, definition); err != nil {
		t.Fatalf("migrate legacy label: %v", err)
	}

	removedValue := definition
	removedValue.AcceptedValues, removedValue.Revision, removedValue.UpdatedAt, removedValue.ChangeRequestID = []string{"high"}, 2, now.Add(time.Hour), "crq_update"
	if _, err := repository.PutLabelDefinition(ctx, removedValue); !errors.Is(err, ErrConflict) {
		t.Fatalf("referenced value removal error = %v", err)
	}
	tombstone := definition
	tombstone.Status, tombstone.Revision, tombstone.UpdatedAt, tombstone.ChangeRequestID = LabelDefinitionTombstoned, 2, now.Add(time.Hour), "crq_tombstone"
	if _, err := repository.PutLabelDefinition(ctx, tombstone); !errors.Is(err, ErrConflict) {
		t.Fatalf("referenced tombstone error = %v", err)
	}

	state.Environment.Generation = "labels-removed"
	state.Environment.Labels = nil
	if _, err := lifecycle.CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PutLabelDefinition(ctx, tombstone); err != nil {
		t.Fatal(err)
	}
	state.Environment.Generation = "label-reuse"
	state.Environment.Labels = map[string]string{"compliance": "standard"}
	if _, err := lifecycle.CreatePlan(ctx, state); !errors.Is(err, ErrConflict) {
		t.Fatalf("tombstoned label reuse error = %v", err)
	}

	reopened, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := reopened.ListLabelDefinitionRevisions(ctx, "compliance")
	if err != nil || len(revisions) != 2 || revisions[0].Status != LabelDefinitionActive || revisions[1].Status != LabelDefinitionTombstoned {
		t.Fatalf("definition revisions = %#v, %v", revisions, err)
	}
}

func TestDefinedLabelRequiresContributorAndTargetWriteAuthorization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	if _, err := repository.PutLabelDefinition(ctx, testLabelDefinition("compliance", []string{"standard"}, 1, now)); err != nil {
		t.Fatal(err)
	}
	governance := NewGovernanceService(repository)
	for _, team := range []Team{
		{Name: "editors", DisplayName: "Editors", AdminUsers: []string{"editor"}, MemberUsers: []string{"editor"}},
		{Name: "contributors", DisplayName: "Contributors", AdminUsers: []string{"contributor"}, MemberUsers: []string{"contributor"}},
	} {
		if _, err := governance.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	for _, grant := range []ResourceGrant{
		{ResourceRID: environmentRID("dev-use2"), Team: "editors", Roles: []string{"environment:editor"}},
		{ResourceRID: "label/compliance", Team: "contributors", Roles: []string{"label:contributor"}},
	} {
		if _, err := governance.PutGrant(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutPolicy(ctx, ApprovalPolicy{ID: "plans", DisplayName: "Plans", Enabled: true, Expression: `approve("test")`}); err != nil {
		t.Fatal(err)
	}
	state := foundation()
	state.Environment.Labels = map[string]string{"compliance": "standard"}
	if _, _, err := governance.ProposePlan(ctx, state, Principal{Username: "editor", Teams: []string{"editors"}}, "", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("target-only label proposal error = %v", err)
	}
	principal := Principal{Username: "operator", Teams: []string{"editors", "contributors"}}
	if _, request, err := governance.ProposePlan(ctx, state, principal, "", ""); err != nil || request.Status != ChangeApproved {
		t.Fatalf("dual-authorized proposal = %#v, %v", request, err)
	}
	state.Environment.Generation = "invalid-label-value"
	state.Environment.Labels["compliance"] = "unknown"
	if _, _, err := governance.ProposePlan(ctx, state, principal, "", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("invalid accepted value error = %v", err)
	}
}

func TestLabelDefinitionHTTPUsesCreatorAdminAndGovernance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	governance := NewGovernanceService(repository)
	for _, team := range []Team{
		{Name: "creators", DisplayName: "Creators", AdminUsers: []string{"creator"}, MemberUsers: []string{"creator"}},
		{Name: "viewers", DisplayName: "Viewers", AdminUsers: []string{"viewer"}, MemberUsers: []string{"viewer"}},
	} {
		if _, err := governance.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	for _, grant := range []ResourceGrant{
		{ResourceRID: "global/label", Team: "creators", Roles: []string{"label:creator"}},
		{ResourceRID: "global/label", Team: "viewers", Roles: []string{"label:viewer"}},
	} {
		if _, err := governance.PutGrant(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutPolicy(ctx, ApprovalPolicy{ID: "labels", DisplayName: "Labels", Enabled: true, Expression: `type.startsWith("CREATE_LABEL") || type.startsWith("UPDATE_LABEL") || type.startsWith("TOMBSTONE_LABEL") ? approve("govern label") : no_op()`}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	body := LabelDefinition{Description: "Compliance tier", SuggestedResourceTypes: []string{"environment", "team"}, AcceptedValues: []string{"standard", "high"}, Rationale: "create controlled vocabulary"}
	denied := governanceRequest(t, handler, http.MethodPut, "/v1/labels/compliance", "viewer", "", body)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("viewer create = %d %s", denied.Code, denied.Body.String())
	}
	body.ID = "other"
	mismatched := governanceRequest(t, handler, http.MethodPut, "/v1/labels/compliance", "creator", "", body)
	if mismatched.Code != http.StatusUnprocessableEntity {
		t.Fatalf("mutable label id = %d %s", mismatched.Code, mismatched.Body.String())
	}
	body.ID = ""
	response := governanceRequest(t, handler, http.MethodPut, "/v1/labels/compliance", "creator", "", body)
	if response.Code != http.StatusCreated {
		t.Fatalf("creator create = %d %s", response.Code, response.Body.String())
	}
	var result struct {
		ChangeRequest ChangeRequest   `json:"changeRequest"`
		Definition    LabelDefinition `json:"definition"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Definition.Revision != 1 || result.ChangeRequest.Execution == nil || result.ChangeRequest.Execution.Status != ExecutionSucceeded {
		t.Fatalf("definition response = %#v, %v", result, err)
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{ResourceRID: "label/compliance", Team: "creators", Roles: []string{"label:administrator"}}); err != nil {
		t.Fatal(err)
	}
	body.Description, body.Rationale = "Updated compliance tier", "clarify usage"
	response = governanceRequest(t, handler, http.MethodPut, "/v1/labels/compliance", "creator", "", body)
	if response.Code != http.StatusCreated {
		t.Fatalf("administrator update = %d %s", response.Code, response.Body.String())
	}
	stored, err := repository.GetLabelDefinition(ctx, "compliance")
	if err != nil || stored.Revision != 2 || stored.Description != body.Description || stored.ID != "compliance" {
		t.Fatalf("updated definition = %#v, %v", stored, err)
	}
	response = governanceRequest(t, handler, http.MethodDelete, "/v1/labels/compliance", "creator", "", map[string]string{"rationale": "retire vocabulary"})
	if response.Code != http.StatusCreated {
		t.Fatalf("administrator tombstone = %d %s", response.Code, response.Body.String())
	}
	stored, err = repository.GetLabelDefinition(ctx, "compliance")
	if err != nil || stored.Revision != 3 || stored.Status != LabelDefinitionTombstoned {
		t.Fatalf("tombstoned definition = %#v, %v", stored, err)
	}
	list := governanceRequest(t, handler, http.MethodGet, "/v1/labels", "creator", "", nil)
	if list.Code != http.StatusOK || list.Body.String() != "{\"definitions\":[]}\n" {
		t.Fatalf("active definition list = %d %s", list.Code, list.Body.String())
	}
	reuse := governanceRequest(t, handler, http.MethodPut, "/v1/labels/compliance", "creator", "", body)
	if reuse.Code != http.StatusConflict {
		t.Fatalf("tombstoned identifier reuse = %d %s", reuse.Code, reuse.Body.String())
	}
}

func TestLabelDefinitionAppliesOnlyAfterRequiredReview(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	governance := NewGovernanceService(repository)
	for _, team := range []Team{
		{Name: "creators", DisplayName: "Creators", AdminUsers: []string{"creator"}, MemberUsers: []string{"creator"}},
		{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}},
	} {
		if _, err := governance.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{
		ResourceRID: "global/label", Team: "creators", Roles: []string{"label:creator"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutPolicy(ctx, ApprovalPolicy{
		ID: "label-review", DisplayName: "Label review", Enabled: true,
		Expression: `type == "CREATE_LABEL_DEFINITION" ? require_team("reviewers", "review controlled vocabulary") : no_op()`,
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := governanceRequest(t, handler, http.MethodPut, "/v1/labels/compliance", "creator", "", LabelDefinition{
		Description: "Compliance tier", SuggestedResourceTypes: []string{"environment"}, AcceptedValues: []string{"standard"},
		Rationale: "create controlled vocabulary",
	})
	var proposed struct {
		ChangeRequest ChangeRequest `json:"changeRequest"`
	}
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &proposed) != nil ||
		proposed.ChangeRequest.Status != ChangePendingApprovals {
		t.Fatalf("pending definition = %d %#v %s", response.Code, proposed, response.Body.String())
	}
	if _, err := repository.GetLabelDefinition(ctx, "compliance"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pending definition was applied: %v", err)
	}
	alternateResponse := governanceRequest(t, handler, http.MethodPut, "/v1/labels/compliance", "creator", "", LabelDefinition{
		Description: "Alternate compliance tier", SuggestedResourceTypes: []string{"environment"}, AcceptedValues: []string{"standard", "high"},
		Rationale: "alternate controlled vocabulary",
	})
	var alternate struct {
		ChangeRequest ChangeRequest `json:"changeRequest"`
	}
	if alternateResponse.Code != http.StatusCreated || json.Unmarshal(alternateResponse.Body.Bytes(), &alternate) != nil ||
		alternate.ChangeRequest.ID == proposed.ChangeRequest.ID {
		t.Fatalf("content-addressed alternate definition = %d %#v %s", alternateResponse.Code, alternate, alternateResponse.Body.String())
	}
	approved := governanceRequest(t, handler, http.MethodPost, "/v1/change-requests/"+proposed.ChangeRequest.ID+"/approve", "reviewer", "", map[string]string{"rationale": "approved"})
	if approved.Code != http.StatusOK {
		t.Fatalf("definition approval = %d %s", approved.Code, approved.Body.String())
	}
	stored, err := repository.GetLabelDefinition(ctx, "compliance")
	if err != nil || stored.ChangeRequestID != proposed.ChangeRequest.ID {
		t.Fatalf("approved definition = %#v, %v", stored, err)
	}
	request, err := repository.GetChangeRequest(ctx, proposed.ChangeRequest.ID)
	if err != nil || request.Execution == nil || request.Execution.Status != ExecutionSucceeded ||
		request.Execution.Items[0].ResultID != "label-definition/compliance/1" {
		t.Fatalf("definition execution = %#v, %v", request.Execution, err)
	}
	stale := governanceRequest(t, handler, http.MethodPost, "/v1/change-requests/"+alternate.ChangeRequest.ID+"/approve", "reviewer", "", map[string]string{"rationale": "approve alternate"})
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale competing definition = %d %s", stale.Code, stale.Body.String())
	}
	if current, err := repository.GetLabelDefinition(ctx, "compliance"); err != nil || current.Description != "Compliance tier" {
		t.Fatalf("stale proposal changed definition = %#v, %v", current, err)
	}
}

func TestLabelDefinitionProtectsTeamAndPipelineReferences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	definition := testLabelDefinition("compliance", []string{"standard", "high"}, 1, now)
	if _, err := repository.PutLabelDefinition(ctx, definition); err != nil {
		t.Fatal(err)
	}
	governance := NewGovernanceService(repository)
	team, err := governance.PutTeam(ctx, Team{
		Name: "operators", DisplayName: "Operators", AdminUsers: []string{"admin"}, MemberUsers: []string{"member"},
		Labels: map[string]string{"compliance": "standard"},
	})
	if err != nil {
		t.Fatal(err)
	}
	invalidTeam := team
	invalidTeam.Labels = map[string]string{"compliance": "unknown"}
	if _, err := governance.PutTeam(ctx, invalidTeam); !errors.Is(err, ErrConflict) {
		t.Fatalf("invalid Team label value error = %v", err)
	}
	removedValue := definition
	removedValue.AcceptedValues, removedValue.Revision, removedValue.UpdatedAt, removedValue.ChangeRequestID = []string{"high"}, 2, now.Add(time.Hour), "crq_update"
	if _, err := repository.PutLabelDefinition(ctx, removedValue); !errors.Is(err, ErrConflict) {
		t.Fatalf("Team reference value removal error = %v", err)
	}
	team.Labels = nil
	if _, err := governance.PutTeam(ctx, team); err != nil {
		t.Fatal(err)
	}
	bundles, err := NewBundleService(repository, mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	pipeline, err := bundles.PutPipeline(ctx, ExportPipeline{
		ID: "compliance-export", DisplayName: "Compliance export", BuildStrategy: BundleStrategySnapshot,
		Selections:     []BundleSelectionRule{{Type: BundleSelectEnvironment, IDs: []string{"dev-use2"}, ReleaseStrategy: BundleReleasesAll}},
		RequiredLabels: []BundleLabelRequirement{{ResourceTypes: []string{BundleLabelEnvironment}, Name: "compliance", Values: []string{"standard"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tombstone := definition
	tombstone.Status, tombstone.Revision, tombstone.UpdatedAt, tombstone.ChangeRequestID = LabelDefinitionTombstoned, 2, now.Add(time.Hour), "crq_tombstone"
	if _, err := repository.PutLabelDefinition(ctx, tombstone); !errors.Is(err, ErrConflict) {
		t.Fatalf("Pipeline reference tombstone error = %v", err)
	}
	pipeline.RequiredLabels = nil
	if _, err := bundles.PutPipeline(ctx, pipeline); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PutLabelDefinition(ctx, tombstone); err != nil {
		t.Fatal(err)
	}
}

func TestTeamLabelMutationRequiresLabelContributor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	if _, err := repository.PutLabelDefinition(ctx, testLabelDefinition("compliance", []string{"standard"}, 1, now)); err != nil {
		t.Fatal(err)
	}
	governance := NewGovernanceService(repository)
	target, err := governance.PutTeam(ctx, Team{
		Name: "target", DisplayName: "Target", AdminUsers: []string{"admin"}, MemberUsers: []string{"member"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutTeam(ctx, Team{
		Name: "contributors", DisplayName: "Contributors", AdminUsers: []string{"admin"}, MemberUsers: []string{"admin"},
	}); err != nil {
		t.Fatal(err)
	}
	target.Labels = map[string]string{"compliance": "standard"}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	denied := governanceRequest(t, handler, http.MethodPut, "/v1/teams/target", "admin", "", target)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("Team administrator without label contributor = %d %s", denied.Code, denied.Body.String())
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{
		ResourceRID: "label/compliance", Team: "contributors", Roles: []string{"label:contributor"},
	}); err != nil {
		t.Fatal(err)
	}
	allowed := governanceRequest(t, handler, http.MethodPut, "/v1/teams/target", "admin", "", target)
	if allowed.Code != http.StatusOK {
		t.Fatalf("dual-authorized Team label mutation = %d %s", allowed.Code, allowed.Body.String())
	}
}

func testLabelDefinition(id string, values []string, revision uint64, now time.Time) LabelDefinition {
	return LabelDefinition{ID: id, Description: "Controlled label", SuggestedResourceTypes: []string{"environment"}, AcceptedValues: values,
		Status: LabelDefinitionActive, Rationale: "govern vocabulary", CreatedBy: "admin", UpdatedBy: "admin", ChangeRequestID: "crq_label",
		CreatedAt: now, UpdatedAt: now, Revision: revision}
}
