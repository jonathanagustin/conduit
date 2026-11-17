// platform-material-preparer is the reference EXTERNAL_MATERIAL_PREPARER: the
// executable the Spoke Agent runs to prepare registry access and apply or roll
// back Secret mutations. It reads one request on standard input and answers
// with its exit status, which is all the Agent reads.
//
// See docs/operations/material-preparer.md for the contract, the exit
// statuses and the RBAC it needs.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jonathanagustin/conduit/internal/materialpreparer"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stderr, os.Getenv))
}

// run is main without the process. Nothing is ever written to standard
// output: the contract has no response body, and a preparer that printed one
// would be one edit away from printing what it had just read.
func run(ctx context.Context, arguments []string, input io.Reader, diagnostics io.Writer, getenv func(string) string) int {
	if len(arguments) != 0 {
		// The Agent passes none. Refusing them keeps a request -- or a
		// credential -- from ever being put on a command line, where every
		// process on the node can read it.
		fmt.Fprintln(diagnostics, "platform-material-preparer takes no arguments; the request is read from standard input")
		return materialpreparer.ExitInvalidRequest
	}
	directory := strings.TrimSpace(getenv("MATERIAL_PREPARER_SERVICEACCOUNT_DIR"))
	if directory == "" {
		directory = materialpreparer.DefaultServiceAccountDirectory
	}
	namespaces, err := materialpreparer.ParseNamespaces(getenv("MATERIAL_PREPARER_NAMESPACES"))
	if err != nil {
		fmt.Fprintln(diagnostics, "MATERIAL_PREPARER_NAMESPACES:", err)
		return materialpreparer.ExitMisconfigured
	}
	source := strings.TrimSpace(getenv("MATERIAL_PREPARER_SOURCE_NAMESPACE"))
	if source == "" {
		if source, err = materialpreparer.PodNamespace(directory); err != nil {
			fmt.Fprintln(diagnostics, err)
			return materialpreparer.ExitMisconfigured
		}
	}
	secrets, err := materialpreparer.NewInClusterSecrets(getenv, directory)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return materialpreparer.ExitMisconfigured
	}
	err = materialpreparer.Run(ctx, input, materialpreparer.Config{SourceNamespace: source, ManagedNamespaces: namespaces}, secrets)
	if err == nil {
		return materialpreparer.ExitOK
	}
	var failure *materialpreparer.Failure
	if errors.As(err, &failure) {
		fmt.Fprintln(diagnostics, failure.Reason)
		return failure.Code
	}
	fmt.Fprintln(diagnostics, "the request failed")
	return materialpreparer.ExitAPIFailure
}
