// Package localcmd runs one local command with argv, stdin, and a timeout.
// It does not interpret a reply and it does not approve a change.
package localcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

var (
	ErrTimeout  = errors.New("command timed out")
	ErrNotFound = errors.New("command was not found")
	ErrFailed   = errors.New("command failed")
	ErrEmpty    = errors.New("command is empty")
	// ErrStartFailed marks an ErrFailed where the process never ran: the
	// start itself failed for a reason other than a missing command. Nothing
	// read the prompt, so running it again repeats nothing.
	ErrStartFailed = errors.New("command could not be started")
)

// ExecFunc is the shape of Execute. An adapter holds one so that a caller can
// put a wrapper around every run; nil means Execute.
type ExecFunc func(ctx context.Context, argv []string, timeout time.Duration, stdin []byte) ([]byte, error)

// Run calls the wrapper, or Execute when there is none.
func (run ExecFunc) Run(ctx context.Context, argv []string, timeout time.Duration, stdin []byte) ([]byte, error) {
	if run == nil {
		return Execute(ctx, argv, timeout, stdin)
	}
	return run(ctx, argv, timeout, stdin)
}

// Execute starts argv in a temporary directory and writes stdin to the process.
// stdout is returned even when the process fails, so the caller can digest it.
func Execute(ctx context.Context, argv []string, timeout time.Duration, stdin []byte) ([]byte, error) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return nil, ErrEmpty
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	dir, err := os.MkdirTemp("", "conduit-local-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	command.Dir = dir
	// The child gets an explicit allowlist, never the caller's environment,
	// so a model CLI cannot read the Hub's own secrets.
	command.Env = childEnv()
	command.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return stdout.Bytes(), ErrTimeout
	}
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return stdout.Bytes(), fmt.Errorf("%w: %s", ErrNotFound, CommandName(argv[0]))
		}
		var exited *exec.ExitError
		if !errors.As(err, &exited) {
			return stdout.Bytes(), fmt.Errorf("%w: %w: %v", ErrFailed, ErrStartFailed, err)
		}
		return stdout.Bytes(), fmt.Errorf("%w: %v", ErrFailed, err)
	}
	return stdout.Bytes(), nil
}

// CommandName is the last path segment of a command.
func CommandName(path string) string {
	name := path
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		name = path[i+1:]
	}
	return name
}

// ParseArgv splits a command line on spaces. Double quotes group one
// argument. There is no shell expansion.
func ParseArgv(line string) ([]string, error) {
	var argv []string
	var current strings.Builder
	quoted := false
	for _, r := range strings.TrimSpace(line) {
		switch {
		case r == '"':
			quoted = !quoted
		case r == '\\' && !quoted:
			return nil, fmt.Errorf("command line does not allow backslashes")
		case unicode.IsSpace(r) && !quoted:
			if current.Len() > 0 {
				argv = append(argv, current.String())
				current.Reset()
			}
		default:
			if r == '$' || r == '`' || r == ';' || r == '|' || r == '&' {
				return nil, fmt.Errorf("command line does not allow shell syntax")
			}
			current.WriteRune(r)
		}
	}
	if quoted {
		return nil, fmt.Errorf("command line has an unclosed quote")
	}
	if current.Len() > 0 {
		argv = append(argv, current.String())
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("command line is empty")
	}
	return argv, nil
}
