package composition

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/assistant/adapters/toolmcp"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	identity "github.com/jonathanagustin/conduit/internal/identity/domain"
)

// mcpTestClient is a hand-written MCP client for the Streamable HTTP
// transport: one JSON-RPC message per POST. It is deliberately not the SDK's
// client, so the server is driven by the wire protocol and not by the library
// that implements it. It keeps every exchange for the recorded session.
type mcpTestClient struct {
	t        *testing.T
	endpoint string
	subject  string
	groups   string
	version  string
	nextID   int
	session  []mcpExchange
}

type mcpExchange struct {
	Request  json.RawMessage `json:"request"`
	Status   int             `json:"status"`
	Response json.RawMessage `json:"response,omitempty"`
}

type rpcReply struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (client *mcpTestClient) send(method string, params any, notification bool) (int, rpcReply) {
	client.t.Helper()
	message := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		message["params"] = params
	}
	if !notification {
		client.nextID++
		message["id"] = client.nextID
	}
	body, err := json.Marshal(message)
	if err != nil {
		client.t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, client.endpoint, bytes.NewReader(body))
	if err != nil {
		client.t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if client.version != "" {
		request.Header.Set("MCP-Protocol-Version", client.version)
	}
	if client.subject != "" {
		request.Header.Set("X-Platform-Subject", client.subject)
	}
	if client.groups != "" {
		request.Header.Set("X-Platform-Groups", client.groups)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		client.t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		client.t.Fatal(err)
	}
	exchange := mcpExchange{Request: body, Status: response.StatusCode}
	var reply rpcReply
	if json.Valid(raw) {
		exchange.Response = raw
		_ = json.Unmarshal(raw, &reply)
	}
	client.session = append(client.session, exchange)
	return response.StatusCode, reply
}

// handshake is initialize followed by notifications/initialized.
func (client *mcpTestClient) handshake() json.RawMessage {
	client.t.Helper()
	status, reply := client.send("initialize", map[string]any{
		"protocolVersion": toolmcp.ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "conduit-mcp-test-client", "version": "1.0.0"},
	}, false)
	if status != http.StatusOK || reply.Error != nil {
		client.t.Fatalf("initialize = %d %+v", status, reply.Error)
	}
	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(reply.Result, &initialized); err != nil {
		client.t.Fatal(err)
	}
	client.version = initialized.ProtocolVersion
	if status, _ := client.send("notifications/initialized", nil, true); status != http.StatusAccepted {
		client.t.Fatalf("notifications/initialized = %d, want 202", status)
	}
	return reply.Result
}

func (client *mcpTestClient) callTool(name string, arguments any) (rpcReply, map[string]any, string) {
	client.t.Helper()
	status, reply := client.send("tools/call", map[string]any{"name": name, "arguments": arguments}, false)
	if status != http.StatusOK {
		client.t.Fatalf("tools/call %s = %d", name, status)
	}
	if reply.Error != nil {
		return reply, nil, ""
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent map[string]any `json:"structuredContent"`
		IsError           bool           `json:"isError"`
	}
	if err := json.Unmarshal(reply.Result, &result); err != nil || len(result.Content) != 1 || result.Content[0].Type != "text" {
		client.t.Fatalf("tools/call %s result = %s (%v)", name, reply.Result, err)
	}
	return reply, result.StructuredContent, result.Content[0].Text
}

func newMCPHub(t *testing.T, options controlplane.HandlerOptions) (controlplane.Repository, *httptest.Server) {
	t.Helper()
	repository := controlplane.NewMemoryRepository()
	handler, err := NewHub(repository, options, Options{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return repository, server
}

func seedChange(t *testing.T, repository controlplane.Repository, after string) string {
	t.Helper()
	request, err := controlplane.NewGovernanceService(repository).ProposeHumanChange(context.Background(), "PLAN", "sample-app-1:memory", "dev", []controlplane.EntityChange{{
		ID: "memory-limit", Type: controlplane.ChangeTypeUpdateEntity, InstallationID: "sample-app",
		Diff: map[string]controlplane.ChangeDiffEntry{"limits.memory": {Before: "256Mi", After: after}},
		ReferencedResources: map[string]controlplane.ReferencedResource{
			"environment":  {RID: "environment/dev", Name: "dev"},
			"installation": {RID: "installation/dev/sample-app", Name: "sample-app"},
			"product":      {RID: "product/app.example:sample-app", Name: "sample-app"},
		},
	}}, controlplane.Principal{Username: "operator"}, "Proposed memory limit", "headroom")
	if err != nil {
		t.Fatal(err)
	}
	return request.ID
}

func TestMCPHandshakeListsAndCallsTheReadTools(t *testing.T) {
	repository, hub := newMCPHub(t, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	changeID := seedChange(t, repository, "512Mi")
	// local-developer is the development operator the catalog lets read the
	// dev environment; see TestAssistantQueryUsesTheCatalog.
	client := &mcpTestClient{t: t, endpoint: hub.URL + "/v1/mcp", subject: "local-developer"}

	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
		Capabilities map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal(client.handshake(), &initialized); err != nil {
		t.Fatal(err)
	}
	if initialized.ProtocolVersion != "2025-11-25" || initialized.ServerInfo.Name != "conduit-hub" || initialized.Capabilities["tools"] == nil {
		t.Fatalf("initialize result = %+v", initialized)
	}

	status, listed := client.send("tools/list", map[string]any{}, false)
	if status != http.StatusOK || listed.Error != nil {
		t.Fatalf("tools/list = %d %+v", status, listed.Error)
	}
	var tools struct {
		Tools []struct {
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"inputSchema"`
			Annotations struct {
				ReadOnlyHint bool `json:"readOnlyHint"`
			} `json:"annotations"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(listed.Result, &tools); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
		if !tool.Annotations.ReadOnlyHint || tool.InputSchema["type"] != "object" {
			t.Errorf("tool %s is not listed as a read-only tool with an object schema", tool.Name)
		}
	}
	// Exactly the read tools: no governed_write, no propose_governed_action.
	if want := []string{"query_ontology_objects", "review_change_diff"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("tools/list = %v, want %v", names, want)
	}

	_, queried, _ := client.callTool("query_ontology_objects", map[string]any{
		"objectType": "ChangeRequest", "filter": map[string]any{"field": "status", "value": "PENDING_APPROVALS"},
	})
	if queried["ok"] != true || queried["matchedCount"] != float64(1) || queried["auditEventId"] == "" {
		t.Fatalf("query result = %v", queried)
	}
	_, diff, text := client.callTool("review_change_diff", map[string]any{"changeRequestId": changeID})
	if diff["ok"] != true || diff["changeRequestId"] != changeID || !strings.Contains(text, `"after":"512Mi"`) {
		t.Fatalf("diff result = %s", text)
	}

	// Authorization is the caller's: an operator holding no grant on the
	// environment gets no object from the same query.
	stranger := &mcpTestClient{t: t, endpoint: hub.URL + "/v1/mcp", subject: "ada"}
	stranger.handshake()
	_, hidden, _ := stranger.callTool("query_ontology_objects", map[string]any{
		"objectType": "ChangeRequest", "filter": map[string]any{"field": "status", "value": "PENDING_APPROVALS"},
	})
	if hidden["ok"] != true || hidden["matchedCount"] != nil || hidden["entities"] != nil {
		t.Fatalf("an operator with no grant read the catalog over MCP: %v", hidden)
	}

	// The same validation the console gets: an unknown argument is refused.
	_, invalid, _ := client.callTool("query_ontology_objects", map[string]any{"objectType": "Environment", "limit": 5000})
	if invalid["ok"] != false || !strings.Contains(invalid["error"].(string), "tool call is not valid") {
		t.Fatalf("an invalid argument was not refused by the tool contract: %v", invalid)
	}

	if path := os.Getenv("CONDUIT_MCP_RECORD_SESSION"); path != "" {
		record := map[string]any{
			"client":    "conduit-mcp-test-client (internal/hub/composition/mcp_test.go), a hand-written JSON-RPC client; not a third-party MCP client",
			"server":    "the Hub HTTP surface (composition.NewHub) on an in-memory repository, served by net/http/httptest",
			"transport": "Streamable HTTP, stateless, application/json responses",
			"endpoint":  "POST /v1/mcp",
			"exchanges": client.session,
		}
		raw, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The transport is stateless: there is no stream to open and no session to
// delete, so only POST is routed.
func TestMCPServesPostOnly(t *testing.T) {
	_, hub := newMCPHub(t, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		request, _ := http.NewRequest(method, hub.URL+"/v1/mcp", nil)
		request.Header.Set("Accept", "text/event-stream")
		request.Header.Set("X-Platform-Subject", "ada")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusMethodNotAllowed && response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s /v1/mcp = %d, want it refused", method, response.StatusCode)
		}
	}
}

func TestMCPRefusesACallerWithNoOperatorCredential(t *testing.T) {
	// No identity resolver, no development headers, no client certificate.
	repository, hub := newMCPHub(t, controlplane.HandlerOptions{})
	client := &mcpTestClient{t: t, endpoint: hub.URL + "/v1/mcp", subject: "ada"}
	for _, method := range []string{"initialize", "tools/list", "tools/call"} {
		params := map[string]any{"name": "query_ontology_objects", "arguments": map[string]any{"objectType": "Environment"}}
		status, reply := client.send(method, params, false)
		if status != http.StatusUnauthorized || reply.Result != nil {
			t.Fatalf("%s without a credential = %d %s", method, status, reply.Result)
		}
	}
	events, err := repository.ListAuthorizationAudit(context.Background(), "assistant/query_ontology_objects")
	if err != nil || len(events) != 0 {
		t.Fatalf("an unauthenticated call reached the tool port: %d audit events, %v", len(events), err)
	}
}

func TestMCPOffersNoWriteToolAndRunsNone(t *testing.T) {
	repository, hub := newMCPHub(t, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	client := &mcpTestClient{t: t, endpoint: hub.URL + "/v1/mcp", subject: "ada", groups: "Operator"}
	client.handshake()
	for name, arguments := range map[string]any{
		"governed_write": map[string]any{"action": "add_eval_case", "justification": "s", "value": "512Mi", "expectedOutcome": "PASS", "confirm": true},
		"propose_governed_action": map[string]any{
			"actionId": "cancel_packaging_run", "targetEntityId": "sample-app-1", "targetObjectType": "PackagingRun",
			"environment": "dev", "installationId": "sample-app", "productId": "app.example:sample-app",
		},
	} {
		reply, _, _ := client.callTool(name, arguments)
		if reply.Error == nil || reply.Result != nil {
			t.Fatalf("%s was callable over MCP: %s", name, reply.Result)
		}
		for _, resource := range []string{"assistant/" + name, "packaging-run/sample-app-1"} {
			if events, _ := repository.ListAuthorizationAudit(context.Background(), resource); len(events) != 0 {
				t.Fatalf("%s reached the tool port: %d audit events on %s", name, len(events), resource)
			}
		}
	}
	if stored, err := repository.ListChangeRequests(context.Background(), ""); err != nil || len(stored) != 0 {
		t.Fatalf("a change request was opened over MCP: %v %v", stored, err)
	}
}

func TestMCPCallIsAuditedAsTheCallerWithTheMCPOrigin(t *testing.T) {
	repository, hub := newMCPHub(t, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	client := &mcpTestClient{t: t, endpoint: hub.URL + "/v1/mcp", subject: "ada"}
	client.handshake()
	_, result, _ := client.callTool("query_ontology_objects", map[string]any{"objectType": "Environment"})

	// The same tool from the console surface, as another operator.
	console := httptest.NewRequest(http.MethodPost, hub.URL+"/v1/assistant/tools", strings.NewReader(`{"mode":"explore","tool":"query_ontology_objects","arguments":{"objectType":"Environment"}}`))
	console.Header.Set("Content-Type", "application/json")
	console.Header.Set("X-Platform-Subject", "grace")
	console.RequestURI = ""
	response, err := http.DefaultClient.Do(console)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("console call = %v %v", response, err)
	}
	response.Body.Close()

	events, err := repository.ListAuthorizationAudit(context.Background(), "assistant/query_ontology_objects")
	if err != nil || len(events) != 2 {
		t.Fatalf("audit events = %d, %v; want one per call", len(events), err)
	}
	origins := map[string]string{}
	for _, event := range events {
		origins[event.Subject] = event.Context["origin"].(string)
		if event.Action != "assistant.query_ontology_objects" || event.Context["argsSha256"] == "" {
			t.Fatalf("audit event = %+v", event)
		}
		if event.Subject == "ada" && event.ID != result["auditEventId"] {
			t.Fatalf("the MCP result names audit event %v, the chain holds %s", result["auditEventId"], event.ID)
		}
	}
	if want := map[string]string{"ada": "mcp", "grace": "console"}; !reflect.DeepEqual(origins, want) {
		t.Fatalf("audit origins by subject = %v, want %v", origins, want)
	}
}

func TestMCPBoundsAnOversizedResult(t *testing.T) {
	repository, hub := newMCPHub(t, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	changeID := seedChange(t, repository, strings.Repeat("A", 4*toolmcp.MaxResultBytes))
	client := &mcpTestClient{t: t, endpoint: hub.URL + "/v1/mcp", subject: "ada"}
	client.handshake()
	reply, result, text := client.callTool("review_change_diff", map[string]any{"changeRequestId": changeID})
	if len(text) > toolmcp.MaxResultBytes || len(reply.Result) > 3*toolmcp.MaxResultBytes {
		t.Fatalf("an oversized result was returned whole: %d text bytes, %d result bytes", len(text), len(reply.Result))
	}
	if result["truncated"] != true || result["bytes"].(float64) < float64(4*toolmcp.MaxResultBytes) || result["hunks"] != nil || result["auditEventId"] == "" {
		t.Fatalf("the bounded result = %s", text)
	}
	if strings.Contains(text, "AAAA") {
		t.Fatal("the bounded result still carries the oversized content")
	}
}

// A signed-in browser's session cookie is an operator credential here as it is
// everywhere else, and under the same rule: a cookie-authenticated POST must
// come from the console's origin and carry the CSRF header. A bearer token or
// a client certificate is exempt, which is what an MCP client sends.
func TestMCPAcceptsASessionCookieOnlyWithTheCSRFHeader(t *testing.T) {
	t.Parallel()
	harness := newSignInHarness(t, signInSetup{})
	session := harness.signIn()
	list := func(edit func(*http.Request)) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, signInOrigin+"/v1/mcp",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		request.Header.Set("MCP-Protocol-Version", toolmcp.ProtocolVersion)
		request.AddCookie(&http.Cookie{Name: session.Name, Value: session.Value})
		edit(request)
		return harness.do(request)
	}
	if response := list(func(request *http.Request) { request.Header.Set("Origin", signInOrigin) }); response.Code != http.StatusUnauthorized {
		t.Fatalf("a cookie POST with no CSRF header answered %d %s, want 401", response.Code, response.Body)
	}
	if response := list(func(request *http.Request) {
		request.Header.Set("Origin", "https://evil.example")
		request.Header.Set(identity.CSRFHeaderName, identity.CSRFHeaderValue)
	}); response.Code != http.StatusUnauthorized {
		t.Fatalf("a cross-site cookie POST answered %d, want 401", response.Code)
	}
	response := list(func(request *http.Request) {
		request.Header.Set("Origin", signInOrigin)
		request.Header.Set(identity.CSRFHeaderName, identity.CSRFHeaderValue)
	})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), toolmcp.Tools()[0]) {
		t.Fatalf("a console-origin cookie POST with the CSRF header answered %d %s", response.Code, response.Body)
	}
}
