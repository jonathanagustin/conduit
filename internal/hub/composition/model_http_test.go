package composition

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appcli "github.com/jonathanagustin/conduit/internal/appgen/adapters/modelcli"
	appmodelhttp "github.com/jonathanagustin/conduit/internal/appgen/adapters/modelhttp"
	"github.com/jonathanagustin/conduit/internal/appgen/adapters/recorded"
	assistantcli "github.com/jonathanagustin/conduit/internal/assistant/adapters/modelcli"
	assistantmodelhttp "github.com/jonathanagustin/conduit/internal/assistant/adapters/modelhttp"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/modelhttp"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/deterministic"
	packagingcli "github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/modelcli"
	packagingmodelhttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/modelhttp"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
)

func envOf(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestModelTransportsCannotBothBeConfigured(t *testing.T) {
	getenv := envOf(map[string]string{
		"CONDUIT_MODEL_CMD": "codex exec -", "CONDUIT_MODEL_BASE_URL": "https://model.example/v1", "CONDUIT_MODEL_NAME": "m",
	})
	if model, err := assistantModel(getenv); !errors.Is(err, errModelBothConfigured) || model != nil {
		t.Fatalf("assistant: %v %v", model, err)
	}
	if generator, err := appGenerator(getenv); !errors.Is(err, errModelBothConfigured) || generator != nil {
		t.Fatalf("appgen: %v %v", generator, err)
	}
	if runner, err := planningPlanner(getenv, nil); !errors.Is(err, errModelBothConfigured) || runner != nil {
		t.Fatalf("planning: %v %v", runner, err)
	}
	for _, name := range []string{"CONDUIT_MODEL_CMD", "CONDUIT_MODEL_BASE_URL"} {
		if !strings.Contains(errModelBothConfigured.Error(), name) {
			t.Fatalf("the error does not name %s", name)
		}
	}
}

func TestNoModelConfigurationIsUnchanged(t *testing.T) {
	for _, getenv := range []func(string) string{nil, envOf(nil), envOf(map[string]string{
		// Without a base URL the other HTTP variables select nothing.
		"CONDUIT_MODEL_NAME": "m", "CONDUIT_MODEL_API_KEY_FILE": "/nonexistent", "CONDUIT_MODEL_ALLOW_INSECURE_HTTP": "true",
	})} {
		if model, err := assistantModel(getenv); err != nil || model != nil {
			t.Fatalf("assistant: %v %v", model, err)
		}
		generator, err := appGenerator(getenv)
		if _, ok := generator.(recorded.Adapter); err != nil || !ok {
			t.Fatalf("appgen: %T %v", generator, err)
		}
		if runner, err := planningPlanner(getenv, nil); err != nil || runner != nil {
			t.Fatalf("planning: %v %v", runner, err)
		}
	}
}

func TestModelCommandAloneStillSelectsTheCommand(t *testing.T) {
	getenv := envOf(map[string]string{"CONDUIT_MODEL_CMD": "codex exec -"})
	model, err := assistantModel(getenv)
	if _, ok := model.(assistantcli.Runner); err != nil || !ok {
		t.Fatalf("assistant: %T %v", model, err)
	}
	generator, err := appGenerator(getenv)
	if _, ok := generator.(appcli.Runner); err != nil || !ok {
		t.Fatalf("appgen: %T %v", generator, err)
	}
	runner, err := planningPlanner(getenv, nil)
	if _, ok := runner.(planner.Planner); err != nil || !ok {
		t.Fatalf("planning: %T %v", runner, err)
	}
}

func TestModelBaseURLSelectsTheHTTPAdapters(t *testing.T) {
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorization = request.Header.Get("Authorization")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"model": "served", "choices": []any{map[string]any{"message": map[string]any{"content": "reply"}}},
		})
	}))
	t.Cleanup(server.Close)
	key := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(key, []byte("file-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	getenv := envOf(map[string]string{
		"CONDUIT_MODEL_BASE_URL": server.URL + "/v1", "CONDUIT_MODEL_NAME": "asked",
		"CONDUIT_MODEL_API_KEY_FILE": key, "CONDUIT_MODEL_TIMEOUT": "5s",
	})
	model, err := assistantModel(getenv)
	if err != nil {
		t.Fatal(err)
	}
	adapter, ok := model.(assistantmodelhttp.Adapter)
	if !ok {
		t.Fatalf("assistant model is %T", model)
	}
	text, provenance, err := model.Complete(context.Background(), "prompt")
	if err != nil || text != "reply" || provenance.Adapter != modelhttp.AdapterName {
		t.Fatalf("text %q provenance %+v err %v", text, provenance, err)
	}
	if authorization != "Bearer file-key" || adapter.ReportedModel() != "served" {
		t.Fatalf("authorization %q reported %q", authorization, adapter.ReportedModel())
	}
	generator, err := appGenerator(getenv)
	if _, ok := generator.(appmodelhttp.Adapter); err != nil || !ok {
		t.Fatalf("appgen: %T %v", generator, err)
	}
	runner, err := planningPlanner(getenv, nil)
	if err != nil {
		t.Fatal(err)
	}
	planningRunner, ok := runner.(planner.Planner)
	if !ok {
		t.Fatalf("planning: %T", runner)
	}
	httpModel, ok := planningRunner.Model.(planner.HTTPModel)
	if !ok || len(planningRunner.Argv) != 0 {
		t.Fatalf("planning model is %T with argv %v", planningRunner.Model, planningRunner.Argv)
	}
	if identity := httpModel.Identity(); identity.Adapter != modelhttp.AdapterName || identity.ConfiguredModel != "asked" {
		t.Fatalf("planning identity = %+v", identity)
	}
	proposer, err := modelProposer(getenv)
	if _, ok := proposer.(packagingmodelhttp.Adapter); err != nil || !ok {
		t.Fatalf("packaging proposer: %T %v", proposer, err)
	}
	writer, err := packagingWriter(getenv)
	if _, ok := writer.(packagingmodelhttp.Adapter); err != nil || !ok {
		t.Fatalf("package writer: %T %v", writer, err)
	}
}

func TestPackagingWiringFollowsTheTransportRule(t *testing.T) {
	both := envOf(map[string]string{
		"CONDUIT_MODEL_CMD": "codex exec -", "CONDUIT_MODEL_BASE_URL": "https://model.example/v1", "CONDUIT_MODEL_NAME": "m",
	})
	if proposer, err := modelProposer(both); !errors.Is(err, errModelBothConfigured) || proposer != nil {
		t.Fatalf("proposer: %v %v", proposer, err)
	}
	if writer, err := packagingWriter(both); !errors.Is(err, errModelBothConfigured) || writer != nil {
		t.Fatalf("writer: %v %v", writer, err)
	}
	// Neither set is unchanged: the recorded proposer and the deterministic writer.
	proposer, err := modelProposer(envOf(nil))
	if _, isHTTP := proposer.(packagingmodelhttp.Adapter); err != nil || isHTTP || proposer == nil {
		t.Fatalf("proposer with no model: %T %v", proposer, err)
	}
	if _, isCLI := proposer.(packagingcli.Runner); isCLI {
		t.Fatalf("proposer with no model is a command runner")
	}
	writer, err := packagingWriter(envOf(nil))
	if _, ok := writer.(deterministic.Writer); err != nil || !ok {
		t.Fatalf("writer with no model: %T %v", writer, err)
	}
	// A command carries the operator-configured name, and only that.
	named := envOf(map[string]string{"CONDUIT_MODEL_CMD": "codex exec -", "CONDUIT_MODEL_NAME": " operator-chosen "})
	proposer, err = modelProposer(named)
	if runner, ok := proposer.(packagingcli.Runner); err != nil || !ok || runner.ModelName != "operator-chosen" {
		t.Fatalf("proposer: %+v %v", proposer, err)
	}
	writer, err = packagingWriter(named)
	if runner, ok := writer.(packagingcli.Runner); err != nil || !ok || runner.ModelName != "operator-chosen" {
		t.Fatalf("writer: %+v %v", writer, err)
	}
	runner, err := planningPlanner(named, nil)
	if planningRunner, ok := runner.(planner.Planner); err != nil || !ok || planningRunner.ModelName != "operator-chosen" || planningRunner.Model != nil {
		t.Fatalf("planner: %+v %v", runner, err)
	}
}

func TestPlanningAuditRecordsConfiguredAndProviderModel(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	err := (planningAudit{repository: repository}).Record(t.Context(), planner.Record{
		SessionID: "session", Action: planner.ActionPlan, Outcome: "pass", Adapter: modelhttp.AdapterName,
		ConfiguredModel: "asked", ProviderModel: "served-2026-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := repository.ListAuthorizationAudit(t.Context(), "planning-session/session")
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	got := events[0].Context
	if got["adapter"] != modelhttp.AdapterName || got["configuredModel"] != "asked" || got["providerModel"] != "served-2026-01" {
		t.Fatalf("audit context = %+v", got)
	}
}

func TestPackagingProposalAuditRecordsConfiguredAndProviderModel(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	err := (modelAudit{repository: repository}).Record(t.Context(), "run-1", packagingdomain.Provenance{
		Adapter: modelhttp.AdapterName, ConfiguredModel: "asked", ProviderModel: "served-2026-01",
	}, string(packagingdomain.OutcomePass), "ok")
	if err != nil {
		t.Fatal(err)
	}
	events, err := repository.ListAuthorizationAudit(t.Context(), "packaging-run/run-1")
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if got := events[0].Context; got["configuredModel"] != "asked" || got["providerModel"] != "served-2026-01" {
		t.Fatalf("audit context = %+v", got)
	}
}

func TestModelBaseURLConfigurationErrorsFailEachWiring(t *testing.T) {
	cases := map[string]map[string]string{
		"plaintext to a non-loopback host": {"CONDUIT_MODEL_BASE_URL": "http://vllm.models.svc:8000/v1", "CONDUIT_MODEL_NAME": "m"},
		"opt-in must be exactly true": {
			"CONDUIT_MODEL_BASE_URL": "http://vllm.models.svc:8000/v1", "CONDUIT_MODEL_NAME": "m", "CONDUIT_MODEL_ALLOW_INSECURE_HTTP": "1",
		},
		"missing model name": {"CONDUIT_MODEL_BASE_URL": "https://model.example/v1"},
		"missing key file": {
			"CONDUIT_MODEL_BASE_URL": "https://model.example/v1", "CONDUIT_MODEL_NAME": "m", "CONDUIT_MODEL_API_KEY_FILE": "/nonexistent/key",
		},
		"bad timeout": {"CONDUIT_MODEL_BASE_URL": "https://model.example/v1", "CONDUIT_MODEL_NAME": "m", "CONDUIT_MODEL_TIMEOUT": "-1s"},
	}
	for name, values := range cases {
		getenv := envOf(values)
		if _, err := assistantModel(getenv); err == nil {
			t.Fatalf("%s: assistant accepted it", name)
		}
		if _, err := appGenerator(getenv); err == nil {
			t.Fatalf("%s: appgen accepted it", name)
		}
		if _, err := planningPlanner(getenv, nil); err == nil {
			t.Fatalf("%s: planning accepted it", name)
		}
	}
	_, err := modelHTTPClient(envOf(cases["plaintext to a non-loopback host"]))
	if !errors.Is(err, modelhttp.ErrInsecureEndpoint) || !strings.Contains(err.Error(), "CONDUIT_MODEL_BASE_URL") {
		t.Fatalf("plaintext: %v", err)
	}
	optedIn := envOf(map[string]string{
		"CONDUIT_MODEL_BASE_URL": "http://vllm.models.svc:8000/v1", "CONDUIT_MODEL_NAME": "m", "CONDUIT_MODEL_ALLOW_INSECURE_HTTP": "true",
	})
	if client, err := modelHTTPClient(optedIn); err != nil || client == nil {
		t.Fatalf("explicit opt-in: %v", err)
	}
}

func TestIntelligenceUsesTheHTTPModelForLiveEvals(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"model": "served", "choices": []any{map[string]any{"message": map[string]any{
				"content": `{"field":"memory","value":"512Mi","rationale":"headroom"}`,
			}}},
		})
	}))
	t.Cleanup(server.Close)
	for _, name := range []string{
		"CONDUIT_MODEL_CMD", "CONDUIT_MODEL_BASE_URL", "CONDUIT_MODEL_NAME", "CONDUIT_MODEL_API_KEY_FILE",
		"CONDUIT_MODEL_CA_FILE", "CONDUIT_MODEL_ALLOW_INSECURE_HTTP", "CONDUIT_MODEL_TIMEOUT",
	} {
		t.Setenv(name, "")
	}
	if liveEvalProposer() != nil || intelligenceAdapter() != "recorded" {
		t.Fatal("no configuration must stay recorded")
	}
	t.Setenv("CONDUIT_MODEL_BASE_URL", server.URL)
	t.Setenv("CONDUIT_MODEL_NAME", "asked")
	if intelligenceAdapter() != modelhttp.AdapterName {
		t.Fatalf("adapter = %q", intelligenceAdapter())
	}
	proposer := liveEvalProposer()
	if proposer == nil {
		t.Fatal("no live proposer for the HTTP model")
	}
	value, err := proposer.Propose(context.Background(), "memory", "512Mi")
	if err != nil || value != "512Mi" {
		t.Fatalf("value %q err %v", value, err)
	}
	t.Setenv("CONDUIT_MODEL_CMD", "codex exec -")
	if liveEvalProposer() != nil || intelligenceAdapter() != "recorded" {
		t.Fatal("both transports set must not pick one")
	}
}
