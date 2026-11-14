package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/jonathanagustin/conduit/internal/conduitcli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := execute(ctx, conduitcli.Runtime{}, os.Stderr); err != nil {
		stop()
		os.Exit(processStatus(err))
	}
}

func processStatus(err error) int {
	if err == nil {
		return 0
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		if code := coded.ExitCode(); code > 0 && code < 128 {
			return code
		}
	}
	return 1
}

// execute is separated from main so the one decision here -- whether the process
// failed, and what it says on the way out -- can be tested in-process. Testing it
// through the built binary works, but coverage instrumentation cannot see into a
// subprocess, so nothing measures it.
func execute(ctx context.Context, runtime conduitcli.Runtime, diagnostics io.Writer) error {
	if err := conduitcli.NewRoot(runtime).ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintln(diagnostics, "conduit:", err)
		return err
	}
	return nil
}
