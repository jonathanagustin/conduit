// Package modelcli runs a local command behind the proposal port.
// It does not interpret a vendor protocol and it does not approve a change.
package modelcli

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

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// DefaultArgv is the documented local command. It is not started unless
// CONDUIT_MODEL_CMD is set. The last argument reads the prompt from stdin.
func DefaultArgv() []string {
	return []string{"codex", "exec", "--skip-git-repo-check", "--sandbox", "read-only", "--ephemeral", "-"}
}

// Runner starts one local command. The prompt is written to stdin. The
// process working directory is temporary and is removed afterwards.
type Runner struct {
	Argv    []string
	Timeout time.Duration
}

func (runner Runner) Propose(ctx context.Context, input domain.PackagingContext) (domain.Proposal, domain.Provenance, error) {
	if len(runner.Argv) == 0 || strings.TrimSpace(runner.Argv[0]) == "" {
		return domain.Proposal{}, domain.Provenance{}, domain.ErrModelUnavailable
	}
	prompt := []byte(domain.Prompt(input))
	provenance := domain.Provenance{Adapter: commandName(runner.Argv[0]), PromptSHA256: domain.DigestText(prompt)}
	dir, err := os.MkdirTemp("", "conduit-model-")
	if err != nil {
		return domain.Proposal{}, provenance, err
	}
	defer os.RemoveAll(dir)
	timeout := runner.Timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(runCtx, runner.Argv[0], runner.Argv[1:]...)
	command.Dir = dir
	command.Stdin = bytes.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	provenance.ResponseSHA256 = domain.DigestText(stdout.Bytes())
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return domain.Proposal{}, provenance, domain.ErrModelTimeout
	}
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return domain.Proposal{}, provenance, fmt.Errorf("%w: %s", domain.ErrModelUnavailable, commandName(runner.Argv[0]))
		}
		return domain.Proposal{}, provenance, fmt.Errorf("%w: %v", domain.ErrModelFailed, err)
	}
	proposal, err := domain.ParseProposal(stdout.Bytes())
	if err != nil {
		return domain.Proposal{}, provenance, err
	}
	return proposal, provenance, nil
}

func commandName(path string) string {
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
