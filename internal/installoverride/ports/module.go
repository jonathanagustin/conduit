package ports

import (
	"context"

	"github.com/jonathanagustin/conduit/internal/installoverride/domain"
)

// ModuleDraft is metadata admission intent, not a workload execution request.
type ModuleDraft struct {
	Action         string            `json:"action"`
	Environment    string            `json:"environment"`
	InstallationID string            `json:"installationId,omitempty"`
	Module         string            `json:"module"`
	Version        string            `json:"version,omitempty"`
	ReleaseChannel string            `json:"releaseChannel,omitempty"`
	Variables      map[string]string `json:"variables,omitempty"`
	Flags          domain.Flags      `json:"flags"`
	Rationale      string            `json:"rationale"`
}

type ModuleReview struct {
	ReviewID                 string          `json:"changeRequestId,omitempty"`
	Status                   string          `json:"status"`
	Proposal                 domain.Proposal `json:"proposal"`
	DryRun                   bool            `json:"dryRun"`
	SecretRequirementsEffect string          `json:"secretRequirementsEffect"`
	ValueTier                string          `json:"valueTier"`
}

type ModuleAdmission struct {
	ReviewID                 string `json:"changeRequestId"`
	ProposalID               string `json:"proposalId"`
	InstallationID           string `json:"installationId"`
	Module                   string `json:"module"`
	Version                  string `json:"version"`
	Revision                 uint64 `json:"revision,omitempty"`
	Status                   string `json:"status"`
	SecretRequirementsEffect string `json:"secretRequirementsEffect"`
	ValueTier                string `json:"valueTier"`
}

type ModuleController interface {
	ProposeModuleOverride(context.Context, ModuleDraft, bool) (ModuleReview, error)
	ApplyModuleOverride(context.Context, string) (ModuleAdmission, error)
}
