package composition

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jonathanagustin/conduit/internal/localcmd"
	"net/http"
	"net/http/httptest"
	"testing"

	apphttp "github.com/jonathanagustin/conduit/internal/appgen/adapters/modelhttp"
	packaginghttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/modelhttp"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
)

func TestFactoriesSelectTheirCapabilityModel(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": request.Model + "-served",
			"choices": []any{map[string]any{"message": map[string]any{"content": `{"text":"done"}`}}}})
	}))
	t.Cleanup(endpoint.Close)
	get := envOf(map[string]string{
		"CONDUIT_MODEL_BASE_URL": endpoint.URL, "CONDUIT_MODEL_NAME": "global",
		"CONDUIT_ASSISTANT_MODEL_NAME": "assistant", "CONDUIT_PLANNING_MODEL_NAME": "planning",
		"CONDUIT_PACKAGING_MODEL_NAME": "packaging", "CONDUIT_APPGEN_MODEL_NAME": "appgen",
	})
	assistant, err := assistantModel(get)
	if err != nil {
		t.Fatal(err)
	}
	_, served, err := assistant.Complete(context.Background(), "prompt")
	if err != nil || served.ConfiguredModel != "assistant" || served.ProviderModel != "assistant-served" {
		t.Fatalf("assistant chose the wrong model: %+v / %v", served, err)
	}
	planning, err := planningPlanner(get, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := planning.(planner.Planner).Model.Identity().ConfiguredModel; got != "planning" {
		t.Fatal(got)
	}
	proposer, err := modelProposer(get)
	if err != nil {
		t.Fatal(err)
	}
	if got := proposer.(packaginghttp.Adapter).Client.RequestedModel(); got != "packaging" {
		t.Fatal(got)
	}
	writer, err := packagingWriter(get)
	if err != nil {
		t.Fatal(err)
	}
	if got := writer.(packaginghttp.Adapter).Client.RequestedModel(); got != "packaging" {
		t.Fatal(got)
	}
	generator, err := appGenerator(get)
	if err != nil {
		t.Fatal(err)
	}
	if got := generator.(apphttp.Adapter).Client.RequestedModel(); got != "appgen" {
		t.Fatal(got)
	}
}

func TestCapabilityCommandCannotOverrideHTTPWithHostExecution(t *testing.T) {
	get := envOf(map[string]string{"CONDUIT_MODEL_BASE_URL": "https://models.example/v1", "CONDUIT_MODEL_NAME": "shared", "CONDUIT_ASSISTANT_MODEL_CMD": "operator-cli"})
	if _, err := assistantModel(get); !errors.Is(err, localcmd.ErrLocalCommandUnsupported) {
		t.Fatal(err)
	}
}
