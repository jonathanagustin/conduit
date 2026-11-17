package conduitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/modelgovernor/adapters/memory"
)

// editedTranscripts returns a run with its reply rewritten once edit is set.
type editedTranscripts struct {
	modelgovernor.TranscriptStore
	edit bool
}

func (store *editedTranscripts) Run(ctx context.Context, runID string, now time.Time) ([]modelgovernor.TranscriptRecord, error) {
	records, err := store.TranscriptStore.Run(ctx, runID, now)
	if store.edit {
		for index := range records {
			records[index].Response = []byte("edited")
		}
	}
	return records, err
}

func TestAICommandsReadUsageAndExportAVerifiedTranscript(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"model": "served", "choices": []any{map[string]any{"message": map[string]any{"content": `{"text":"Noted."}`}}},
			"usage": map[string]any{"prompt_tokens": 30, "completion_tokens": 4},
		})
	}))
	t.Cleanup(model.Close)
	for name, value := range map[string]string{
		"CONDUIT_MODEL_CMD": "", "CONDUIT_MODEL_BASE_URL": model.URL, "CONDUIT_MODEL_NAME": "asked",
		"CONDUIT_MODEL_API_KEY_FILE": "", "CONDUIT_MODEL_CA_FILE": "", "CONDUIT_MODEL_TIMEOUT": "",
		modelgovernor.EnvTranscripts: "on", modelgovernor.EnvTranscriptRetention: "1h",
		modelgovernor.EnvDailyTokenBudget: "1000", modelgovernor.EnvDailyCallBudget: "",
		modelgovernor.EnvGlobalConcurrency: "", modelgovernor.EnvOwnerConcurrency: "", modelgovernor.EnvQueueWait: "", modelgovernor.EnvMaxAttempts: "",
	} {
		t.Setenv(name, value)
	}
	repository := controlplane.NewMemoryRepository()
	store := &editedTranscripts{TranscriptStore: memory.NewTranscripts()}
	handler, err := composition.NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"},
	}, composition.Options{ModelGovernance: composition.ModelGovernance{Transcripts: store}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	run := func(subject string, args ...string) (string, error) {
		var output, diagnostics bytes.Buffer
		root := NewRoot(Runtime{Stdout: &output, Stderr: &diagnostics, Getenv: func(string) string { return "" }})
		root.SetArgs(append([]string{"--server", server.URL, "--development-subject", subject}, args...))
		err := root.Execute()
		return output.String(), err
	}

	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/assistant/turns", strings.NewReader(`{"mode":"explore","message":"remember this"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Platform-Subject", "ada")
	response, err := http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("turn: %v %v", response, err)
	}
	_ = response.Body.Close()

	out, err := run("ada", "ai", "usage")
	if err != nil || !strings.Contains(out, `"owner": "ada"`) || !strings.Contains(out, `"remainingTokens": 966`) {
		t.Fatalf("own usage: %v\n%s", err, out)
	}
	if out, err := run("ada", "ai", "usage", "--all"); err == nil || out != "" {
		t.Fatalf("a non-administrator read every owner's usage: %s", out)
	}
	if out, err := run("root", "ai", "usage", "--all", "--owner", "ada", "--days", "7"); err != nil || !strings.Contains(out, `"promptTokens": 30`) {
		t.Fatalf("administrator usage: %v\n%s", err, out)
	}
	if _, err := run("ada", "ai", "usage", "--owner", "grace"); err == nil {
		t.Fatal("--owner without --all was accepted")
	}

	listed, err := run("root", "ai", "transcript", "list", "--owner", "ada")
	var list struct {
		Runs []struct {
			RunID string `json:"runId"`
		} `json:"runs"`
	}
	if err != nil || json.Unmarshal([]byte(listed), &list) != nil || len(list.Runs) != 1 {
		t.Fatalf("list: %v\n%s", err, listed)
	}
	runID := list.Runs[0].RunID
	if out, err := run("ada", "ai", "transcript", "export", runID); err == nil || out != "" {
		t.Fatalf("the owner exported their own transcript through the CLI: %s", out)
	}
	exported, err := run("root", "ai", "transcript", "export", runID)
	if err != nil || !strings.Contains(exported, `"verified": true`) || !strings.Contains(exported, "remember this") {
		t.Fatalf("export: %v\n%s", err, exported)
	}
	if _, err := run("root", "ai", "transcript", "export", "not-a-run"); err == nil {
		t.Fatal("a malformed run id was sent to the Hub")
	}
	// A transcript that does not verify is printed and the command fails.
	store.edit = true
	tampered, err := run("root", "ai", "transcript", "export", runID)
	if err == nil || !strings.Contains(tampered, `"verified": false`) || !strings.Contains(err.Error(), "does not verify") {
		t.Fatalf("a tampered export: err = %v\n%s", err, tampered)
	}
}
