package modelhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/assistant/domain"
	shared "github.com/jonathanagustin/conduit/internal/modelhttp"
)

var _ domain.Model = Adapter{}

func client(t *testing.T, handler http.HandlerFunc, timeout time.Duration) *shared.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	built, err := shared.New(shared.Config{BaseURL: server.URL, Model: "asked", Timeout: timeout})
	if err != nil {
		t.Fatal(err)
	}
	return built
}

func TestCompleteReturnsTheTextAndProvenance(t *testing.T) {
	adapter := Adapter{Client: client(t, func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"model": "served", "choices": []any{map[string]any{"message": map[string]any{"content": `{"text":"hi"}`}}},
		})
	}, time.Second)}
	text, provenance, err := adapter.Complete(context.Background(), "the prompt")
	if err != nil {
		t.Fatal(err)
	}
	if text != `{"text":"hi"}` || adapter.ReportedModel() != "served" {
		t.Fatalf("text %q model %q", text, adapter.ReportedModel())
	}
	want := domain.Provenance{
		Adapter: "openai-http", PromptSHA256: domain.Digest([]byte("the prompt")), ResponseSHA256: domain.Digest([]byte(text)),
	}
	if provenance != want {
		t.Fatalf("provenance = %+v", provenance)
	}
}

func TestCompleteMapsFailuresOntoThePortErrors(t *testing.T) {
	if _, _, err := (Adapter{}).Complete(context.Background(), "p"); !errors.Is(err, domain.ErrModelUnavailable) {
		t.Fatalf("no client: %v", err)
	}
	failing := Adapter{Client: client(t, func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}, time.Second)}
	_, provenance, err := failing.Complete(context.Background(), "p")
	if !errors.Is(err, domain.ErrModelFailed) || !errors.Is(err, shared.ErrUpstream) || errors.Is(err, domain.ErrModelTimeout) {
		t.Fatalf("500: %v", err)
	}
	if provenance.PromptSHA256 != domain.Digest([]byte("p")) {
		t.Fatal("a failed call lost the prompt digest")
	}
	release := make(chan struct{})
	slow := Adapter{Client: client(t, func(_ http.ResponseWriter, request *http.Request) {
		select {
		case <-release:
		case <-request.Context().Done():
		}
	}, 50*time.Millisecond)}
	// Registered after the server's own cleanup so it runs first: Close waits
	// for the handler, and the handler waits for this.
	t.Cleanup(func() { close(release) })
	if _, _, err := slow.Complete(context.Background(), "p"); !errors.Is(err, domain.ErrModelTimeout) || !errors.Is(err, shared.ErrTimeout) {
		t.Fatalf("timeout: %v", err)
	}
}
