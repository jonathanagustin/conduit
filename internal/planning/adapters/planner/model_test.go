package planner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/jonathanagustin/conduit/internal/modelhttp"
	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

// claimedPlanJSON is a plan whose text claims a model name. That claim is
// model-authored and must never reach the binding or the audit record.
const claimedPlanJSON = `{"model":"claude-sonnet-4-20250514","summary":"A small Node site.","baseImage":"node:20-slim","buildSteps":["npm ci","npm run build"],"ports":[8080,3000],"networkAllow":["registry.npmjs.org:443"],"sso":{"required":true,"notes":"operator login"},"risks":["no lockfile"]}`

func acceptedSession(t *testing.T, runner Planner, repoURL string) *domain.Session {
	t.Helper()
	ctx := context.Background()
	service := application.NewService(runner)
	session, err := service.Create(ctx, repoURL, "main")
	if err != nil {
		t.Fatal(err)
	}
	if session, err = service.Turn(ctx, session.ID, "package this"); err != nil {
		t.Fatal(err)
	}
	if session, err = service.Accept(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	return session
}

func TestBindingModelNeverComesFromModelAuthoredText(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script fixture is a shell")
	}
	fixture, _ := makeFixture(t)
	server := serveGit(t, fixture)
	replies := append(inspectReplies(askNone), toolRead, claimedPlanJSON)

	// A model CLI with no configured name: the binding says so explicitly.
	audit := &memoryAudit{}
	script := writeModelScript(t, replies...)
	session := acceptedSession(t, Planner{Argv: []string{script}, Audit: audit}, server.URL+"/src.git")
	if session.Binding.Model != domain.UnverifiedModelLabel {
		t.Fatalf("binding model = %q, want the unverified marker %q", session.Binding.Model, domain.UnverifiedModelLabel)
	}
	if !strings.HasPrefix(session.Binding.Model, "unverified") {
		t.Fatalf("unverified marker %q does not say unverified", session.Binding.Model)
	}
	for _, row := range audit.rows {
		if row.ConfiguredModel != "" || row.ProviderModel != "" {
			t.Fatalf("audit recorded a model id with none configured: %+v", row)
		}
	}

	// A configured name wins over what the model claims.
	audit = &memoryAudit{}
	script = writeModelScript(t, replies...)
	session = acceptedSession(t, Planner{Argv: []string{script}, ModelName: "operator-chosen", Audit: audit}, server.URL+"/src.git")
	if session.Binding.Model != "operator-chosen" {
		t.Fatalf("binding model = %q, want operator-chosen", session.Binding.Model)
	}
	if len(audit.rows) == 0 {
		t.Fatal("no audit rows")
	}
	for _, row := range audit.rows {
		if row.ConfiguredModel != "operator-chosen" || row.ProviderModel != "" {
			t.Fatalf("audit model ids = %q/%q", row.ConfiguredModel, row.ProviderModel)
		}
		if row.Adapter == "" || strings.Contains(row.Adapter, "/") {
			t.Fatalf("audit adapter = %q", row.Adapter)
		}
	}
}

// chatFake serves canned chat completions in order and keeps each prompt.
type chatFake struct {
	mu      sync.Mutex
	replies []string
	model   string
	prompts []string
}

func (fake *chatFake) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	raw, _ := io.ReadAll(request.Body)
	var body struct {
		Model    string `json:"model"`
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(raw, &body)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(body.Messages) == 1 {
		fake.prompts = append(fake.prompts, body.Messages[0].Content)
	}
	if len(fake.replies) == 0 {
		http.Error(writer, "no reply left", http.StatusInternalServerError)
		return
	}
	reply := fake.replies[0]
	fake.replies = fake.replies[1:]
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"model":   fake.model,
		"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": reply}}},
	})
}

func TestPlanningTurnRunsOverHTTPAndRecordsProviderModel(t *testing.T) {
	fixture, sha := makeFixture(t)
	git := serveGit(t, fixture)
	fake := &chatFake{replies: append(inspectReplies(askNone), toolRead, claimedPlanJSON), model: "served-model-2026-01"}
	endpoint := httptest.NewServer(fake)
	t.Cleanup(endpoint.Close)
	client, err := modelhttp.New(modelhttp.Config{BaseURL: endpoint.URL + "/v1", Model: "requested-model"})
	if err != nil {
		t.Fatal(err)
	}
	audit := &memoryAudit{}
	session := acceptedSession(t, Planner{Model: HTTPModel{Client: client}, Audit: audit}, git.URL+"/src.git")

	if session.Plan == nil || session.ResolvedCommit != sha {
		t.Fatalf("plan/commit = %+v %q", session.Plan, session.ResolvedCommit)
	}
	want := "requested-model (provider reported served-model-2026-01)"
	if session.Binding.Model != want {
		t.Fatalf("binding model = %q, want %q", session.Binding.Model, want)
	}
	if len(audit.rows) != 3 {
		t.Fatalf("audit rows = %d, want inspect, questions and plan: %+v", len(audit.rows), audit.rows)
	}
	for _, row := range audit.rows {
		if row.Adapter != modelhttp.AdapterName || row.ConfiguredModel != "requested-model" || row.ProviderModel != "served-model-2026-01" {
			t.Fatalf("audit identity = %+v", row)
		}
		if row.Outcome != "pass" || len(row.PromptSHA256) != 64 || len(row.ResponseSHA256) != 64 {
			t.Fatalf("audit record = %+v", row)
		}
	}
	// The untrusted-data framing reaches the HTTP transport unchanged.
	if len(fake.prompts) < 2 {
		t.Fatalf("prompts = %d", len(fake.prompts))
	}
	if !strings.Contains(fake.prompts[0], untrustedRule) {
		t.Fatal("first prompt lacks the untrusted-data rule")
	}
	if !strings.Contains(fake.prompts[1], untrustedBegin+"\ninspect_repo: ") || !strings.Contains(fake.prompts[1], untrustedEnd) {
		t.Fatalf("tool results were not framed as untrusted data:\n%s", fake.prompts[1])
	}
}

func TestPlannerWithoutAModelIsNotConfigured(t *testing.T) {
	session := &domain.Session{ID: "s", Intake: domain.Intake{RepoURL: "https://example.invalid/r.git", Ref: "main"}}
	if _, err := (Planner{}).Turn(context.Background(), session, "go"); err != domain.ErrModelNotConfigured {
		t.Fatalf("err = %v, want ErrModelNotConfigured", err)
	}
}

func TestModelLabel(t *testing.T) {
	cases := []struct{ configured, provider, want string }{
		{"", "", domain.UnverifiedModelLabel},
		{"a", "", "a"},
		{"a", "a", "a"},
		{"a", "b", "a (provider reported b)"},
		{"", "b", "b (provider reported)"},
		{"a\nb", "x\x00y", "a b (provider reported x y)"},
	}
	for _, c := range cases {
		if got := ModelLabel(c.configured, c.provider); got != c.want {
			t.Errorf("ModelLabel(%q, %q) = %q, want %q", c.configured, c.provider, got, c.want)
		}
	}
	if got := cleanModelID(strings.Repeat("m", 500)); len(got) != maxModelIDLength {
		t.Fatalf("long id kept %d bytes", len(got))
	}
}
