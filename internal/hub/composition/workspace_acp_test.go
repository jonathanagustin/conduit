package composition

import (
	"bufio"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"errors"
	"github.com/jonathanagustin/conduit/api/agui"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	devdomain "github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	httpidentity "github.com/jonathanagustin/conduit/internal/identity/adapters/httpidentity"
	oidcverifier "github.com/jonathanagustin/conduit/internal/identity/adapters/oidcverifier"
	identitydomain "github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/workspaceacp/adapters/memorybridge"
	acpdomain "github.com/jonathanagustin/conduit/internal/workspaceacp/domain"
)

type oidcTestHarness struct {
	handler    http.Handler
	repository controlplane.Repository
	mintToken  func(username string, groups []string) string
}

func setupKeycloakOIDCHub(t *testing.T) oidcTestHarness {
	t.Helper()
	now := time.Now().UTC()
	clock := func() time.Time { return now }
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := identitydomain.NormalizeClaimPolicy(identitydomain.ClaimPolicy{
		Issuer:               "https://keycloak.example.test/realms/conduit",
		ClientID:             "conduit-console",
		UsernameClaim:        "preferred_username",
		GroupsClaim:          "groups",
		TokenPurposeClaim:    "typ",
		ExpectedTokenPurpose: "Bearer",
		RequiredScopes:       []string{"openid"},
	})
	if err != nil {
		t.Fatal(err)
	}
	keySet := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&privKey.PublicKey}}
	resolver := httpidentity.OperatorResolver(policy, oidcverifier.NewWithKeySet(policy, keySet, clock), clock)

	repo := controlplane.NewMemoryRepository()
	gov := controlplane.NewGovernanceService(repo)
	ctx := t.Context()

	if _, err := gov.PutTeam(ctx, controlplane.Team{
		Name:        "workspace-devs",
		DisplayName: "Workspace Developers",
		AdminUsers:  []string{"alice"},
		MemberUsers: []string{"alice", "carol"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := gov.PutTeam(ctx, controlplane.Team{
		Name:        "release-reviewers",
		DisplayName: "Release Reviewers",
		AdminUsers:  []string{"bob"},
		MemberUsers: []string{"bob"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := gov.PutGrant(ctx, controlplane.ResourceGrant{
		ResourceRID: "environment/dev-use2",
		Team:        "workspace-devs",
		Operations: []string{
			controlplane.OperationReadResource,
			controlplane.OperationProposeChange,
			controlplane.OperationOperate,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := gov.PutGrant(ctx, controlplane.ResourceGrant{
		ResourceRID: "environment/dev-use2",
		Team:        "release-reviewers",
		Operations: []string{
			controlplane.OperationReadResource,
			controlplane.OperationOperate,
		},
	}); err != nil {
		t.Fatal(err)
	}

	hub, err := NewHub(repo, controlplane.HandlerOptions{
		OperatorIdentityResolver: resolver,
		BootstrapAdministrators:  []string{"root"},
	}, Options{WorkspaceACPBridges: memorybridge.NewPool()})
	if err != nil {
		t.Fatal(err)
	}

	mint := func(username string, groups []string) string {
		signer, signErr := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: privKey}, nil)
		if signErr != nil {
			t.Fatal(signErr)
		}
		claims := jwt.Claims{
			Issuer:    policy.Issuer,
			Subject:   "sub-" + username,
			Audience:  jwt.Audience{policy.ClientID},
			IssuedAt:  jwt.NewNumericDate(now.Add(-30 * time.Second)),
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)),
			Expiry:    jwt.NewNumericDate(now.Add(30 * time.Minute)),
		}
		extra := map[string]any{
			"preferred_username": username,
			"groups":             groups,
			"typ":                "Bearer",
			"scope":              "openid profile",
		}
		tok, serErr := jwt.Signed(signer).Claims(claims).Claims(extra).Serialize()
		if serErr != nil {
			t.Fatal(serErr)
		}
		return tok
	}

	return oidcTestHarness{
		handler:    hub,
		repository: repo,
		mintToken:  mint,
	}
}

func doBearerJSON(t *testing.T, handler http.Handler, token, method, path, body string, extraHeaders map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestWorkspaceACPOverOIDCGovernanceAuditAndConsoleMirror(t *testing.T) {
	t.Parallel()
	h := setupKeycloakOIDCHub(t)
	srv := httptest.NewServer(h.handler)
	defer srv.Close()

	aliceToken := h.mintToken("alice", []string{"workspace-devs"})
	bobToken := h.mintToken("bob", []string{"release-reviewers"})
	carolToken := h.mintToken("carol", []string{"workspace-devs"})

	// 1. No direct /api/assistant or unguarded bridge route exists on the Hub.
	for _, directPath := range []string{"/api/assistant/health", "/api/assistant/threads", "/acp"} {
		res := doBearerJSON(t, h.handler, aliceToken, http.MethodGet, directPath, "", nil)
		if res.Code != http.StatusNotFound {
			t.Fatalf("direct path %s returned %d, want 404", directPath, res.Code)
		}
	}

	// 2. Forged insecure identity header is rejected when Keycloak/OIDC is active.
	forgedReq := httptest.NewRequest(http.MethodPost, "/v1/dev-workspaces", strings.NewReader(`{"profileId":"go-service","environmentId":"dev-use2","title":"Forged"}`))
	forgedReq.Header.Set("Content-Type", "application/json")
	forgedReq.Header.Set("X-Platform-Subject", "alice")
	forgedRec := httptest.NewRecorder()
	h.handler.ServeHTTP(forgedRec, forgedReq)
	if forgedRec.Code != http.StatusUnauthorized {
		t.Fatalf("forged X-Platform-Subject under OIDC returned %d, want 401", forgedRec.Code)
	}

	// 3. Create workspace as alice via Keycloak OIDC token.
	createdRes := doBearerJSON(t, h.handler, aliceToken, http.MethodPost, "/v1/dev-workspaces", `{"profileId":"go-service","environmentId":"dev-use2","title":"ACP Workspace"}`, nil)
	if createdRes.Code != http.StatusCreated {
		t.Fatalf("create workspace = %d %s", createdRes.Code, createdRes.Body.String())
	}
	var createdWS struct {
		ID string `json:"workspaceId"`
	}
	if err := json.Unmarshal(createdRes.Body.Bytes(), &createdWS); err != nil {
		t.Fatal(err)
	}
	acpPath := "/v1/dev-workspaces/" + createdWS.ID + "/acp"
	branch := acpdomain.DefaultBranch("alice", createdWS.ID)

	// 4. Protocol version pin check during initialize.
	badInit := doBearerJSON(t, h.handler, aliceToken, http.MethodPost, acpPath, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":99}}`, nil)
	if badInit.Code != http.StatusBadRequest {
		t.Fatalf("initialize(v99) = %d %s, want 400", badInit.Code, badInit.Body.String())
	}
	goodInit := doBearerJSON(t, h.handler, aliceToken, http.MethodPost, acpPath, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`, map[string]string{"X-Conduit-Workspace-Branch": branch})
	if goodInit.Code != http.StatusOK {
		t.Fatalf("initialize(v1) = %d %s", goodInit.Code, goodInit.Body.String())
	}

	// 5. Single-user and single-branch session scoping: carol cannot open ACP on alice's workspace,
	// and alice cannot bind a branch outside workspace/alice/<workspaceID>.
	crossUser := doBearerJSON(t, h.handler, carolToken, http.MethodPost, acpPath, `{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}`, nil)
	if crossUser.Code != http.StatusForbidden {
		t.Fatalf("carol session/new on alice workspace = %d, want 403", crossUser.Code)
	}
	escapeBranch := doBearerJSON(t, h.handler, aliceToken, http.MethodPost, acpPath, `{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"_meta":{"branch":"refs/heads/main"}}}`, nil)
	if escapeBranch.Code != http.StatusForbidden {
		t.Fatalf("branch escape session/new = %d, want 403", escapeBranch.Code)
	}

	sessRes := doBearerJSON(t, h.handler, aliceToken, http.MethodPost, acpPath, `{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}`, map[string]string{"X-Conduit-Workspace-Branch": branch})
	if sessRes.Code != http.StatusOK {
		t.Fatalf("session/new = %d %s", sessRes.Code, sessRes.Body.String())
	}
	var sessEnvelope struct {
		Result struct {
			SessionID string `json:"sessionId"`
			Meta      struct {
				ThreadID string `json:"threadId"`
				Branch   string `json:"branch"`
			} `json:"_meta"`
		} `json:"result"`
	}
	if err := json.Unmarshal(sessRes.Body.Bytes(), &sessEnvelope); err != nil {
		t.Fatal(err)
	}
	sessionID := sessEnvelope.Result.SessionID
	threadID := sessEnvelope.Result.Meta.ThreadID
	if sessEnvelope.Result.Meta.Branch != branch || !strings.HasPrefix(threadID, "wsp-acp-") {
		t.Fatalf("unexpected session metadata: %+v", sessEnvelope.Result)
	}

	// 6. Denied low-risk file edit blocks the change!
	deniedGate, deniedFrames := runPromptAndSettle(t, srv.URL, aliceToken, createdWS.ID, branch, sessionID, "run-deny-1", "edit README.md: should-not-land", func(gate acpdomain.PermissionGate) {
		res := doBearerJSON(t, h.handler, aliceToken, http.MethodPost,
			"/v1/dev-workspaces/"+createdWS.ID+"/acp/permissions/"+gate.RequestID,
			`{"sessionId":"`+sessionID+`","runId":"run-deny-1","decision":"deny"}`,
			map[string]string{"X-Conduit-Workspace-Branch": branch},
		)
		if res.Code != http.StatusOK {
			t.Fatalf("deny permission = %d %s", res.Code, res.Body.String())
		}
	})
	if deniedGate.RiskTier != acpdomain.RiskLowConfirm {
		t.Fatalf("expected low_confirm risk tier, got %s", deniedGate.RiskTier)
	}
	_ = deniedFrames
	fileCheck1 := doBearerJSON(t, h.handler, aliceToken, http.MethodGet, "/v1/dev-workspaces/"+createdWS.ID+"/file?path=README.md", "", nil)
	if strings.Contains(fileCheck1.Body.String(), "should-not-land") {
		t.Fatalf("denied edit modified README.md: %s", fileCheck1.Body.String())
	}

	// 7. Confirmed low-risk file edit commits to workspace AND mirrors to console AG-UI stream!
	confirmedGate, _ := runPromptAndSettle(t, srv.URL, aliceToken, createdWS.ID, branch, sessionID, "run-edit-2", "edit README.md: confirmed-acp-edit", func(gate acpdomain.PermissionGate) {
		// Before confirming, verify README.md is still unchanged while pending!
		pendingFile := doBearerJSON(t, h.handler, aliceToken, http.MethodGet, "/v1/dev-workspaces/"+createdWS.ID+"/file?path=README.md", "", nil)
		if strings.Contains(pendingFile.Body.String(), "confirmed-acp-edit") {
			t.Fatalf("pending edit modified README.md before Confirm: %s", pendingFile.Body.String())
		}
		res := doBearerJSON(t, h.handler, aliceToken, http.MethodPost,
			"/v1/dev-workspaces/"+createdWS.ID+"/acp/permissions/"+gate.RequestID,
			`{"sessionId":"`+sessionID+`","runId":"run-edit-2","decision":"confirm"}`,
			map[string]string{"X-Conduit-Workspace-Branch": branch},
		)
		if res.Code != http.StatusOK {
			t.Fatalf("confirm permission = %d %s", res.Code, res.Body.String())
		}
	})
	if confirmedGate.RiskTier != acpdomain.RiskLowConfirm {
		t.Fatalf("expected low_confirm, got %s", confirmedGate.RiskTier)
	}
	fileCheck2 := doBearerJSON(t, h.handler, aliceToken, http.MethodGet, "/v1/dev-workspaces/"+createdWS.ID+"/file?path=README.md", "", nil)
	if !strings.Contains(fileCheck2.Body.String(), "confirmed-acp-edit") {
		t.Fatalf("confirmed edit missing from workspace README.md: %s", fileCheck2.Body.String())
	}

	// 8. High-risk command ("run command: git push origin main") opens a ChangeRequest,
	// blocks while pending (409), blocks until a separate human reviewer (bob) approves!
	runPromptAndSettle(t, srv.URL, aliceToken, createdWS.ID, branch, sessionID, "run-cmd-3", "run command: git push origin main", func(gate acpdomain.PermissionGate) {
		if gate.RiskTier != acpdomain.RiskHighChangeRequest || gate.ChangeRequestID == "" {
			t.Fatalf("expected high_change_request with ChangeRequestID, got %+v", gate)
		}
		// Pending ChangeRequest blocks owner Confirm with 409 Conflict!
		pendingConfirm := doBearerJSON(t, h.handler, aliceToken, http.MethodPost,
			"/v1/dev-workspaces/"+createdWS.ID+"/acp/permissions/"+gate.RequestID,
			`{"sessionId":"`+sessionID+`","runId":"run-cmd-3","decision":"confirm"}`,
			map[string]string{"X-Conduit-Workspace-Branch": branch},
		)
		if pendingConfirm.Code != http.StatusConflict {
			t.Fatalf("pending ChangeRequest confirm = %d %s, want 409", pendingConfirm.Code, pendingConfirm.Body.String())
		}

		// Author self-approval on the ChangeRequest is refused by governance; separate reviewer bob approves!
		selfApprove := doBearerJSON(t, h.handler, aliceToken, http.MethodPost,
			"/v1/change-requests/"+gate.ChangeRequestID+"/approve",
			`{"rationale":"self approve attempt"}`,
			nil,
		)
		if selfApprove.Code == http.StatusOK {
			t.Fatalf("author self-approved ChangeRequest: %d %s", selfApprove.Code, selfApprove.Body.String())
		}

		bobApprove := doBearerJSON(t, h.handler, bobToken, http.MethodPost,
			"/v1/change-requests/"+gate.ChangeRequestID+"/approve",
			`{"rationale":"reviewed high-risk command"}`,
			nil,
		)
		if bobApprove.Code != http.StatusOK {
			t.Fatalf("bob approve ChangeRequest = %d %s", bobApprove.Code, bobApprove.Body.String())
		}

		// Now owner Confirm settles the governed gate!
		afterApprove := doBearerJSON(t, h.handler, aliceToken, http.MethodPost,
			"/v1/dev-workspaces/"+createdWS.ID+"/acp/permissions/"+gate.RequestID,
			`{"sessionId":"`+sessionID+`","runId":"run-cmd-3","decision":"confirm"}`,
			map[string]string{"X-Conduit-Workspace-Branch": branch},
		)
		if afterApprove.Code != http.StatusOK {
			t.Fatalf("post-review confirm = %d %s", afterApprove.Code, afterApprove.Body.String())
		}
	})

	// 9. Also run one tool_call read turn so all three categories (tool_call, file_write, command) are exercised.
	runPromptAndSettle(t, srv.URL, aliceToken, createdWS.ID, branch, sessionID, "run-read-4", "inspect workspace", func(gate acpdomain.PermissionGate) {
		res := doBearerJSON(t, h.handler, aliceToken, http.MethodPost,
			"/v1/dev-workspaces/"+createdWS.ID+"/acp/permissions/"+gate.RequestID,
			`{"sessionId":"`+sessionID+`","runId":"run-read-4","decision":"confirm"}`,
			map[string]string{"X-Conduit-Workspace-Branch": branch},
		)
		if res.Code != http.StatusOK {
			t.Fatalf("read confirm = %d %s", res.Code, res.Body.String())
		}
	})

	// 10. Verify console AG-UI mirror stream on BOTH /v1/dev-workspaces/{id}/agent-runs AND /v1/agent-runs
	// contains the README.md file edit and passes agui.Sequence!
	for _, streamTarget := range []struct {
		path  string
		runID string
		body  string
	}{
		{
			path:  "/v1/dev-workspaces/" + createdWS.ID + "/agent-runs",
			runID: "console-stream-1",
			body:  `{"threadId":"` + threadID + `","runId":"console-stream-1","protocolVersion":"1.0","messages":[],"forwardedProps":{"conduit":{"mode":"snapshot"}}}`,
		},
		{
			path:  "/v1/agent-runs",
			runID: "console-stream-2",
			body:  `{"threadId":"` + threadID + `","runId":"console-stream-2","protocolVersion":"1.0","messages":[],"forwardedProps":{"conduit":{"mode":"snapshot"}}}`,
		},
	} {
		streamRes := doBearerJSON(t, h.handler, aliceToken, http.MethodPost, streamTarget.path, streamTarget.body, nil)
		if streamRes.Code != http.StatusOK || !strings.HasPrefix(streamRes.Header().Get("Content-Type"), "text/event-stream") {
			t.Fatalf("%s = %d %v %s", streamTarget.path, streamRes.Code, streamRes.Header(), streamRes.Body.String())
		}
		seq := agui.NewSequence(threadID, streamTarget.runID)
		sawFileEdit := false
		for _, line := range strings.Split(streamRes.Body.String(), "\n") {
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			raw := []byte(strings.TrimPrefix(line, "data: "))
			if err := seq.Accept(raw); err != nil {
				t.Fatalf("%s invalid AG-UI frame %s: %v", streamTarget.path, string(raw), err)
			}
			if strings.Contains(string(raw), `"README.md"`) && strings.Contains(string(raw), `"edits"`) {
				sawFileEdit = true
			}
		}
		if !seq.Complete() {
			t.Fatalf("%s AG-UI sequence did not terminate", streamTarget.path)
		}
		if !sawFileEdit {
			t.Fatalf("%s console stream did not include mirrored README.md edit: %s", streamTarget.path, streamRes.Body.String())
		}
	}

	// 11. Verify every tool_call, file_write, and command is recorded in the authorization audit log
	// with sessionId, runId, and threadId!
	auditRecords, err := h.repository.ListAuthorizationAudit(t.Context(), "workspace/"+createdWS.ID)
	if err != nil {
		t.Fatal(err)
	}
	seenActions := map[string]bool{}
	for _, ev := range auditRecords {
		if !strings.HasPrefix(ev.Action, "workspace.acp.") {
			continue
		}
		seenActions[ev.Action] = true
		sid, _ := ev.Context["sessionId"].(string)
		rid, _ := ev.Context["runId"].(string)
		tid, _ := ev.Context["threadId"].(string)
		if sid == "" || rid == "" || tid == "" {
			t.Fatalf("audit event %s missing session/run/thread IDs: %+v", ev.Action, ev.Context)
		}
	}
	for _, required := range []string{
		"workspace.acp.tool_call",
		"workspace.acp.file_write",
		"workspace.acp.command",
		"workspace.acp.permission_requested",
		"workspace.acp.permission_decided",
	} {
		if !seenActions[required] {
			t.Fatalf("missing required audit action %q; seen=%v", required, seenActions)
		}
	}
}

func runPromptAndSettle(
	t *testing.T,
	baseURL, token, workspaceID, branch, sessionID, runID, promptText string,
	onPermission func(acpdomain.PermissionGate),
) (acpdomain.PermissionGate, []map[string]any) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      10,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId": sessionID,
			"prompt":    []any{map[string]any{"type": "text", "text": promptText}},
			"_meta":     map[string]any{"runId": runID, "branch": branch},
		},
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, baseURL+"/v1/dev-workspaces/"+workspaceID+"/acp", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-Conduit-Workspace-Branch", branch)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session/prompt HTTP %d", resp.StatusCode)
	}

	var capturedGate acpdomain.PermissionGate
	var frames []map[string]any
	settled := false
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimPrefix(line, "data: ")
		var frame map[string]any
		if err := json.Unmarshal([]byte(raw), &frame); err != nil {
			t.Fatal(err)
		}
		frames = append(frames, frame)
		if frame["type"] == "permission_request" && !settled {
			settled = true
			reqBytes, _ := json.Marshal(frame["request"])
			if err := json.Unmarshal(reqBytes, &capturedGate); err != nil {
				t.Fatal(err)
			}
			go onPermission(capturedGate)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return capturedGate, frames
}

func TestWorkspaceACPUnconfiguredDoesNotFabricateAgentSessions(t *testing.T) {
	repo := controlplane.NewMemoryRepository()
	getenv := func(string) string { return "" }
	spaces := assembleDevWorkspace(repo, controlplane.HandlerOptions{}, getenv)
	workspace, err := spaces.Create(t.Context(), devdomain.Caller{Username: "alice"}, "profile", "dev", "unconfigured")
	if err != nil {
		t.Fatal(err)
	}
	service := assembleWorkspaceACP(repo, spaces, Options{}, getenv)
	caller := acpdomain.Caller{Username: "alice"}
	if _, err := service.Initialize(t.Context(), caller, workspace.ID, "", 1); !errors.Is(err, acpdomain.ErrDisabled) {
		t.Fatalf("unconfigured initialize: %v", err)
	}
	if _, err := service.NewSession(t.Context(), caller, workspace.ID, "", ""); !errors.Is(err, acpdomain.ErrDisabled) {
		t.Fatalf("fabricated session: %v", err)
	}
}
