package composition

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectproto"
	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1/plansv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

const approvalProcedure = "/conduit.plans.v1.PlanApprovalService/ApprovePlan"

type approvalOrchestrator struct {
	mu   sync.Mutex
	ids  []string
	fail bool
}

func (*approvalOrchestrator) Driver() string { return controlplane.PlanOrchestratorTemporal }
func (driver *approvalOrchestrator) Begin(_ context.Context, id string) (controlplane.PlanExecutionHandoff, error) {
	driver.mu.Lock()
	defer driver.mu.Unlock()
	driver.ids = append(driver.ids, id)
	if driver.fail {
		return controlplane.PlanExecutionHandoff{}, errors.New("PRIVATE-SEQUENCER-DIAGNOSTIC")
	}
	return controlplane.PlanExecutionHandoff{Driver: controlplane.PlanOrchestratorTemporal, PlanID: id, WorkflowID: "plan-execute-" + id}, nil
}
func (driver *approvalOrchestrator) calls() int {
	driver.mu.Lock()
	defer driver.mu.Unlock()
	return len(driver.ids)
}

type approvalFixture struct {
	repository *controlplane.MemoryRepository
	server     *httptest.Server
	driver     *approvalOrchestrator
	restCalls  atomic.Int32
	rpcCalls   atomic.Int32
}

func newApprovalFixture(t *testing.T, governed bool, required int, fail bool) *approvalFixture {
	t.Helper()
	f := &approvalFixture{repository: controlplane.NewMemoryRepository(), driver: &approvalOrchestrator{fail: fail}}
	governance := controlplane.NewGovernanceService(f.repository)
	for _, team := range []controlplane.Team{{Name: "operators", DisplayName: "Operators", AdminUsers: []string{"owner"}, MemberUsers: []string{"alice"}}, {Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"owner"}, MemberUsers: []string{"bob", "author"}}, {Name: "readers", DisplayName: "Readers", AdminUsers: []string{"owner"}, MemberUsers: []string{"reader"}}} {
		if _, err := governance.PutTeam(t.Context(), team); err != nil {
			t.Fatal(err)
		}
	}
	for _, grant := range []controlplane.ResourceGrant{{ResourceRID: "environment/dev", Team: "operators", Operations: []string{controlplane.OperationOperate}}, {ResourceRID: "environment/dev", Team: "readers", Operations: []string{controlplane.OperationReadResource}}} {
		if _, err := governance.PutGrant(t.Context(), grant); err != nil {
			t.Fatal(err)
		}
	}
	created := time.Date(2026, time.November, 16, 1, 2, 3, 123456789, time.UTC)
	record := controlplane.PlanRecord{ID: "plan", DesiredStateDigest: "sha256:desired", Plan: controlplane.Plan{Environment: "dev", Generation: "one", Steps: []controlplane.PlanStep{{InstallationID: "app", Product: "team:app"}}}, Status: controlplane.PlanAwaitingApproval, RequiredApprovals: required, Revision: 9007199254740993, CreatedAt: created, UpdatedAt: created, Lease: &controlplane.PlanLease{Token: "PRIVATE-EXECUTION-TOKEN"}}
	if governed {
		record.ChangeRequestID = "cr"
		if _, err := f.repository.CreateChangeRequest(t.Context(), controlplane.ChangeRequest{ID: "cr", PlanID: "plan", TargetKind: "PLAN", TargetID: "plan", Environment: "dev", Title: "Review", Status: controlplane.ChangePendingApprovals, Author: controlplane.Principal{Username: "author"}, Requirements: []controlplane.ReviewerRequirement{{ID: "independent-review", Kind: controlplane.RequirementTeam, Team: "reviewers", Reason: "independent review"}}, Revision: 1, CreatedAt: created, UpdatedAt: created}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.repository.CreatePlan(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHub(f.repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}}, Options{PlanOrchestrator: f.driver})
	if err != nil {
		t.Fatal(err)
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == approvalProcedure {
			f.rpcCalls.Add(1)
		} else if r.URL.Path == "/v1/plans/plan/approvals" {
			f.restCalls.Add(1)
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

type approvalActorTransport struct {
	actor       string
	represented string
	next        http.RoundTripper
}

func (transport approvalActorTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	copy.Header = request.Header.Clone()
	copy.Header.Set("X-Platform-Subject", transport.actor)
	if transport.represented != "" {
		copy.Header.Set("X-Platform-Service-Account", "true")
		copy.Header.Set("X-Platform-On-Behalf-Of", transport.represented)
	}
	return transport.next.RoundTrip(copy)
}

func invokePlanApproval(t *testing.T, f *approvalFixture, rpc bool, actor, rationale string, represented ...string) (int, *plansv1.ApprovePlanResponse, error) {
	t.Helper()
	actingFor := ""
	if len(represented) > 0 {
		actingFor = represented[0]
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if rpc {
		base := f.server.Client()
		client := *base
		client.Transport = approvalActorTransport{actor: actor, represented: actingFor, next: base.Transport}
		api := plansv1connect.NewPlanApprovalServiceClient(connect.NewClient(connecthttp.NewTransport(&client, f.server.URL)))
		response, err := api.ApprovePlan(ctx, &plansv1.ApprovePlanRequest{PlanId: "plan", Rationale: rationale})
		return 0, response, err
	}
	body, _ := json.Marshal(map[string]string{"rationale": rationale})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, f.server.URL+"/v1/plans/plan/approvals", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Platform-Subject", actor)
	if actingFor != "" {
		request.Header.Set("X-Platform-Service-Account", "true")
		request.Header.Set("X-Platform-On-Behalf-Of", actingFor)
	}
	response, err := f.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode == http.StatusOK {
		var record controlplane.PlanRecord
		if json.Unmarshal(data, &record) != nil || (record.Lease != nil && record.Lease.Token != "") {
			t.Fatalf("native result leaked execution token: %s", data)
		}
	}
	return response.StatusCode, nil, nil
}

func approvalState(t *testing.T, f *approvalFixture) (controlplane.PlanRecord, *controlplane.ChangeRequest) {
	t.Helper()
	record, err := f.repository.GetPlan(t.Context(), "plan")
	if err != nil {
		t.Fatal(err)
	}
	var change *controlplane.ChangeRequest
	if record.ChangeRequestID != "" {
		stored, err := f.repository.GetChangeRequest(t.Context(), record.ChangeRequestID)
		if err != nil {
			t.Fatal(err)
		}
		change = &stored
	}
	return record, change
}

func assertApprovalProjection(t *testing.T, response *plansv1.ApprovePlanResponse, record controlplane.PlanRecord) {
	t.Helper()
	if response == nil || response.Plan == nil {
		t.Fatal("missing command projection")
	}
	plan := response.Plan
	if plan.Id != record.ID || plan.Environment != record.Plan.Environment || plan.Status != string(record.Status) || plan.StepCount != int64(len(record.Plan.Steps)) || plan.ApprovalCount != int64(len(record.Approvals)) || plan.RequiredApprovals != int64(record.RequiredApprovals) || plan.Revision != record.Revision || !plan.CreatedAt.AsTime().Equal(record.CreatedAt) || !plan.UpdatedAt.AsTime().Equal(record.UpdatedAt) || response.ChangeRequestId != record.ChangeRequestID || len(response.Approvals) != len(record.Approvals) {
		t.Fatalf("command projection differs from stored native result: %v %+v", response, record)
	}
	for i, approval := range response.Approvals {
		native := record.Approvals[i]
		if approval.Subject != native.Subject || approval.Rationale != native.Rationale || !approval.CreatedAt.AsTime().Equal(native.CreatedAt) {
			t.Fatalf("approval evidence changed: %v %+v", approval, native)
		}
	}
	if strings.Contains(response.String(), "PRIVATE") || strings.Contains(response.String(), "EXECUTION-TOKEN") {
		t.Fatal("execution credential leaked")
	}
}

func normalizedApprovalState(record controlplane.PlanRecord, change *controlplane.ChangeRequest) (controlplane.PlanRecord, *controlplane.ChangeRequest) {
	record.UpdatedAt = time.Time{}
	for i := range record.Approvals {
		record.Approvals[i].CreatedAt = time.Time{}
	}
	if change != nil {
		change.UpdatedAt = time.Time{}
		for i := range change.Reviews {
			change.Reviews[i].CreatedAt = time.Time{}
		}
	}
	return record, change
}

func approvalAudit(t *testing.T, f *approvalFixture) []string {
	t.Helper()
	events, err := f.repository.ListAuthorizationAudit(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	var projected []string
	for _, event := range events {
		if event.Action == controlplane.OperationOperate || event.Action == "CHANGE_REQUEST_APPROVE" {
			projected = append(projected, strings.Join([]string{event.Subject, event.Action, event.ResourceRID, event.Decision, event.Reason}, "|"))
		}
	}
	sort.Strings(projected)
	return projected
}

func TestPlanApprovalConnectMatchesNativeAuthorityStateAndAudit(t *testing.T) {
	for _, governed := range []bool{false, true} {
		for _, actor := range []string{"alice", "reader", "outsider", "bob", "author", "root"} {
			t.Run(actor+map[bool]string{false: "/direct", true: "/governed"}[governed], func(t *testing.T) {
				native := newApprovalFixture(t, governed, 1, false)
				rpc := newApprovalFixture(t, governed, 1, false)
				beforeNative, beforeCR := approvalState(t, native)
				status, _, _ := invokePlanApproval(t, native, false, actor, "  reviewed  ")
				_, response, err := invokePlanApproval(t, rpc, true, actor, "  reviewed  ")
				allowed := (!governed && (actor == "alice" || actor == "root")) || (governed && actor == "bob")
				if allowed {
					if status != 200 || err != nil {
						t.Fatalf("eligible reviewer refused: REST=%d RPC=%v", status, err)
					}
				} else if status != 403 || connect.CodeOf(err) != connect.CodePermissionDenied {
					t.Fatalf("ineligible reviewer admitted: REST=%d RPC=%v", status, err)
				}
				nativeRecord, nativeCR := approvalState(t, native)
				rpcRecord, rpcCR := approvalState(t, rpc)
				if !allowed {
					if !reflect.DeepEqual(beforeNative, nativeRecord) || !reflect.DeepEqual(beforeCR, nativeCR) {
						t.Fatal("denied REST changed business state")
					}
					if !reflect.DeepEqual(beforeNative, rpcRecord) || !reflect.DeepEqual(beforeCR, rpcCR) {
						t.Fatal("denied RPC changed business state")
					}
				} else {
					assertApprovalProjection(t, response, rpcRecord)
					if rpcRecord.Status != controlplane.PlanApproved || len(rpcRecord.Approvals) != 1 || rpcRecord.Approvals[0].Rationale != "reviewed" || rpcRecord.Approvals[0].Subject != actor {
						t.Fatalf("native transition changed: %+v", rpcRecord)
					}
				}
				nativeRecord, nativeCR = normalizedApprovalState(nativeRecord, nativeCR)
				rpcRecord, rpcCR = normalizedApprovalState(rpcRecord, rpcCR)
				if !reflect.DeepEqual(nativeRecord, rpcRecord) || !reflect.DeepEqual(nativeCR, rpcCR) {
					t.Fatalf("persisted state diverged: REST=%+v %+v RPC=%+v %+v", nativeRecord, nativeCR, rpcRecord, rpcCR)
				}
				if len(approvalAudit(t, native)) == 0 || !reflect.DeepEqual(approvalAudit(t, native), approvalAudit(t, rpc)) {
					t.Fatalf("audit diverged: REST=%v RPC=%v", approvalAudit(t, native), approvalAudit(t, rpc))
				}
				want := 0
				if allowed {
					want = 1
				}
				if native.driver.calls() != want || rpc.driver.calls() != want {
					t.Fatal("sequencing handoff differs from native transition")
				}
				if native.restCalls.Load() != 1 || native.rpcCalls.Load() != 0 || rpc.rpcCalls.Load() != 1 || rpc.restCalls.Load() != 0 {
					t.Fatal("command retried or replayed through REST")
				}
			})
		}
	}
}

func TestPlanApprovalConnectReportsRecordedApprovalWhenSequencingFails(t *testing.T) {
	for _, governed := range []bool{false, true} {
		actor := "alice"
		if governed {
			actor = "bob"
		}
		native := newApprovalFixture(t, governed, 1, true)
		rpc := newApprovalFixture(t, governed, 1, true)
		status, _, _ := invokePlanApproval(t, native, false, actor, "reviewed")
		_, _, err := invokePlanApproval(t, rpc, true, actor, "reviewed")
		if status != http.StatusBadGateway || connect.CodeOf(err) != connect.CodeUnavailable || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatalf("handoff result: REST=%d RPC=%v", status, err)
		}
		var failure *connect.Error
		if !errors.As(err, &failure) || len(failure.Details()) != 1 {
			t.Fatalf("missing recorded outcome detail: %v", err)
		}
		message, decodeErr := connectproto.UnmarshalErrorDetail(failure.Details()[0])
		outcome, ok := message.(*plansv1.PlanApprovalOutcome)
		if decodeErr != nil || !ok || outcome.PlanId != "plan" || !outcome.ApprovalRecorded || !outcome.HandoffFailed {
			t.Fatalf("wrong recorded outcome: %v %v", message, decodeErr)
		}
		for _, fixture := range []*approvalFixture{native, rpc} {
			record, _ := approvalState(t, fixture)
			if record.Status != controlplane.PlanApproved || len(record.Approvals) != 1 || fixture.driver.calls() != 1 {
				t.Fatalf("recorded approval hidden or repeated: %+v", record)
			}
		}
		if rpc.rpcCalls.Load() != 1 || rpc.restCalls.Load() != 0 {
			t.Fatal("failed handoff replayed approval")
		}
	}
}

func TestPlanApprovalConnectMatchesNativePartialDuplicateAndStateFailures(t *testing.T) {
	native := newApprovalFixture(t, false, 2, false)
	rpc := newApprovalFixture(t, false, 2, false)
	for _, step := range []struct {
		actor, rationale string
		status           int
		code             connect.Code
		count            int
		approved         bool
	}{{"alice", "", 409, connect.CodeFailedPrecondition, 0, false}, {"alice", "first review", 200, 0, 1, false}, {"alice", "duplicate", 409, connect.CodeAlreadyExists, 1, false}, {"root", "second review", 200, 0, 2, true}} {
		before, _ := approvalState(t, rpc)
		status, _, _ := invokePlanApproval(t, native, false, step.actor, step.rationale)
		_, response, err := invokePlanApproval(t, rpc, true, step.actor, step.rationale)
		if status != step.status {
			t.Fatalf("native status=%d expected=%d", status, step.status)
		}
		if step.code == 0 {
			if err != nil {
				t.Fatal(err)
			}
		} else if connect.CodeOf(err) != step.code {
			t.Fatalf("typed command error: %v expected=%v", err, step.code)
		}
		after, _ := approvalState(t, rpc)
		nativeAfter, _ := approvalState(t, native)
		if len(after.Approvals) != step.count || (after.Status == controlplane.PlanApproved) != step.approved {
			t.Fatalf("partial or duplicate approval changed state: %+v", after)
		}
		if step.code != 0 && !reflect.DeepEqual(before, after) {
			t.Fatal("refused command changed the Plan")
		}
		if step.code == 0 {
			assertApprovalProjection(t, response, after)
			if after.Revision != before.Revision+1 {
				t.Fatal("approval revision did not advance exactly once")
			}
		}
		after, _ = normalizedApprovalState(after, nil)
		nativeAfter, _ = normalizedApprovalState(nativeAfter, nil)
		if !reflect.DeepEqual(after, nativeAfter) {
			t.Fatal("native/RPC partial state diverged")
		}
		want := 0
		if step.approved {
			want = 1
		}
		if rpc.driver.calls() != want || native.driver.calls() != want {
			t.Fatal("partial or duplicate approval sequenced execution")
		}
	}
	for _, fixture := range []*approvalFixture{native, rpc} {
		if _, err := fixture.repository.UpdatePlan(t.Context(), "plan", func(record *controlplane.PlanRecord) error { record.Status = controlplane.PlanRunning; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := approvalState(t, rpc)
	status, _, _ := invokePlanApproval(t, native, false, "root", "late")
	_, _, err := invokePlanApproval(t, rpc, true, "root", "late")
	after, _ := approvalState(t, rpc)
	if status != 409 || connect.CodeOf(err) != connect.CodeFailedPrecondition || !reflect.DeepEqual(before, after) || rpc.driver.calls() != 1 {
		t.Fatalf("running Plan approval changed state: REST=%d RPC=%v", status, err)
	}
	if rpc.rpcCalls.Load() != 5 || rpc.restCalls.Load() != 0 {
		t.Fatal("explicit commands were retried or REST-replayed")
	}
}

func newDelegatedApprovalFixture(t *testing.T, governed bool) *approvalFixture {
	t.Helper()
	_, repository, _ := delegationHub(t)
	f := &approvalFixture{repository: repository, driver: &approvalOrchestrator{}}
	record := controlplane.PlanRecord{ID: "plan", DesiredStateDigest: "seed", Plan: controlplane.Plan{Environment: "dev"}, Status: controlplane.PlanAwaitingApproval, RequiredApprovals: 1, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0)}
	if governed {
		record.ChangeRequestID = "cr"
		if _, err := repository.CreateChangeRequest(t.Context(), controlplane.ChangeRequest{ID: "cr", PlanID: "plan", TargetKind: "PLAN", TargetID: "plan", Environment: "dev", Status: controlplane.ChangePendingApprovals, Author: controlplane.Principal{Username: "alice"}, Requirements: []controlplane.ReviewerRequirement{{ID: "independent", Kind: controlplane.RequirementTeam, Team: "automation"}}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repository.CreatePlan(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}}, Options{PlanOrchestrator: f.driver})
	if err != nil {
		t.Fatal(err)
	}
	f.server = httptest.NewServer(handler)
	t.Cleanup(f.server.Close)
	return f
}

func TestPlanApprovalConnectRetainsCarrierAndDelegatedReviewerIndependence(t *testing.T) {
	for _, tc := range []struct {
		actor, represented string
		governed, allowed  bool
	}{{"alice", "", false, true}, {"alice", "bob", false, true}, {"robot", "alice", false, false}, {"bob", "alice", false, false}, {"robot", "alice", true, false}, {"robot", "", true, true}, {"alice", "", true, false}} {
		t.Run(tc.actor+"/"+tc.represented+map[bool]string{true: "/governed", false: "/direct"}[tc.governed], func(t *testing.T) {
			native := newDelegatedApprovalFixture(t, tc.governed)
			rpc := newDelegatedApprovalFixture(t, tc.governed)
			before, beforeCR := approvalState(t, rpc)
			status, _, _ := invokePlanApproval(t, native, false, tc.actor, "reviewed", tc.represented)
			_, response, err := invokePlanApproval(t, rpc, true, tc.actor, "reviewed", tc.represented)
			if tc.allowed {
				if status != 200 || err != nil {
					t.Fatalf("authorized carrier lost authority: REST=%d RPC=%v", status, err)
				}
			} else if status != 403 || connect.CodeOf(err) != connect.CodePermissionDenied {
				t.Fatalf("delegation widened approval: REST=%d RPC=%v", status, err)
			}
			after, afterCR := approvalState(t, rpc)
			if tc.allowed {
				assertApprovalProjection(t, response, after)
				if len(after.Approvals) != 1 || after.Approvals[0].Subject != tc.actor {
					t.Fatal("approval did not retain carrier subject")
				}
			} else if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeCR, afterCR) {
				t.Fatal("refused delegation changed business state")
			}
			if !reflect.DeepEqual(approvalAudit(t, native), approvalAudit(t, rpc)) {
				t.Fatal("delegated audit diverged")
			}
			if !tc.governed && !(tc.actor == "bob" && tc.represented == "alice") {
				events, auditErr := rpc.repository.ListAuthorizationAudit(t.Context(), "environment/dev")
				if auditErr != nil {
					t.Fatal(auditErr)
				}
				found := false
				for _, event := range events {
					if event.Action == controlplane.OperationOperate {
						found = true
						if event.Subject != tc.actor || event.Context["onBehalfOf"] != tc.represented {
							t.Fatalf("carrier audit lost delegation: %+v", event)
						}
					}
				}
				if !found {
					t.Fatal("missing resource decision audit")
				}
			}
		})
	}
}

type approvalReadCounter struct {
	controlplane.Repository
	reads atomic.Int32
}

func (repository *approvalReadCounter) GetPlan(ctx context.Context, id string) (controlplane.PlanRecord, error) {
	repository.reads.Add(1)
	return repository.Repository.GetPlan(ctx, id)
}

func TestPlanApprovalConnectProductionAuthUnknownMethodsAndMissingPlan(t *testing.T) {
	repository := &approvalReadCounter{Repository: controlplane.NewMemoryRepository()}
	handler, err := NewHub(repository, controlplane.HandlerOptions{BootstrapAdministrators: []string{"root"}}, Options{PlanOrchestrator: &approvalOrchestrator{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"", "root"} {
		request := httptest.NewRequest(http.MethodPost, approvalProcedure, strings.NewReader("invalid JSON"))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Platform-Subject", actor)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 401 || repository.reads.Load() != 0 {
			t.Fatalf("production headers or malformed body bypassed auth: %d %s reads=%d", response.Code, response.Body.String(), repository.reads.Load())
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := approvalProcedure
		if method == http.MethodPost {
			path = "/conduit.plans.v1.PlanApprovalService/ApproveEverything"
		}
		response := driveAs(t, handler, method, path, "root", "")
		if response.Code != 404 && response.Code != 405 {
			t.Fatalf("unknown command served: %d", response.Code)
		}
		if repository.reads.Load() != 0 {
			t.Fatal("unknown command read a Plan")
		}
	}
	fixture := newApprovalFixture(t, false, 1, false)
	request := httptest.NewRequest(http.MethodPost, approvalProcedure, strings.NewReader(`{"planId":"missing","rationale":"reviewed"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Platform-Subject", "root")
	// Drive the mounted endpoint over the actual server for the missing resource.
	clientRequest, err := http.NewRequestWithContext(t.Context(), http.MethodPost, fixture.server.URL+approvalProcedure, request.Body)
	if err != nil {
		t.Fatal(err)
	}
	clientRequest.Header = request.Header
	response, err := fixture.server.Client().Do(clientRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 404 || !strings.Contains(string(body), "not_found") || fixture.driver.calls() != 0 {
		t.Fatalf("missing Plan command: %d %s", response.StatusCode, body)
	}
	record, _ := approvalState(t, fixture)
	if len(record.Approvals) != 0 || record.Status != controlplane.PlanAwaitingApproval {
		t.Fatal("missing Plan command changed existing Plan")
	}
}
