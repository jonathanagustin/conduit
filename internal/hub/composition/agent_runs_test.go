package composition

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/api/agui"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
)

func TestAgentRunsSharePlanningOwnershipAndBoundAudit(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	handler := planningHandler(http.NotFoundHandler(), repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"bob"}}, readyPlanningRunner{})
	created := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions", `{"repoUrl":"https://github.com/org/app","ref":"main"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var session planningdomain.Session
	if err := json.Unmarshal(created.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"threadId": session.ID, "runId": "run-1", "protocolVersion": "1.0", "messages": []any{map[string]any{"id": "user-1", "role": "user", "content": "sentinel-secret-context"}}})
	denied := planningRequest(t, handler, "bob", http.MethodPost, "/v1/agent-runs", string(body))
	if denied.Code != http.StatusNotFound || strings.Contains(denied.Body.String(), "data:") {
		t.Fatalf("cross-owner run=%d %s", denied.Code, denied.Body.String())
	}
	response := planningRequest(t, handler, "alice", http.MethodPost, "/v1/agent-runs", string(body))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("run=%d %v %s", response.Code, response.Header(), response.Body.String())
	}
	sequence := agui.NewSequence(session.ID, "run-1")
	for _, line := range strings.Split(response.Body.String(), "\n") {
		if strings.HasPrefix(line, "data: ") {
			if err := sequence.Accept([]byte(strings.TrimPrefix(line, "data: "))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !sequence.Complete() {
		t.Fatal("mounted run did not terminate")
	}
	loaded := planningRequest(t, handler, "alice", http.MethodGet, "/v1/planning-sessions/"+session.ID, "")
	if err := json.Unmarshal(loaded.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Status != planningdomain.StatusReady || session.Plan == nil || session.Binding != nil {
		t.Fatalf("run did not use the native session safely: %+v", session)
	}
	events, err := repository.ListAuthorizationAudit(t.Context(), "planning-session/"+session.ID)
	if err != nil || len(events) < 3 {
		t.Fatalf("audit=%v %v", events, err)
	}
	for _, event := range events {
		if !strings.HasPrefix(event.Action, "agent.") {
			continue
		}
		if event.Subject != "alice" || event.Context["threadId"] != session.ID || event.Context["runId"] != "run-1" || len(event.Context["payloadSha256"].(string)) != 64 {
			t.Fatalf("unbound audit=%+v", event)
		}
	}
	encoded, _ := json.Marshal(events)
	if strings.Contains(string(encoded), "sentinel-secret-context") {
		t.Fatal("run audit stored raw context instead of its digest")
	}
}

func TestAgentRunAuthenticatesBeforeParsingMalformedInput(t *testing.T) {
	handler := planningHandler(http.NotFoundHandler(), controlplane.NewMemoryRepository(), controlplane.HandlerOptions{}, readyPlanningRunner{})
	response := planningRequest(t, handler, "forged-header", http.MethodPost, "/v1/agent-runs", `not-json`)
	if response.Code != http.StatusUnauthorized || strings.Contains(response.Body.String(), "not-json") {
		t.Fatalf("auth boundary=%d %s", response.Code, response.Body.String())
	}
}
