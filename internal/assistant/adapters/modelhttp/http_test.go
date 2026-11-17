package modelhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/assistant/domain"
	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	shared "github.com/jonathanagustin/conduit/internal/modelhttp"
)

var _ domain.Model = Adapter{}

func TestProviderIdentityBelongsToEachCompletion(t *testing.T) {
	adapter := Adapter{Client: client(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		prompt := body.Messages[0].Content
		model := "served-" + prompt
		if prompt == "omitted" {
			model = ""
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": model,
			"choices": []any{map[string]any{"message": map[string]any{"content": `{"text":"done"}`}}}})
	}, time.Second)}
	check := func(prompt string) {
		t.Helper()
		_, provenance, err := adapter.Complete(t.Context(), prompt)
		want := "served-" + prompt
		if prompt == "omitted" {
			want = ""
		}
		if err != nil || provenance.ConfiguredModel != "asked" || provenance.ProviderModel != want {
			t.Errorf("%s: model provenance=%+v error=%v", prompt, provenance, err)
		}
	}
	check("named")
	check("omitted") // A previous successful call must not fill in a missing id.
	var calls sync.WaitGroup
	for i := 0; i < 24; i++ {
		calls.Add(1)
		go func() { defer calls.Done(); check(fmt.Sprint(i)) }()
	}
	calls.Wait()
}

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
	if text != `{"text":"hi"}` || provenance.ProviderModel != "served" {
		t.Fatalf("text %q model %q", text, provenance.ProviderModel)
	}
	want := domain.Provenance{
		Adapter: "openai-http", ConfiguredModel: "asked", ProviderModel: "served", PromptSHA256: domain.Digest([]byte("the prompt")), ResponseSHA256: domain.Digest([]byte(text)),
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

// A call the governor refused is a limit, not a model failure, and an audit
// outage is an audit outage: each keeps its own port error and the wait.
func TestGovernorRefusalsKeepTheirOwnPortErrors(t *testing.T) {
	refuse := func(refusal error) error {
		built := client(t, func(http.ResponseWriter, *http.Request) { t.Error("a refused call reached the endpoint") }, time.Second)
		adapter := Adapter{Client: built.With(func(context.Context, string, string, func(context.Context) (shared.Completion, error)) (shared.Completion, error) {
			return shared.Completion{}, refusal
		})}
		_, _, err := adapter.Complete(context.Background(), "p")
		return err
	}
	busy := refuse(&modelgovernor.BusyError{Scope: "owner", RetryAfter: 3 * time.Second})
	var hint interface{ RetryAfterHint() time.Duration }
	if !errors.Is(busy, domain.ErrModelBusy) || errors.Is(busy, domain.ErrModelFailed) || !errors.As(busy, &hint) || hint.RetryAfterHint() != 3*time.Second {
		t.Fatalf("busy = %v", busy)
	}
	if budget := refuse(&modelgovernor.BudgetError{Unit: "calls", Limit: 1, Used: 1, RetryAfter: time.Hour}); !errors.Is(budget, domain.ErrModelBudgetExceeded) || errors.Is(budget, domain.ErrModelFailed) {
		t.Fatalf("budget = %v", budget)
	}
	if audit := refuse(modelgovernor.ErrAuditUnavailable); !errors.Is(audit, domain.ErrAuditUnavailable) || errors.Is(audit, domain.ErrModelFailed) {
		t.Fatalf("audit = %v", audit)
	}
}
