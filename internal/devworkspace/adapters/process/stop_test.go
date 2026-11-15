package process

import (
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"os/exec"
	"testing"
	"time"
)

func TestCancelledStopDoesNotKillRunningPreview(t *testing.T) {
	runtime := New([]string{"sleep", "30"}, nil)
	workspace := domain.Workspace{ID: "wsp_cancel", Owner: "ada"}
	if _, err := runtime.Start(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = runtime.Stop(context.Background(), workspace.ID) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtime.Stop(ctx, workspace.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled stop: %v", err)
	}
	status, err := runtime.Status(context.Background(), workspace.ID)
	if err != nil || status.Status != domain.StatusRunning {
		t.Fatalf("cancelled stop changed runtime: %#v %v", status, err)
	}

	select {
	case <-runtime.runs[workspace.ID].done:
		t.Fatal("cancelled stop killed the child")
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := runtime.Stop(context.Background(), workspace.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.runs[workspace.ID].done:
	default:
		t.Fatal("reported stopped before process wait completed")
	}
}
func TestStopRequiresAProcessHandleAndObservedExit(t *testing.T) {
	runtime := New(nil, nil)
	runtime.runs["missing"] = &child{status: domain.StatusRunning, done: make(chan struct{})}
	if _, err := runtime.Stop(context.Background(), "missing"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("missing process handle: %v", err)
	}
	if runtime.runs["missing"].status != domain.StatusRunning {
		t.Fatal("missing handle marked stopped")
	}
	// The owned command has already exited. Model a missing supervisor exit
	// notification; even ErrProcessDone from Kill is not a completed wait receipt.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	runtime.runs["unconfirmed"] = &child{cmd: cmd, status: domain.StatusRunning, done: make(chan struct{})}
	if _, err := runtime.Stop(context.Background(), "unconfirmed"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unconfirmed exit: %v", err)
	}
	if runtime.runs["unconfirmed"].status != domain.StatusRunning || runtime.runs["unconfirmed"].closed {
		t.Fatal("unconfirmed exit marked stopped")
	}
	close(runtime.runs["unconfirmed"].done)
	if _, err := runtime.Stop(context.Background(), "unconfirmed"); err != nil {
		t.Fatalf("confirmed retry: %v", err)
	}
}
