package modelhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	shared "github.com/jonathanagustin/conduit/internal/modelhttp"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
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
	// The ids are the configured name and the response's model field.
	if provenance.ConfiguredModel != "asked" || provenance.ProviderModel != "served" {
		t.Fatalf("model ids = %q/%q", provenance.ConfiguredModel, provenance.ProviderModel)
	}
}

type imageTools struct{ domain.Toolbox }

func (imageTools) InspectImage(context.Context, string) (domain.ImageInspect, error) {
	return domain.ImageInspect{Digest: "sha256:" + strings.Repeat("7", 64)}, nil
}

func TestProposePackageIsThePackageWriterOverHTTP(t *testing.T) {
	var _ application.PackageWriter = Adapter{}
	reply := `{"source":"src","zarf":"kind: ZarfPackageConfig","cr":"kind: Package","rationale":"one service"}`
	var prompt string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		if len(body.Messages) == 1 {
			prompt = body.Messages[0].Content
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"model": "served", "choices": []any{map[string]any{"message": map[string]any{"content": reply}}},
		})
	}))
	t.Cleanup(server.Close)
	client, err := shared.New(shared.Config{BaseURL: server.URL, Model: "asked"})
	if err != nil {
		t.Fatal(err)
	}
	input := domain.PackagingContext{RunID: "run-1", ImageRef: "registry.example/app:v1"}
	proposal, provenance, err := Adapter{Client: client, Tools: imageTools{}}.ProposePackage(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Source != "src" || proposal.CR != "kind: Package" {
		t.Fatalf("proposal = %+v", proposal)
	}
	if provenance.ConfiguredModel != "asked" || provenance.ProviderModel != "served" {
		t.Fatalf("model ids = %q/%q", provenance.ConfiguredModel, provenance.ProviderModel)
	}
	if len(provenance.ToolCalls) != 1 || provenance.ToolCalls[0].Name != "inspect_image" {
		t.Fatalf("tool calls = %+v", provenance.ToolCalls)
	}
	// The digest covers the prompt that was sent, inspected facts included.
	if provenance.PromptSHA256 != domain.DigestText([]byte(prompt)) || !strings.Contains(prompt, "inspect_image: ") {
		t.Fatalf("prompt digest does not match the sent prompt")
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
