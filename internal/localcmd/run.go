// Package localcmd retains argv utilities and the retired model-command API.
// Execute is an explicit refusal and never starts a host process.
package localcmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

var (
	ErrStartFailed             = errors.New("command could not be started")
	ErrLocalCommandUnsupported = errors.New("local model commands are unsupported; configure CONDUIT_MODEL_BASE_URL for HTTP or leave model configuration unset for offline operation")
	ErrTimeout                 = errors.New("command timed out")
	ErrNotFound                = errors.New("command was not found")
	ErrFailed                  = errors.New("command failed")
	ErrEmpty                   = errors.New("command is empty")
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

// Execute refuses the retired model subprocess transport. It never starts a
// process, reads credentials or creates a working directory.
func Execute(ctx context.Context, argv []string, timeout time.Duration, stdin []byte) ([]byte, error) {
	return nil, ErrLocalCommandUnsupported
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
