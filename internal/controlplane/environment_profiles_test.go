package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEnvironmentProfilePersistsImmutableProvenanceHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "state")
	repository, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	service := NewEnvironmentProfileService(repository)
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	created, err := service.Put(ctx, EnvironmentProfile{
		EnvironmentID: "prod-use1", DisplayName: "Production", Description: "Primary production cluster",
		README: "# Operations\nSee the runbook.", Accreditation: "STANDARD",
		OperationalResponsibilityEnabled: true, RegisteredNamespaces: []string{"finance", "platform"},
	}, "operator-one", "establish production ownership metadata")
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 || created.UpdatedBy != "operator-one" || created.Source != EnvironmentProfileSourceOperatorAPI {
		t.Fatalf("created profile = %#v", created)
	}
	now = now.Add(time.Hour)
	created.Accreditation, created.SupportOverride = "FEDRAMP_HIGH", "MISSION_CRITICAL"
	updated, err := service.Put(ctx, created, "operator-two", "record approved accreditation")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || updated.CreatedAt != created.CreatedAt || updated.UpdatedBy != "operator-two" {
		t.Fatalf("updated profile = %#v", updated)
	}
	if _, err := service.Put(ctx, created, "operator-three", "stale overwrite"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale profile error = %v", err)
	}

	reopened, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := reopened.ListEnvironmentProfileRevisions(ctx, created.EnvironmentID)
	if err != nil || len(revisions) != 2 || revisions[0].Accreditation != "STANDARD" ||
		revisions[1].Accreditation != "FEDRAMP_HIGH" || revisions[0].Rationale == revisions[1].Rationale {
		t.Fatalf("immutable profile revisions = %#v, %v", revisions, err)
	}
}

func TestEnvironmentProfileRequiresExistingContactTeams(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewEnvironmentProfileService(repository)
	profile := EnvironmentProfile{EnvironmentID: "prod", Accreditation: "STANDARD", OwnerTeam: "missing"}
	if _, err := service.Put(ctx, profile, "operator", "assign contact"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing contact team error = %v", err)
	}
	if _, err := NewGovernanceService(repository).PutTeam(ctx, Team{
		Name: "contacts", DisplayName: "Contacts", AdminUsers: []string{"contact-admin"}, MemberUsers: []string{"contact-admin"},
	}); err != nil {
		t.Fatal(err)
	}
	profile.OwnerTeam, profile.CoOwnerTeams = "contacts", []string{"contacts"}
	if _, err := service.Put(ctx, profile, "operator", "invalid duplicate ownership"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate contact ownership error = %v", err)
	}
}

func TestEnvironmentProfileHTTPEnforcesEnvironmentRBAC(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	governance := NewGovernanceService(repository)
	for _, team := range []Team{
		{Name: "editors", DisplayName: "Editors", AdminUsers: []string{"editor"}, MemberUsers: []string{"editor"}},
		{Name: "viewers", DisplayName: "Viewers", AdminUsers: []string{"viewer"}, MemberUsers: []string{"viewer"}},
	} {
		if _, err := governance.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	for _, grant := range []ResourceGrant{
		{ResourceRID: environmentRID("prod"), Team: "editors", Roles: []string{"environment:editor"}},
		{ResourceRID: environmentRID("prod"), Team: "viewers", Roles: []string{"environment:viewer"}},
	} {
		if _, err := governance.PutGrant(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutPolicy(ctx, ApprovalPolicy{
		ID: "profile-updates", DisplayName: "Profile updates", Enabled: true,
		Expression: `type == "UPDATE_ENVIRONMENT_PROPERTIES" ? approve("authorized profile update") : no_op()`,
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	profile := EnvironmentProfile{Accreditation: "STANDARD", DisplayName: "Production", Rationale: "document environment"}
	response := governanceRequest(t, handler, http.MethodPut, "/v1/environments/prod/profile", "viewer", "", profile)
	if response.Code != http.StatusForbidden {
		t.Fatalf("viewer profile write = %d %s", response.Code, response.Body.String())
	}
	response = governanceRequest(t, handler, http.MethodPut, "/v1/environments/prod/profile", "editor", "", profile)
	if response.Code != http.StatusCreated {
		t.Fatalf("editor profile write = %d %s", response.Code, response.Body.String())
	}
	response = governanceRequest(t, handler, http.MethodGet, "/v1/environments/prod/profile", "viewer", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("viewer profile read = %d %s", response.Code, response.Body.String())
	}
	response = governanceRequest(t, handler, http.MethodGet, "/v1/environments/prod/profile/revisions", "viewer", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("viewer profile history = %d %s", response.Code, response.Body.String())
	}
	audit, err := repository.ListAuthorizationAudit(ctx, environmentRID("prod"))
	if err != nil || len(audit) < 4 {
		t.Fatalf("profile authorization audit = %#v, %v", audit, err)
	}
}

func TestEnvironmentProfileAppliesWhenChangeRequestIsApproved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	governance := NewGovernanceService(repository)
	for _, team := range []Team{
		{Name: "editors", DisplayName: "Editors", AdminUsers: []string{"editor"}, MemberUsers: []string{"editor"}},
		{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}},
	} {
		if _, err := governance.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{
		ResourceRID: environmentRID("prod"), Team: "editors", Roles: []string{"environment:editor"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutPolicy(ctx, ApprovalPolicy{
		ID: "profile-review", DisplayName: "Profile review", Enabled: true,
		Expression: `type == "UPDATE_ENVIRONMENT_PROPERTIES" ? require_team("reviewers", "review Environment profile") : no_op()`,
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := governanceRequest(t, handler, http.MethodPut, "/v1/environments/prod/profile", "editor", "", EnvironmentProfile{
		Accreditation: "FEDRAMP_HIGH", DisplayName: "Production", Rationale: "raise compliance profile",
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("profile proposal = %d %s", response.Code, response.Body.String())
	}
	var proposed struct {
		ChangeRequest ChangeRequest `json:"changeRequest"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &proposed); err != nil || proposed.ChangeRequest.Status != ChangePendingApprovals {
		t.Fatalf("profile proposal = %#v, %v", proposed, err)
	}
	if _, err := repository.GetEnvironmentProfile(ctx, "prod"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pending profile was applied: %v", err)
	}
	alternateResponse := governanceRequest(t, handler, http.MethodPut, "/v1/environments/prod/profile", "editor", "", EnvironmentProfile{
		Accreditation: "FEDRAMP_HIGH", DisplayName: "Alternate production", Rationale: "alternate proposal",
	})
	var alternate struct {
		ChangeRequest ChangeRequest `json:"changeRequest"`
	}
	if alternateResponse.Code != http.StatusCreated || json.Unmarshal(alternateResponse.Body.Bytes(), &alternate) != nil ||
		alternate.ChangeRequest.ID == proposed.ChangeRequest.ID {
		t.Fatalf("content-addressed alternate proposal = %d %#v %s", alternateResponse.Code, alternate, alternateResponse.Body.String())
	}
	response = governanceRequest(t, handler, http.MethodPost,
		"/v1/change-requests/"+proposed.ChangeRequest.ID+"/approve", "reviewer", "", map[string]string{"rationale": "approved by compliance"})
	if response.Code != http.StatusOK {
		t.Fatalf("profile approval = %d %s", response.Code, response.Body.String())
	}
	stored, err := repository.GetEnvironmentProfile(ctx, "prod")
	if err != nil || stored.Accreditation != "FEDRAMP_HIGH" || stored.UpdatedBy != "editor" {
		t.Fatalf("approved profile = %#v, %v", stored, err)
	}
	changeRequest, err := repository.GetChangeRequest(ctx, proposed.ChangeRequest.ID)
	if err != nil || changeRequest.Execution == nil || changeRequest.Execution.Status != ExecutionSucceeded ||
		len(changeRequest.Execution.Items) != 1 || changeRequest.Execution.Items[0].ResultID != "environment-profile/prod/1" {
		t.Fatalf("profile execution evidence = %#v, %v", changeRequest, err)
	}
	response = governanceRequest(t, handler, http.MethodPost,
		"/v1/change-requests/"+alternate.ChangeRequest.ID+"/approve", "reviewer", "", map[string]string{"rationale": "approved alternate"})
	if response.Code != http.StatusConflict {
		t.Fatalf("stale approved profile = %d %s", response.Code, response.Body.String())
	}
	alternateRequest, err := repository.GetChangeRequest(ctx, alternate.ChangeRequest.ID)
	if err != nil || alternateRequest.Execution == nil || alternateRequest.Execution.Status != ExecutionFailed {
		t.Fatalf("stale profile execution evidence = %#v, %v", alternateRequest, err)
	}
}

func TestEnvironmentProfileDoesNotChangePlanEligibilityWithoutExplicitPolicy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	state := assessedFoundation()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	plainRepository := NewMemoryRepository()
	profileRepository := NewMemoryRepository()
	profileService := NewEnvironmentProfileService(profileRepository)
	profileService.now = func() time.Time { return now }
	if _, err := profileService.Put(ctx, EnvironmentProfile{
		EnvironmentID: state.Environment.ID, Accreditation: "STANDARD", DisplayName: "Displayed only",
		OperationalResponsibilityEnabled: true,
	}, "operator", "record compliance metadata"); err != nil {
		t.Fatal(err)
	}
	plainLifecycle, profiledLifecycle := NewLifecycle(plainRepository), NewLifecycle(profileRepository)
	plainLifecycle.now, profiledLifecycle.now = func() time.Time { return now }, func() time.Time { return now }
	plain, err := plainLifecycle.CreatePlan(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	profiled, err := profiledLifecycle.CreatePlan(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if plain.ID != profiled.ID || plain.DesiredStateDigest != profiled.DesiredStateDigest ||
		plain.Status != profiled.Status || !reflect.DeepEqual(plain.Plan, profiled.Plan) {
		t.Fatalf("presentation/compliance metadata changed Plan eligibility:\nplain=%#v\nprofiled=%#v", plain, profiled)
	}
}

func TestEnvironmentAccreditationIsAvailableOnlyToExplicitApprovalPolicy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	profiles := NewEnvironmentProfileService(repository)
	state := assessedFoundation()
	profile, err := profiles.Put(ctx, EnvironmentProfile{
		EnvironmentID: state.Environment.ID, Accreditation: "STANDARD",
	}, "operator", "record accreditation")
	if err != nil {
		t.Fatal(err)
	}
	governance := NewGovernanceService(repository)
	if _, err := governance.PutPolicy(ctx, ApprovalPolicy{
		ID: "standard-only", DisplayName: "Standard accreditation only", Enabled: true,
		Expression: `referencedResources.environment.resource.accreditation == "STANDARD" ? approve("standard environment") : reject("accreditation requires review")`,
	}); err != nil {
		t.Fatal(err)
	}
	_, request, err := governance.ProposePlan(ctx, state, Principal{Username: "author"}, "", "")
	if err != nil || request.Status != ChangeApproved {
		t.Fatalf("STANDARD policy result = %#v, %v", request, err)
	}
	profile.Accreditation = "FEDRAMP_HIGH"
	if _, err := profiles.Put(ctx, profile, "compliance", "raise accreditation"); err != nil {
		t.Fatal(err)
	}
	state.Environment.Generation = "fedcba9876543210"
	_, request, err = governance.ProposePlan(ctx, state, Principal{Username: "author"}, "", "")
	if err != nil || request.Status != ChangeRejected {
		t.Fatalf("FEDRAMP policy result = %#v, %v", request, err)
	}
}

// Every length limit on an Environment profile, asserted at the value it must
// accept as well as the one it must not. These are all reachable from the
// operator API, and a limit only tested from above is satisfied by refusing
// everything -- which is why `>` and `>=` were indistinguishable at all five.
func TestEnvironmentProfileLengthLimitsAreAssertedAtTheirEdges(t *testing.T) {
	t.Parallel()
	base := func() EnvironmentProfile {
		createdAt := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
		return EnvironmentProfile{
			EnvironmentID: "prod-use1", Accreditation: "STANDARD",
			Source: EnvironmentProfileSourceOperatorAPI, UpdatedBy: "operator-one",
			Rationale: "bounds", CreatedAt: createdAt, UpdatedAt: createdAt, Revision: 1,
		}
	}
	for _, limit := range []struct {
		name    string
		maximum int
		set     func(*EnvironmentProfile, string)
	}{
		{"displayName", 256, func(p *EnvironmentProfile, v string) { p.DisplayName = v }},
		{"description", 4096, func(p *EnvironmentProfile, v string) { p.Description = v }},
		{"readme", 64 * 1024, func(p *EnvironmentProfile, v string) { p.README = v }},
		{"registeredNamespace", 256, func(p *EnvironmentProfile, v string) { p.RegisteredNamespaces = []string{v} }},
	} {
		t.Run(limit.name, func(t *testing.T) {
			profile := base()
			limit.set(&profile, strings.Repeat("v", limit.maximum))
			if err := validateEnvironmentProfile(profile); err != nil {
				t.Errorf("%s of exactly %d was refused: %v", limit.name, limit.maximum, err)
			}
			profile = base()
			limit.set(&profile, strings.Repeat("v", limit.maximum+1))
			if err := validateEnvironmentProfile(profile); err == nil {
				t.Errorf("%s of %d was accepted", limit.name, limit.maximum+1)
			}
		})
	}

	// The support override is bounded twice over, and the two bounds disagree.
	// accreditationPattern accepts at most 64 characters, so nothing between 65
	// and 128 can ever be accepted and the 128 limit has no acceptance edge at
	// all. What it still decides is WHICH refusal an operator gets, and that is
	// the only thing that can be asserted about it.
	t.Run("supportOverride", func(t *testing.T) {
		profile := base()
		profile.SupportOverride = "S" + strings.Repeat("X", 127)
		err := validateEnvironmentProfile(profile)
		if err == nil || !strings.Contains(err.Error(), "invalid support override") {
			t.Errorf("an override of exactly 128 was reported as %v, not as a bad override", err)
		}
		profile.SupportOverride = "S" + strings.Repeat("X", 128)
		err = validateEnvironmentProfile(profile)
		if err == nil || !strings.Contains(err.Error(), "invalid Environment profile") {
			t.Errorf("an override of 129 was reported as %v, not as an over-long profile", err)
		}
		// And the length that actually decides acceptance is the pattern's.
		profile.SupportOverride = "S" + strings.Repeat("X", 63)
		if err := validateEnvironmentProfile(profile); err != nil {
			t.Errorf("an override at the pattern's own limit was refused: %v", err)
		}
	})
}

// Applying an approved profile change is idempotent. The apply is driven from
// approval, and an approval can be replayed -- by a retry, by a second reader of
// the same queue -- so a second apply has to return what is already stored rather
// than write a second revision and stamp a fresh execution over the evidence of
// the first.
func TestEnvironmentProfileApplyIsIdempotentOnceItHasSucceeded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	governance := NewGovernanceService(repository)
	for _, team := range []Team{
		{Name: "editors", DisplayName: "Editors", AdminUsers: []string{"editor"}, MemberUsers: []string{"editor"}},
		{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}},
	} {
		if _, err := governance.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{
		ResourceRID: environmentRID("prod"), Team: "editors", Roles: []string{"environment:editor"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutPolicy(ctx, ApprovalPolicy{
		ID: "profile-review", DisplayName: "Profile review", Enabled: true,
		Expression: `type == "UPDATE_ENVIRONMENT_PROPERTIES" ? require_team("reviewers", "review Environment profile") : no_op()`,
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := governanceRequest(t, handler, http.MethodPut, "/v1/environments/prod/profile", "editor", "", EnvironmentProfile{
		Accreditation: "FEDRAMP_HIGH", DisplayName: "Production", Rationale: "raise compliance profile",
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("profile proposal = %d %s", response.Code, response.Body.String())
	}
	var proposed struct {
		ChangeRequest ChangeRequest `json:"changeRequest"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &proposed); err != nil {
		t.Fatal(err)
	}
	response = governanceRequest(t, handler, http.MethodPost,
		"/v1/change-requests/"+proposed.ChangeRequest.ID+"/approve", "reviewer", "", map[string]string{"rationale": "approved"})
	if response.Code != http.StatusOK {
		t.Fatalf("profile approval = %d %s", response.Code, response.Body.String())
	}
	applied, err := repository.GetChangeRequest(ctx, proposed.ChangeRequest.ID)
	if err != nil || applied.Execution == nil || applied.Execution.Status != ExecutionSucceeded {
		t.Fatalf("first apply = %#v, %v", applied, err)
	}
	first, err := repository.GetEnvironmentProfile(ctx, "prod")
	if err != nil {
		t.Fatal(err)
	}

	// Let the profile move on after the approval, which is the only situation in
	// which re-applying differs from not: an identical replay is indistinguishable
	// either way, so a test that does not create drift proves nothing here.
	service := NewEnvironmentProfileService(repository)
	service.now = func() time.Time { return time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC) }
	moved, err := service.Put(ctx, EnvironmentProfile{
		EnvironmentID: "prod", Accreditation: "FEDRAMP_HIGH", DisplayName: "Production, renamed",
		Revision: first.Revision,
	}, "editor-two", "rename after the approval")
	if err != nil || moved.Revision == first.Revision {
		t.Fatalf("drifting the profile = %#v, %v", moved, err)
	}

	// The second apply runs against a request that already carries a successful
	// execution. It must hand back what is stored NOW, not write its own older
	// payload back over the newer one.
	again, request, err := service.ApplyApprovedChangeRequest(ctx, applied)
	if err != nil {
		t.Fatalf("re-applying a succeeded change request: %v", err)
	}
	if again.Revision != moved.Revision || again.DisplayName != moved.DisplayName {
		t.Errorf("re-apply did not return the current profile: %#v, wanted %#v", again, moved)
	}
	if request.Execution == nil || !request.Execution.CompletedAt.Equal(applied.Execution.CompletedAt) {
		t.Errorf("re-apply restamped the execution: %#v, was %#v", request.Execution, applied.Execution)
	}
	stored, err := repository.GetEnvironmentProfile(ctx, "prod")
	if err != nil || stored.Revision != moved.Revision || stored.DisplayName != moved.DisplayName {
		t.Errorf("re-apply overwrote the newer profile: %#v, %v", stored, err)
	}

	// The same replay by a caller whose copy predates the execution. It cannot
	// take the shortcut, so it tries the write, and the write now fails because
	// the profile has moved on. That failure must not be recorded over the
	// evidence of the apply that succeeded: an execution that reads FAILED for a
	// change that was applied is worse than no record at all.
	stale := applied
	stale.Execution = nil
	_, failed, err := service.ApplyApprovedChangeRequest(ctx, stale)
	if err == nil {
		t.Errorf("a stale caller re-applied over a newer profile: %#v", failed)
	}
	// The Change Request comes back identified even when the apply failed. When
	// the record could not be updated there is nothing newer to return, so the
	// caller gets what it passed in -- not an empty request it cannot act on.
	if failed.ID != applied.ID {
		t.Errorf("the failed apply returned request %q, not %q", failed.ID, applied.ID)
	}
	if failed.Execution != nil && failed.Execution.Status != ExecutionSucceeded {
		t.Errorf("the returned execution was downgraded to %q", failed.Execution.Status)
	}
	persisted, err := repository.GetChangeRequest(ctx, applied.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Execution == nil || persisted.Execution.Status != ExecutionSucceeded ||
		!persisted.Execution.CompletedAt.Equal(applied.Execution.CompletedAt) {
		t.Errorf("a failed replay overwrote the successful execution: %#v", persisted.Execution)
	}
	if current, err := repository.GetEnvironmentProfile(ctx, "prod"); err != nil ||
		current.Revision != moved.Revision {
		t.Errorf("the failed replay still changed the profile: %#v, %v", current, err)
	}
}

// The guard inside the update is a second line of defence, reached only when the
// caller's copy of the Change Request is stale -- two readers of the same
// approval queue, or a retry that re-read before the first apply committed. The
// caller sees no execution and re-applies; the record already has one, and the
// evidence of the first apply must survive that.
func TestEnvironmentProfileApplyDoesNotRestampEvidenceForAStaleCaller(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	governance := NewGovernanceService(repository)
	for _, team := range []Team{
		{Name: "editors", DisplayName: "Editors", AdminUsers: []string{"editor"}, MemberUsers: []string{"editor"}},
		{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}},
	} {
		if _, err := governance.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{
		ResourceRID: environmentRID("prod"), Team: "editors", Roles: []string{"environment:editor"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutPolicy(ctx, ApprovalPolicy{
		ID: "profile-review", DisplayName: "Profile review", Enabled: true,
		Expression: `type == "UPDATE_ENVIRONMENT_PROPERTIES" ? require_team("reviewers", "review Environment profile") : no_op()`,
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := governanceRequest(t, handler, http.MethodPut, "/v1/environments/prod/profile", "editor", "", EnvironmentProfile{
		Accreditation: "FEDRAMP_HIGH", DisplayName: "Production", Rationale: "raise compliance profile",
	})
	var proposed struct {
		ChangeRequest ChangeRequest `json:"changeRequest"`
	}
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &proposed) != nil {
		t.Fatalf("profile proposal = %d %s", response.Code, response.Body.String())
	}
	response = governanceRequest(t, handler, http.MethodPost,
		"/v1/change-requests/"+proposed.ChangeRequest.ID+"/approve", "reviewer", "", map[string]string{"rationale": "approved"})
	if response.Code != http.StatusOK {
		t.Fatalf("profile approval = %d %s", response.Code, response.Body.String())
	}
	applied, err := repository.GetChangeRequest(ctx, proposed.ChangeRequest.ID)
	if err != nil || applied.Execution == nil {
		t.Fatalf("first apply = %#v, %v", applied, err)
	}

	// The stale caller: same request, but its copy predates the execution.
	stale := applied
	stale.Execution = nil
	service := NewEnvironmentProfileService(repository)
	service.now = func() time.Time { return time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC) }
	_, returned, err := service.ApplyApprovedChangeRequest(ctx, stale)
	if err != nil {
		t.Fatalf("a stale caller's apply failed: %v", err)
	}
	if returned.Execution == nil || !returned.Execution.CompletedAt.Equal(applied.Execution.CompletedAt) {
		t.Errorf("the first apply's evidence was restamped: %#v, was %#v", returned.Execution, applied.Execution)
	}
	persisted, err := repository.GetChangeRequest(ctx, applied.ID)
	if err != nil || persisted.Execution == nil || !persisted.Execution.CompletedAt.Equal(applied.Execution.CompletedAt) {
		t.Errorf("the persisted evidence was restamped: %#v, %v", persisted.Execution, err)
	}
}
