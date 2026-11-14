package packagingtools

import (
	"context"
	"strings"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/fixtures"
)

func (service *Service) DryRunGates(_ context.Context, input domain.GateInput) (domain.Validation, error) {
	source := []byte(input.Source)
	digest := strings.TrimSpace(input.SourceDigest)
	if digest == "" {
		digest = domain.DigestBytes(source)
	}
	template := strings.TrimSpace(input.TemplateVersion)
	if template == "" {
		template = fixtures.TemplateVersion
	}
	tools := strings.TrimSpace(input.ToolVersions)
	if tools == "" {
		tools = fixtures.ToolVersions
	}
	result := fixtures.Gate(source, []byte(input.Zarf), []byte(input.CR), digest, template, tools)
	if result.Outcome != domain.OutcomePass {
		return result, nil
	}
	if reason := firstDockerfileReason(input.Dockerfile, input.Dockerignore); reason != "" {
		result.Outcome = domain.OutcomeFailRequired
		result.Reason = reason
	}
	return result, nil
}
