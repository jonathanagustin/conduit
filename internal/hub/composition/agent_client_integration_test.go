package composition

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	planningapp "github.com/jonathanagustin/conduit/internal/planning/application"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
)

type clientIntegrationPlanner struct{ calls atomic.Int32 }

func (p *clientIntegrationPlanner) Turn(ctx context.Context, session *planningdomain.Session, _ string) (*planningapp.TurnResult, error) {
	p.calls.Add(1)
	if len(session.Answers) == 0 {
		questions := []planningdomain.Question{{ID: "port", Name: "port", Type: planningdomain.QuestionString, Required: true}}
		if err := planningapp.QuestionsObserved(ctx, questions); err != nil {
			return nil, err
		}
		if err := planningapp.FrontendToolCalled(ctx, "fill_field", json.RawMessage(`{"field":"question:port","value":"8080"}`)); err != nil {
			return nil, err
		}
		return &planningapp.TurnResult{NeedsInput: questions, ResolvedCommit: strings.Repeat("a", 40)}, nil
	}
	if session.Answers[0].Value == "9090" {
		return &planningapp.TurnResult{NeedsInput: []planningdomain.Question{{ID: "port", Name: "port", Type: planningdomain.QuestionString, Required: true, ValidationError: "Choose another port."}}}, nil
	}
	return &planningapp.TurnResult{Plan: &planningdomain.Plan{Text: fmt.Sprintf("Build the reviewed port %s.", session.Answers[0].Value)}}, nil
}

// The console gate opts in after installing its frozen Node dependencies. Pure
// Go test runs do not require a JavaScript toolchain; the complete gate does.
func TestPlanningJavaScriptClientIntegration(t *testing.T) {
	if os.Getenv("CONDUIT_TEST_AGENT_CLIENT") != "1" {
		t.Skip("executed by scripts/test-console.sh with the frozen console toolchain")
	}
	planner := &clientIntegrationPlanner{}
	repository := controlplane.NewMemoryRepository()
	server := httptest.NewServer(planningHandler(http.NotFoundHandler(), repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}, planner))
	defer server.Close()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", "src/test/fixtures/agent-client.mjs")
	command.Dir = filepath.Join(root, "web")
	command.Env = append(os.Environ(), "CONDUIT_AGENT_FIXTURE_ORIGIN="+server.URL)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("JavaScript client: %v\n%s", err, output)
	}
	if planner.calls.Load() != 3 {
		t.Fatalf("snapshot or ACK repeated model execution: calls=%d", planner.calls.Load())
	}
	t.Log(string(output))
}
