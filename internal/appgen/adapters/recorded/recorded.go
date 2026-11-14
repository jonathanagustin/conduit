// Package recorded serves the checked-in HTTP application. It does not start a process.
package recorded

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/appgen/domain"
)

//go:embed testdata/hello/spec.json
var specJSON []byte

// Adapter is the fallback when no local command is configured.
type Adapter struct{}

func (Adapter) Generate(ctx context.Context, request domain.Request) (domain.Spec, domain.Provenance, error) {
	if ctx.Err() != nil {
		return domain.Spec{}, domain.Provenance{}, ctx.Err()
	}
	prompt := []byte(domain.Prompt(request))
	provenance := domain.Provenance{
		Adapter: "recorded", PromptSHA256: domain.Digest(prompt), ResponseSHA256: domain.Digest(specJSON),
	}
	spec, err := domain.ParseSpec(specJSON)
	if err != nil {
		return domain.Spec{}, provenance, fmt.Errorf("%w: recorded spec %v", domain.ErrSpecInvalid, err)
	}
	return spec, provenance, nil
}
