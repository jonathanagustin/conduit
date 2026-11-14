// Package application scores a proposal with the packaging memory ceiling
// and records a change request that still needs a person.
package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// Result is one live proposal after the deterministic memory gate.
type Result struct {
	RunID           string            `json:"runId"`
	Label           string            `json:"label"`
	Outcome         string            `json:"outcome,omitempty"`
	Reason          string            `json:"reason,omitempty"`
	Proposal        domain.Proposal   `json:"proposal"`
	Provenance      domain.Provenance `json:"provenance"`
	ChangeRequestID string            `json:"changeRequestId,omitempty"`
	// AttachmentSHA256 lists the bound intake files, as lowercase sha256 hex.
	AttachmentSHA256 []string `json:"attachmentSha256,omitempty"`
	Error            string   `json:"error,omitempty"`
}

// Attachments loads stored intake files by digest as bound, read-only context.
type Attachments interface {
	Resolve(ctx context.Context, digests []string) ([]domain.Attachment, error)
}

// PendingChange is a proposal that passed the gate and still needs a person.
type PendingChange struct {
	Environment    string
	InstallationID string
	ProductID      string
	TargetID       string
	Title          string
	Description    string
	Author         string
	Value          string
}

// Opener stores a change request and does not approve it.
type Opener interface {
	OpenPending(ctx context.Context, change PendingChange) (string, error)
}

// Auditor records the command or model id, the exchange digests, and the gate result.
type Auditor interface {
	Record(ctx context.Context, runID string, provenance domain.Provenance, outcome, reason string) error
}

// Facts loads the packaging run's recorded chart, memory, and image lines.
type Facts interface {
	Load(ctx context.Context, runID string) (domain.PackagingContext, error)
}

// Service asks the port for a proposal and then applies the packaging gate.
type Service struct {
	Proposer domain.ChangeProposer
	Facts    Facts
	Open     Opener
	Audit    Auditor
	// Attachments binds intake files named by digest. Nil refuses a request that names any.
	Attachments Attachments
	Now         func() time.Time
}

func (service Service) Propose(ctx context.Context, input domain.PackagingContext, environment, author string) (Result, error) {
	result := Result{RunID: input.RunID, Label: "Live model call"}
	if service.Proposer == nil {
		result.Error = domain.ErrModelUnavailable.Error()
		return result, nil
	}
	input = service.withFacts(ctx, input)
	if len(input.AttachmentSHA256) > 0 {
		if service.Attachments == nil {
			result.Error = "attachments are not configured"
			return result, nil
		}
		bound, err := service.Attachments.Resolve(ctx, input.AttachmentSHA256)
		if err != nil {
			result.Error = err.Error()
			return result, nil
		}
		input.Attachments = bound
		input.AttachmentSHA256 = input.AttachmentSHA256[:0:0]
		for _, item := range bound {
			input.AttachmentSHA256 = append(input.AttachmentSHA256, item.SHA256)
		}
		result.AttachmentSHA256 = input.AttachmentSHA256
	}
	proposal, provenance, err := service.Proposer.Propose(ctx, input)
	result.Provenance = provenance
	if err != nil {
		result.Error = err.Error()
		_ = service.audit(ctx, input.RunID, provenance, "error", result.Error)
		return result, nil
	}
	result.Proposal = proposal
	outcome, reason, blocked := domain.AssessMemory(proposal.Value)
	result.Outcome = string(outcome)
	result.Reason = reason
	if err := service.audit(ctx, input.RunID, provenance, result.Outcome, reason); err != nil {
		return Result{}, err
	}
	if blocked {
		return result, nil
	}
	if service.Open == nil {
		return Result{}, errors.New("change request opener is not configured")
	}
	id, err := service.Open.OpenPending(ctx, PendingChange{
		Environment:    environment,
		InstallationID: input.RunID,
		TargetID:       input.RunID + ":" + proposal.Field,
		Title:          "Proposed memory limit " + proposal.Value,
		Description:    proposal.Rationale,
		Author:         author,
		Value:          proposal.Value,
	})
	if err != nil {
		return Result{}, err
	}
	result.ChangeRequestID = id
	return result, nil
}

func (service Service) withFacts(ctx context.Context, input domain.PackagingContext) domain.PackagingContext {
	if service.Facts == nil || strings.TrimSpace(input.RunID) == "" {
		return input
	}
	loaded, err := service.Facts.Load(ctx, input.RunID)
	if err != nil {
		return input
	}
	fill := func(current *string, recorded string) {
		if strings.TrimSpace(*current) == "" {
			*current = strings.TrimSpace(recorded)
		}
	}
	fill(&input.ChartValues, loaded.ChartValues)
	fill(&input.Resources, loaded.Resources)
	fill(&input.MemoryRequest, loaded.MemoryRequest)
	fill(&input.MemoryLimit, loaded.MemoryLimit)
	fill(&input.ImageRef, loaded.ImageRef)
	fill(&input.ImageDigest, loaded.ImageDigest)
	fill(&input.GateRules, loaded.GateRules)
	if len(input.AttachmentSHA256) == 0 {
		input.AttachmentSHA256 = loaded.AttachmentSHA256
	}
	return input
}

func (service Service) audit(ctx context.Context, runID string, provenance domain.Provenance, outcome, reason string) error {
	if service.Audit == nil || strings.TrimSpace(provenance.Adapter) == "" && provenance.PromptSHA256 == "" {
		return nil
	}
	return service.Audit.Record(ctx, runID, provenance, outcome, reason)
}
