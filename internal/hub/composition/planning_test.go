package composition

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
	planningapp "github.com/jonathanagustin/conduit/internal/planning/application"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
	planningworkflow "github.com/jonathanagustin/conduit/internal/planning/workflow"
)

type readyPlanningRunner struct{}

func (readyPlanningRunner) Turn(ctx context.Context, session *planningdomain.Session, _ string) (*planningapp.TurnResult, error) {
	if planningActor(ctx) != "alice" {
		return nil, errors.New("missing authenticated audit actor")
	}
	return &planningapp.TurnResult{Plan: &planningdomain.Plan{Text: "Package the app"}, ResolvedCommit: strings.Repeat("a", 40), Model: "fixture", Profile: planningdomain.ProfileRef{Name: "onboard", Version: "1", Hash: strings.Repeat("b", 64)}}, nil
}

type planningAuditFailure struct {
	controlplane.Repository
	fail bool
}

func (repository *planningAuditFailure) AppendAuthorizationAudit(ctx context.Context, event controlplane.AuthorizationAuditEvent) (controlplane.AuthorizationAuditEvent, error) {
	if repository.fail {
		return controlplane.AuthorizationAuditEvent{}, errors.New("audit unavailable")
	}
	return repository.Repository.AppendAuthorizationAudit(ctx, event)
}

func planningRequest(t *testing.T, handler http.Handler, actor, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if actor != "" {
		request.Header.Set("X-Platform-Subject", actor)
	}
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestPlanningOwnershipAndAcceptanceAudit(t *testing.T) {
	repository := &planningAuditFailure{Repository: controlplane.NewMemoryRepository()}
	handler := planningHandler(http.NotFoundHandler(), repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"bob"}}, readyPlanningRunner{})
	secure := planningHandler(http.NotFoundHandler(), repository, controlplane.HandlerOptions{}, readyPlanningRunner{})
	response := planningRequest(t, secure, "", "POST", "/v1/planning-sessions", `{"repoUrl":"https://github.com/org/app","ref":"main"}`)
	if response.Code != 401 {
		t.Fatalf("anonymous: %d", response.Code)
	}
	response = planningRequest(t, handler, "alice", "POST", "/v1/planning-sessions", `{"repoUrl":"https://github.com/org/app","ref":"main"}`)
	var session planningdomain.Session
	if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &session) != nil || session.ID == "" {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	path := "/v1/planning-sessions/" + session.ID
	for _, route := range []struct{ method, suffix, body string }{{"GET", "", ""}, {"GET", "/binding", ""}, {"GET", "/events", ""}, {"POST", "/turns", `{}`}, {"POST", "/answers", `{"answers":[]}`}, {"POST", "/accept", `{}`}, {"POST", "/compute", `{}`}} {
		response = planningRequest(t, handler, "bob", route.method, path+route.suffix, route.body)
		if response.Code != 404 {
			t.Fatalf("other operator %s %s: %d %s", route.method, route.suffix, response.Code, response.Body.String())
		}
	}
	response = planningRequest(t, handler, "alice", "POST", path+"/compute", `{}`)
	if response.Code != 409 || !strings.Contains(response.Body.String(), "COMPUTE_BEFORE_ACCEPT") {
		t.Fatalf("pre-accept compute: %s", response.Body.String())
	}
	response = planningRequest(t, handler, "alice", "POST", path+"/turns", `{"message":"onboard this app"}`)
	if response.Code != 200 {
		t.Fatalf("turn: %d %s", response.Code, response.Body.String())
	}
	repository.fail = true
	response = planningRequest(t, handler, "alice", "POST", path+"/accept", `{}`)
	if response.Code != 502 {
		t.Fatalf("audit failure: %d %s", response.Code, response.Body.String())
	}
	response = planningRequest(t, handler, "alice", "GET", path+"/binding", "")
	if response.Code != 409 {
		t.Fatalf("failed audit bound session: %d", response.Code)
	}
	repository.fail = false
	response = planningRequest(t, handler, "alice", "POST", path+"/accept", `{}`)
	if response.Code != 200 {
		t.Fatalf("accept: %d %s", response.Code, response.Body.String())
	}
	events, err := repository.ListAuthorizationAudit(t.Context(), "planning-session/"+session.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("accept receipt: %v %+v", err, events)
	}
	event := events[0]
	if event.Subject != "alice" || event.Action != planningapp.ActionAccept || event.Context["planHash"] == "" || event.Context["profileHash"] != strings.Repeat("b", 64) || event.Context["resolvedCommit"] != strings.Repeat("a", 40) || event.Context["intakeMessageSha256"] == "" {
		t.Fatalf("receipt lost binding: %+v", event)
	}
	response = planningRequest(t, handler, "alice", "POST", path+"/compute", `{}`)
	if response.Code != http.StatusNotImplemented || !strings.Contains(response.Body.String(), "COMPUTE_NOT_CONFIGURED") {
		t.Fatalf("unconfigured compute: %d %s", response.Code, response.Body.String())
	}
}

func TestPlanningModelAuditUsesCredentialOwner(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	ctx := context.WithValue(t.Context(), planningActorKey{}, "alice")
	err := (planningAudit{repository: repository}).Record(ctx, planner.Record{SessionID: "session", Action: planner.ActionInspect, Outcome: "pass", PromptSHA256: strings.Repeat("a", 64), ResponseSHA256: strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	events, err := repository.ListAuthorizationAudit(t.Context(), "planning-session/session")
	if err != nil || len(events) != 1 || events[0].Subject != "alice" || events[0].Context["promptSha256"] != strings.Repeat("a", 64) {
		t.Fatalf("model audit: %+v %v", events, err)
	}
}

func TestPlanningModelAuditCarriesBoundRunIdentifiers(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	ctx := context.WithValue(t.Context(), planningActorKey{}, "alice")
	ctx = planningapp.WithRunScope(ctx, planningapp.RunScope{ThreadID: "session", RunID: "run-1"})
	err := (planningAudit{repository: repository}).Record(ctx, planner.Record{SessionID: "session", Action: planner.ActionInspect, Outcome: "pass"})
	if err != nil {
		t.Fatal(err)
	}
	events, err := repository.ListAuthorizationAudit(t.Context(), "planning-session/session")
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if events[0].Subject != "alice" || events[0].Context["threadId"] != "session" || events[0].Context["runId"] != "run-1" {
		t.Fatalf("lost run binding: %+v", events[0])
	}
}

func TestMountPlanningRefusesNonLoopbackTemporalAddress(t *testing.T) {
	t.Setenv("CONDUIT_TEMPORAL_ADDRESS", "10.0.0.1:7233")
	_, err := mountPlanning(http.NotFoundHandler(), controlplane.NewMemoryRepository(), controlplane.HandlerOptions{})
	if err == nil || !strings.Contains(err.Error(), "refusing non-loopback") {
		t.Fatalf("expected non-loopback error, got %v", err)
	}
}

type fakeEncodedValue struct {
	val any
}

func (f fakeEncodedValue) HasValue() bool { return true }
func (f fakeEncodedValue) Get(valuePtr any) error {
	raw, err := json.Marshal(f.val)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, valuePtr)
}

type fakeUpdateHandle struct {
	session *planningdomain.Session
}

func (h fakeUpdateHandle) WorkflowID() string { return "" }
func (h fakeUpdateHandle) RunID() string      { return "" }
func (h fakeUpdateHandle) UpdateID() string   { return "upd-1" }
func (h fakeUpdateHandle) Get(_ context.Context, valuePtr any) error {
	if valuePtr == nil {
		return nil
	}
	rv := reflect.ValueOf(valuePtr)
	if rv.Kind() == reflect.Ptr {
		rv.Elem().Set(reflect.ValueOf(*h.session))
	}
	return nil
}

type fakeTemporalWorkflowClient struct {
	mu       sync.Mutex
	sessions map[string]*planningdomain.Session
	owners   map[string]string
}

func newFakeTemporalWorkflowClient() *fakeTemporalWorkflowClient {
	return &fakeTemporalWorkflowClient{
		sessions: make(map[string]*planningdomain.Session),
		owners:   make(map[string]string),
	}
}

func (c *fakeTemporalWorkflowClient) ExecuteWorkflow(_ context.Context, options client.StartWorkflowOptions, _ any, args ...any) (client.WorkflowRun, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	in := args[0].(planningworkflow.Input)
	intake, err := planningdomain.ParseIntake(in.RepoURL, in.Ref)
	if err != nil {
		return nil, err
	}
	s := &planningdomain.Session{
		ID:      in.SessionID,
		Status:  planningdomain.StatusStarted,
		Profile: planningdomain.ProfileRef{Name: planningdomain.ProfileOnboard},
		Intake:  intake,
	}
	c.sessions[options.ID] = s
	c.owners[options.ID] = in.Actor
	return nil, nil
}

func (c *fakeTemporalWorkflowClient) QueryWorkflow(_ context.Context, workflowID string, _ string, queryType string, _ ...any) (converter.EncodedValue, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.sessions[workflowID]
	if !ok {
		return nil, planningdomain.ErrNotFound
	}
	if queryType == planningworkflow.QueryOwner {
		return fakeEncodedValue{val: c.owners[workflowID]}, nil
	}
	return fakeEncodedValue{val: planningdomain.CloneSession(s)}, nil
}

func (c *fakeTemporalWorkflowClient) UpdateWorkflow(_ context.Context, options client.UpdateWorkflowOptions) (client.WorkflowUpdateHandle, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.sessions[options.WorkflowID]
	if !ok {
		return nil, planningdomain.ErrNotFound
	}
	switch options.UpdateName {
	case planningworkflow.UpdateTurn:
		req := options.Args[0].(planningworkflow.TurnRequest)
		planText := `{"summary":"Excalidraw web app","baseImage":"cgr.dev/chainguard/node"}`
		plan := planningdomain.Plan{Text: planText}
		plan.SHA256 = planningdomain.PlanHash(plan)
		s.Status = planningdomain.StatusReady
		s.Messages = append(s.Messages,
			planningdomain.Message{Role: planningdomain.RoleOperator, Text: req.Message},
			planningdomain.Message{Role: planningdomain.RoleModel, Text: "Generated Excalidraw plan."},
		)
		s.Inspection = &planningdomain.Inspection{Language: "typescript", PackageManager: "yarn", ToolCalls: []string{"inspect_repo"}}
		s.Plan = &plan
		s.ResolvedCommit = strings.Repeat("a", 40)
		s.Model = planningdomain.DefaultModelLabel
		s.Profile = planningdomain.ProfileRef{Name: "onboard", Version: "1", Hash: strings.Repeat("b", 64)}
	}
	cp := planningdomain.CloneSession(s)
	return fakeUpdateHandle{session: cp}, nil
}

func TestPlanningHandlerWithTemporalServesSessionsAndSSEAgentRuns(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	wfClient := newFakeTemporalWorkflowClient()
	handler := planningHandlerWithTemporal(
		http.NotFoundHandler(),
		repository,
		controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true},
		nil,
		wfClient,
	)

	resp := planningRequest(t, handler, "alice", "POST", "/v1/planning-sessions", `{"repoUrl":"https://github.com/excalidraw/excalidraw","ref":"main"}`)
	var created planningdomain.Session
	if resp.Code != http.StatusCreated || json.Unmarshal(resp.Body.Bytes(), &created) != nil || created.ID == "" {
		t.Fatalf("create via Temporal adapter: %d %s", resp.Code, resp.Body.String())
	}

	// Verify SSE /v1/agent-runs streams structured tool calls and plan over the same Temporal-backed session
	sseBodyJSON, _ := json.Marshal(map[string]any{
		"threadId":        created.ID,
		"runId":           "run-1",
		"protocolVersion": "1.0",
		"messages": []any{
			map[string]any{"id": "user-1", "role": "user", "content": "Package Excalidraw with Zarf and UDS"},
		},
	})
	sseResp := planningRequest(t, handler, "alice", "POST", "/v1/agent-runs", string(sseBodyJSON))
	if sseResp.Code != http.StatusOK {
		t.Fatalf("SSE /v1/agent-runs: %d %s", sseResp.Code, sseResp.Body.String())
	}
	sseBody := sseResp.Body.String()
	if !strings.Contains(sseBody, "RUN_STARTED") || !strings.Contains(sseBody, "STATE_DELTA") || !strings.Contains(sseBody, "inspect_repo") || !strings.Contains(sseBody, "RUN_FINISHED") {
		t.Fatalf("SSE stream missing expected events:\n%s", sseBody)
	}
}

func TestPlanningAcceptDoesNotInventWorkspaceCompute(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := tmpDir + "/packaging-workflow"
	buildCmd := exec.Command("go", "build", "-o", binPath, "github.com/jonathanagustin/conduit/cmd/packaging-workflow")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build packaging-workflow: %v\n%s", err, string(out))
	}

	remote := newTestGitRemote(t, "excalidraw", map[string]string{
		"package.json":  `{"name":"excalidraw","version":"0.17.6","scripts":{"start":"yarn start"}}`,
		"yarn.lock":     "# yarn lockfile v1\n",
		"tsconfig.json": `{"compilerOptions":{"strict":true}}`,
	})

	t.Setenv("CONDUIT_MODEL_CMD", binPath+" agent-exec")
	// The fixture source is a loopback server, which the default policy refuses.
	t.Setenv("CONDUIT_SOURCE_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("DEVWORKSPACE_GIT_ROOT", tmpDir+"/devworkspaces")

	repository := controlplane.NewMemoryRepository()
	opts := controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		BootstrapAdministrators:              []string{"alice"},
	}
	spaces := assembleDevWorkspace(repository, opts, os.Getenv)
	devMounted := mountDevWorkspace(http.NotFoundHandler(), repository, opts, spaces)
	handler, err := mountPlanning(devMounted, repository, opts)
	if err != nil {
		t.Fatalf("mountPlanning: %v", err)
	}

	// 1. Create planning session for Excalidraw
	createReq, _ := json.Marshal(map[string]string{
		"repoUrl": remote.repoURL,
		"ref":     "main",
	})
	resp := planningRequest(t, handler, "alice", "POST", "/v1/planning-sessions", string(createReq))
	var created planningdomain.Session
	if resp.Code != http.StatusCreated || json.Unmarshal(resp.Body.Bytes(), &created) != nil || created.ID == "" {
		t.Fatalf("create session: %d %s", resp.Code, resp.Body.String())
	}

	// 2. Turn 1 via SSE /v1/agent-runs -> real agent inspects repo & elicits node_version NeedsInput
	turn1Body, _ := json.Marshal(map[string]any{
		"threadId":        created.ID,
		"runId":           "run-1",
		"protocolVersion": "1.0",
		"messages": []any{
			map[string]any{"id": "u1", "role": "user", "content": "Inspect Excalidraw and prepare packaging"},
		},
	})
	sse1 := planningRequest(t, handler, "alice", "POST", "/v1/agent-runs", string(turn1Body))
	if sse1.Code != http.StatusOK || !strings.Contains(sse1.Body.String(), "inspect_repo") || !strings.Contains(sse1.Body.String(), "node_version") {
		t.Fatalf("SSE turn 1 missing inspect_repo or node_version:\n%s", sse1.Body.String())
	}

	// 3. Submit answer and run Turn 2 -> real agent produces Ready plan
	ansResp := planningRequest(t, handler, "alice", "POST", "/v1/planning-sessions/"+created.ID+"/answers", `{"answers":[{"id":"node_version","value":"20"}]}`)
	if ansResp.Code != http.StatusOK {
		t.Fatalf("submit answers: %d %s", ansResp.Code, ansResp.Body.String())
	}
	turn2Body, _ := json.Marshal(map[string]any{
		"threadId":        created.ID,
		"runId":           "run-2",
		"protocolVersion": "1.0",
		"messages": []any{
			map[string]any{"id": "u2", "role": "user", "content": "Write the Excalidraw Zarf and UDS packaging plan"},
		},
	})
	sse2 := planningRequest(t, handler, "alice", "POST", "/v1/agent-runs", string(turn2Body))
	if sse2.Code != http.StatusOK || !strings.Contains(sse2.Body.String(), "Excalidraw") {
		t.Fatalf("SSE turn 2 missing Excalidraw plan:\n%s", sse2.Body.String())
	}

	// 4. Accept session
	accResp := planningRequest(t, handler, "alice", "POST", "/v1/planning-sessions/"+created.ID+"/accept", `{}`)
	if accResp.Code != http.StatusOK {
		t.Fatalf("accept session: %d %s", accResp.Code, accResp.Body.String())
	}

	// Configuring a source root and executable planner does not configure a real
	// image builder, image inspector or package creator. Do not fabricate success.
	beforeAudit, err := repository.ListAuthorizationAudit(context.Background(), "planning-session/"+created.ID)
	if err != nil {
		t.Fatal(err)
	}
	compResp := planningRequest(t, handler, "alice", "POST", "/v1/planning-sessions/"+created.ID+"/compute", `{}`)
	if compResp.Code != http.StatusNotImplemented || !strings.Contains(compResp.Body.String(), planningdomain.CodeComputeNotConfigured) {
		t.Fatalf("unconfigured compute: %d %s", compResp.Code, compResp.Body.String())
	}
	wsID := "wsp_" + created.ID
	treeResp := planningRequest(t, handler, "alice", "GET", "/v1/dev-workspaces/"+wsID+"/tree", "")
	if treeResp.Code != http.StatusNotFound {
		t.Fatalf("unconfigured compute created a workspace: %d %s", treeResp.Code, treeResp.Body.String())
	}
	afterAudit, err := repository.ListAuthorizationAudit(context.Background(), "planning-session/"+created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeAudit, afterAudit) {
		t.Fatal("unconfigured compute wrote a packaging audit")
	}
	workspaceDir := tmpDir + "/devworkspaces/workspaces/wsp_" + created.ID
	if _, err := os.Stat(workspaceDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unconfigured compute changed workspace: %v", err)
	}
	bindingResp := planningRequest(t, handler, "alice", "GET", "/v1/planning-sessions/"+created.ID+"/binding", "")
	var bound planningdomain.Binding
	if bindingResp.Code != http.StatusOK || json.Unmarshal(bindingResp.Body.Bytes(), &bound) != nil {
		t.Fatalf("accepted binding was lost: %d %s", bindingResp.Code, bindingResp.Body.String())
	}
	if bound.ResolvedCommit != remote.defaultSHA || bound.ResolvedCommit == legacyPlaceholderSHA {
		t.Fatalf("bound ResolvedCommit = %q, want %q", bound.ResolvedCommit, remote.defaultSHA)
	}
}
