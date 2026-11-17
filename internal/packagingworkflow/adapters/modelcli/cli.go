// Package modelcli runs a local command behind the proposal port.
// It does not interpret a vendor protocol and it does not approve a change.
package modelcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	packagingskill "github.com/jonathanagustin/conduit/skills/packaging"
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
	Tools   domain.Toolbox
	// ModelName is the operator-configured model name, recorded in the
	// provenance. The command's reply is never a source for it.
	ModelName string
	// Exec wraps the run; nil runs the command directly.
	Exec localcmd.ExecFunc
}

func (runner Runner) Propose(ctx context.Context, input domain.PackagingContext) (domain.Proposal, domain.Provenance, error) {
	if len(runner.Argv) == 0 || strings.TrimSpace(runner.Argv[0]) == "" {
		return domain.Proposal{}, domain.Provenance{}, domain.ErrModelUnavailable
	}
	input = input.WithSkill(packagingskill.Source, packagingskill.SHA256())
	calls := runner.inspectFacts(ctx, &input)
	prompt := []byte(domain.Prompt(input))
	provenance := domain.Provenance{Adapter: localcmd.CommandName(runner.Argv[0]), ConfiguredModel: strings.TrimSpace(runner.ModelName), PromptSHA256: domain.DigestText(prompt), ToolCalls: calls}.WithSkillDigest(input)
	stdout, err := runner.Exec.Run(ctx, runner.Argv, runner.Timeout, prompt)
	provenance.ResponseSHA256 = domain.DigestText(stdout)
	if err != nil {
		return domain.Proposal{}, provenance, modelError(err)
	}
	proposal, err := domain.ParseProposal(stdout)
	if err != nil {
		return domain.Proposal{}, provenance, err
	}
	return proposal, provenance, nil
}

func modelError(err error) error {
	switch {
	case errors.Is(err, localcmd.ErrTimeout):
		return domain.ErrModelTimeout
	case errors.Is(err, localcmd.ErrNotFound), errors.Is(err, localcmd.ErrEmpty):
		return fmt.Errorf("%w: %v", domain.ErrModelUnavailable, err)
	default:
		return fmt.Errorf("%w: %w", domain.ErrModelFailed, err)
	}
}

// ParseArgv splits a command line on spaces. Double quotes group one
// argument. There is no shell expansion.
func ParseArgv(line string) ([]string, error) {
	return localcmd.ParseArgv(line)
}

func (runner Runner) inspectFacts(ctx context.Context, input *domain.PackagingContext) []domain.ToolCallRecord {
	return InspectFacts(ctx, runner.Tools, input)
}

// InspectFacts adds the inspected image facts to the prompt context. Both
// model transports use it, so the prompt does not depend on the transport.
func InspectFacts(ctx context.Context, tools domain.Toolbox, input *domain.PackagingContext) []domain.ToolCallRecord {
	if tools == nil {
		return nil
	}
	ref := strings.TrimSpace(input.ImageRef)
	if ref == "" {
		ref = strings.TrimSpace(input.ImageDigest)
	}
	if ref == "" {
		return nil
	}
	got, err := tools.InspectImage(ctx, ref)
	calls := []domain.ToolCallRecord{{Name: "inspect_image"}}
	var body strings.Builder
	if err != nil {
		fmt.Fprintf(&body, "inspect_image error: %v\n", err)
	} else {
		encoded, encodeErr := json.Marshal(got)
		if encodeErr != nil {
			fmt.Fprintf(&body, "inspect_image error: %v\n", encodeErr)
		} else {
			fmt.Fprintf(&body, "inspect_image: %s\n", encoded)
		}
	}
	if strings.TrimSpace(input.ToolFacts) == "" {
		input.ToolFacts = body.String()
	} else {
		input.ToolFacts = strings.TrimSpace(input.ToolFacts) + "\n" + body.String()
	}
	return calls
}

func (runner Runner) ProposePackage(ctx context.Context, input domain.PackagingContext) (domain.PackageProposal, domain.Provenance, error) {
	if len(runner.Argv) == 0 || strings.TrimSpace(runner.Argv[0]) == "" {
		return domain.PackageProposal{}, domain.Provenance{}, domain.ErrModelUnavailable
	}
	if strings.TrimSpace(input.Task) == "" {
		input.Task = domain.TaskPackage
	}
	input = input.WithSkill(packagingskill.Source, packagingskill.SHA256())
	calls := runner.inspectFacts(ctx, &input)
	prompt := []byte(domain.Prompt(input))
	provenance := domain.Provenance{Adapter: localcmd.CommandName(runner.Argv[0]), ConfiguredModel: strings.TrimSpace(runner.ModelName), PromptSHA256: domain.DigestText(prompt), ToolCalls: calls}.WithSkillDigest(input)
	stdout, err := runner.Exec.Run(ctx, runner.Argv, runner.Timeout, prompt)
	provenance.ResponseSHA256 = domain.DigestText(stdout)
	if err != nil {
		return domain.PackageProposal{}, provenance, modelError(err)
	}
	proposal, err := domain.ParsePackageProposal(stdout)
	if err != nil {
		return domain.PackageProposal{}, provenance, err
	}
	return proposal, provenance, nil
}
