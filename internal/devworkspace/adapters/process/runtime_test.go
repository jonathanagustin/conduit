package process

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

func TestFailedStartDoubleStartAndStopAfterCrash(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "starts")
	runtime := New([]string{"sh", "-c", "echo $$ >> " + marker + "; sleep 30"}, nil)
	workspace := domain.Workspace{ID: "wsp_one", Owner: "ada"}
	failing := New([]string{"false"}, nil)
	if _, err := failing.Start(context.Background(), workspace); err == nil {
		t.Fatal("failed start was accepted")
	}
	first, err := runtime.Start(context.Background(), workspace)
	if err != nil || first.Status != domain.StatusRunning {
		t.Fatalf("start = %#v %v", first, err)
	}
	second, err := runtime.Start(context.Background(), workspace)
	if err != nil || second.ID != first.ID {
		t.Fatalf("second start = %#v %v", second, err)
	}
	body, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(body), "\n"); lines != 1 {
		t.Fatalf("start count = %d\n%s", lines, body)
	}
	if _, err := runtime.Stop(context.Background(), workspace.ID); err != nil {
		t.Fatal(err)
	}
	crashing := New([]string{"sh", "-c", "echo password=hunter2; sleep 0.2; exit 1"}, nil)
	crashSpace := domain.Workspace{ID: "wsp_crash", Owner: "ada"}
	started, err := crashing.Start(context.Background(), crashSpace)
	if err != nil || started.Status != domain.StatusRunning {
		t.Fatalf("crash start = %#v %v", started, err)
	}
	if err := crashing.Crash(crashSpace.ID); err != nil {
		t.Fatal(err)
	}
	status, err := crashing.Status(context.Background(), crashSpace.ID)
	if err != nil || status.Status != domain.StatusFailed {
		t.Fatalf("after crash = %#v %v", status, err)
	}
	stopped, err := crashing.Stop(context.Background(), crashSpace.ID)
	if err != nil || stopped.Status != domain.StatusStopped {
		t.Fatalf("stop after crash = %#v %v", stopped, err)
	}
	deadline := time.Now().Add(time.Second)
	var page domain.LogPage
	for time.Now().Before(deadline) {
		page, err = crashing.Logs(context.Background(), crashSpace.ID, "", 20)
		if err == nil && len(page.Lines) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || strings.Contains(strings.Join(page.Lines, "\n"), "hunter2") {
		t.Fatalf("logs = %#v %v", page, err)
	}
	if InheritedSecret(append(CommandEnv(), "TOKEN=nope")) && !InheritedSecret([]string{"TOKEN=nope"}) {
		t.Fatal("secret check inverted")
	}
	if InheritedSecret(CommandEnv()) {
		t.Fatal("child environment inherited a credential")
	}
}
