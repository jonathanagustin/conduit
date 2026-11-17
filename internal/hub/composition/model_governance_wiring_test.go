package composition

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	appdomain "github.com/jonathanagustin/conduit/internal/appgen/domain"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/modelgovernor/adapters/memory"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/repoclone"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
)

// auditingGovernor is a governor that audits to repository the way a Hub's
// does, with nothing else configured.
func auditingGovernor(repository controlplane.Repository) *modelgovernor.Governor {
	return modelgovernor.New(modelgovernor.Config{AuditRequired: true}, modelgovernor.Dependencies{
		Usage: memory.NewUsage(), Audit: modelCallAudit{repository: repository}, Metrics: modelgovernor.NewMetrics(),
	})
}

// callEveryCapability builds each capability's model transport the way NewHub
// does and makes one call through it. The replies do not have to parse: the
// call is what is under test, not what the capability makes of the answer.
func callEveryCapability(t *testing.T, governor *modelgovernor.Governor) {
	t.Helper()
	done := assemblingWith(governor)
	assistant, err := assistantModel(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	generator, err := appGenerator(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	proposer, err := modelProposer(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := packagingWriter(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	evals := liveEvalProposer()
	plans, err := planningPlanner(os.Getenv, nil)
	if err != nil {
		t.Fatal(err)
	}
	done()
	if assistant == nil || generator == nil || proposer == nil || writer == nil || evals == nil || plans == nil {
		t.Fatalf("a capability has no model: %v %v %v %v %v %v", assistant, generator, proposer, writer, evals, plans)
	}
	// No context below carries a governor: each transport must have kept the
	// one it was built under.
	ctx := context.Background()
	_, _, _ = assistant.Complete(ctx, "hello")
	_, _, _ = generator.Generate(ctx, appdomain.Request{Name: "app", Description: "an app"})
	_, _, _ = proposer.Propose(ctx, packagingdomain.PackagingContext{RunID: "run", GateRules: packagingdomain.MemoryGateRule})
	_, _, _ = writer.ProposePackage(ctx, packagingdomain.PackagingContext{RunID: "run"})
	_, _ = evals.Propose(ctx, "memory", "512Mi")
	planning := plans.(planner.Planner)
	checkout := t.TempDir()
	planning.Clone = func(context.Context, string, string) (repoclone.Checkout, func(), error) {
		return repoclone.Checkout{Dir: checkout, SHA: "0123456789abcdef0123456789abcdef01234567"}, nil, nil
	}
	_, _ = planning.Turn(ctx, &planningdomain.Session{ID: "session", Intake: planningdomain.Intake{RepoURL: "https://example.test/repo.git", Ref: "main"}}, "plan it")
}

func capabilitiesAudited(t *testing.T, repository controlplane.Repository, transport string) []string {
	t.Helper()
	seen := map[string]bool{}
	for _, event := range auditEvents(t, repository, modelgovernor.ActionModelCall) {
		if event.Context["transport"] != transport {
			t.Fatalf("event over %v, want %s: %+v", event.Context["transport"], transport, event.Context)
		}
		capability, _ := event.Context["capability"].(string)
		seen[capability] = true
	}
	out := make([]string, 0, len(seen))
	for capability := range seen {
		out = append(out, capability)
	}
	sort.Strings(out)
	return out
}

const everyCapability = "appgen assistant intelligence packaging planning"

func TestEveryCapabilityIsGovernedOverHTTP(t *testing.T) {
	for _, name := range governanceEnvironment {
		t.Setenv(name, "")
	}
	server := newScriptedServer(t, &scriptedEndpoint{replies: []string{"not a document any capability accepts"}})
	t.Setenv("CONDUIT_MODEL_BASE_URL", server)
	t.Setenv("CONDUIT_MODEL_NAME", "asked-model")
	repository := controlplane.NewMemoryRepository()
	callEveryCapability(t, auditingGovernor(repository))
	if got := strings.Join(capabilitiesAudited(t, repository, "http"), " "); got != everyCapability {
		t.Fatalf("capabilities audited over http = %q, want %q", got, everyCapability)
	}
}

func TestEveryCapabilityIsGovernedOverTheModelCommand(t *testing.T) {
	for _, name := range governanceEnvironment {
		t.Setenv(name, "")
	}
	script := filepath.Join(t.TempDir(), "model")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s' 'not a document any capability accepts'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONDUIT_MODEL_CMD", script)
	repository := controlplane.NewMemoryRepository()
	callEveryCapability(t, auditingGovernor(repository))
	if got := strings.Join(capabilitiesAudited(t, repository, "cli"), " "); got != everyCapability {
		t.Fatalf("capabilities audited over cli = %q, want %q", got, everyCapability)
	}
}

// A planning turn run by the workflow worker has no request. Every round of
// it is still audited, with the digest of that round's own prompt -- which
// grows with tool output -- and the rounds are chained into one run.
func TestEveryRoundOfAWorkerPlanningTurnIsAuditedAndChained(t *testing.T) {
	for _, name := range governanceEnvironment {
		t.Setenv(name, "")
	}
	endpoint := &scriptedEndpoint{replies: []string{
		`{"toolCalls":[{"name":"inspect_repo","arguments":{}}]}`,
		`{"toolCalls":[{"name":"read_file","arguments":{"path":"README.md"}}]}`,
		"A small site.",
		`{"questions":[]}`,
	}}
	t.Setenv("CONDUIT_MODEL_BASE_URL", newScriptedServer(t, endpoint))
	t.Setenv("CONDUIT_MODEL_NAME", "asked-model")
	repository := controlplane.NewMemoryRepository()
	// Built outside any Hub assembly, as StartPlanningWorker builds it: the
	// transport itself keeps no governor, so the turn's scope is what governs.
	built, err := planningPlanner(os.Getenv, repository)
	if err != nil {
		t.Fatal(err)
	}
	planning := built.(planner.Planner)
	checkout := t.TempDir()
	if err := os.WriteFile(filepath.Join(checkout, "README.md"), []byte("fixture readme\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	planning.Clone = func(context.Context, string, string) (repoclone.Checkout, func(), error) {
		return repoclone.Checkout{Dir: checkout, SHA: "0123456789abcdef0123456789abcdef01234567"}, nil, nil
	}
	worker := turnScopedPlanner{runner: planning, governor: auditingGovernor(repository)}
	session := &planningdomain.Session{ID: "session-7", Intake: planningdomain.Intake{RepoURL: "https://example.test/repo.git", Ref: "main"}}
	// The scripted replies stop short of a plan, so the turn ends in an error;
	// the rounds it made are what is checked.
	_, _ = worker.Turn(context.Background(), session, "plan it")
	prompts := endpoint.Prompts()
	events := auditEvents(t, repository, modelgovernor.ActionModelCall)
	if len(prompts) < 3 || len(events) != len(prompts) {
		t.Fatalf("%d model calls and %d audit events", len(prompts), len(events))
	}
	round := func(event controlplane.AuthorizationAuditEvent) int {
		number, _ := strconv.Atoi(fmt.Sprint(event.Context["round"]))
		return number
	}
	sort.Slice(events, func(i, j int) bool { return round(events[i]) < round(events[j]) })
	previous := ""
	for index, event := range events {
		if round(event) != index+1 || event.Context["runId"] != events[0].Context["runId"] ||
			event.Context["sessionId"] != "planning-session/session-7" || event.Context["capability"] != "planning" {
			t.Fatalf("event %d = %+v", index, event.Context)
		}
		if event.Context["promptSha256"] != modelgovernor.Digest([]byte(prompts[index])) {
			t.Fatalf("round %d is not audited with the digest of its own prompt", index+1)
		}
		if event.Context["previousChainSha256"] != previous {
			t.Fatalf("round %d does not link to round %d", index+1, index)
		}
		previous, _ = event.Context["chainSha256"].(string)
	}
	if prompts[0] == prompts[1] {
		t.Fatal("the fixture did not grow the prompt between rounds; the test proves nothing")
	}
}

func newScriptedServer(t *testing.T, endpoint *scriptedEndpoint) string {
	t.Helper()
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)
	return server.URL
}
