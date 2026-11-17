package planner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	packagingtools "github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/packagingtools"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/repoclone"
	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

type observerFixture struct {
	deny     string
	failure  error
	started  []string
	finished []bool
}

func (o *observerFixture) Started(_ context.Context, name string, _ json.RawMessage) (string, error) {
	o.started = append(o.started, name)
	if name == o.deny {
		return "", o.failure
	}
	return name + "-call", nil
}
func (o *observerFixture) Finished(_ context.Context, _ string, success bool) error {
	o.finished = append(o.finished, success)
	return nil
}

func TestObserverVetoPreventsToolExecution(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	veto := errors.New("run audit unavailable")
	observer := &observerFixture{deny: "inspect_repo", failure: veto}
	ctx := application.WithRunScope(t.Context(), application.RunScope{ThreadID: "thread", RunID: "run", Tools: observer})
	executed := false
	_, calls, inspection, err := runToolRequests(ctx, packagingtools.New(packagingtools.WithRepoRoot(root)), []packagingdomain.ToolRequest{{Name: "inspect_repo", Arguments: json.RawMessage(`{}`)}}, nil, nil, func(string, string) { executed = true })
	if !errors.Is(err, veto) || executed || len(calls) != 0 || inspection != nil || len(observer.finished) != 0 {
		t.Fatalf("veto ignored: err=%v executed=%v calls=%v inspection=%v observer=%+v", err, executed, calls, inspection, observer)
	}
}

const inspectAndRead = `{"toolCalls":[{"name":"inspect_repo","arguments":{}},{"name":"read_file","arguments":{"path":"package.json"}}]}`

func TestInspectionDoesNotIgnoreVetoAfterEarlierToolSucceeded(t *testing.T) {
	fixture, _ := makeFixture(t)
	server := serveGit(t, fixture)
	veto := errors.New("run audit unavailable")
	observer := &observerFixture{deny: "read_file", failure: veto}
	ctx := application.WithRunScope(t.Context(), application.RunScope{ThreadID: "thread", RunID: "run", Tools: observer})
	service := application.NewService(Planner{Model: writeModelScript(t, inspectAndRead, "summary", askNone)})
	session, err := service.Create(ctx, server.URL+"/src.git", "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Turn(ctx, session.ID, "inspect"); !errors.Is(err, veto) {
		t.Fatalf("inspection continued past an audit veto: %v", err)
	}
	got, err := service.Get(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Inspection != nil || got.Plan != nil || len(observer.finished) != 1 || !observer.finished[0] {
		t.Fatalf("partial turn was committed: %+v observer=%+v", got, observer)
	}
}

func TestLaterPhasePropagatesToolVeto(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	veto := errors.New("run audit unavailable")
	observer := &observerFixture{deny: "read_file", failure: veto}
	ctx := application.WithRunScope(t.Context(), application.RunScope{ThreadID: "thread", RunID: "run", Tools: observer})
	runner := Planner{Model: writeModelScript(t, inspectAndRead, askNone)}
	parsed := false
	err := runner.runPhase(ctx, &domain.Session{ID: "thread"}, repoclone.Checkout{Dir: root}, packagingtools.New(packagingtools.WithRepoRoot(root)), ActionQuestions, "fixture", func([]byte) error { parsed = true; return nil })
	if !errors.Is(err, veto) || parsed {
		t.Fatalf("later phase continued past an audit veto: %v parsed=%v", err, parsed)
	}
}

type completionVeto struct {
	observerFixture
	err error
}

func (o *completionVeto) Finished(ctx context.Context, id string, success bool) error {
	_ = o.observerFixture.Finished(ctx, id, success)
	return o.err
}

func TestCompletionAuditFailureStopsTheNextTool(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	veto := errors.New("completion audit unavailable")
	observer := &completionVeto{err: veto}
	ctx := application.WithRunScope(t.Context(), application.RunScope{ThreadID: "thread", RunID: "run", Tools: observer})
	_, _, _, err := runToolRequests(ctx, packagingtools.New(packagingtools.WithRepoRoot(root)), []packagingdomain.ToolRequest{{Name: "inspect_repo", Arguments: json.RawMessage(`{}`)}, {Name: "read_file", Arguments: json.RawMessage(`{"path":"package.json"}`)}}, nil, nil, nil)
	if !errors.Is(err, veto) || len(observer.started) != 1 || len(observer.finished) != 1 || !observer.finished[0] {
		t.Fatalf("completion veto ignored: %v observer=%+v", err, observer)
	}
}
