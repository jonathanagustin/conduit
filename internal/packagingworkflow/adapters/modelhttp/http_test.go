package modelhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	shared "github.com/jonathanagustin/conduit/internal/modelhttp"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

var _ domain.ChangeProposer = Adapter{}

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

func TestProposeParsesTheProposalFromTheCompletion(t *testing.T) {
	reply := `{"field":"memory","value":"512Mi","rationale":"headroom"}`
	adapter := adapterFor(t, http.StatusOK, reply)
	proposal, provenance, err := adapter.Propose(context.Background(), domain.PackagingContext{
		RunID: "run-1", MemoryLimit: "512Mi", MemoryRequest: "512Mi", GateRules: domain.MemoryGateRule,
	})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Value != "512Mi" || adapter.ReportedModel() != "served" {
		t.Fatalf("proposal %+v model %q", proposal, adapter.ReportedModel())
	}
	if provenance.Adapter != "openai-http" || provenance.PromptSHA256 == "" || provenance.SkillSHA256 == "" ||
		provenance.ResponseSHA256 != domain.DigestText([]byte(reply)) {
		t.Fatalf("provenance = %+v", provenance)
	}
}

func TestProposeMapsFailuresOntoThePortErrors(t *testing.T) {
	if _, _, err := (Adapter{}).Propose(context.Background(), domain.PackagingContext{}); !errors.Is(err, domain.ErrModelUnavailable) {
		t.Fatalf("no client: %v", err)
	}
	_, _, err := adapterFor(t, http.StatusUnauthorized, "").Propose(context.Background(), domain.PackagingContext{RunID: "r"})
	if !errors.Is(err, domain.ErrModelFailed) || !errors.Is(err, shared.ErrUnauthorized) {
		t.Fatalf("401: %v", err)
	}
	_, _, err = adapterFor(t, http.StatusOK, "not-json").Propose(context.Background(), domain.PackagingContext{RunID: "r"})
	if err == nil || errors.Is(err, domain.ErrModelFailed) {
		t.Fatalf("an unparseable proposal must be the parser's error: %v", err)
	}
}
