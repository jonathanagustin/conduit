package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/conduitcli"
)

// main() is pure wiring, and the only thing it decides is what the process
// leaves behind: a non-zero status and a prefixed line on stderr when the
// command fails, and neither when it does not. Scripts and CI read exactly that,
// so it is worth an assertion even though there is no logic here to unit test.
//
// This builds and runs the real binary rather than calling main() in-process,
// because the exit status is the behaviour and os.Exit cannot be observed from
// inside the test binary.
func TestConduitReportsFailureThroughItsExitStatus(t *testing.T) {
	t.Parallel()
	binary := filepath.Join(t.TempDir(), "conduit")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building conduit: %v\n%s", err, output)
	}

	t.Run("a failing command exits non-zero and says which program failed", func(t *testing.T) {
		t.Parallel()
		command := exec.Command(binary, "no-such-command")
		output, err := command.CombinedOutput()
		var exitError *exec.ExitError
		if err == nil {
			t.Fatalf("an unknown command exited zero: %s", output)
		} else if !asExitError(err, &exitError) || exitError.ExitCode() != 1 {
			t.Fatalf("an unknown command exited %v: %s", err, output)
		}
		if !strings.Contains(string(output), "conduit:") {
			t.Errorf("the failure was not attributed to conduit: %s", output)
		}
	})

	t.Run("a successful command exits zero and attributes nothing", func(t *testing.T) {
		t.Parallel()
		output, err := exec.Command(binary, "--help").CombinedOutput()
		if err != nil {
			t.Fatalf("--help exited %v: %s", err, output)
		}
		if strings.Contains(string(output), "conduit:") {
			t.Errorf("a successful command still reported a failure: %s", output)
		}
	})
}

func asExitError(err error, target **exec.ExitError) bool {
	exitError, ok := err.(*exec.ExitError)
	if ok {
		*target = exitError
	}
	return ok
}

// The same contract, in-process, so it is measurable: what execute returns is
// what main turns into an exit status, and what it writes is the only thing an
// operator sees when a command fails.
func TestExecuteAttributesFailuresAndStaysQuietOtherwise(t *testing.T) {
	// Not parallel: execute builds its own root command, which reads os.Args the
	// way it will in production. Handing the arguments in instead would test a
	// path main never takes.
	saved := os.Args
	t.Cleanup(func() { os.Args = saved })
	runtime := conduitcli.Runtime{Stdout: io.Discard, Stderr: io.Discard}
	var diagnostics bytes.Buffer

	os.Args = []string{"conduit", "no-such-command"}
	if err := execute(context.Background(), runtime, &diagnostics); err == nil {
		t.Fatal("an unknown command returned no error")
	}
	if !strings.HasPrefix(diagnostics.String(), "conduit:") {
		t.Errorf("the failure was written as %q", diagnostics.String())
	}

	diagnostics.Reset()
	os.Args = []string{"conduit", "--help"}
	if err := execute(context.Background(), runtime, &diagnostics); err != nil {
		t.Fatalf("--help returned %v", err)
	}
	if diagnostics.Len() != 0 {
		t.Errorf("a successful command wrote %q", diagnostics.String())
	}
}

func TestProcessStatusKeepsTheDefaultAndHonorsAnExplicitCode(t *testing.T) {
	t.Parallel()
	if processStatus(nil) != 0 {
		t.Fatal("success was not status 0")
	}
	if processStatus(errors.New("usage")) != 1 {
		t.Fatal("an ordinary failure was not status 1")
	}
	rejected := conduitcli.StatusError{Status: 2, Err: errors.New("rejected")}
	if processStatus(rejected) != 2 {
		t.Fatal("an explicit status was dropped")
	}
	if processStatus(fmt.Errorf("command: %w", rejected)) != 2 {
		t.Fatal("a wrapped status was dropped")
	}
}
