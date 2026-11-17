// Package modelhttpfixture serves deterministic model replies only for tests.
// It has no executable entry point and is never wired into a product service.
package modelhttpfixture

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Configure binds a loopback Chat Completions fixture and clears model command
// configuration. Every fixture must receive a real HTTP completion request.
func Configure(t *testing.T, complete func(string) (string, error)) *httptest.Server {
	t.Helper()
	if complete == nil {
		complete = planningReply
	}
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", 400)
			return
		}
		var body struct {
			Model    string                           `json:"model"`
			Messages []struct{ Role, Content string } `json:"messages"`
			Stream   bool                             `json:"stream"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&body); err != nil || body.Model != "fixture-requested" || len(body.Messages) != 1 || body.Stream {
			t.Errorf("unexpected completion request: %+v / %v", body, err)
			http.Error(w, "invalid fixture request", 400)
			return
		}
		calls.Add(1)
		reply, err := complete(body.Messages[0].Content)
		if err != nil {
			t.Error(err)
			http.Error(w, "fixture does not support this prompt", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "fixture-served", "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": reply}}}})
	}))
	t.Cleanup(func() {
		server.Close()
		if calls.Load() == 0 {
			t.Error("model HTTP fixture received no completion requests")
		}
	})
	for _, key := range []string{"CONDUIT_MODEL_CMD", "CONDUIT_MODEL_API_KEY_FILE", "CONDUIT_MODEL_CA_FILE", "CONDUIT_AGENT_PROVIDER"} {
		t.Setenv(key, "")
	}
	for _, capability := range []string{"ASSISTANT", "PLANNING", "PACKAGING", "APPGEN"} {
		for _, field := range []string{"CMD", "NAME", "TIMEOUT"} {
			t.Setenv("CONDUIT_"+capability+"_MODEL_"+field, "")
		}
	}
	t.Setenv("CONDUIT_MODEL_BASE_URL", server.URL+"/v1")
	t.Setenv("CONDUIT_MODEL_NAME", "fixture-requested")
	t.Setenv("CONDUIT_MODEL_TIMEOUT", "5s")
	return server
}

func planningReply(prompt string) (string, error) {
	const read = `{"toolCalls":[{"name":"read_file","arguments":{"path":"package.json"}}]}`
	if strings.Contains(prompt, "Inspect the bound source repository for this planning session.") {
		if !strings.Contains(prompt, "inspect_repo: ") {
			return `{"toolCalls":[{"name":"inspect_repo","arguments":{}}]}`, nil
		}
		if !strings.Contains(prompt, "read_file: ") {
			return read, nil
		}
		return "Test fixture inspected the bound source via the permitted tools.", nil
	}
	if !strings.Contains(prompt, "read_file: ") {
		return read, nil
	}
	if strings.Contains(prompt, "Decide what you must ask the operator") {
		return `{"questions":[{"id":"node_version","prompt":"Which Node runtime?","choices":["18","20"],"required":true}]}`, nil
	}
	if strings.Contains(prompt, "Write the packaging plan for this source repository.") {
		return `{"summary":"Excalidraw deterministic HTTP test fixture plan","baseImage":"node:20-slim","buildSteps":["npm ci","npm run build"],"ports":[8080],"networkAllow":["registry.npmjs.org:443"],"sso":{"required":true,"notes":"fixture"},"risks":["test fixture; not real provider evidence"]}`, nil
	}
	return "", fmt.Errorf("unrecognized deterministic test prompt")
}
