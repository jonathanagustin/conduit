package agent

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// The execution record is what an operator reads to find out what this Agent
// did and in which order. Sequence numbers are its only ordering: the tasks
// carry timestamps, but several can share one when a clock is coarse or
// injected, so a duplicated or zero-based sequence makes the record ambiguous
// exactly when it is being read to explain a failure.
func TestExecutionRecorderNumbersTasksFromOneInOrder(t *testing.T) {
	t.Parallel()
	frozen := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	recorder := newExecutionRecorder(func() time.Time { return frozen })

	for _, namespace := range []string{"first", "second", "third"} {
		recorder.add(controlplane.ExecutionPhaseApply, controlplane.ExecutionTaskSucceeded,
			"installation", namespace, frozen, nil)
	}
	tasks := recorder.execution.Tasks
	if len(tasks) != 3 {
		t.Fatalf("recorded %d tasks, want 3", len(tasks))
	}
	for index, task := range tasks {
		if task.Sequence != index+1 {
			t.Errorf("task %d has sequence %d, want %d", index, task.Sequence, index+1)
		}
	}
}

func TestExecutionRecorderRecordsAndRedactsFailures(t *testing.T) {
	t.Parallel()
	frozen := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	recorder := newExecutionRecorder(func() time.Time { return frozen })

	recorder.add(controlplane.ExecutionPhaseApply, controlplane.ExecutionTaskFailed,
		"installation", "example", frozen, errors.New("helm upgrade failed: context deadline exceeded"))
	tasks := recorder.execution.Tasks
	if len(tasks) != 1 {
		t.Fatalf("recorded %d tasks, want 1", len(tasks))
	}
	if !strings.Contains(tasks[0].Failure, "helm upgrade failed") {
		t.Errorf("failure = %q, want the error's text", tasks[0].Failure)
	}

	// A successful task carries no failure text at all, so an operator
	// scanning for failures is not misled by an empty-but-present field.
	recorder.add(controlplane.ExecutionPhaseApply, controlplane.ExecutionTaskSucceeded,
		"installation", "example", frozen, nil)
	if recorder.execution.Tasks[1].Failure != "" {
		t.Errorf("a successful task carries failure text %q", recorder.execution.Tasks[1].Failure)
	}
}

// A nil clock means the recorder uses the real one rather than panicking, and
// the started timestamp is taken once at construction rather than per task.
func TestExecutionRecorderToleratesANilClock(t *testing.T) {
	t.Parallel()
	recorder := newExecutionRecorder(nil)
	if recorder.started().IsZero() {
		t.Error("a recorder built with no clock has no start time")
	}
	if recorder.now().Location() != time.UTC {
		t.Errorf("recorder time is in %v, want UTC", recorder.now().Location())
	}
}
