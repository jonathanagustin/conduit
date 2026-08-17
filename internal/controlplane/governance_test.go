package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGovernedPlanRequiresEveryEntityApprovalAndPersists(t *testing.T) {
	ctx := context.Background()
	repository, err := NewFileRepository(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewGovernanceService(repository)
	if _, err := service.PutTeam(ctx, Team{
		Name: "release-managers", DisplayName: "Release managers",
		AdminGroups: []string{"release-admins"}, MemberGroups: []string{"release-reviewers"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutPolicy(ctx, ApprovalPolicy{
		ID: "release-review", DisplayName: "Release review", Enabled: true,
		Expression: `type == "CREATE_ENTITY" ? require_team("release-managers", "release review") : no_op()`,
	}); err != nil {
		t.Fatal(err)
	}
	author, err := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	plan, request, err := service.ProposePlan(ctx, foundation(), author, "Foundation rollout", "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.ChangeRequestID != request.ID || request.Status != ChangePendingApprovals || len(request.Requirements) != len(plan.Plan.Steps) {
		t.Fatalf("plan=%#v request=%#v", plan, request)
	}
	outsider, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "outsider@example.test"})
	if _, _, err := service.Approve(ctx, request.ID, outsider, "looks good"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider approval error = %v", err)
	}
	reviewer, _ := service.ResolvePrincipal(ctx, IdentityClaims{
		Username: "reviewer@example.test", Groups: []string{"release-reviewers"},
	})
	request, plan, err = service.Approve(ctx, request.ID, reviewer, "verified rollout")
	if err != nil {
		t.Fatal(err)
	}
	if request.Status != ChangeApproved || plan.Status != PlanApproved || !request.RequirementsSatisfied {
		t.Fatalf("plan=%#v request=%#v", plan, request)
	}

	reopened, err := NewFileRepository(filepath.Join(filepath.Dir(repository.root), "state"))
	if err != nil {
		t.Fatal(err)
	}
	stored, err := reopened.GetChangeRequest(ctx, request.ID)
	if err != nil || stored.Status != ChangeApproved || len(stored.Reviews) != 1 {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	events, err := reopened.ListAuthorizationAudit(ctx, request.ID)
	if err != nil || len(events) != 3 || events[0].Categories[0] != AuditCategoryRequestCreate ||
		events[1].Decision != "DENY" || events[2].Decision != "ALLOW" ||
		!contains(events[2].Categories, AuditCategoryRequestApprove) {
		t.Fatalf("events=%#v err=%v", events, err)
	}
}

func TestSecretRotationPlanIsGovernedWithoutProviderDisclosure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	if _, err := service.PutPolicy(ctx, ApprovalPolicy{
		ID: "secret-review", DisplayName: "Secret rotation review", Enabled: true,
		Expression: `approve("fixture approval")`,
	}); err != nil {
		t.Fatal(err)
	}
	state := foundation()
	state.Environment.Secrets = []ExternalSecretAssignment{{
		Name: "database", Revision: 8, ProviderRef: "vault://platform/dev/database",
		KubernetesSecretName: "database-runtime", Keys: []string{"password"},
	}}
	state.Entities[0].SecretRequirements = []SecretRequirement{{
		Name: "database-credentials", Secret: "database", Keys: []string{"password"},
	}}
	_, request, err := service.ProposePlan(ctx, state, Principal{Username: "author@example.test"}, "Rotate database credential", "")
	if err != nil {
		t.Fatal(err)
	}
	if request.Status != ChangeApproved || len(request.Changes) != 2 {
		t.Fatalf("governed rotation request = %#v", request)
	}
	coordinates, ok := request.Changes[1].Request["secretCoordinates"].([]any)
	coordinate, coordinateOK := map[string]any(nil), false
	if ok && len(coordinates) == 1 {
		coordinate, coordinateOK = coordinates[0].(map[string]any)
	}
	if !coordinateOK || coordinate["assignment"] != "database" || coordinate["revision"] != float64(8) {
		t.Fatalf("reviewed secret coordinates = %#v", request.Changes[1].Request["secretCoordinates"])
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("vault://")) || bytes.Contains(encoded, []byte("actual-password")) {
		t.Fatalf("Change Request disclosed provider or secret material: %s", encoded)
	}
}

func TestGovernanceHTTPWorkflow(t *testing.T) {
	repository := NewMemoryRepository()
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		BootstrapAdministrators:              []string{"admin@example.test"},
	})
	response := governanceRequest(t, handler, http.MethodPut, "/v1/teams/release-managers", "admin@example.test", "", Team{
		DisplayName: "Release managers", AdminUsers: []string{"admin@example.test"}, MemberGroups: []string{"reviewers"},
		MemberUsers: []string{"author@example.test"},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("team = %d %s", response.Code, response.Body.String())
	}
	response = governanceRequest(t, handler, http.MethodPost, "/v1/resource-grants", "admin@example.test", "", ResourceGrant{
		ResourceRID: environmentRID("dev-use2"), Team: "release-managers",
		Operations: []string{OperationProposeChange, OperationReadResource},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("grant = %d %s", response.Code, response.Body.String())
	}
	response = governanceRequest(t, handler, http.MethodPut, "/v1/approval-policies/release-review", "admin@example.test", "", ApprovalPolicy{
		DisplayName: "Release review", Enabled: true,
		Expression: `require_team("release-managers", "release review")`,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("policy = %d %s", response.Code, response.Body.String())
	}
	response = governanceRequest(t, handler, http.MethodPost, "/v1/plans", "author@example.test", "", foundation())
	if response.Code != http.StatusCreated {
		t.Fatalf("plan = %d %s", response.Code, response.Body.String())
	}
	changeRequestID := response.Header().Get("X-Change-Request-ID")
	if changeRequestID == "" {
		t.Fatal("missing change request header")
	}
	response = governanceRequest(t, handler, http.MethodPost, "/v1/change-requests/"+changeRequestID+"/approve", "reviewer@example.test", "reviewers", map[string]string{"rationale": "verified"})
	if response.Code != http.StatusOK {
		t.Fatalf("approve = %d %s", response.Code, response.Body.String())
	}
	var result struct {
		ChangeRequest ChangeRequest `json:"changeRequest"`
		Plan          PlanRecord    `json:"plan"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ChangeRequest.Status != ChangeApproved || result.Plan.Status != PlanApproved {
		t.Fatalf("result=%#v", result)
	}

	unauthenticated := NewHandler(NewMemoryRepository())
	response = governanceRequest(t, unauthenticated, http.MethodGet, "/v1/change-requests", "", "", nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d %s", response.Code, response.Body.String())
	}
}

func TestRoleAuthorizationScopesInheritanceAndLabelDualControl(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	for _, team := range []Team{
		{Name: "environment-viewers", DisplayName: "Environment viewers", AdminGroups: []string{"viewers"}, MemberGroups: []string{"viewers"}},
		{Name: "environment-operators", DisplayName: "Environment operators", AdminGroups: []string{"operators"}, MemberGroups: []string{"operators"}},
		{Name: "labelers", DisplayName: "Label contributors", AdminGroups: []string{"labelers"}, MemberGroups: []string{"labelers"}},
		{Name: "environment-admins", DisplayName: "Environment admins", AdminGroups: []string{"admins"}, MemberGroups: []string{"admins"}},
		{Name: "restricted-release", DisplayName: "Restricted contributors", AdminGroups: []string{"release-owners"}, MemberGroups: []string{"release-owners"}},
	} {
		if _, err := service.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	grants := []ResourceGrant{
		{ResourceRID: "global/environment", Team: "environment-viewers", Roles: []string{"environment:viewer"}},
		{ResourceRID: environmentRID("dev-use2"), Team: "environment-operators", Roles: []string{"environment:operator"}},
		{ResourceRID: "label/secure", Team: "labelers", Roles: []string{"label:contributor"}},
		{ResourceRID: environmentRID("dev-use2"), Team: "environment-admins", Roles: []string{"environment:administrator"}},
		{ResourceRID: "release-channel/STAGING", Team: "restricted-release", Roles: []string{"release-channel:restricted-contributor"}},
		{ResourceRID: productRID("app.example:example-api"), Team: "restricted-release", Relationship: RelationshipOwner},
	}
	for _, grant := range grants {
		if _, err := service.PutGrant(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	viewer, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "viewer", Groups: []string{"viewers"}})
	if !service.Authorize(ctx, viewer, environmentRID("any-environment"), OperationReadResource) ||
		!service.Authorize(ctx, viewer, installationRID("any-environment", "entity"), OperationReadResource) ||
		service.Authorize(ctx, viewer, environmentRID("any-environment"), OperationOperate) {
		t.Fatalf("global viewer authorization is not least-privilege")
	}
	operator, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "operator", Groups: []string{"operators", "labelers"}})
	if !service.Authorize(ctx, operator, installationRID("dev-use2", "example-api"), OperationOperate) {
		t.Fatal("Environment operation did not additively inherit to its Entity")
	}
	if !service.AuthorizeLabelApplication(ctx, operator, "secure", environmentRID("dev-use2"), OperationOperate) {
		t.Fatal("dual-authorized label application was rejected")
	}
	labeler, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "labeler", Groups: []string{"labelers"}})
	if service.AuthorizeLabelApplication(ctx, labeler, "secure", environmentRID("dev-use2"), OperationOperate) {
		t.Fatal("label contribution bypassed target-resource write authorization")
	}
	admin, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "admin", Groups: []string{"admins"}})
	if !service.Authorize(ctx, admin, installationRID("dev-use2", "example-api"), OperationManagePermissions) {
		t.Fatal("Environment administrator could not manage additive Entity grants")
	}
	if _, err := service.PutGrant(ctx, ResourceGrant{
		ResourceRID: environmentRID("dev-use2"), Team: "labelers", Roles: []string{"product:administrator"},
	}); err == nil {
		t.Fatal("canonical Product role was accepted on an Environment")
	}
	restricted, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "release-owner", Groups: []string{"release-owners"}})
	if !service.AuthorizeReleaseChannelContribution(ctx, restricted, "STAGING", "app.example:example-api") ||
		service.AuthorizeReleaseChannelContribution(ctx, restricted, "STAGING", "app.example:other") {
		t.Fatal("restricted Release Channel contribution did not enforce Product ownership")
	}
}

func TestPermissionAdministratorRoleCanManageGrants(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"bootstrap"},
	})
	response := governanceRequest(t, handler, http.MethodPut, "/v1/teams/permission-admins", "bootstrap", "", Team{
		DisplayName: "Permission admins", AdminUsers: []string{"bootstrap"}, MemberUsers: []string{"permissions-user"},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("team = %d %s", response.Code, response.Body.String())
	}
	response = governanceRequest(t, handler, http.MethodPost, "/v1/resource-grants", "bootstrap", "", ResourceGrant{
		ResourceRID: "platform/root", Team: "permission-admins", Roles: []string{"permissions:administrator"},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("bootstrap grant = %d %s", response.Code, response.Body.String())
	}
	response = governanceRequest(t, handler, http.MethodPost, "/v1/resource-grants", "permissions-user", "", ResourceGrant{
		ResourceRID: "global/environment", Team: "permission-admins", Roles: []string{"environment:viewer"},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("delegated permission administration = %d %s", response.Code, response.Body.String())
	}
}

func TestCatalogAndArtifactRoutesRejectUnauthenticatedRequests(t *testing.T) {
	handler := NewHandler(NewMemoryRepository())
	requests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/v1/artifacts/blobs", "artifact"},
		{http.MethodPut, "/v1/environments/private/profile", `{}`},
		{http.MethodGet, "/v1/environments/private/profile", ""},
		{http.MethodGet, "/v1/environments/private/profile/revisions", ""},
		{http.MethodPut, "/v1/export-pipelines/private", `{}`},
		{http.MethodPost, "/v1/export-pipelines/private/build", ""},
		{http.MethodPost, "/v1/export-pipelines/private/preview", ""},
		{http.MethodGet, "/v1/export-pipelines/private/schedule-runs", ""},
		{http.MethodPost, "/v1/modules/releases", `{"definitions":[]}`},
		{http.MethodPost, "/v1/promotions/evaluations", `{}`},
		{http.MethodPost, "/v1/products/app.example:example-api/promotion-stages/DEV/RELEASE/pause", `{"rationale":"hold"}`},
		{http.MethodPost, "/v1/products/app.example:example-api/promotion-stages/DEV/RELEASE/resume", `{"rationale":"clear"}`},
		{http.MethodGet, "/v1/products/app.example:example-api/promotion-stages/DEV/RELEASE", ""},
		{http.MethodPost, "/v1/products/app.example:example-api/retire", `{"rationale":"obsolete"}`},
		{http.MethodPost, "/v1/products/app.example:example-api/releases/1.2.3/retire", `{"rationale":"obsolete"}`},
		{http.MethodGet, "/v1/catalog-retirements", ""},
		{http.MethodPost, "/v1/plans/pln_private/undo-unmanage", `{"reason":"undo"}`},
		{http.MethodPut, "/v1/labels/private", `{}`},
		{http.MethodDelete, "/v1/labels/private", `{"rationale":"retire"}`},
		{http.MethodGet, "/v1/labels", ""},
		{http.MethodGet, "/v1/labels/private", ""},
		{http.MethodGet, "/v1/labels/private/revisions", ""},
		{http.MethodPut, "/v1/modules/example/auto-update-policy", `{"enabled":true,"rationale":"approved"}`},
		{http.MethodGet, "/v1/modules/example/auto-update-policy", ""},
		{http.MethodPost, "/v1/module-installations/modi_example/update/automatic", ""},
	}
	for _, item := range requests {
		request := httptest.NewRequest(item.method, item.path, bytes.NewBufferString(item.body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d %s", item.method, item.path, response.Code, response.Body.String())
		}
	}
}

func governanceRequest(t *testing.T, handler http.Handler, method, path, subject, groups string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	if subject != "" {
		request.Header.Set("X-Platform-Subject", subject)
	}
	if groups != "" {
		request.Header.Set("X-Platform-Groups", groups)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestCELPolicyRequireAnyAndDoNotMerge(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	for _, team := range []Team{
		{Name: "security", DisplayName: "Security", AdminUsers: []string{"sec@example.test"}, MemberUsers: []string{"sec@example.test"}},
		{Name: "operations", DisplayName: "Operations", AdminGroups: []string{"ops"}, MemberGroups: []string{"ops"}},
	} {
		if _, err := service.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.PutPolicy(ctx, ApprovalPolicy{
		ID: "security-or-ops", DisplayName: "Security or operations", Enabled: true,
		Expression: `require_any([require_team("security", "security"), require_team("operations", "operations")], "one trusted reviewer")`,
	}); err != nil {
		t.Fatal(err)
	}
	author, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author@example.test"})
	_, request, err := service.ProposePlan(ctx, foundation(), author, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if request.Status == ChangeError {
		t.Fatalf("policy evaluation failed: %s (%#v)", request.Error, request.Evaluations)
	}
	request, _, approvedByLabelling, err := service.SetLabels(ctx, request.ID, author, []string{"Do not merge"})
	if err != nil {
		t.Fatal(err)
	}
	if approvedByLabelling {
		t.Fatal("labelling a pending request reported that it approved it")
	}
	reviewer, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "operator@example.test", Groups: []string{"ops"}})
	request, plan, err := service.Approve(ctx, request.ID, reviewer, "approved")
	if err != nil {
		t.Fatal(err)
	}
	if !request.RequirementsSatisfied || request.Status != ChangePendingApprovals || plan.Status != PlanAwaitingApproval {
		t.Fatalf("Do not merge was not honored: request=%#v plan=%#v", request, plan)
	}
	request, plan, approvedByLabelling, err = service.SetLabels(ctx, request.ID, author, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Removing a do-not-merge label from a request whose requirements are
	// already satisfied IS the approval, and has to be reported as one --
	// otherwise the caller that applies approved changes would stop applying
	// this one, which is the legitimate case the replay guard must not eat.
	if !approvedByLabelling {
		t.Fatal("removing do-not-merge approved the request but was not reported as approving it")
	}
	if request.Status != ChangeApproved || plan.Status != PlanApproved {
		t.Fatalf("request=%#v plan=%#v", request, plan)
	}
}

func TestChangeRequestRejectCancelAndReopen(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	_, _ = service.PutTeam(ctx, Team{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}})
	_, _ = service.PutPolicy(ctx, ApprovalPolicy{ID: "review", DisplayName: "Review", Enabled: true, Expression: `require_team("reviewers", "review")`})
	author, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author"})
	reviewer, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "reviewer"})
	_, request, err := service.ProposePlan(ctx, foundation(), author, "", "")
	if err != nil {
		t.Fatal(err)
	}
	request, plan, err := service.Reject(ctx, request.ID, reviewer, "unsafe rollout")
	if err != nil || request.Status != ChangeRejected || plan.Status != PlanAwaitingApproval {
		t.Fatalf("request=%#v plan=%#v err=%v", request, plan, err)
	}
	request, plan, err = service.Reopen(ctx, request.ID, author)
	if err != nil || request.Status != ChangePendingApprovals || len(request.Reviews) != 0 {
		t.Fatalf("request=%#v plan=%#v err=%v", request, plan, err)
	}
	plan, err = NewLifecycle(repository).CancelPlan(ctx, plan.ID, author.Username, "superseded")
	if err != nil {
		t.Fatal(err)
	}
	request, err = service.Cancel(ctx, request.ID, author, "superseded")
	if err != nil || request.Status != ChangeCancelled {
		t.Fatalf("request=%#v err=%v", request, err)
	}
	request, plan, err = service.Reopen(ctx, request.ID, author)
	if err != nil || request.Status != ChangePendingApprovals || plan.Status != PlanAwaitingApproval || plan.Cancellation != nil {
		t.Fatalf("request=%#v plan=%#v err=%v", request, plan, err)
	}
}

func TestCELRoleAndOperationRequirementsUseResourceGrants(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	_, err := service.PutTeam(ctx, Team{Name: "environment-operators", DisplayName: "Environment operators", AdminGroups: []string{"operators"}, MemberGroups: []string{"operators"}})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.PutGrant(ctx, ResourceGrant{
		ResourceRID: environmentRID("dev-use2"), Team: "environment-operators", Relationship: RelationshipOwner,
		Roles: []string{"environment:operator"}, Operations: []string{"environment:approve-change"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if grant.Relationship != RelationshipOwner {
		t.Fatalf("grant=%#v", grant)
	}
	for _, policy := range []ApprovalPolicy{
		{ID: "operator-role", DisplayName: "Operator role", Enabled: true, Expression: `require_role(referencedResources.environment.rid, "environment:operator", "operator role")`},
		{ID: "approval-operation", DisplayName: "Approval operation", Enabled: true, Expression: `require_operation(referencedResources.environment.rid, "environment:approve-change", "approval operation")`},
	} {
		if _, err := service.PutPolicy(ctx, policy); err != nil {
			t.Fatal(err)
		}
	}
	author, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author"})
	_, request, err := service.ProposePlan(ctx, foundation(), author, "", "")
	if err != nil {
		t.Fatal(err)
	}
	reviewer, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "operator", Groups: []string{"operators"}})
	request, plan, err := service.Approve(ctx, request.ID, reviewer, "authorized for both")
	if err != nil || request.Status != ChangeApproved || plan.Status != PlanApproved {
		t.Fatalf("request=%#v plan=%#v err=%v", request, plan, err)
	}
	for _, change := range request.Changes {
		owners, ok := change.ReferencedResources["environment"].Resource["ownerTeams"].([]any)
		if !ok || len(owners) != 1 || owners[0] != "environment-operators" {
			t.Fatalf("resource=%#v", change.ReferencedResources["environment"].Resource)
		}
	}
}

func TestPolicyRejectionNoOpAndRuntimeErrorStatuses(t *testing.T) {
	for _, test := range []struct{ name, expression, wantStatus string }{
		{name: "reject", expression: `reject("policy rejected change")`, wantStatus: ChangeRejected},
		{name: "all no-op", expression: `no_op()`, wantStatus: ChangeError},
		{name: "runtime error", expression: `1 / 0`, wantStatus: ChangeError},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repository := NewMemoryRepository()
			service := NewGovernanceService(repository)
			if _, err := service.PutPolicy(ctx, ApprovalPolicy{ID: "policy", DisplayName: "Policy", Enabled: true, Expression: test.expression}); err != nil {
				t.Fatal(err)
			}
			author, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author"})
			plan, request, err := service.ProposePlan(ctx, foundation(), author, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if request.Status != test.wantStatus || plan.Status != PlanAwaitingApproval || request.Error == "" {
				t.Fatalf("request=%#v plan=%#v", request, plan)
			}
		})
	}
}

func TestServiceAccountDelegationRequiresGrantAndPopulatesCEL(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	for _, team := range []Team{
		{Name: "automation", DisplayName: "Automation", AdminUsers: []string{"delivery-bot"}, MemberUsers: []string{"delivery-bot"}},
		{Name: "developers", DisplayName: "Developers", AdminUsers: []string{"developer"}, MemberUsers: []string{"developer"}},
	} {
		if _, err := service.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.PutGrant(ctx, ResourceGrant{ResourceRID: "platform/root", Team: "automation", Operations: []string{OperationActOnBehalf}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutGrant(ctx, ResourceGrant{ResourceRID: environmentRID("dev-use2"), Team: "developers", Operations: []string{OperationProposeChange, OperationReadResource}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutPolicy(ctx, ApprovalPolicy{ID: "delegated", DisplayName: "Delegated", Enabled: true, Expression: `onBehalfOf.username == "developer" ? approve("delegated author accepted") : reject("delegation required")`}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	body, err := json.Marshal(foundation())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/plans", bytes.NewReader(body))
	request.Header.Set("X-Platform-Subject", "delivery-bot")
	request.Header.Set("X-Platform-Service-Account", "true")
	request.Header.Set("X-Platform-On-Behalf-Of", "developer")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("delegated plan = %d %s", response.Code, response.Body.String())
	}
	stored, err := repository.GetChangeRequest(ctx, response.Header().Get("X-Change-Request-ID"))
	if err != nil || stored.Author.Username != "delivery-bot" || stored.Author.OnBehalfOf == nil || stored.Author.OnBehalfOf.Username != "developer" || stored.Status != ChangeApproved {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}

	request = httptest.NewRequest(http.MethodPost, "/v1/plans", bytes.NewReader(body))
	request.Header.Set("X-Platform-Subject", "delivery-bot")
	request.Header.Set("X-Platform-Service-Account", "true")
	request.Header.Set("X-Platform-On-Behalf-Of", "unauthorized")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unauthorized delegation = %d %s", response.Code, response.Body.String())
	}
}

// The arity guards on Teams and Grants. Each adds two counts and compares the sum
// to zero, which is only the same as "neither is present" while it stays a sum:
// a difference reads one administrator user and one administrator group as none.
func TestTeamAndGrantArityGuardsCountRatherThanCancel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service := NewGovernanceService(NewMemoryRepository())

	if _, err := service.PutTeam(ctx, Team{
		Name: "split-team", DisplayName: "Split team",
		AdminUsers: []string{"admin"}, AdminGroups: []string{"admin-group"},
		MemberUsers: []string{"member"}, MemberGroups: []string{"member-group"},
	}); err != nil {
		t.Errorf("a Team with one administrator of each kind was refused: %v", err)
	}
	for _, missing := range []Team{
		{Name: "no-admin", DisplayName: "No admin", MemberUsers: []string{"member"}},
		{Name: "no-member", DisplayName: "No member", AdminUsers: []string{"admin"}},
	} {
		if _, err := service.PutTeam(ctx, missing); err == nil {
			t.Errorf("Team %s was accepted without both an administrator and a member", missing.Name)
		}
	}

	if _, err := service.PutTeam(ctx, Team{
		Name: "grantees", DisplayName: "Grantees",
		AdminUsers: []string{"admin"}, MemberUsers: []string{"member"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutGrant(ctx, ResourceGrant{
		ResourceRID: "ri.platform.main.product.example", Team: "grantees",
		Roles: []string{"viewer"}, Operations: []string{"read"},
	}); err != nil {
		t.Errorf("a Grant carrying one role and one operation was refused: %v", err)
	}
	if _, err := service.PutGrant(ctx, ResourceGrant{
		ResourceRID: "ri.platform.main.product.example", Team: "grantees",
	}); err == nil {
		t.Error("a Grant with no relationship, role, or operation was accepted")
	}

	// Both published relationships are accepted, and nothing else is.
	for _, relationship := range []string{RelationshipOwner, RelationshipCoOwner} {
		if _, err := service.PutGrant(ctx, ResourceGrant{
			ResourceRID: "ri.platform.main.product." + relationship, Team: "grantees",
			Relationship: relationship,
		}); err != nil {
			t.Errorf("relationship %s was refused: %v", relationship, err)
		}
	}
	if _, err := service.PutGrant(ctx, ResourceGrant{
		ResourceRID: "ri.platform.main.product.maintainer", Team: "grantees", Relationship: "MAINTAINER",
	}); err == nil {
		t.Error("an unpublished relationship was accepted")
	}
}

// What a change request is called, what the audit trail says about it, and which
// statuses it can be cancelled from. All three are read by people rather than by
// code, which is exactly why nothing had asserted them.
func TestChangeRequestTitleAuditTrailAndCancellableStatuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	_, _ = service.PutTeam(ctx, Team{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}})
	_, _ = service.PutPolicy(ctx, ApprovalPolicy{ID: "review", DisplayName: "Review", Enabled: true, Expression: `require_team("reviewers", "review")`})
	author, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author"})
	reviewer, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "reviewer"})

	// An untitled proposal is named after what it reconciles, so a queue of them
	// is still readable. A titled one keeps the title it was given.
	_, generated, err := service.ProposePlan(ctx, foundation(), author, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if generated.Title == "" || !strings.Contains(generated.Title, generated.Environment) {
		t.Errorf("untitled proposal was not named after its Environment: %q", generated.Title)
	}
	titledState := foundation()
	titledState.Environment.Generation = "fedcba9876543210"
	_, titled, err := service.ProposePlan(ctx, titledState, author, "Roll out the API", "")
	if err != nil {
		t.Fatal(err)
	}
	if titled.Title != "Roll out the API" {
		t.Errorf("a given title was overwritten: %q", titled.Title)
	}

	// Each assertion below names the action it is about. An audit trail carries
	// several entries per request, so "some entry says ALLOW" is satisfied by an
	// entry from a different step -- which is how three of these first passed
	// while proving nothing.
	audited := func(id, action string) []AuthorizationAuditEvent {
		t.Helper()
		events, err := repository.ListAuthorizationAudit(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		var matched []AuthorizationAuditEvent
		for _, event := range events {
			if event.Action == action {
				matched = append(matched, event)
			}
		}
		return matched
	}

	// Creating the request is audited, and only once it has actually persisted.
	created := audited(titled.ID, "CHANGE_REQUEST_CREATE")
	if len(created) != 1 || created[0].Decision != "ALLOW" {
		t.Errorf("creating a change request was audited as %#v", created)
	}

	// A refused operation is audited too, and as DENY: an audit trail that only
	// records what succeeded cannot answer who tried. The two refusals below take
	// different routes -- one is turned away before the write is attempted, the
	// other fails inside it -- and both have to end up in the trail.
	if _, _, err := service.Reject(ctx, titled.ID, author, "not mine to reject"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a non-reviewer rejected a change request: %v", err)
	}
	if events := audited(titled.ID, "CHANGE_REQUEST_REJECT"); len(events) != 1 ||
		events[0].Decision != "DENY" || events[0].Subject != "author" {
		t.Errorf("a rejection by a non-reviewer was audited as %#v", events)
	}
	if _, _, err := service.Reject(ctx, titled.ID, reviewer, "unsafe"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Reject(ctx, titled.ID, reviewer, "again"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("a rejected request was rejected again: %v", err)
	}
	rejections := audited(titled.ID, "CHANGE_REQUEST_REJECT")
	if len(rejections) != 3 || rejections[1].Decision != "ALLOW" || rejections[2].Decision != "DENY" {
		t.Errorf("rejection audit trail = %#v", rejections)
	}

	// Cancellation is a transition, not a free-form write: it is reachable while
	// the request is pending or approved, and refused once already cancelled.
	if _, err := service.Cancel(ctx, generated.ID, author, "superseded"); err != nil {
		t.Fatalf("cancelling a pending request: %v", err)
	}
	if _, err := service.Cancel(ctx, generated.ID, author, "again"); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("cancelling an already-cancelled request = %v", err)
	}
	cancellations := audited(generated.ID, "CHANGE_REQUEST_CANCEL")
	if len(cancellations) != 2 || cancellations[0].Decision != "ALLOW" || cancellations[1].Decision != "DENY" {
		t.Errorf("cancellation audit trail = %#v", cancellations)
	}
	// A change request against something other than a Plan is created by a
	// different entry point, with its own audit call. The Plan path above does not
	// reach it, so the creation audit there proves nothing about this one.
	direct, _, err := service.ProposeChanges(ctx, "EXPORT_PIPELINE_LIFECYCLE_ARCHIVE", "nightly-export", "dev-use2",
		[]EntityChange{{
			ID: "chg-1", Type: ChangeTypeUpdateEntity, InstallationID: "example-api",
			ReferencedResources: map[string]ReferencedResource{
				"environment":  {RID: environmentRID("dev-use2"), Name: "dev-use2"},
				"product":      {RID: productRID("app.example:example-api"), Name: "app.example:example-api"},
				"installation": {RID: installationRID("dev-use2", "example-api"), Name: "example-api"},
			},
		}},
		author, "Archive the nightly export", "no longer needed")
	if err != nil {
		t.Fatal(err)
	}
	if events := audited(direct.ID, "CHANGE_REQUEST_CREATE"); len(events) != 1 || events[0].Decision != "ALLOW" {
		t.Errorf("a non-Plan change request was audited on creation as %#v", events)
	}

	// An approved request is still cancellable, which is a separate branch from
	// the pending one above.
	approvedState := foundation()
	approvedState.Environment.Generation = "00112233445566ff"
	_, approved, err := service.ProposePlan(ctx, approvedState, author, "Approved then withdrawn", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Approve(ctx, approved.ID, reviewer, "looks right"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Cancel(ctx, approved.ID, author, "superseded"); err != nil {
		t.Errorf("cancelling an approved request was refused: %v", err)
	}
}

// The two repository implementations have to agree, and the file-backed one was
// reached almost entirely through the in-memory one's tests. Everything below is
// asserted against both, because a divergence between them is a bug that only
// appears in production -- the in-memory repository is the test fixture, and the
// file repository is what actually runs.
func TestGovernanceRepositoriesAgreeOnRevisionsAndReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fileRoot := filepath.Join(t.TempDir(), "state")
	fileRepository, err := NewFileRepository(fileRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, implementation := range []struct {
		name       string
		repository Repository
	}{
		{"memory", NewMemoryRepository()},
		{"file", fileRepository},
	} {
		t.Run(implementation.name, func(t *testing.T) {
			repository := implementation.repository

			// A first write is revision 1 and nothing else. A second must follow it
			// exactly; the revision is how two writers find out they collided.
			team := Team{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"admin"}, MemberUsers: []string{"member"}, Revision: 1}
			if _, err := repository.PutTeam(ctx, team); err != nil {
				t.Fatalf("first Team write: %v", err)
			}
			if _, err := repository.PutTeam(ctx, team); !errors.Is(err, ErrConflict) {
				t.Errorf("replaying revision 1 = %v", err)
			}
			team.Revision, team.DisplayName = 2, "Reviewers, renamed"
			if _, err := repository.PutTeam(ctx, team); err != nil {
				t.Errorf("second Team write: %v", err)
			}
			team.Revision = 4
			if _, err := repository.PutTeam(ctx, team); !errors.Is(err, ErrConflict) {
				t.Errorf("skipping a revision = %v", err)
			}
			if stored, err := repository.GetTeam(ctx, "reviewers"); err != nil || stored.DisplayName != "Reviewers, renamed" || stored.Revision != 2 {
				t.Errorf("stored Team = %#v, %v", stored, err)
			}
			if _, err := repository.GetTeam(ctx, "no-such-team"); !errors.Is(err, ErrNotFound) {
				t.Errorf("reading an absent Team = %v", err)
			}
			if _, err := repository.GetTeam(ctx, "../../etc/passwd"); !errors.Is(err, ErrNotFound) {
				t.Errorf("reading a traversing name = %v", err)
			}

			// The same contract on Grants and Policies, which have their own copies
			// of it rather than sharing one.
			grant := ResourceGrant{ID: "grn_one", ResourceRID: "ri.platform.main.product.example", Team: "reviewers", Roles: []string{"viewer"}, Revision: 1}
			if _, err := repository.PutResourceGrant(ctx, grant); err != nil {
				t.Fatalf("first Grant write: %v", err)
			}
			if _, err := repository.PutResourceGrant(ctx, grant); !errors.Is(err, ErrConflict) {
				t.Errorf("replaying Grant revision 1 = %v", err)
			}
			grant.Revision = 2
			if _, err := repository.PutResourceGrant(ctx, grant); err != nil {
				t.Errorf("second Grant write: %v", err)
			}

			policy := ApprovalPolicy{ID: "pol_one", DisplayName: "Policy", Enabled: true, Expression: "no_op()", Revision: 1}
			if _, err := repository.PutApprovalPolicy(ctx, policy); err != nil {
				t.Fatalf("first Policy write: %v", err)
			}
			if _, err := repository.PutApprovalPolicy(ctx, policy); !errors.Is(err, ErrConflict) {
				t.Errorf("replaying Policy revision 1 = %v", err)
			}
			policy.Revision, policy.Enabled = 2, false
			if _, err := repository.PutApprovalPolicy(ctx, policy); err != nil {
				t.Errorf("second Policy write: %v", err)
			}
			if stored, err := repository.GetApprovalPolicy(ctx, "pol_one"); err != nil || stored.Enabled || stored.Revision != 2 {
				t.Errorf("stored Policy = %#v, %v", stored, err)
			}

			// A Change Request update carries the record forward one revision. The
			// revision is what a concurrent writer collides on, so it has to move in
			// the direction that makes a collision detectable.
			created, err := repository.CreateChangeRequest(ctx, ChangeRequest{
				ID: "crq_one", TargetKind: "PLAN", TargetID: "pln_one", Environment: "dev",
				Title: "Conformance", Status: ChangePendingApprovals,
				Author: Principal{Username: "author"}, Revision: 1,
			})
			if err != nil {
				t.Fatalf("creating a Change Request: %v", err)
			}
			updated, err := repository.UpdateChangeRequest(ctx, created.ID, func(record *ChangeRequest) error {
				record.Title = "Conformance, renamed"
				return nil
			})
			if err != nil || updated.Revision != created.Revision+1 {
				t.Errorf("updated Change Request revision = %d, was %d (%v)", updated.Revision, created.Revision, err)
			}
			if reread, err := repository.GetChangeRequest(ctx, created.ID); err != nil || reread.Revision != updated.Revision {
				t.Errorf("persisted Change Request revision = %#v, %v", reread, err)
			}

			// An audit entry is written once. Appending the same identity again
			// returns what is already there rather than overwriting it, because an
			// audit trail that a retry can rewrite is not evidence.
			event := AuthorizationAuditEvent{
				ID: "aud_conformance", SchemaVersion: AuditSchemaV3,
				Categories: []string{AuditCategoryAuthorizationCheck}, TraceID: "ri.example",
				Subject: "subject", Action: "READ", ResourceRID: "ri.example", Decision: "ALLOW",
				Reason: "first", CreatedAt: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
				RequestFields: map[string]any{
					"authorizationCheckTargets":    []string{"ri.example"},
					"authorizationCheckOperations": []string{"READ"},
				},
				ResultFields: map[string]any{
					"authorizationCheckSucceededTargets": []string{"ri.example"},
					"authorizationCheckFailedTargets":    []string{},
				},
			}
			if _, err := repository.AppendAuthorizationAudit(ctx, event); err != nil {
				t.Fatalf("first audit append: %v", err)
			}
			rewritten := event
			rewritten.Reason, rewritten.Decision = "rewritten", "DENY"
			if stored, err := repository.AppendAuthorizationAudit(ctx, rewritten); err != nil || stored.Reason != "first" {
				t.Errorf("re-appending an audit entry = %#v, %v", stored, err)
			}
			byRID, err := repository.ListAuthorizationAudit(ctx, "ri.example")
			if err != nil || len(byRID) != 1 {
				t.Errorf("audit by RID = %#v, %v", byRID, err)
			}
			if other, err := repository.ListAuthorizationAudit(ctx, "ri.something-else"); err != nil || len(other) != 0 {
				t.Errorf("audit filtered to another RID returned %#v, %v", other, err)
			}
			if all, err := repository.ListAuthorizationAudit(ctx, ""); err != nil || len(all) != 1 {
				t.Errorf("unfiltered audit = %#v, %v", all, err)
			}
		})
	}
}

// A write that fails has to be reported. Swallowing it leaves the caller believing
// a Team or an audit entry is durable when the bytes never landed, and for the
// audit trail in particular that is the difference between evidence and a claim.
// The only way to reach those branches is to make the filesystem refuse, so this
// takes the write permission away rather than mocking the repository.
func TestFileRepositoryReportsWritesItCouldNotMake(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("running as root, which ignores the directory permissions this test relies on")
	}
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "state")
	repository, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	team := Team{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"admin"}, MemberUsers: []string{"member"}, Revision: 1}
	if _, err := repository.PutTeam(ctx, team); err != nil {
		t.Fatalf("the first write should succeed while the directory is writable: %v", err)
	}
	event := AuthorizationAuditEvent{
		ID: "aud_unwritable", SchemaVersion: AuditSchemaV3,
		Categories: []string{AuditCategoryAuthorizationCheck}, TraceID: "ri.example",
		Subject: "subject", Action: "READ", ResourceRID: "ri.example", Decision: "ALLOW",
		Reason: "before", CreatedAt: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
		RequestFields: map[string]any{
			"authorizationCheckTargets":    []string{"ri.example"},
			"authorizationCheckOperations": []string{"READ"},
		},
		ResultFields: map[string]any{
			"authorizationCheckSucceededTargets": []string{"ri.example"},
			"authorizationCheckFailedTargets":    []string{},
		},
	}
	if _, err := repository.AppendAuthorizationAudit(ctx, event); err != nil {
		t.Fatalf("the first audit append should succeed: %v", err)
	}

	if _, err := repository.PutApprovalPolicy(ctx, ApprovalPolicy{
		ID: "pol_one", DisplayName: "Policy", Enabled: true, Expression: "no_op()", Revision: 1,
	}); err != nil {
		t.Fatalf("the first Policy write should succeed: %v", err)
	}

	for _, directory := range []string{
		filepath.Join(root, "teams"), filepath.Join(root, "authorization-audit"), filepath.Join(root, "approval-policies"),
	} {
		if err := os.Chmod(directory, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(directory, 0o700) })
	}

	team.Revision, team.DisplayName = 2, "Reviewers, renamed"
	if _, err := repository.PutTeam(ctx, team); err == nil {
		t.Error("a Team write into an unwritable directory reported success")
	}
	event.ID, event.Reason = "aud_unwritable_two", "after"
	if _, err := repository.AppendAuthorizationAudit(ctx, event); err == nil {
		t.Error("an audit append into an unwritable directory reported success")
	}
	if _, err := repository.PutApprovalPolicy(ctx, ApprovalPolicy{
		ID: "pol_one", DisplayName: "Policy", Enabled: false, Expression: "no_op()", Revision: 2,
	}); err == nil {
		t.Error("a Policy write into an unwritable directory reported success")
	}
}

// The audit event's target lists, which are what an auditor filters on. The
// decision string and the lists have to agree: an ALLOW that files its resource
// under "failed" reads as a denial to everything downstream that does not parse
// the decision field itself.
func TestAuditEventFilesItsResourceUnderTheMatchingOutcome(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	_, _ = service.PutTeam(ctx, Team{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}})
	_, _ = service.PutPolicy(ctx, ApprovalPolicy{ID: "review", DisplayName: "Review", Enabled: true, Expression: `require_team("reviewers", "review")`})
	author, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author"})
	_, request, err := service.ProposePlan(ctx, foundation(), author, "Audit targets", "")
	if err != nil {
		t.Fatal(err)
	}
	// One refusal and one success, so both sides of the decision are present.
	if _, _, err := service.Reject(ctx, request.ID, author, "not mine"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a non-reviewer rejected: %v", err)
	}
	reviewer, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "reviewer"})
	if _, _, err := service.Approve(ctx, request.ID, reviewer, "looks right"); err != nil {
		t.Fatalf("approving: %v", err)
	}
	events, err := repository.ListAuthorizationAudit(ctx, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The stored event has been through JSON, so a []string comes back as []any.
	// Reading it as []string alone silently yields nil and every assertion below
	// then passes against an empty list -- which is exactly what it is meant to
	// catch.
	targets := func(value any) []string {
		switch typed := value.(type) {
		case []string:
			return typed
		case []any:
			result := make([]string, 0, len(typed))
			for _, element := range typed {
				text, ok := element.(string)
				if !ok {
					t.Fatalf("target list contained %T", element)
				}
				result = append(result, text)
			}
			return result
		}
		t.Fatalf("target list was %T", value)
		return nil
	}
	checked := 0
	for _, event := range events {
		// Change Request creation is audited under a different schema, with no
		// target lists at all. Only the authorization-check events are in scope.
		raw, carriesTargets := event.ResultFields["authorizationCheckSucceededTargets"]
		if !carriesTargets {
			continue
		}
		succeeded := targets(raw)
		failed := targets(event.ResultFields["authorizationCheckFailedTargets"])
		switch event.Decision {
		case "ALLOW":
			if len(succeeded) != 1 || succeeded[0] != event.ResourceRID || len(failed) != 0 {
				t.Errorf("ALLOW %s filed succeeded=%v failed=%v", event.Action, succeeded, failed)
			}
			checked++
		case "DENY":
			if len(failed) != 1 || failed[0] != event.ResourceRID || len(succeeded) != 0 {
				t.Errorf("DENY %s filed succeeded=%v failed=%v", event.Action, succeeded, failed)
			}
			checked++
		}
	}
	if checked < 2 {
		t.Fatalf("only %d audit entries carried a decision; wanted both an ALLOW and a DENY", checked)
	}
}

// A policy expression that does not compile is refused when the policy is
// written, not when a change request first runs into it. Accepting it defers the
// failure to whoever proposes next, and leaves an enabled policy that cannot run.
func TestApprovalPolicyRejectsAnExpressionThatDoesNotCompile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service := NewGovernanceService(NewMemoryRepository())
	for _, expression := range []string{
		`require_team("reviewers"`,           // unbalanced
		`no_such_function()`,                 // not in the environment
		`type == `,                           // incomplete
		`require_team("reviewers", "why") +`, // trailing operator
	} {
		_, err := service.PutPolicy(ctx, ApprovalPolicy{
			ID: "invalid", DisplayName: "Invalid", Enabled: true, Expression: expression,
		})
		if err == nil {
			t.Errorf("policy expression %q was accepted", expression)
			continue
		}
		// Refusing is not enough. Compilation reports what is wrong and where;
		// skipping that report still refuses the policy, but by way of the
		// downstream program build, whose message is "unsupported expr: <nil>" --
		// nothing the author of the policy can act on. The reported issue is the
		// behaviour, not the refusal.
		if !strings.Contains(err.Error(), "ERROR: <input>:") {
			t.Errorf("expression %q was refused as %v, without saying where", expression, err)
		}
	}
	if _, err := service.PutPolicy(ctx, ApprovalPolicy{
		ID: "valid", DisplayName: "Valid", Enabled: true, Expression: `no_op()`,
	}); err != nil {
		t.Errorf("a compiling expression was refused: %v", err)
	}
}

// A Plan mirrors its Change Request: how many approvals it needs, who has given
// them, and whether it is approved. The projection runs on every change to the
// request, so each field has to survive a request that moves backwards as well
// as forwards.
func TestPlanMirrorsItsChangeRequestThroughEveryTransition(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	for _, team := range []Team{
		{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}},
		{Name: "security", DisplayName: "Security", AdminUsers: []string{"auditor"}, MemberUsers: []string{"auditor"}},
	} {
		if _, err := service.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	// Two policies rather than one expression: requirements come from separate
	// enabled policies, and combining two require_team calls with && yields none.
	for _, policy := range []ApprovalPolicy{
		{ID: "review", DisplayName: "Review", Enabled: true, Expression: `require_team("reviewers", "review")`},
		{ID: "sign-off", DisplayName: "Sign off", Enabled: true, Expression: `require_team("security", "sign off")`},
	} {
		if _, err := service.PutPolicy(ctx, policy); err != nil {
			t.Fatal(err)
		}
	}
	author, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author"})
	reviewer, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "reviewer"})
	auditor, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "auditor"})
	plan, request, err := service.ProposePlan(ctx, foundation(), author, "Mirror", "")
	if err != nil {
		t.Fatal(err)
	}
	// A Plan carries a cancellation only while it is cancelled. Every writer of
	// Cancellation sets PlanCancelled in the same breath, and the one transition
	// back into PlanAwaitingApproval clears it -- so the pair (awaiting approval,
	// still cancelled) does not occur. That is what makes the awaiting-approval
	// clause in the projection redundant, and pinning it here is what keeps that
	// argument true rather than merely believed.
	cancellationMatchesStatus := func(where string, plan PlanRecord) {
		t.Helper()
		if plan.Cancellation != nil && plan.Status != PlanCancelled {
			t.Errorf("%s: Plan is %s but still carries a cancellation: %#v", where, plan.Status, plan.Cancellation)
		}
	}
	cancellationMatchesStatus("proposed", plan)

	if plan.RequiredApprovals != len(request.Requirements) || plan.RequiredApprovals < 2 {
		t.Fatalf("required approvals = %d, requirements = %d", plan.RequiredApprovals, len(request.Requirements))
	}
	if plan.Status != PlanAwaitingApproval || len(plan.Approvals) != 0 {
		t.Fatalf("proposed Plan = %#v", plan)
	}

	// One approval of two: recorded, but not yet approved.
	_, plan, err = service.Approve(ctx, request.ID, reviewer, "looks right")
	if err != nil {
		t.Fatal(err)
	}
	cancellationMatchesStatus("after one approval", plan)
	if len(plan.Approvals) != 1 || plan.Approvals[0].Subject != "reviewer" || plan.Status != PlanAwaitingApproval {
		t.Fatalf("Plan after one approval = %#v", plan)
	}
	if plan.Approvals[0].Rationale != "looks right" {
		t.Errorf("the approval rationale was not projected: %#v", plan.Approvals[0])
	}

	// A rejection is a review too, and it must NOT be projected as an approval.
	_, plan, err = service.Reject(ctx, request.ID, auditor, "not yet")
	if err != nil {
		t.Fatal(err)
	}
	cancellationMatchesStatus("after a rejection", plan)
	if plan.Status != PlanAwaitingApproval {
		t.Errorf("Plan after a rejection = %#v", plan.Status)
	}
	for _, approval := range plan.Approvals {
		if approval.Subject == "auditor" {
			t.Errorf("a rejection was projected as an approval: %#v", plan.Approvals)
		}
	}

	// Reopening clears the reviews, and the Plan has to follow it back.
	request, plan, err = service.Reopen(ctx, request.ID, author)
	if err != nil {
		t.Fatal(err)
	}
	cancellationMatchesStatus("after reopening", plan)
	if len(plan.Approvals) != 0 || plan.Status != PlanAwaitingApproval {
		t.Fatalf("Plan after reopening = %#v", plan)
	}

	// Both approvals: now the Plan is approved.
	if _, _, err := service.Approve(ctx, request.ID, reviewer, "again"); err != nil {
		t.Fatal(err)
	}
	_, plan, err = service.Approve(ctx, request.ID, auditor, "signed off")
	if err != nil {
		t.Fatal(err)
	}
	cancellationMatchesStatus("fully approved", plan)
	if plan.Status != PlanApproved || len(plan.Approvals) != 2 {
		t.Fatalf("fully approved Plan = %#v", plan)
	}
	if plan.Cancellation != nil {
		t.Errorf("an approved Plan carried a cancellation: %#v", plan.Cancellation)
	}
	cancellationMatchesStatus("fully approved", plan)

	// An approved Plan does not stay approved if the request stops being approved.
	// Cancelling is the transition available from approved -- rejecting is not --
	// and it has to take the approval away, or a Plan carries an approval its
	// Change Request no longer gives it.
	if _, err := service.Cancel(ctx, request.ID, author, "withdrawn"); err != nil {
		t.Fatal(err)
	}
	// Cancelling does not itself re-project; reopening does, and that is where an
	// approved Plan has to give the approval back.
	request, plan, err = service.Reopen(ctx, request.ID, author)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != PlanAwaitingApproval {
		t.Errorf("reopening left its already-approved Plan %s", plan.Status)
	}
	if len(plan.Approvals) != 0 {
		t.Errorf("reopening kept %d approvals", len(plan.Approvals))
	}
	cancellationMatchesStatus("after reopening an approved Plan", plan)

	// And approving a request whose Plan was cancelled revives the Plan, taking
	// the cancellation with it: a Plan cannot be approved and cancelled at once.
	cancelled, err := NewLifecycle(repository).CancelPlan(ctx, plan.ID, author.Username, "superseded")
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != PlanCancelled || cancelled.Cancellation == nil {
		t.Fatalf("cancelled Plan = %#v", cancelled)
	}
	if _, _, err := service.Approve(ctx, request.ID, reviewer, "revived"); err != nil {
		t.Fatal(err)
	}
	_, plan, err = service.Approve(ctx, request.ID, auditor, "revived")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != PlanApproved {
		t.Errorf("approving a cancelled Plan's request left it %s", plan.Status)
	}
	if plan.Cancellation != nil {
		t.Errorf("the revived Plan kept its cancellation: %#v", plan.Cancellation)
	}
	cancellationMatchesStatus("revived", plan)
}

// "Did any policy have an opinion?" is counted across every enabled policy, and
// a change where none did is an error rather than a silent pass. The counter is
// only ever compared to zero, so a single policy cannot show whether it counts up
// or down -- it takes two policies of different kinds, where counting one the
// wrong way cancels the other out exactly.
func TestPolicyOpinionsAreCountedNotCancelled(t *testing.T) {
	t.Parallel()
	approve := ApprovalPolicy{ID: "approve", DisplayName: "Approve", Enabled: true, Expression: `approve("fine")`}
	reject := ApprovalPolicy{ID: "reject", DisplayName: "Reject", Enabled: true, Expression: `reject("no")`}
	require := ApprovalPolicy{ID: "require", DisplayName: "Require", Enabled: true, Expression: `require_team("reviewers", "review")`}

	for _, pair := range []struct {
		name     string
		policies []ApprovalPolicy
		want     string
	}{
		{"an approval and a rejection", []ApprovalPolicy{approve, reject}, ChangeRejected},
		{"an approval and a requirement", []ApprovalPolicy{approve, require}, ChangePendingApprovals},
		{"a rejection and a requirement", []ApprovalPolicy{reject, require}, ChangeRejected},
	} {
		t.Run(pair.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			service := NewGovernanceService(NewMemoryRepository())
			if _, err := service.PutTeam(ctx, Team{
				Name: "reviewers", DisplayName: "Reviewers",
				AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"},
			}); err != nil {
				t.Fatal(err)
			}
			for _, policy := range pair.policies {
				if _, err := service.PutPolicy(ctx, policy); err != nil {
					t.Fatal(err)
				}
			}
			author, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author"})
			_, request, err := service.ProposePlan(ctx, foundation(), author, "Opinions", "")
			if err != nil {
				t.Fatal(err)
			}
			if request.Status != pair.want {
				t.Errorf("status = %q, wanted %q (error %q)", request.Status, pair.want, request.Error)
			}
			// The failure mode being guarded against announces itself in the error.
			if strings.Contains(request.Error, "returned no_op") {
				t.Errorf("two policies with opinions were counted as none: %q", request.Error)
			}
		})
	}

	// And the case the counter exists for: every enabled policy declining to have
	// an opinion is an error, not an approval.
	ctx := context.Background()
	service := NewGovernanceService(NewMemoryRepository())
	if _, err := service.PutPolicy(ctx, ApprovalPolicy{
		ID: "silent", DisplayName: "Silent", Enabled: true, Expression: `no_op()`,
	}); err != nil {
		t.Fatal(err)
	}
	author, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author"})
	_, request, err := service.ProposePlan(ctx, foundation(), author, "Silent", "")
	if err != nil {
		t.Fatal(err)
	}
	if request.Status != ChangeError || !strings.Contains(request.Error, "returned no_op") {
		t.Errorf("a change no policy had an opinion on = %q %q", request.Status, request.Error)
	}
}
