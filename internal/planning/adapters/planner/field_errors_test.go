package planner

import (
	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
	"runtime"
	"strings"
	"testing"
)

func TestModelFieldErrorsRetainKnownQuestionPolicy(t *testing.T) {
	original := []domain.Question{{ID: "port", Name: "Port", Type: domain.QuestionSelect, Required: false, Options: []string{"8080", "9090"}}}
	questions, found, err := parseFieldErrors([]byte(`{"fieldErrors":[{"id":"port","message":"Choose another port."}]}`), original)
	if err != nil || !found || questions[0].ValidationError != "Choose another port." || questions[0].Type != domain.QuestionSelect || questions[0].Required || len(questions[0].Options) != 2 {
		t.Fatalf("questions=%+v found=%t err=%v", questions, found, err)
	}
	if original[0].ValidationError != "" {
		t.Fatal("parsing changed the source session")
	}
	for _, raw := range []string{`{"fieldErrors":[]}`, `{"fieldErrors":[{"id":"unknown","message":"x"}]}`, `{"fieldErrors":[{"id":"port","message":""}]}`, `{"fieldErrors":[{"id":"port","message":"x"},{"id":"port","message":"y"}]}`, `{"fieldErrors":[{"id":"port","message":"x","approve":true}]}`, `{"fieldErrors":[{"id":"port","message":"x"}],"summary":"a plan"}`, `{"fieldErrors":[{"id":"port","message":"` + strings.Repeat("x", 4097) + `"}]}`} {
		if _, found, err := parseFieldErrors([]byte(raw), original); !found || err == nil {
			t.Fatalf("invalid fieldErrors accepted: %s", raw)
		}
	}
}

func TestScriptedPlanValidationReturnsErrorsThenAcceptsCorrection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell model fixture")
	}
	fixture, _ := makeFixture(t)
	server := serveGit(t, fixture)
	replies := append(inspectReplies(askOne), toolRead, `{"fieldErrors":[{"id":"node_version","message":"Choose the other offered version."}]}`, toolRead, planJSON)
	script := writeModelScript(t, replies...)
	service := application.NewService(Planner{Model: script, Audit: &memoryAudit{}})
	session, err := service.Create(t.Context(), server.URL+"/src.git", "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Turn(t.Context(), session.ID, "inspect"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Answer(t.Context(), session.ID, []domain.Answer{{ID: "node_version", Value: "20"}}); err != nil {
		t.Fatal(err)
	}
	rejected, err := service.Turn(t.Context(), session.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Status != domain.StatusNeedsInput || rejected.Plan != nil || len(rejected.Answers) != 0 || rejected.NeedsInput[0].ValidationError != "Choose the other offered version." {
		t.Fatalf("model error did not reopen the form: %+v", rejected)
	}
	if _, err := service.Answer(t.Context(), session.ID, []domain.Answer{{ID: "node_version", Value: "18"}}); err != nil {
		t.Fatal(err)
	}
	corrected, err := service.Turn(t.Context(), session.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if corrected.Status != domain.StatusReady || corrected.Plan == nil || corrected.Binding != nil || corrected.NeedsInput[0].ValidationError != "" {
		t.Fatalf("corrected model phase: %+v", corrected)
	}
}
