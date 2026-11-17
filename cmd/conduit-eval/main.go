// Command conduit-eval scores model output for the planning and packaging
// prompts against the committed corpus. See docs/operations/model-evals.md.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/jonathanagustin/conduit/internal/modeleval"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	status := modeleval.Main(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	os.Exit(status)
}
