package composition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/modelgovernor/adapters/memory"
)

// scriptedEndpoint is an OpenAI-compatible endpoint that returns its replies
// in order and keeps the exact prompt of every request.
type scriptedEndpoint struct {
	mu      sync.Mutex
	replies []string
	prompts []string
	usage   bool
	// hold, when set, blocks every request until it is closed.
	hold    chan struct{}
	entered chan struct{}
}

func (endpoint *scriptedEndpoint) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	_ = json.NewDecoder(request.Body).Decode(&body)
	endpoint.mu.Lock()
	index := len(endpoint.prompts)
	if len(body.Messages) > 0 {
		endpoint.prompts = append(endpoint.prompts, body.Messages[0].Content)
	}
	if index >= len(endpoint.replies) {
		index = len(endpoint.replies) - 1
	}
	reply, hold, entered := endpoint.replies[index], endpoint.hold, endpoint.entered
	endpoint.mu.Unlock()
	if hold != nil {
		if entered != nil {
			entered <- struct{}{}
		}
		select {
		case <-hold:
		case <-request.Context().Done():
			return
		}
	}
	document := map[string]any{"model": "served-model", "choices": []any{map[string]any{"message": map[string]any{"content": reply}}}}
	if endpoint.usage {
		document["usage"] = map[string]any{"prompt_tokens": 40, "completion_tokens": 2}
	}
	_ = json.NewEncoder(writer).Encode(document)
}

func (endpoint *scriptedEndpoint) Prompts() []string {
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	return append([]string{}, endpoint.prompts...)
}

var governanceEnvironment = []string{
	"CONDUIT_MODEL_CMD", "CONDUIT_MODEL_BASE_URL", "CONDUIT_MODEL_NAME", "CONDUIT_MODEL_API_KEY_FILE",
	"CONDUIT_MODEL_CA_FILE", "CONDUIT_MODEL_ALLOW_INSECURE_HTTP", "CONDUIT_MODEL_TIMEOUT",
	modelgovernor.EnvGlobalConcurrency, modelgovernor.EnvOwnerConcurrency, modelgovernor.EnvQueueWait, modelgovernor.EnvMaxAttempts,
	modelgovernor.EnvDailyTokenBudget, modelgovernor.EnvDailyCallBudget, modelgovernor.EnvTranscripts, modelgovernor.EnvTranscriptRetention,
}

// governedHub builds a Hub whose model is the endpoint, with root as the
// only administrator.
func governedHub(
	t *testing.T, endpoint http.Handler, repository controlplane.Repository, hub Options, env map[string]string,
) (http.Handler, controlplane.Repository) {
	t.Helper()
	for _, name := range governanceEnvironment {
		t.Setenv(name, "")
	}
	if endpoint != nil {
		server := httptest.NewServer(endpoint)
		t.Cleanup(server.Close)
		t.Setenv("CONDUIT_MODEL_BASE_URL", server.URL)
		t.Setenv("CONDUIT_MODEL_NAME", "asked-model")
	}
	for name, value := range env {
		t.Setenv(name, value)
	}
	if repository == nil {
		repository = controlplane.NewMemoryRepository()
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"},
	}, hub)
	if err != nil {
		t.Fatal(err)
	}
	return handler, repository
}

func turn(t *testing.T, handler http.Handler, actor, message string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"mode": "explore", "message": message})
	return planningRequest(t, handler, actor, http.MethodPost, "/v1/assistant/turns", string(body))
}

func auditEvents(t *testing.T, repository controlplane.Repository, action string) []controlplane.AuthorizationAuditEvent {
	t.Helper()
	all, err := repository.ListAuthorizationAudit(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	var out []controlplane.AuthorizationAuditEvent
	for _, event := range all {
		if event.Action == action {
			out = append(out, event)
		}
	}
	return out
}

func TestEveryAssistantModelCallIsAuditedIncludingATextOnlyReply(t *testing.T) {
	endpoint := &scriptedEndpoint{usage: true, replies: []string{
		`{"text":"","toolCalls":[{"name":"query_ontology_objects","arguments":{"objectType":"product"}}]}`,
		`{"text":"There are no products."}`,
		`{"text":"Just text."}`,
	}}
	handler, repository := governedHub(t, endpoint, nil, Options{}, nil)

	if first := turn(t, handler, "ada", "which products exist?"); first.Code != http.StatusOK || !strings.Contains(first.Body.String(), "There are no products.") {
		t.Fatalf("turn = %d %s", first.Code, first.Body.String())
	}
	events := auditEvents(t, repository, modelgovernor.ActionModelCall)
	prompts := endpoint.Prompts()
	if len(events) != 2 || len(prompts) != 2 {
		t.Fatalf("%d model-call audit events for %d model calls", len(events), len(prompts))
	}
	run, _ := events[0].Context["runId"].(string)
	previous := ""
	for index, event := range events {
		context := event.Context
		if event.Subject != "ada" || event.ResourceRID != modelgovernor.ResourcePrefix+run || !modelgovernor.ValidRunID(run) {
			t.Fatalf("event %d is %s on %s", index, event.Subject, event.ResourceRID)
		}
		// The digest is of the exact text the endpoint received.
		if context["promptSha256"] != modelgovernor.Digest([]byte(prompts[index])) ||
			context["responseSha256"] != modelgovernor.Digest([]byte(endpoint.replies[index])) {
			t.Fatalf("event %d digests = %v / %v", index, context["promptSha256"], context["responseSha256"])
		}
		if fmt.Sprint(context["round"]) != strconv.Itoa(index+1) || context["previousChainSha256"] != previous ||
			context["capability"] != "assistant" || context["transport"] != "http" || context["outcome"] != "ok" ||
			context["configuredModel"] != "asked-model" || context["providerModel"] != "served-model" || context["transcript"] != "off" {
			t.Fatalf("event %d context = %v", index, context)
		}
		previous, _ = context["chainSha256"].(string)
		encoded, _ := json.Marshal(event)
		if strings.Contains(string(encoded), "which products exist") || strings.Contains(string(encoded), "There are no products") {
			t.Fatalf("the audit event holds prompt or response text: %s", encoded)
		}
	}

	// A successful text-only reply is one model call and one audit event, in
	// a run of its own.
	if second := turn(t, handler, "ada", "say something"); second.Code != http.StatusOK || !strings.Contains(second.Body.String(), "Just text.") {
		t.Fatalf("text-only turn = %d %s", second.Code, second.Body.String())
	}
	events = auditEvents(t, repository, modelgovernor.ActionModelCall)
	if len(events) != 3 || fmt.Sprint(events[2].Context["round"]) != "1" || events[2].Context["runId"] == run ||
		events[2].Context["previousChainSha256"] != "" {
		t.Fatalf("after a text-only reply: %d events, last = %v", len(events), events[len(events)-1].Context)
	}

	// Usage: the caller's own, with the provider's token counts.
	var own struct {
		Owner  string
		Budget modelgovernor.Budget
		Usage  []modelgovernor.UsageRow
	}
	response := planningRequest(t, handler, "ada", http.MethodGet, "/v1/ai/usage", "")
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &own) != nil || own.Owner != "ada" || len(own.Usage) != 1 {
		t.Fatalf("own usage = %d %s", response.Code, response.Body.String())
	}
	row := own.Usage[0]
	if row.Calls != 3 || row.Capability != "assistant" || row.Transport != "http" || row.ProviderModel != "served-model" ||
		row.PromptTokens == nil || *row.PromptTokens != 120 || row.CompletionTokens == nil || *row.CompletionTokens != 6 ||
		own.Budget.Used.Calls != 3 || own.Budget.Used.Tokens != 126 || own.Budget.RemainingTokens != nil {
		t.Fatalf("own usage row = %+v, budget = %+v", row, own.Budget)
	}
	if other := planningRequest(t, handler, "grace", http.MethodGet, "/v1/ai/usage", ""); other.Code != http.StatusOK ||
		strings.Contains(other.Body.String(), "ada") || !strings.Contains(other.Body.String(), `"usage":[]`) {
		t.Fatalf("another operator's usage read = %d %s", other.Code, other.Body.String())
	}
	if refused := planningRequest(t, handler, "grace", http.MethodGet, "/v1/admin/ai/usage", ""); refused.Code != http.StatusForbidden ||
		strings.Contains(refused.Body.String(), "ada") {
		t.Fatalf("a non-administrator read every owner's usage: %d %s", refused.Code, refused.Body.String())
	}
	if all := planningRequest(t, handler, "root", http.MethodGet, "/v1/admin/ai/usage", ""); all.Code != http.StatusOK ||
		!strings.Contains(all.Body.String(), `"owner":"ada"`) {
		t.Fatalf("administrator usage = %d %s", all.Code, all.Body.String())
	}
}

func TestTokensAreNullWhenTheProviderReportsNoUsage(t *testing.T) {
	endpoint := &scriptedEndpoint{replies: []string{`{"text":"a reply of some length, with no usage object"}`}}
	handler, _ := governedHub(t, endpoint, nil, Options{}, nil)
	if response := turn(t, handler, "ada", "hi"); response.Code != http.StatusOK {
		t.Fatalf("turn = %d %s", response.Code, response.Body.String())
	}
	usage := planningRequest(t, handler, "ada", http.MethodGet, "/v1/ai/usage", "").Body.String()
	if !strings.Contains(usage, `"promptTokens":null`) || !strings.Contains(usage, `"completionTokens":null`) || !strings.Contains(usage, `"callsWithTokens":0`) {
		t.Fatalf("usage = %s: token counts must be null, not estimated", usage)
	}
}

// modelCallAuditOutage fails the audit write of a model call and nothing else.
type modelCallAuditOutage struct{ controlplane.Repository }

func (outage modelCallAuditOutage) AppendAuthorizationAudit(ctx context.Context, event controlplane.AuthorizationAuditEvent) (controlplane.AuthorizationAuditEvent, error) {
	if event.Action == modelgovernor.ActionModelCall {
		return controlplane.AuthorizationAuditEvent{}, errors.New("audit store is down")
	}
	return outage.Repository.AppendAuthorizationAudit(ctx, event)
}

func TestATurnWhoseModelCallCannotBeAuditedFails(t *testing.T) {
	endpoint := &scriptedEndpoint{replies: []string{`{"text":"a reply that must not be shown"}`}}
	handler, _ := governedHub(t, endpoint, modelCallAuditOutage{controlplane.NewMemoryRepository()}, Options{}, nil)
	response := turn(t, handler, "ada", "hi")
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"code":"AUDIT_UNAVAILABLE"`) ||
		strings.Contains(response.Body.String(), "must not be shown") {
		t.Fatalf("turn = %d %s", response.Code, response.Body.String())
	}
	if len(endpoint.Prompts()) != 1 {
		t.Fatalf("the model was called %d times", len(endpoint.Prompts()))
	}
}

func TestAnOwnerOverTheConcurrencyLimitGets429WithRetryAfter(t *testing.T) {
	endpoint := &scriptedEndpoint{
		replies: []string{`{"text":"done"}`}, hold: make(chan struct{}), entered: make(chan struct{}, 4),
	}
	handler, _ := governedHub(t, endpoint, nil, Options{AppgenSandbox: &testAppgenSandbox{}, AppgenPublisher: &testAppgenPublisher{}},
		map[string]string{modelgovernor.EnvOwnerConcurrency: "1", modelgovernor.EnvQueueWait: "1500ms"})
	held := make(chan *httptest.ResponseRecorder, 1)
	go func() { held <- turn(t, handler, "ada", "the slow one") }()
	<-endpoint.entered

	refused := turn(t, handler, "ada", "one too many")
	if refused.Code != http.StatusTooManyRequests || refused.Header().Get("Retry-After") != "2" ||
		!strings.Contains(refused.Body.String(), `"code":"MODEL_BUSY"`) {
		t.Fatalf("over the limit = %d Retry-After %q %s", refused.Code, refused.Header().Get("Retry-After"), refused.Body.String())
	}
	// A capability whose own handler knows nothing of the limit answers the
	// same way: app generation maps a model failure to 502.
	generation := planningRequest(t, handler, "ada", http.MethodPost, "/v1/app-generations", `{"name":"hello-http","description":"a page","confirm":true}`)
	if generation.Code != http.StatusTooManyRequests || generation.Header().Get("Retry-After") != "2" ||
		!strings.Contains(generation.Body.String(), `"code":"MODEL_BUSY"`) {
		t.Fatalf("app generation over the limit = %d Retry-After %q %s", generation.Code, generation.Header().Get("Retry-After"), generation.Body.String())
	}
	if len(endpoint.Prompts()) != 1 {
		t.Fatalf("a refused call reached the model: %d prompts", len(endpoint.Prompts()))
	}
	close(endpoint.hold)
	if first := <-held; first.Code != http.StatusOK {
		t.Fatalf("the call under the limit = %d %s", first.Code, first.Body.String())
	}
	// The slot is free again.
	endpoint.mu.Lock()
	endpoint.hold = nil
	endpoint.mu.Unlock()
	if after := turn(t, handler, "ada", "again"); after.Code != http.StatusOK {
		t.Fatalf("after the slot freed = %d %s", after.Code, after.Body.String())
	}
}

func TestAnOwnerOverTheDailyBudgetGets429AndSeesItInUsage(t *testing.T) {
	endpoint := &scriptedEndpoint{replies: []string{`{"text":"ok"}`}}
	handler, _ := governedHub(t, endpoint, nil, Options{}, map[string]string{modelgovernor.EnvDailyCallBudget: "1"})
	if first := turn(t, handler, "ada", "one"); first.Code != http.StatusOK {
		t.Fatalf("first = %d %s", first.Code, first.Body.String())
	}
	refused := turn(t, handler, "ada", "two")
	seconds, _ := strconv.Atoi(refused.Header().Get("Retry-After"))
	if refused.Code != http.StatusTooManyRequests || !strings.Contains(refused.Body.String(), `"code":"MODEL_BUDGET_EXCEEDED"`) ||
		seconds < 1 || seconds > 24*3600 {
		t.Fatalf("over budget = %d Retry-After %q %s", refused.Code, refused.Header().Get("Retry-After"), refused.Body.String())
	}
	if len(endpoint.Prompts()) != 1 {
		t.Fatal("a call over budget reached the model")
	}
	usage := planningRequest(t, handler, "ada", http.MethodGet, "/v1/ai/usage", "").Body.String()
	if !strings.Contains(usage, `"dailyCallBudget":1`) || !strings.Contains(usage, `"remainingCalls":0`) || !strings.Contains(usage, `"dailyTokenBudget":null`) {
		t.Fatalf("usage = %s", usage)
	}
	if other := turn(t, handler, "grace", "mine"); other.Code != http.StatusOK {
		t.Fatalf("one owner's budget refused another: %d %s", other.Code, other.Body.String())
	}
}

// tamperedTranscripts changes what a run reads back, the way an edit to the
// table would.
type tamperedTranscripts struct {
	modelgovernor.TranscriptStore
	tamper bool
}

func (store *tamperedTranscripts) Run(ctx context.Context, runID string, now time.Time) ([]modelgovernor.TranscriptRecord, error) {
	records, err := store.TranscriptStore.Run(ctx, runID, now)
	if store.tamper {
		for index := range records {
			records[index].Response = []byte(`{"text":"rewritten after the fact"}`)
			records[index].StoredResponseSHA256 = modelgovernor.Digest(records[index].Response)
		}
	}
	return records, err
}

func TestATranscriptIsExportedOnlyToAnAdministratorAndVerifiedAgainstTheAudit(t *testing.T) {
	endpoint := &scriptedEndpoint{replies: []string{`{"text":"Noted."}`}}
	store := &tamperedTranscripts{TranscriptStore: memory.NewTranscripts()}
	handler, repository := governedHub(t, endpoint, nil, Options{ModelGovernance: ModelGovernance{Transcripts: store}},
		map[string]string{modelgovernor.EnvTranscripts: "on", modelgovernor.EnvTranscriptRetention: "1h"})
	if response := turn(t, handler, "ada", "the deploy password=hunter2secret, remember it"); response.Code != http.StatusOK {
		t.Fatalf("turn = %d %s", response.Code, response.Body.String())
	}
	calls := auditEvents(t, repository, modelgovernor.ActionModelCall)
	if len(calls) != 1 || calls[0].Context["transcript"] != "stored" || calls[0].Context["storedPromptSha256"] == nil {
		t.Fatalf("model-call audit = %+v", calls)
	}
	run, _ := calls[0].Context["runId"].(string)
	path := "/v1/admin/ai/transcripts/" + run

	// Not the owner, not another operator: only the administrator.
	for _, actor := range []string{"ada", "grace"} {
		refused := planningRequest(t, handler, actor, http.MethodGet, path, "")
		if refused.Code != http.StatusForbidden || strings.Contains(refused.Body.String(), "deploy") || strings.Contains(refused.Body.String(), "Noted") {
			t.Fatalf("%s exported a transcript: %d %s", actor, refused.Code, refused.Body.String())
		}
		if listed := planningRequest(t, handler, actor, http.MethodGet, "/v1/admin/ai/transcripts", ""); listed.Code != http.StatusForbidden {
			t.Fatalf("%s listed transcripts: %d", actor, listed.Code)
		}
	}
	exported := planningRequest(t, handler, "root", http.MethodGet, path, "")
	var export modelgovernor.Export
	if exported.Code != http.StatusOK || json.Unmarshal(exported.Body.Bytes(), &export) != nil {
		t.Fatalf("export = %d %s", exported.Code, exported.Body.String())
	}
	if !export.Verified || !export.Complete || len(export.Exchanges) != 1 || export.Owner != "ada" {
		t.Fatalf("export = %+v", export)
	}
	exchange := export.Exchanges[0]
	if strings.Contains(exchange.Prompt, "hunter2secret") || !strings.Contains(exchange.Prompt, "[REDACTED]") ||
		exchange.PromptFidelity != modelgovernor.FidelityRedacted || exchange.ResponseFidelity != modelgovernor.FidelityExact ||
		exchange.SentPromptSHA256 != modelgovernor.Digest([]byte(endpoint.Prompts()[0])) ||
		exchange.StoredPromptSHA256 == exchange.SentPromptSHA256 || exchange.Response != `{"text":"Noted."}` ||
		exchange.AuditEventID != calls[0].ID {
		t.Fatalf("exchange = %+v", exchange)
	}
	// The export and both refusals are in the audit log.
	exports := auditEvents(t, repository, modelgovernor.ActionTranscriptExport)
	allowed, denied := 0, 0
	for _, event := range exports {
		if event.ResourceRID != modelgovernor.ResourcePrefix+run {
			t.Fatalf("export audit on %s", event.ResourceRID)
		}
		switch {
		case event.Decision == "ALLOW" && event.Subject == "root":
			allowed++
		case event.Decision == "DENY" && (event.Subject == "ada" || event.Subject == "grace"):
			denied++
		}
	}
	if allowed != 1 || denied != 2 {
		t.Fatalf("export audit: %d allowed, %d denied, of %+v", allowed, denied, exports)
	}
	if listed := planningRequest(t, handler, "root", http.MethodGet, "/v1/admin/ai/transcripts?owner=ada", ""); listed.Code != http.StatusOK ||
		!strings.Contains(listed.Body.String(), run) || strings.Contains(listed.Body.String(), "deploy") {
		t.Fatalf("list = %d %s", listed.Code, listed.Body.String())
	}

	// A record edited after the fact, stored digest and all, does not verify.
	store.tamper = true
	tampered := planningRequest(t, handler, "root", http.MethodGet, path, "")
	if tampered.Code != http.StatusOK || json.Unmarshal(tampered.Body.Bytes(), &export) != nil || export.Verified || len(export.Mismatches) == 0 {
		t.Fatalf("a tampered transcript verified: %d %s", tampered.Code, tampered.Body.String())
	}
	if missing := planningRequest(t, handler, "root", http.MethodGet, "/v1/admin/ai/transcripts/airun_00000000000000000000000000000000", ""); missing.Code != http.StatusNotFound {
		t.Fatalf("an unknown run = %d", missing.Code)
	}
}

// exportAuditOutage fails the audit write of an export.
type exportAuditOutage struct{ controlplane.Repository }

func (outage exportAuditOutage) AppendAuthorizationAudit(ctx context.Context, event controlplane.AuthorizationAuditEvent) (controlplane.AuthorizationAuditEvent, error) {
	if event.Action == modelgovernor.ActionTranscriptExport {
		return controlplane.AuthorizationAuditEvent{}, errors.New("audit store is down")
	}
	return outage.Repository.AppendAuthorizationAudit(ctx, event)
}

func TestTranscriptsAreOffByDefaultAndAnUnauditedExportIsNotServed(t *testing.T) {
	endpoint := &scriptedEndpoint{replies: []string{`{"text":"Noted."}`}}
	store := memory.NewTranscripts()
	handler, repository := governedHub(t, endpoint, exportAuditOutage{controlplane.NewMemoryRepository()},
		Options{ModelGovernance: ModelGovernance{Transcripts: store}}, nil)
	if response := turn(t, handler, "ada", "nothing is kept of this"); response.Code != http.StatusOK {
		t.Fatalf("turn = %d %s", response.Code, response.Body.String())
	}
	runs, err := store.Runs(context.Background(), modelgovernor.RunFilter{}, time.Now())
	if err != nil || len(runs) != 0 {
		t.Fatalf("with transcripts off the store holds %+v", runs)
	}
	run, _ := auditEvents(t, repository, modelgovernor.ActionModelCall)[0].Context["runId"].(string)
	refused := planningRequest(t, handler, "root", http.MethodGet, "/v1/admin/ai/transcripts/"+run, "")
	if refused.Code != http.StatusServiceUnavailable || strings.Contains(refused.Body.String(), "exchanges") {
		t.Fatalf("an export that could not be audited = %d %s", refused.Code, refused.Body.String())
	}
}

func TestAModelCommandIsGovernedLikeAnHTTPModel(t *testing.T) {
	dir := t.TempDir()
	seen := filepath.Join(dir, "stdin")
	script := filepath.Join(dir, "model")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat > "+seen+"\nprintf '%s' '{\"text\":\"from the command\"}'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	handler, repository := governedHub(t, nil, nil, Options{}, map[string]string{
		"CONDUIT_MODEL_CMD": script, "CONDUIT_MODEL_NAME": "local-model", modelgovernor.EnvDailyCallBudget: "1",
	})
	if response := turn(t, handler, "ada", "hi"); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "from the command") {
		t.Fatalf("turn = %d %s", response.Code, response.Body.String())
	}
	stdin, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	events := auditEvents(t, repository, modelgovernor.ActionModelCall)
	if len(events) != 1 || events[0].Context["transport"] != "cli" || events[0].Context["configuredModel"] != "local-model" ||
		events[0].Context["providerModel"] != "" || events[0].Context["promptSha256"] != modelgovernor.Digest(stdin) ||
		events[0].Context["responseSha256"] != modelgovernor.Digest([]byte(`{"text":"from the command"}`)) {
		t.Fatalf("events = %+v", events)
	}
	// A command reports no tokens, so the call budget is what binds.
	if refused := turn(t, handler, "ada", "again"); refused.Code != http.StatusTooManyRequests ||
		!strings.Contains(refused.Body.String(), `"code":"MODEL_BUDGET_EXCEEDED"`) {
		t.Fatalf("over the call budget = %d %s", refused.Code, refused.Body.String())
	}
}

func TestAGovernanceSettingThatDoesNotParseStopsStartup(t *testing.T) {
	for _, name := range governanceEnvironment {
		t.Setenv(name, "")
	}
	t.Setenv(modelgovernor.EnvTranscripts, "on")
	_, err := NewHub(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}, Options{})
	if !errors.Is(err, modelgovernor.ErrInvalidConfig) {
		t.Fatalf("transcripts on with no retention started: %v", err)
	}
}
