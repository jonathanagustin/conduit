package planner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

type frontendFixture struct {
	questions bool
	calls     int
}

func (f *frontendFixture) QuestionsObserved(context.Context, []domain.Question) error {
	f.questions = true
	return nil
}
func (f *frontendFixture) Call(_ context.Context, _ string, _ json.RawMessage) error {
	if !f.questions {
		return errors.New("frontend call preceded question parsing")
	}
	f.calls++
	return nil
}

func TestModelFrontendHandoffStopsBeforeTheNextPhase(t *testing.T) {
	fixture, _ := makeFixture(t)
	server := serveGit(t, fixture)
	reply := `{"questions":[],"clientToolCalls":[{"name":"request_confirmation","arguments":{"message":"Review before planning?"}}]}`
	replies := inspectReplies(reply)
	script := writeModelScript(t, replies...)
	observer := &frontendFixture{}
	ctx := application.WithRunScope(t.Context(), application.RunScope{ThreadID: "fixture", RunID: "run", Frontend: observer, Questions: observer, Grounding: `{"context":"grounding-sentinel"}`})
	service := application.NewService(Planner{Argv: []string{script}})
	session, err := service.Create(ctx, server.URL+"/src.git", "main")
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.Turn(ctx, session.ID, "inspect")
	if err != nil {
		t.Fatal(err)
	}
	if observer.calls != 1 || session.Plan != nil {
		t.Fatal("frontend handoff continued into plan generation")
	}
	count, err := os.ReadFile(filepath.Join(filepath.Dir(script), "n"))
	if err != nil {
		t.Fatal(err)
	}
	if string(count) != strconv.Itoa(len(replies))+"\n" {
		t.Fatalf("unexpected extra model call: %s", count)
	}
	prompt, err := os.ReadFile(filepath.Join(filepath.Dir(script), "prompt-1.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prompt), "grounding-sentinel") {
		t.Fatal("run conversation/context never reached the model")
	}
}
