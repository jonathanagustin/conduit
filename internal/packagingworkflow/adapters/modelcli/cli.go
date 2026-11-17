// Package modelcli retains the retired command adapter as an explicit refusal.
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
)

// Runner is a compatibility shape; model methods refuse without calling tools or processes.
type Runner struct {
	Argv    []string
	Timeout time.Duration
	Tools   domain.Toolbox
	// ModelName is the operator-configured model name, recorded in the
	// provenance. The command's reply is never a source for it.
	ModelName string
}

func (runner Runner) Propose(ctx context.Context, input domain.PackagingContext) (domain.Proposal, domain.Provenance, error) {
	return domain.Proposal{}, domain.Provenance{}, modelError(localcmd.ErrLocalCommandUnsupported)
}

func modelError(err error) error {
	switch {
	case errors.Is(err, localcmd.ErrLocalCommandUnsupported):
		return fmt.Errorf("%w: %w", domain.ErrModelUnavailable, err)
	case errors.Is(err, localcmd.ErrTimeout):
		return domain.ErrModelTimeout
	case errors.Is(err, localcmd.ErrNotFound), errors.Is(err, localcmd.ErrEmpty):
		return fmt.Errorf("%w: %v", domain.ErrModelUnavailable, err)
	default:
		return fmt.Errorf("%w: %v", domain.ErrModelFailed, err)
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
	return domain.PackageProposal{}, domain.Provenance{}, modelError(localcmd.ErrLocalCommandUnsupported)
}
