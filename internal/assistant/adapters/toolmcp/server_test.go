package toolmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jonathanagustin/conduit/internal/assistant/application"
	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

type invocation struct {
	actor     application.Actor
	mode      string
	name      string
	arguments string
	origin    string
}

type fakeTools struct {
	calls  []invocation
	result application.Result
	err    error
}

func (tools *fakeTools) Invoke(ctx context.Context, actor application.Actor, mode, name string, arguments json.RawMessage) (application.Result, error) {
	tools.calls = append(tools.calls, invocation{actor: actor, mode: mode, name: name, arguments: string(arguments), origin: OriginOf(ctx, "console")})
	return tools.result, tools.err
}

func serve(t *testing.T, tools Invoker) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(Handler(Config{Tools: tools, Caller: func(request *http.Request) (application.Actor, error) {
		subject := request.Header.Get("X-Test-Subject")
		if subject == "" {
			return application.Actor{}, errors.New("unauthorized")
		}
		return application.Actor{Username: subject, Groups: []string{"Viewer"}}, nil
	}}))
	t.Cleanup(server.Close)
	return server
}

type subjectTransport struct{ subject string }

func (transport subjectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("X-Test-Subject", transport.subject)
	return http.DefaultTransport.RoundTrip(request)
}

// connect drives the endpoint with the MCP Go SDK's own client, a second
// client implementation besides the hand-written one in the Hub tests.
func connect(t *testing.T, server *httptest.Server, subject string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "sdk-test-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: server.URL, HTTPClient: &http.Client{Transport: subjectTransport{subject: subject}}, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestToolsAreTheReadOnlyModeAllowlist(t *testing.T) {
	if got, want := Tools(), []string{domain.ToolQuery, domain.ToolDiff}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tools = %v, want %v", got, want)
	}
	for _, name := range Tools() {
		if !domain.ModeAllows(Mode, name) {
			t.Fatalf("%s is served but the assistant's %s mode does not allow it", name, Mode)
		}
	}
	for _, write := range []string{domain.ToolWrite, domain.ToolPropose} {
		if domain.ModeAllows(Mode, write) {
			t.Fatalf("the MCP mode %s allows %s", Mode, write)
		}
	}
}

func TestSDKClientCallReachesTheToolPortAsTheCallerInReadMode(t *testing.T) {
	tools := &fakeTools{result: application.Result{Tool: domain.ToolQuery, OK: true, MatchedCount: 1, AuditEventID: "aud_1",
		Entities: []application.ObjectHit{{ID: "dev", Type: "Environment", Label: "dev"}}}}
	session := connect(t, serve(t, tools), "ada")
	if version := session.InitializeResult().ProtocolVersion; version != ProtocolVersion {
		t.Fatalf("negotiated protocol version %q, want the pinned %q", version, ProtocolVersion)
	}
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	if !reflect.DeepEqual(names, Tools()) {
		t.Fatalf("tools/list = %v, want %v", names, Tools())
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: domain.ToolQuery, Arguments: json.RawMessage(`{"objectType":"Environment"}`),
	})
	if err != nil || result.IsError {
		t.Fatalf("call = %+v, %v", result, err)
	}
	want := invocation{
		actor: application.Actor{Username: "ada", Groups: []string{"Viewer"}}, mode: domain.ModeConfigDiff,
		name: domain.ToolQuery, arguments: `{"objectType":"Environment"}`, origin: Origin,
	}
	if len(tools.calls) != 1 || !reflect.DeepEqual(tools.calls[0], want) {
		t.Fatalf("the tool port saw %+v, want %+v", tools.calls, want)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if text != `{"tool":"query_ontology_objects","ok":true,"matchedCount":1,"entities":[{"id":"dev","objectType":"Environment","label":"dev"}],"auditEventId":"aud_1"}` {
		t.Fatalf("result text = %s", text)
	}

	// A write tool is not on the endpoint, whatever the tool port would do.
	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: domain.ToolWrite, Arguments: json.RawMessage(`{"action":"promote"}`)}); err == nil {
		t.Fatal("governed_write was callable")
	}
	if len(tools.calls) != 1 {
		t.Fatalf("a write tool reached the tool port: %+v", tools.calls)
	}
}

func TestUnauthenticatedRequestNeverReachesTheToolPort(t *testing.T) {
	tools := &fakeTools{}
	server := serve(t, tools)
	response, err := http.Post(server.URL, "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"query_ontology_objects","arguments":{"objectType":"Environment"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || response.Header.Get("WWW-Authenticate") == "" || len(tools.calls) != 0 {
		t.Fatalf("status %d, %d tool calls", response.StatusCode, len(tools.calls))
	}
}

func TestOversizedResultIsReplacedAndFaultDetailIsWithheld(t *testing.T) {
	big := &fakeTools{result: application.Result{Tool: domain.ToolDiff, OK: true, AuditEventID: "aud_2", ChangeRequestID: "crq_1",
		Hunks: []application.Hunk{{Path: "values", Before: "", After: strings.Repeat("x", 2*MaxResultBytes)}}}}
	result := call(context.Background(), big, application.Actor{Username: "ada"}, domain.ToolDiff, json.RawMessage(`{"changeRequestId":"crq_1"}`))
	text := result.Content[0].(*mcp.TextContent).Text
	if len(text) > MaxResultBytes || !strings.Contains(text, `"truncated":true`) || !strings.Contains(text, `"auditEventId":"aud_2"`) || strings.Contains(text, "xxxx") {
		t.Fatalf("bounded result (%d bytes) = %.200s", len(text), text)
	}
	// Exactly at the limit is returned whole.
	fits := &fakeTools{result: application.Result{Tool: domain.ToolDiff, OK: true, Hunks: []application.Hunk{{Path: "p", After: "small"}}}}
	if whole := call(context.Background(), fits, application.Actor{}, domain.ToolDiff, nil); strings.Contains(whole.Content[0].(*mcp.TextContent).Text, "truncated") {
		t.Fatal("a small result was replaced")
	}

	broken := &fakeTools{err: errors.New("pq: connection to 10.0.0.7 refused")}
	failed := call(context.Background(), broken, application.Actor{}, domain.ToolQuery, nil)
	if text := failed.Content[0].(*mcp.TextContent).Text; !failed.IsError || strings.Contains(text, "10.0.0.7") {
		t.Fatalf("a store fault's detail was returned: %s", text)
	}
}
