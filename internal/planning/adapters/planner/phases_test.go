package planner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

func eventTypes(session *domain.Session) []string {
	var out []string
	for _, event := range session.Events {
		out = append(out, event.Type)
	}
	return out
}

func countType(session *domain.Session, kind string) int {
	n := 0
	for _, event := range session.Events {
		if event.Type == kind {
			n++
		}
	}
	return n
}

func TestQuestionsAskedAnsweredThenPlanWritten(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script fixture is a shell")
	}
	fixture, sha := makeFixture(t)
	server := serveGit(t, fixture)
	replies := append(inspectReplies(askOne), toolRead, planJSON)
	script := writeModelScript(t, replies...)
	audit := &memoryAudit{}
	service := application.NewService(Planner{Argv: []string{script}, Audit: audit})
	ctx := context.Background()
	session, err := service.Create(ctx, server.URL+"/src.git", "main")
	if err != nil {
		t.Fatal(err)
	}

	session, err = service.Turn(ctx, session.ID, "package this")
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != domain.StatusNeedsInput || session.Plan != nil {
		t.Fatalf("status/plan = %s %+v", session.Status, session.Plan)
	}
	if len(session.NeedsInput) != 1 {
		t.Fatalf("needsInput = %+v", session.NeedsInput)
	}
	question := session.NeedsInput[0]
	if question.ID != "node_version" || question.Label != "Which Node version?" || question.Type != domain.QuestionSelect || !question.Required || strings.Join(question.Options, ",") != "18,20" {
		t.Fatalf("question = %+v", question)
	}
	if countType(session, domain.EventNeedsInput) != 1 {
		t.Fatalf("events = %v", eventTypes(session))
	}
	// Accept stays refused while the question is open, and a turn does not
	// write the plan over it.
	if _, err := service.Accept(ctx, session.ID); !errors.Is(err, domain.ErrPlanRequired) {
		t.Fatalf("accept before plan = %v", err)
	}
	same, err := service.Turn(ctx, session.ID, "")
	if err != nil || same.Plan != nil || same.Status != domain.StatusNeedsInput {
		t.Fatalf("turn with open question = %+v %v", same, err)
	}

	session, err = service.Answer(ctx, session.ID, []domain.Answer{{ID: "node_version", Value: "20"}})
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != domain.StatusPlanning {
		t.Fatalf("status after answer = %s", session.Status)
	}
	session, err = service.Turn(ctx, session.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != domain.StatusReady || session.Plan == nil {
		t.Fatalf("status/plan = %s %+v", session.Status, session.Plan)
	}
	if session.Plan.SHA256 != domain.PlanHash(*session.Plan) {
		t.Fatalf("plan hash %q is not the canonical hash", session.Plan.SHA256)
	}
	want := `{"summary":"A small Node site.","baseImage":"node:20-slim","buildSteps":["npm ci","npm run build"],"ports":[3000,8080],"networkAllow":["registry.npmjs.org:443"],"sso":{"required":true,"notes":"operator login"},"risks":["no lockfile"]}`
	if session.Plan.Text != want {
		t.Fatalf("plan text = %s", session.Plan.Text)
	}
	if countType(session, domain.EventPlan) != 1 {
		t.Fatalf("events = %v", eventTypes(session))
	}

	// The plan prompt carries the operator's answer and demands read_file.
	planPrompt := readPrompt(t, script, 7)
	if !strings.Contains(planPrompt, "Which Node version?: 20") || !strings.Contains(planPrompt, "MUST call read_file") {
		t.Fatalf("plan prompt = %s", planPrompt)
	}
	if !strings.Contains(readPrompt(t, script, 4), "MUST call read_file") {
		t.Fatal("questions prompt does not require read_file")
	}

	var actions []string
	for _, row := range audit.rows {
		actions = append(actions, row.Action)
		if row.PromptSHA256 == "" || row.ResponseSHA256 == "" || row.Outcome != "pass" || row.ResolvedCommit != sha {
			t.Fatalf("audit row = %+v", row)
		}
	}
	if got := strings.Join(actions, ","); got != ",planning.questions,planning.plan" {
		t.Fatalf("audit actions = %q", got)
	}
	if got := strings.Join(audit.rows[2].ToolCalls, ","); got != "read_file,read_file" {
		t.Fatalf("plan tool calls = %q", got)
	}

	accepted, err := service.Accept(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Binding.Plan.SHA256 != session.Plan.SHA256 {
		t.Fatalf("binding hash %q != plan hash %q", accepted.Binding.Plan.SHA256, session.Plan.SHA256)
	}
}

func TestNoQuestionsWritesThePlanInTheSameTurn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script fixture is a shell")
	}
	fixture, _ := makeFixture(t)
	server := serveGit(t, fixture)
	replies := append(inspectReplies(askNone), toolRead, planJSON)
	audit := &memoryAudit{}
	service := application.NewService(Planner{Argv: []string{writeModelScript(t, replies...)}, Audit: audit})
	ctx := context.Background()
	session, err := service.Create(ctx, server.URL+"/src.git", "main")
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.Turn(ctx, session.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != domain.StatusReady || session.Plan == nil || len(session.NeedsInput) != 0 {
		t.Fatalf("status/plan/questions = %s %+v %+v", session.Status, session.Plan, session.NeedsInput)
	}
	if countType(session, domain.EventNeedsInput) != 0 || countType(session, domain.EventPlan) != 1 {
		t.Fatalf("events = %v", eventTypes(session))
	}
	if len(audit.rows) != 3 || audit.rows[1].Action != ActionQuestions || audit.rows[2].Action != ActionPlan {
		t.Fatalf("audit = %+v", audit.rows)
	}
}

func TestPlanHashIsStableAcrossFormatting(t *testing.T) {
	t.Parallel()
	variants := []string{
		planJSON,
		"Here is the plan:\n```json\n" + `{"risks":["no  lockfile"],"sso":{"notes":"operator login","required":true},"networkAllow":["registry.npmjs.org:443","registry.npmjs.org:443"],"ports":[3000,8080,3000],"buildSteps":[" npm ci","npm run build "],"baseImage":"node:20-slim","summary":"A small   Node site.\n"}` + "\n```\n",
	}
	var first *domain.Plan
	for i, raw := range variants {
		plan, err := parsePlan([]byte(raw))
		if err != nil {
			t.Fatalf("variant %d: %v", i, err)
		}
		if plan.SHA256 != domain.PlanHash(*plan) {
			t.Fatalf("variant %d: hash is not PlanHash", i)
		}
		if first == nil {
			first = plan
			continue
		}
		if plan.Text != first.Text || plan.SHA256 != first.SHA256 {
			t.Fatalf("variant %d differs:\n%s\n%s", i, plan.Text, first.Text)
		}
	}
	other, err := parsePlan([]byte(strings.Replace(planJSON, "node:20-slim", "node:22-slim", 1)))
	if err != nil || other.SHA256 == first.SHA256 {
		t.Fatalf("a changed plan kept the hash: %v", err)
	}
}

func TestParseRejectsMalformedReplies(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]string{
		"no plan object":   "I think it is fine.",
		"no base image":    `{"summary":"x","buildSteps":["a"]}`,
		"no build steps":   `{"summary":"x","baseImage":"y"}`,
		"bad port":         `{"summary":"x","baseImage":"y","buildSteps":["a"],"ports":[70000]}`,
		"empty plan json":  `{}`,
		"not a plan reply": askOne,
	} {
		if _, err := parsePlan([]byte(raw)); err == nil {
			t.Errorf("parsePlan accepted %s", name)
		}
	}
	for name, raw := range map[string]string{
		"no questions key": `{"summary":"x"}`,
		"missing prompt":   `{"questions":[{"id":"a"}]}`,
		"repeated id":      `{"questions":[{"id":"a","prompt":"p"},{"id":"a","prompt":"q"}]}`,
	} {
		if _, err := parseQuestions([]byte(raw)); err == nil {
			t.Errorf("parseQuestions accepted %s", name)
		}
	}
	got, err := parseQuestions([]byte(`{"questions":[{"id":"a","prompt":"Free text?"},{"id":"b","prompt":"Maybe?","required":false}]}`))
	if err != nil || len(got) != 2 || got[0].Type != domain.QuestionString || !got[0].Required || got[1].Required {
		t.Fatalf("questions = %+v %v", got, err)
	}
}

func TestMissingReadFilePromptsOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script fixture is a shell")
	}
	fixture, _ := makeFixture(t)
	server := serveGit(t, fixture)
	// The model answers the questions phase without reading a file; the
	// planner nudges once, then accepts the answer after the read.
	replies := []string{
		toolInspect, "summary", // inspection
		askNone,  // questions: no read yet, so the planner nudges
		toolRead, // questions retry reads
		askNone,
		toolRead, planJSON,
	}
	script := writeModelScript(t, replies...)
	service := application.NewService(Planner{Argv: []string{script}})
	ctx := context.Background()
	session, err := service.Create(ctx, server.URL+"/src.git", "main")
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.Turn(ctx, session.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if session.Plan == nil {
		t.Fatalf("no plan; events = %v", eventTypes(session))
	}
	if !strings.Contains(readPrompt(t, script, 4), "You have not called read_file yet") {
		t.Fatal("the nudge was not sent")
	}
}

func TestModelNotConfigured(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, svc := range map[string]*application.Service{
		"no planner":    application.NewService(nil),
		"empty command": application.NewService(Planner{}),
		"blank command": application.NewService(Planner{Argv: []string{"  "}}),
	} {
		session, err := svc.Create(ctx, "https://example.test/org/repo", "main")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Turn(ctx, session.ID, "go"); !errors.Is(err, domain.ErrModelNotConfigured) {
			t.Errorf("%s: err = %v, want MODEL_NOT_CONFIGURED", name, err)
		}
	}
}

func readPrompt(t *testing.T, script string, n int) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(script), "prompt-"+strconv.Itoa(n)+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
