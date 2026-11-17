// Command packaging-mcp serves the packaging tools over MCP on standard input
// and output.
//
//	packaging-mcp [--repo DIR | DIR]
//
// With a repository root, inspect_repo and read_file read that directory and
// nothing outside it. Without one they are not offered at all.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/packagingmcp"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/packagingtools"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stderr, &mcp.StdioTransport{}))
}

// run parses the arguments and serves until the client disconnects. Standard
// output belongs to the protocol, so every diagnostic goes to stderr.
func run(ctx context.Context, args []string, stderr io.Writer, transport mcp.Transport) int {
	root, err := repositoryRoot(args, stderr)
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stderr, "packaging-mcp:", err)
		}
		return 2
	}
	var options []packagingtools.Option
	if root == "" {
		fmt.Fprintf(stderr, "packaging-mcp: no repository root given; %s are not offered\n", strings.Join(packagingmcp.RepositoryTools(), " and "))
	} else {
		options = append(options, packagingtools.WithRepoRoot(root))
		fmt.Fprintf(stderr, "packaging-mcp: repository root %s\n", root)
	}
	if err := packagingmcp.NewServer(packagingtools.New(options...)).Run(ctx, transport); err != nil {
		fmt.Fprintln(stderr, "packaging-mcp:", err)
		return 1
	}
	return 0
}

// repositoryRoot returns the absolute repository root from --repo or the one
// positional argument, or "" when neither was given. A root that is not an
// existing directory is an error: the server does not start half-bound.
func repositoryRoot(args []string, stderr io.Writer) (string, error) {
	flags := flag.NewFlagSet("packaging-mcp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", "", "source repository directory that inspect_repo and read_file may read")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: packaging-mcp [--repo DIR | DIR]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	root := strings.TrimSpace(*repo)
	switch {
	case flags.NArg() > 1:
		return "", errors.New("at most one repository directory may be given")
	case flags.NArg() == 1 && root != "":
		return "", errors.New("give the repository directory once, as --repo or as the argument")
	case flags.NArg() == 1:
		root = strings.TrimSpace(flags.Arg(0))
		if root == "" {
			return "", errors.New("the repository directory is empty")
		}
	}
	if root == "" {
		return "", nil
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("repository root: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("repository root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository root %s is not a directory", absolute)
	}
	return absolute, nil
}
