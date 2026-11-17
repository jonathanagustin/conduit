package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	planports "github.com/jonathanagustin/conduit/internal/delivery/plan/ports"
	governanceports "github.com/jonathanagustin/conduit/internal/governance/ports"
	review "github.com/jonathanagustin/conduit/internal/planreview/domain"
)

// recordedAnnouncements is every port in Announcements, recording what it was
// told. err is returned from each, which is how a test states "the
// notification store is broken" without having one.
type recordedAnnouncements struct {
	mu         sync.Mutex
	openings   []governanceports.ChangeRequestOpening
	grants     []governanceports.GrantChange
	failures   []planports.PlanFailure
	promotions []catalog.PromotionRecord
	err        error
}

func (recorded *recordedAnnouncements) AwaitingApproval(
	_ context.Context, opening governanceports.ChangeRequestOpening,
) error {
	recorded.mu.Lock()
	defer recorded.mu.Unlock()
	recorded.openings = append(recorded.openings, opening)
	return recorded.err
}

func (recorded *recordedAnnouncements) GrantChanged(_ context.Context, change governanceports.GrantChange) error {
	recorded.mu.Lock()
	defer recorded.mu.Unlock()
	recorded.grants = append(recorded.grants, change)
	return recorded.err
}

func (recorded *recordedAnnouncements) PlanFailed(_ context.Context, failure planports.PlanFailure) error {
	recorded.mu.Lock()
	defer recorded.mu.Unlock()
	recorded.failures = append(recorded.failures, failure)
	return recorded.err
}

func (recorded *recordedAnnouncements) PromotionEvaluated(_ context.Context, record catalog.PromotionRecord) error {
	recorded.mu.Lock()
	defer recorded.mu.Unlock()
	recorded.promotions = append(recorded.promotions, record)
	return recorded.err
}

func (recorded *recordedAnnouncements) all() Announcements {
	return Announcements{
		ChangeRequestOpenings: recorded, GrantChanges: recorded,
		PlanFailures: recorded, PromotionFailures: recorded,
	}
}

// announcingGovernance is a service whose proposals wait for the reviewers
// team, which is the state an approver needs telling about.
func announcingGovernance(t *testing.T, repository Repository) (*GovernanceService, *recordedAnnouncements, Principal, Principal) {
	t.Helper()
	ctx := context.Background()
	service := NewGovernanceService(repository)
	recorded := &recordedAnnouncements{}
	service.SetAnnouncements(recorded.all())
	if _, err := service.PutTeam(ctx, Team{
		Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutPolicy(ctx, ApprovalPolicy{
		ID: "review", DisplayName: "Review", Enabled: true, Expression: `require_team("reviewers", "review")`,
	}); err != nil {
		t.Fatal(err)
	}
	author, err := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author"})
	if err != nil {
		t.Fatal(err)
	}
	reviewer, err := service.ResolvePrincipal(ctx, IdentityClaims{Username: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	return service, recorded, author, reviewer
}

// The gap #840 names: a Plan proposal stored waiting for an approver told
// nobody. It is announced once, as the stored record, and a request that is
// rejected and reopened waits a second time and is announced a second time at
// a later revision -- which is what lets the adapter tell the two waits apart.
func TestAPlanProposalWaitingForApprovalIsAnnouncedAndSoIsItsReopening(t *testing.T) {
	ctx := context.Background()
	service, recorded, author, reviewer := announcingGovernance(t, NewMemoryRepository())
	_, request, err := service.ProposePlan(ctx, foundation(), author, "Deploy the foundation", "")
	if err != nil {
		t.Fatal(err)
	}
	if request.Status != ChangePendingApprovals {
		t.Fatalf("fixture: the proposal is %s, not waiting", request.Status)
	}
	if len(recorded.openings) != 1 {
		t.Fatalf("a proposal waiting for approval was announced %d times", len(recorded.openings))
	}
	opening := recorded.openings[0]
	if opening.ID != request.ID || opening.Environment != "dev-use2" || opening.Author != "author" ||
		opening.Revision != request.Revision || opening.Title != "Deploy the foundation" ||
		!opening.At.Equal(request.UpdatedAt) {
		t.Errorf("the announcement does not describe the stored request: %#v vs %#v", opening, request)
	}

	if _, _, err := service.Reject(ctx, request.ID, reviewer, "unsafe rollout"); err != nil {
		t.Fatal(err)
	}
	if len(recorded.openings) != 1 {
		t.Fatalf("a rejection was announced as a request awaiting approval: %#v", recorded.openings)
	}
	reopened, _, err := service.Reopen(ctx, request.ID, author)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded.openings) != 2 {
		t.Fatalf("a reopened request waits again and was announced %d times in total", len(recorded.openings))
	}
	if again := recorded.openings[1]; again.ID != request.ID || again.Revision != reopened.Revision ||
		again.Revision <= opening.Revision {
		t.Errorf("the second wait is not distinguishable from the first: %#v then %#v", opening, again)
	}
}

// Only a request that waits for a person has an approver to tell. One the
// policies approved outright, and one they could not evaluate, have none.
func TestAProposalThatDoesNotWaitForAPersonIsNotAnnounced(t *testing.T) {
	ctx := context.Background()
	for name, expression := range map[string]string{"approved by policy": `approve("fixture")`, "no policy": ""} {
		t.Run(name, func(t *testing.T) {
			service := NewGovernanceService(NewMemoryRepository())
			recorded := &recordedAnnouncements{}
			service.SetAnnouncements(recorded.all())
			if expression != "" {
				if _, err := service.PutPolicy(ctx, ApprovalPolicy{
					ID: "auto", DisplayName: "Auto", Enabled: true, Expression: expression,
				}); err != nil {
					t.Fatal(err)
				}
			}
			_, request, err := service.ProposePlan(ctx, foundation(), Principal{Username: "author"}, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if request.Status == ChangePendingApprovals {
				t.Fatalf("fixture: the request is waiting")
			}
			if len(recorded.openings) != 0 {
				t.Errorf("a %s request was announced as awaiting approval: %#v", request.Status, recorded.openings)
			}
		})
	}
}

func pipelineChange() []EntityChange {
	return []EntityChange{{
		ID: "chg-1", Type: ChangeTypeArchivePipeline,
		Request: map[string]any{"pipelineRid": "nightly-export"},
		ReferencedResources: map[string]ReferencedResource{
			"bundlingpipeline": {RID: "export-pipeline/nightly-export", Name: "nightly-export"},
		},
	}}
}

// The two other ways a request is stored waiting, each through its own
// function and so each its own emit site.
func TestANonPlanProposalWaitingForApprovalIsAnnounced(t *testing.T) {
	ctx := context.Background()
	t.Run("ProposeChanges", func(t *testing.T) {
		service, recorded, author, _ := announcingGovernance(t, NewMemoryRepository())
		request, _, err := service.ProposeChanges(ctx,
			"EXPORT_PIPELINE_LIFECYCLE_"+ChangeTypeArchivePipeline, "nightly-export", "dev-use2",
			pipelineChange(), author, "Archive the nightly export", "no longer needed")
		if err != nil {
			t.Fatal(err)
		}
		if request.Status != ChangePendingApprovals {
			t.Fatalf("fixture: the request is %s (%s)", request.Status, request.Error)
		}
		if len(recorded.openings) != 1 || recorded.openings[0].ID != request.ID ||
			recorded.openings[0].Environment != "dev-use2" {
			t.Errorf("the waiting request was not announced: %#v", recorded.openings)
		}
	})
	t.Run("ProposeHumanChange", func(t *testing.T) {
		service := NewGovernanceService(NewMemoryRepository())
		recorded := &recordedAnnouncements{}
		service.SetAnnouncements(recorded.all())
		request, err := service.ProposeHumanChange(ctx, "PLAN", "sample-app-1:memory", "dev", []EntityChange{{
			ID: "memory-limit", Type: ChangeTypeUpdateEntity, InstallationID: "sample-app",
			ReferencedResources: map[string]ReferencedResource{
				"environment":  {RID: "environment/dev", Name: "dev"},
				"installation": {RID: "installation/dev/sample-app", Name: "sample-app"},
				"product":      {RID: "product/app.example:sample-app", Name: "sample-app"},
			},
		}}, Principal{Username: "operator"}, "Proposed memory limit 512Mi", "headroom")
		if err != nil {
			t.Fatal(err)
		}
		if len(recorded.openings) != 1 || recorded.openings[0].ID != request.ID ||
			recorded.openings[0].Environment != "dev" || recorded.openings[0].Author != "operator" {
			t.Errorf("a change held for a person was not announced: %#v", recorded.openings)
		}
	})
}

// A membership proposal runs inside a repository transaction. What is
// announced is what committed: the refused proposal that wrote nothing
// announces nothing, and the committed one is announced once, after the
// transaction, by the service that owns the port.
func TestAMembershipProposalIsAnnouncedOnlyOnceItCommits(t *testing.T) {
	for _, kind := range []string{"memory", "file"} {
		t.Run(kind, func(t *testing.T) {
			var repository Repository = NewMemoryRepository()
			if kind == "file" {
				var err error
				if repository, err = NewFileRepository(t.TempDir()); err != nil {
					t.Fatal(err)
				}
			}
			service := NewGovernanceService(repository)
			recorded := &recordedAnnouncements{}
			service.SetAnnouncements(recorded.all())
			for _, team := range []Team{
				{Name: "writers", DisplayName: "Writers", AdminUsers: []string{"writer"}, MemberUsers: []string{"writer"}},
				{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}},
			} {
				if _, err := service.PutTeam(t.Context(), team); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := service.PutGrant(t.Context(), ResourceGrant{
				ResourceRID: "platform/root", Team: "writers", Operations: []string{OperationProposeChange},
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := service.PutPolicy(t.Context(), ApprovalPolicy{
				ID: "membership", DisplayName: "Membership", Enabled: true, Expression: `approve("initial fixture")`,
			}); err != nil {
				t.Fatal(err)
			}
			author, err := service.ResolvePrincipal(t.Context(), IdentityClaims{Username: "writer"})
			if err != nil {
				t.Fatal(err)
			}
			state := foundation()
			state.ReleaseChannels = []ReleaseChannelDefinition{{Name: "STABLE"}, {Name: "CANARY"}}
			for index := range state.Releases {
				state.Releases[index].Channels = []string{"STABLE"}
			}
			baseline, _, err := service.ProposePlan(t.Context(), state, author, "Baseline", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.PutPolicy(t.Context(), ApprovalPolicy{
				ID: "membership", DisplayName: "Membership", Revision: 1, Enabled: true,
				Expression: `type == "CONDUIT_NATIVE_UPDATE_RELEASE_CHANNEL_MEMBERSHIP" ? require_team("reviewers", "membership review") : approve("existing workload")`,
			}); err != nil {
				t.Fatal(err)
			}
			if len(recorded.openings) != 0 {
				t.Fatalf("fixture: the policy-approved baseline was announced: %#v", recorded.openings)
			}
			context := review.Review{
				Title: "Membership change", Rationale: "Reason line one\nTicket CAB-123",
				ValueTier: review.Native, BaseDesiredStateDigest: baseline.DesiredStateDigest,
			}
			candidate := cloneJSON(state)
			candidate.Environment.Generation = "reviewed-membership-one"
			candidate.Releases[0].Channels = []string{"CANARY"}

			refused := cloneJSON(candidate)
			refused.Releases[0].Channels = []string{"UNKNOWN"}
			if _, _, err := service.ProposeMembershipPlan(t.Context(), refused, author, context); !errors.Is(err, ErrInvalidTransition) {
				t.Fatal("fixture: an unknown channel was not refused", err)
			}
			if len(recorded.openings) != 0 {
				t.Fatalf("a proposal that committed nothing was announced: %#v", recorded.openings)
			}

			_, request, err := service.ProposeMembershipPlan(t.Context(), candidate, author, context)
			if err != nil {
				t.Fatal(err)
			}
			if request.Status != ChangePendingApprovals {
				t.Fatalf("fixture: the membership request is %s", request.Status)
			}
			if len(recorded.openings) != 1 || recorded.openings[0].ID != request.ID {
				t.Errorf("the committed membership proposal was announced %d times: %#v",
					len(recorded.openings), recorded.openings)
			}
		})
	}
}

// A grant write names who made it, what the grant was and what it became.
// PutGrant, the fixture path with no subject, has nobody to attribute the
// change to and announces nothing -- the same line the audit record draws.
func TestAGrantWriteIsAnnouncedWithWhatItWasAndWhatItBecame(t *testing.T) {
	ctx := context.Background()
	service := NewGovernanceService(NewMemoryRepository())
	recorded := &recordedAnnouncements{}
	service.SetAnnouncements(recorded.all())
	if _, err := service.PutTeam(ctx, Team{
		Name: "operators", DisplayName: "Operators", AdminUsers: []string{"ops"}, MemberUsers: []string{"ops"},
	}); err != nil {
		t.Fatal(err)
	}
	first, err := service.PutGrantAs(ctx, ResourceGrant{
		ResourceRID: environmentRID("dev-use2"), Team: "operators", Roles: []string{"environment:viewer"},
	}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded.grants) != 1 || recorded.grants[0].Before != nil || recorded.grants[0].Actor != "alice" ||
		recorded.grants[0].After.ID != first.ID || recorded.grants[0].After.Revision != 1 {
		t.Fatalf("a new grant was not announced as new: %#v", recorded.grants)
	}
	second, err := service.PutGrantAs(ctx, ResourceGrant{
		ResourceRID: environmentRID("dev-use2"), Team: "operators", Roles: []string{"environment:operator"},
		Revision: first.Revision,
	}, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded.grants) != 2 {
		t.Fatalf("a changed grant was announced %d times in total", len(recorded.grants))
	}
	change := recorded.grants[1]
	if change.Before == nil || change.Before.Roles[0] != "environment:viewer" ||
		change.After.Roles[0] != "environment:operator" || change.After.Revision != second.Revision ||
		change.Actor != "bob" {
		t.Errorf("the change does not say what the grant was and became: %#v", change)
	}

	if _, err := service.PutGrant(ctx, ResourceGrant{
		ResourceRID: environmentRID("staging"), Team: "operators", Roles: []string{"environment:viewer"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(recorded.grants) != 2 {
		t.Errorf("a grant written with no subject was announced: %#v", recorded.grants[2:])
	}

	// A refused write changed nothing and announces nothing.
	if _, err := service.PutGrantAs(ctx, ResourceGrant{
		ResourceRID: environmentRID("dev-use2"), Team: "operators", Roles: []string{"environment:viewer"},
		Revision: 99,
	}, "mallory"); !errors.Is(err, ErrConflict) {
		t.Fatal("fixture: a stale grant write was not refused", err)
	}
	if len(recorded.grants) != 2 {
		t.Errorf("a refused grant write was announced: %#v", recorded.grants[2:])
	}
}

func finishRequest(t *testing.T, handler http.Handler, running PlanRecord, token string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/plans/"+running.ID+"/finish", bytes.NewReader(encoded))
	request.Header.Set("X-Platform-Environment-ID", running.Plan.Environment)
	request.Header.Set("X-Platform-Agent-ID", running.Lease.AgentID)
	request.Header.Set("X-Platform-Lease-Token", token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// The other half of #840: a Plan an Agent reports FAILED told nobody. The
// report is announced after it is stored, with the Agent's own reason and the
// installation it named. A report the Hub refuses stored nothing and announces
// nothing; a broken notification port does not turn a stored report into a
// refusal, because the Agent would retry work that was recorded.
func TestAPlanAnAgentReportsFailedIsAnnouncedOnceTheReportIsStored(t *testing.T) {
	repository := NewMemoryRepository()
	// Leased against the real clock, because the handler's lifecycle reads it
	// and startFoundationPlan's fixed one would hand over an expired lease.
	lifecycle := NewLifecycle(repository)
	running, err := lifecycle.CreatePlan(t.Context(), foundation())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lifecycle.ApprovePlan(t.Context(), running.ID, "reviewer", "approved"); err != nil {
		t.Fatal(err)
	}
	if running, err = lifecycle.LeaseNextPlan(t.Context(), "dev-use2", "agent", time.Minute); err != nil {
		t.Fatal(err)
	}
	if running, err = lifecycle.StartPlan(t.Context(), running.ID, running.Lease.Token); err != nil {
		t.Fatal(err)
	}
	recorded := &recordedAnnouncements{err: errors.New("the notification store is down")}
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureAgentIdentityHeaders: true, Announcements: recorded.all(),
	})
	report := failedObservedState(running, "example-api")
	body := map[string]any{
		"status": PlanFailed, "failure": "chart rollout failed", "failedInstallationId": "example-api",
		"execution": failedPlanExecution(running, "example-api"),
		"entities":  report.Entities, "secrets": report.Secrets,
	}

	refused := finishRequest(t, handler, running, "not-the-lease-token", body)
	if refused.Code == http.StatusOK {
		t.Fatalf("fixture: a finish with the wrong lease token was accepted: %s", refused.Body.String())
	}
	if len(recorded.failures) != 0 {
		t.Fatalf("a refused finish report was announced: %#v", recorded.failures)
	}

	response := finishRequest(t, handler, running, running.Lease.Token, body)
	if response.Code != http.StatusOK {
		t.Fatalf("a stored failure was refused because the announcement failed: %d %s",
			response.Code, response.Body.String())
	}
	stored, err := repository.GetPlan(t.Context(), running.ID)
	if err != nil || stored.Status != PlanFailed {
		t.Fatalf("fixture: the Plan is %s, %v", stored.Status, err)
	}
	if len(recorded.failures) != 1 {
		t.Fatalf("a Plan reported FAILED was announced %d times", len(recorded.failures))
	}
	failure := recorded.failures[0]
	if failure.PlanID != running.ID || failure.Environment != "dev-use2" ||
		failure.Installation != "example-api" || failure.Reason != "chart rollout failed" ||
		!failure.At.Equal(stored.UpdatedAt) {
		t.Errorf("the announcement does not describe the stored failure: %#v", failure)
	}
}

// Every stored evaluation is handed over; which of them is a failure is the
// adapter's rule. A refused evaluation stored nothing and hands over nothing.
func TestEveryStoredPromotionEvaluationIsHandedToThePort(t *testing.T) {
	repository := NewMemoryRepository()
	recorded := &recordedAnnouncements{err: errors.New("the notification store is down")}
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, Announcements: recorded.all(),
	})
	evaluation := promotionRequest(time.Now().UTC())
	evaluation.Evidence[0].HealthySince = time.Now().UTC()
	post := func(evaluation PromotionEvaluationRequest) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(evaluation)
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/promotions/evaluations", bytes.NewReader(body)))
		return response
	}
	first := post(evaluation)
	if first.Code != http.StatusCreated {
		t.Fatalf("a stored evaluation was refused because the announcement failed: %d %s", first.Code, first.Body.String())
	}
	var running PromotionRecord
	if err := json.Unmarshal(first.Body.Bytes(), &running); err != nil {
		t.Fatal(err)
	}
	if len(recorded.promotions) != 1 || recorded.promotions[0].ID != running.ID {
		t.Fatalf("the stored evaluation was not handed over: %#v", recorded.promotions)
	}

	evaluation.PreviousEvaluationID = running.ID
	evaluation.Termination = catalog.PromotionAborted
	if refused := post(evaluation); refused.Code == http.StatusCreated {
		t.Fatal("fixture: a termination with no reason was accepted")
	}
	if len(recorded.promotions) != 1 {
		t.Fatalf("a refused evaluation was handed over: %#v", recorded.promotions[1:])
	}

	evaluation.TerminationReason = "canary error rate doubled"
	aborted := post(evaluation)
	if aborted.Code != http.StatusCreated {
		t.Fatalf("abort = %d %s", aborted.Code, aborted.Body.String())
	}
	if len(recorded.promotions) != 2 || recorded.promotions[1].Lifecycle != catalog.PromotionAborted ||
		recorded.promotions[1].LifecycleReason != "canary error rate doubled" {
		t.Errorf("the aborted run was not handed over as aborted: %#v", recorded.promotions)
	}
}

// A port that fails must not fail the proposal: the request is stored.
func TestABrokenAnnouncementDoesNotRefuseTheProposalItDescribes(t *testing.T) {
	service, recorded, author, _ := announcingGovernance(t, NewMemoryRepository())
	recorded.err = errors.New("the notification store is down")
	_, request, err := service.ProposePlan(context.Background(), foundation(), author, "", "")
	if err != nil || request.Status != ChangePendingApprovals {
		t.Fatalf("a stored proposal was refused because its announcement failed: %#v %v", request, err)
	}
	if len(recorded.openings) != 1 {
		t.Fatalf("the announcement was not attempted: %#v", recorded.openings)
	}
}
