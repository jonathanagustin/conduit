package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/api/agui"
	"github.com/jonathanagustin/conduit/internal/assistant/adapters/threadmemory"
	"github.com/jonathanagustin/conduit/internal/assistant/application"
	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

type scripted struct {
	mu      sync.Mutex
	replies []string
	calls   int
}

func (model *scripted) Complete(context.Context, string) (string, domain.Provenance, error) {
	model.mu.Lock()
	defer model.mu.Unlock()
	index := model.calls
	model.calls++
	if index >= len(model.replies) {
		index = len(model.replies) - 1
	}
	return model.replies[index], domain.Provenance{Adapter: "script"}, nil
}

type objects struct{}

func (objects) Query(context.Context, application.Actor, domain.QueryArgs) (application.ObjectPage, error) {
	return application.ObjectPage{Hits: []application.ObjectHit{{ID: "env-zeta-7", Type: "Environment", Label: "zeta"}}, ReadOps: 1, Total: 1}, nil
}

type audit struct{}

func (audit) Record(context.Context, application.AuditRecord) (string, error) { return "aud_1", nil }

const (
	askForEnvironments = `{"text":"looking","toolCalls":[{"name":"query_ontology_objects","arguments":{"objectType":"Environment"}}]}`
	answer             = `{"text":"One environment: zeta."}`
)

// handler serves the real application service. The caller is whoever the
// X-Test-User header names, which is the seam the Hub fills with a principal.
func handler(model domain.Model) http.Handler {
	service := application.Service{Model: model, Objects: objects{}, Audit: audit{}, Threads: threadmemory.New()}
	mux := http.NewServeMux()
	Register(mux, Config{
		Tools: service,
		Authorize: func(request *http.Request) error {
			if request.Header.Get("X-Test-User") == "" {
				return errors.New("no credential")
			}
			return nil
		},
		Caller: func(request *http.Request) (application.Actor, error) {
			return application.Actor{Username: request.Header.Get("X-Test-User")}, nil
		},
	})
	return mux
}

func call(t *testing.T, server http.Handler, user, method, path, body string, headers ...string) (int, map[string]any, string) {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != "" {
		request.Header.Set("X-Test-User", user)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	raw := response.Body.String()
	var decoded map[string]any
	if strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatalf("%s %s: body is not JSON: %s", method, path, raw)
		}
	}
	return response.Code, decoded, raw
}

func TestTheOriginalTurnRequestStillWorksAndGainsTheLoop(t *testing.T) {
	server := handler(&scripted{replies: []string{askForEnvironments, answer}})
	status, body, raw := call(t, server, "ada", http.MethodPost, "/v1/assistant/turns",
		`{"mode":"explore","message":"which environments exist?","context":[{"id":"dev","objectType":"Environment","label":"dev"}]}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d %s", status, raw)
	}
	// Every field the console reads today is still there.
	if body["label"] != domain.LabelLiveModel || body["text"] != "One environment: zeta." {
		t.Fatalf("label/text = %v / %v", body["label"], body["text"])
	}
	results := body["results"].([]any)
	if len(results) != 1 || results[0].(map[string]any)["tool"] != domain.ToolQuery || results[0].(map[string]any)["matchedCount"] != float64(1) {
		t.Fatalf("results = %v", results)
	}
	if _, ok := body["provenance"].(map[string]any)["Adapter"]; !ok {
		t.Fatalf("provenance = %v", body["provenance"])
	}
	// What is new is additive.
	toolCalls := body["toolCalls"].([]any)
	first := toolCalls[0].(map[string]any)
	if body["rounds"] != float64(2) || first["id"] != "call_1_1" || first["arguments"].(map[string]any)["objectType"] != "Environment" {
		t.Fatalf("rounds/toolCalls = %v / %v", body["rounds"], toolCalls)
	}
	if body["model"].(map[string]any)["kind"] != "script" {
		t.Fatalf("model = %v", body["model"])
	}
	if _, present := body["threadId"]; present {
		t.Fatal("a stateless turn reported a thread")
	}
}

func TestFailuresCarryAStableCode(t *testing.T) {
	status, body, raw := call(t, handler(nil), "ada", http.MethodPost, "/v1/assistant/turns", `{"mode":"explore","message":"hi"}`)
	if status != http.StatusUnprocessableEntity || body["code"] != "MODEL_NOT_CONFIGURED" || body["label"] != domain.LabelNoModel || body["error"] != domain.ErrModelUnavailable.Error() {
		t.Fatalf("no model = %d %s", status, raw)
	}
	status, body, raw = call(t, handler(&scripted{replies: []string{"I am not JSON"}}), "ada", http.MethodPost, "/v1/assistant/turns", `{"mode":"explore","message":"hi"}`)
	if status != http.StatusBadGateway || body["code"] != "MODEL_REPLY_INVALID" || body["error"] == "" {
		t.Fatalf("parse = %d %s", status, raw)
	}
	status, body, raw = call(t, handler(&scripted{replies: []string{askForEnvironments}}), "ada", http.MethodPost, "/v1/assistant/turns", `{"mode":"explore","message":"hi"}`)
	if status != http.StatusBadGateway || body["code"] != "ROUND_CAP" {
		t.Fatalf("round cap = %d %s", status, raw)
	}
	// What ran before the failure is returned with it.
	partial := body["turn"].(map[string]any)
	if len(partial["results"].([]any)) != domain.MaxRounds-1 || partial["rounds"] != float64(domain.MaxRounds) {
		t.Fatalf("partial turn = %v", partial)
	}
	for name, request := range map[string][]string{
		"empty message": {`{"mode":"explore","message":"  "}`, "INVALID_REQUEST"},
		"unknown field": {`{"mode":"explore","message":"hi","history":[]}`, "INVALID_REQUEST"},
		"not json":      {`nope`, "INVALID_REQUEST"},
	} {
		status, body, raw = call(t, handler(nil), "ada", http.MethodPost, "/v1/assistant/turns", request[0])
		if status != http.StatusBadRequest || body["code"] != request[1] {
			t.Fatalf("%s = %d %s", name, status, raw)
		}
	}
	status, body, raw = call(t, handler(nil), "", http.MethodPost, "/v1/assistant/turns", `{"mode":"explore","message":"hi"}`)
	if status != http.StatusUnauthorized || body["code"] != "UNAUTHORIZED" {
		t.Fatalf("anonymous = %d %s", status, raw)
	}
	status, body, raw = call(t, handler(nil), "ada", http.MethodPost, "/v1/assistant/tools", `{"mode":"explore","tool":"shell","arguments":{}}`)
	if status != http.StatusBadRequest || body["code"] != "TOOL_INVALID" {
		t.Fatalf("bad tool = %d %s", status, raw)
	}
}

func TestThreadRoutesAreScopedToTheirOwner(t *testing.T) {
	// A bare hexadecimal username can occur inside a random thread ID. Keep
	// the owner-leak assertion meaningful without matching legitimate entropy.
	const owner = "operator-ada"
	model := &scripted{replies: []string{askForEnvironments, answer, `{"text":"zeta, as I said."}`}}
	server := handler(model)
	status, created, raw := call(t, server, owner, http.MethodPost, "/v1/assistant/threads", `{}`)
	if status != http.StatusCreated || created["title"] != "" || created["messageCount"] != float64(0) || len(created["messages"].([]any)) != 0 {
		t.Fatalf("create = %d %s", status, raw)
	}
	id := created["id"].(string)
	if !domain.ValidThreadID(id) {
		t.Fatalf("id = %q", id)
	}
	if strings.Contains(raw, "owner") || strings.Contains(raw, owner) {
		t.Fatalf("the owner left the server: %s", raw)
	}
	status, turn, raw := call(t, server, owner, http.MethodPost, "/v1/assistant/turns", `{"mode":"explore","message":"which environments exist?","threadId":"`+id+`"}`)
	if status != http.StatusOK || turn["threadId"] != id {
		t.Fatalf("turn = %d %s", status, raw)
	}

	// Another operator: every route answers as if the thread did not exist.
	for name, attempt := range map[string][]string{
		"read":   {http.MethodGet, "/v1/assistant/threads/" + id, ""},
		"delete": {http.MethodDelete, "/v1/assistant/threads/" + id, ""},
		"fork":   {http.MethodPost, "/v1/assistant/threads/" + id + "/fork", "{}"},
		"turn":   {http.MethodPost, "/v1/assistant/turns", `{"mode":"explore","message":"continue","threadId":"` + id + `"}`},
	} {
		status, body, raw := call(t, server, "grace", attempt[0], attempt[1], attempt[2])
		if status != http.StatusNotFound || body["code"] != "THREAD_NOT_FOUND" || strings.Contains(raw, "zeta") {
			t.Fatalf("foreign %s = %d %s", name, status, raw)
		}
	}
	status, listed, raw := call(t, server, "grace", http.MethodGet, "/v1/assistant/threads", "")
	if status != http.StatusOK || len(listed["threads"].([]any)) != 0 {
		t.Fatalf("foreign list = %d %s", status, raw)
	}
	status, _, raw = call(t, server, "", http.MethodGet, "/v1/assistant/threads/"+id, "")
	if status != http.StatusUnauthorized || strings.Contains(raw, "zeta") {
		t.Fatalf("anonymous read = %d %s", status, raw)
	}

	// The owner resumes: list, read back with real arguments and results, continue.
	status, listed, raw = call(t, server, owner, http.MethodGet, "/v1/assistant/threads", "")
	summary := listed["threads"].([]any)[0].(map[string]any)
	if status != http.StatusOK || summary["id"] != id || summary["title"] != "which environments exist?" || summary["messageCount"] != float64(2) {
		t.Fatalf("list = %d %s", status, raw)
	}
	if _, present := summary["messages"]; present {
		t.Fatalf("the list carries messages: %s", raw)
	}
	status, read, raw := call(t, server, owner, http.MethodGet, "/v1/assistant/threads/"+id, "")
	messages := read["messages"].([]any)
	if status != http.StatusOK || len(messages) != 2 {
		t.Fatalf("read = %d %s", status, raw)
	}
	asked, answered := messages[0].(map[string]any), messages[1].(map[string]any)
	record := answered["toolCalls"].([]any)[0].(map[string]any)
	if asked["role"] != "user" || asked["text"] != "which environments exist?" || answered["role"] != "assistant" || answered["text"] != "One environment: zeta." ||
		record["id"] != "call_1_1" || record["arguments"].(map[string]any)["objectType"] != "Environment" || record["result"].(map[string]any)["matchedCount"] != float64(1) {
		t.Fatalf("messages = %s", raw)
	}
	model.mu.Lock()
	beforeFork := model.calls
	model.mu.Unlock()
	status, copied, raw := call(t, server, owner, http.MethodPost, "/v1/assistant/threads/"+id+"/fork", `{}`)
	if status != http.StatusCreated || copied["id"] == id || len(copied["messages"].([]any)) != 2 || copied["title"] != "Fork of which environments exist?" || strings.Contains(raw, "owner") || strings.Contains(raw, owner) {
		t.Fatalf("fork = %d %s", status, raw)
	}
	forkID := copied["id"].(string)
	model.mu.Lock()
	afterFork := model.calls
	model.mu.Unlock()
	if afterFork != beforeFork {
		t.Fatal("fork invoked model", beforeFork, afterFork)
	}
	status, turn, raw = call(t, server, owner, http.MethodPost, "/v1/assistant/turns", `{"mode":"explore","message":"what was it called?","threadId":"`+id+`"}`)
	if status != http.StatusOK || turn["text"] != "zeta, as I said." {
		t.Fatalf("resume = %d %s", status, raw)
	}
	if status, _, raw = call(t, server, owner, http.MethodDelete, "/v1/assistant/threads/"+id, ""); status != http.StatusNoContent || raw != "" {
		t.Fatalf("delete = %d %s", status, raw)
	}
	if status, body, _ := call(t, server, owner, http.MethodGet, "/v1/assistant/threads/"+id, ""); status != http.StatusNotFound || body["code"] != "THREAD_NOT_FOUND" {
		t.Fatalf("read after delete = %d", status)
	}
	if status, body, raw := call(t, server, owner, http.MethodGet, "/v1/assistant/threads/"+forkID, ""); status != http.StatusOK || len(body["messages"].([]any)) != 2 {
		t.Fatalf("source deletion affected fork: %d %s", status, raw)
	}
	if status, body, _ := call(t, server, owner, http.MethodGet, "/v1/assistant/threads/not-an-id", ""); status != http.StatusNotFound || body["code"] != "THREAD_NOT_FOUND" {
		t.Fatalf("malformed id = %d", status)
	}
}

func TestTheThreadLimitIsAConflict(t *testing.T) {
	server := handler(nil)
	for n := 0; n < domain.MaxThreadsPerOwner; n++ {
		if status, _, raw := call(t, server, "ada", http.MethodPost, "/v1/assistant/threads", fmt.Sprintf(`{"title":"thread %d"}`, n)); status != http.StatusCreated {
			t.Fatalf("create %d = %d %s", n, status, raw)
		}
	}
	status, body, raw := call(t, server, "ada", http.MethodPost, "/v1/assistant/threads", "")
	if status != http.StatusConflict || body["code"] != "THREAD_LIMIT" {
		t.Fatalf("over the limit = %d %s", status, raw)
	}
	if status, _, raw := call(t, server, "grace", http.MethodPost, "/v1/assistant/threads", ""); status != http.StatusCreated {
		t.Fatalf("another owner = %d %s", status, raw)
	}
}

// events reads SSE data lines and checks each against the AG-UI contract and
// the Hub's ordering profile.
func events(t *testing.T, body io.Reader) []map[string]any {
	t.Helper()
	var out []map[string]any
	var sequence *agui.Sequence
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		raw, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			t.Fatalf("not an SSE data line: %q", line)
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			t.Fatal(err)
		}
		if sequence == nil {
			sequence = agui.NewSequence(event["threadId"].(string), event["runId"].(string))
		}
		if err := sequence.Accept([]byte(raw)); err != nil {
			t.Fatalf("event %d (%v) breaks the contract: %v", len(out), event["type"], err)
		}
		out = append(out, event)
	}
	if sequence != nil && !sequence.Complete() {
		t.Fatal("the stream ended without a terminal event")
	}
	return out
}

func kinds(list []map[string]any) string {
	names := make([]string, len(list))
	for i, event := range list {
		names[i] = event["type"].(string)
	}
	return strings.Join(names, " ")
}

func TestAStreamedTurnReportsEachRoundAndToolCallAsItHappens(t *testing.T) {
	server := handler(&scripted{replies: []string{askForEnvironments, answer}})
	request := httptest.NewRequest(http.MethodPost, "/v1/assistant/turns", strings.NewReader(`{"mode":"explore","message":"which environments exist?"}`))
	request.Header.Set("X-Test-User", "ada")
	request.Header.Set("Accept", "text/event-stream")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response = %d %v", response.Code, response.Header())
	}
	list := events(t, response.Body)
	want := "RUN_STARTED STEP_STARTED TOOL_CALL_START TOOL_CALL_ARGS TOOL_CALL_END TOOL_CALL_RESULT STEP_FINISHED STEP_STARTED STEP_FINISHED TEXT_MESSAGE_START TEXT_MESSAGE_CONTENT TEXT_MESSAGE_END RUN_FINISHED"
	if kinds(list) != want {
		t.Fatalf("events = %s", kinds(list))
	}
	if list[1]["stepName"] != "round-1" || list[7]["stepName"] != "round-2" {
		t.Fatalf("steps = %v %v", list[1], list[7])
	}
	if list[2]["toolCallId"] != "call_1_1" || list[2]["toolCallName"] != domain.ToolQuery || list[3]["delta"] != `{"objectType":"Environment"}` {
		t.Fatalf("tool call = %v %v", list[2], list[3])
	}
	var result application.Result
	if err := json.Unmarshal([]byte(list[5]["content"].(string)), &result); err != nil || result.MatchedCount != 1 || result.Entities[0].ID != "env-zeta-7" || result.AuditEventID != "aud_1" {
		t.Fatalf("tool result = %v %v", list[5], err)
	}
	if list[10]["delta"] != "One environment: zeta." {
		t.Fatalf("answer = %v", list[10])
	}
	final := list[12]["result"].(map[string]any)
	if final["text"] != "One environment: zeta." || final["rounds"] != float64(2) || len(final["results"].([]any)) != 1 {
		t.Fatalf("RUN_FINISHED result = %v", final)
	}
	if list[0]["threadId"] != list[0]["runId"] || !strings.HasPrefix(list[0]["runId"].(string), "arun_") {
		t.Fatalf("run identity = %v", list[0])
	}
}

func TestAStreamedTurnFailsAsJSONBeforeItStartsAndAsRunErrorAfter(t *testing.T) {
	status, body, raw := call(t, handler(nil), "ada", http.MethodPost, "/v1/assistant/turns", `{"mode":"explore","message":"hi"}`, "Accept", "text/event-stream")
	if status != http.StatusUnprocessableEntity || body["code"] != "MODEL_NOT_CONFIGURED" {
		t.Fatalf("before start = %d %s", status, raw)
	}
	status, body, raw = call(t, handler(&scripted{replies: []string{answer}}), "ada", http.MethodPost, "/v1/assistant/turns",
		`{"mode":"explore","message":"hi","threadId":"ath_00000000000000000000000000000000"}`, "Accept", "text/event-stream, application/json;q=0.5")
	if status != http.StatusNotFound || body["code"] != "THREAD_NOT_FOUND" {
		t.Fatalf("unknown thread = %d %s", status, raw)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/assistant/turns", strings.NewReader(`{"mode":"explore","message":"loop"}`))
	request.Header.Set("X-Test-User", "ada")
	request.Header.Set("Accept", "text/event-stream")
	response := httptest.NewRecorder()
	handler(&scripted{replies: []string{askForEnvironments}}).ServeHTTP(response, request)
	list := events(t, response.Body)
	last := list[len(list)-1]
	if response.Code != http.StatusOK || last["type"] != "RUN_ERROR" || last["code"] != "ROUND_CAP" || last["message"] == "" {
		t.Fatalf("after start = %d %v", response.Code, last)
	}
	if strings.Count(kinds(list), "TOOL_CALL_RESULT") != domain.MaxRounds-1 {
		t.Fatalf("events = %s", kinds(list))
	}
}

func TestAStreamedTurnOnAThreadUsesTheThreadID(t *testing.T) {
	server := handler(&scripted{replies: []string{answer}})
	_, created, _ := call(t, server, "ada", http.MethodPost, "/v1/assistant/threads", "")
	id := created["id"].(string)
	request := httptest.NewRequest(http.MethodPost, "/v1/assistant/turns", strings.NewReader(`{"mode":"explore","message":"hi","threadId":"`+id+`"}`))
	request.Header.Set("X-Test-User", "ada")
	request.Header.Set("Accept", "text/event-stream")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	list := events(t, response.Body)
	if list[0]["threadId"] != id || list[len(list)-1]["type"] != "RUN_FINISHED" || list[len(list)-1]["result"].(map[string]any)["threadId"] != id {
		t.Fatalf("events = %v", list)
	}
	if _, read, _ := call(t, server, "ada", http.MethodGet, "/v1/assistant/threads/"+id, ""); len(read["messages"].([]any)) != 2 {
		t.Fatalf("the streamed turn was not stored: %v", read)
	}
}

// waiting blocks in Complete until its context ends, then reports why.
type waiting struct {
	entered  chan struct{}
	observed chan error
}

func (model waiting) Complete(ctx context.Context, _ string) (string, domain.Provenance, error) {
	close(model.entered)
	<-ctx.Done()
	model.observed <- ctx.Err()
	return "", domain.Provenance{Adapter: "waiting"}, fmt.Errorf("%w: %w", domain.ErrModelFailed, ctx.Err())
}

func TestAClientThatDisconnectsCancelsTheModelCall(t *testing.T) {
	for _, accept := range []string{"text/event-stream", "application/json"} {
		model := waiting{entered: make(chan struct{}), observed: make(chan error, 1)}
		server := httptest.NewServer(handler(model))
		ctx, cancel := context.WithCancel(context.Background())
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/assistant/turns", strings.NewReader(`{"mode":"explore","message":"slow"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-Test-User", "ada")
		request.Header.Set("Accept", accept)
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			response, err := http.DefaultClient.Do(request)
			if err == nil {
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
			}
		}()
		select {
		case <-model.entered:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: the model was never called", accept)
		}
		cancel()
		select {
		case err := <-model.observed:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s: the model call ended with %v", accept, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: the model call was not cancelled when the client went away", accept)
		}
		<-finished
		server.Close()
	}
}
