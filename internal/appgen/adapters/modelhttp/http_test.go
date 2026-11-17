package modelhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jonathanagustin/conduit/internal/appgen/domain"
	shared "github.com/jonathanagustin/conduit/internal/modelhttp"
)

var _ domain.AppGenerator = Adapter{}

func adapterFor(t *testing.T, status int, content string) Adapter {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(status)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"model": "served", "choices": []any{map[string]any{"message": map[string]any{"content": content}}},
		})
	}))
	t.Cleanup(server.Close)
	client, err := shared.New(shared.Config{BaseURL: server.URL, Model: "asked"})
	if err != nil {
		t.Fatal(err)
	}
	return Adapter{Client: client}
}

func TestGenerateParsesTheSpecFromTheCompletion(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "recorded", "testdata", "hello", "spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := domain.ParseSpec(raw)
	if err != nil {
		t.Fatal(err)
	}
	adapter := adapterFor(t, http.StatusOK, string(raw))
	request := domain.Request{Name: "hello", Description: "a hello application"}
	spec, provenance, err := adapter.Generate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != want.Name || len(spec.Files) != len(want.Files) {
		t.Fatalf("spec = %+v", spec)
	}
	if provenance.Adapter != "openai-http" || provenance.PromptSHA256 != domain.Digest([]byte(domain.Prompt(request))) ||
		provenance.ResponseSHA256 != domain.Digest(raw) {
		t.Fatalf("provenance = %+v", provenance)
	}
	if domain.Label(provenance) != domain.LabelLiveModel || provenance.ProviderModel != "served" {
		t.Fatalf("label %q model %q", domain.Label(provenance), provenance.ProviderModel)
	}
}

func TestGenerateMapsFailuresOntoThePortErrors(t *testing.T) {
	if _, _, err := (Adapter{}).Generate(context.Background(), domain.Request{}); !errors.Is(err, domain.ErrModelUnavailable) {
		t.Fatalf("no client: %v", err)
	}
	_, _, err := adapterFor(t, http.StatusTooManyRequests, "").Generate(context.Background(), domain.Request{Name: "x"})
	if !errors.Is(err, domain.ErrModelFailed) || !errors.Is(err, shared.ErrRateLimited) {
		t.Fatalf("429: %v", err)
	}
	_, _, err = adapterFor(t, http.StatusOK, "not a spec").Generate(context.Background(), domain.Request{Name: "x"})
	if err == nil || errors.Is(err, domain.ErrModelFailed) {
		t.Fatalf("an unparseable spec must be the parser's error: %v", err)
	}
}
