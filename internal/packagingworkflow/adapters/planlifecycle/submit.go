// Package planlifecycle submits a packaging handoff through the ordinary
// Plan lifecycle. It proposes. It does not approve, lease, or execute.
package planlifecycle

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

const authorUsername = "packaging-workflow"

// Submitter proposes one governed Plan for a packaging handoff.
type Submitter struct {
	Repository      controlplane.Repository
	Governance      *controlplane.GovernanceService
	Journal         journal
	Author          controlplane.Principal
	FailAfterSubmit bool
	now             func() time.Time
}

type journal interface {
	Reconcile(context.Context, string) (domain.PlanReceipt, bool, error)
	Remember(context.Context, string, domain.PlanReceipt) error
}

// New binds the delivery kernel's repository. The author may propose. The
// submitter never calls Approve.
func New(repository controlplane.Repository, journal journal) *Submitter {
	return &Submitter{
		Repository: repository,
		Governance: controlplane.NewGovernanceService(repository),
		Journal:    journal,
		Author:     controlplane.Principal{Username: authorUsername, ServiceAccount: true},
		now:        time.Now,
	}
}

// Submit reconciles an existing Plan before it proposes. A repeated call
// returns that Plan and does not create another.
func (s *Submitter) Submit(ctx context.Context, request domain.HandoffRequest) (domain.PlanReceipt, error) {
	key := domain.HandoffKey(request.RunID, request.ProposalRevision, request.ArtifactDigest, request.TargetPlanRef)
	if existing, ok, err := s.Journal.Reconcile(ctx, key); err != nil {
		return domain.PlanReceipt{}, err
	} else if ok {
		existing.Reconciled = true
		return existing, nil
	}
	state, err := desiredState(request)
	if err != nil {
		return domain.PlanReceipt{}, err
	}
	if err := s.suppressed(ctx, state.Environment.ID); err != nil {
		return domain.PlanReceipt{}, err
	}
	record, _, err := s.Governance.ProposePlan(ctx, state, s.Author,
		"Packaging handoff "+request.ProposalRevision,
		"Proposed by the packaging workflow for artifact "+request.ArtifactDigest+". This proposal does not approve or execute the Plan.")
	if err != nil {
		return domain.PlanReceipt{}, err
	}
	if record.Status != controlplane.PlanAwaitingApproval || record.Lease != nil || len(record.Approvals) != 0 {
		return domain.PlanReceipt{}, fmt.Errorf("plan %s left proposal in status %s", record.ID, record.Status)
	}
	receipt := domain.PlanReceipt{PlanID: record.ID, Key: key}
	if err := s.Journal.Remember(ctx, key, receipt); err != nil {
		return receipt, err
	}
	if s.FailAfterSubmit {
		s.FailAfterSubmit = false
		return receipt, errors.New("worker lost the handoff acknowledgement")
	}
	return receipt, nil
}

func (s *Submitter) suppressed(ctx context.Context, environment string) error {
	records, err := s.Repository.ListSuppressions(ctx, environment)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	for _, record := range records {
		if record.Environment != environment || record.RemovedAt != nil {
			continue
		}
		if !record.StartsAt.IsZero() && record.StartsAt.After(now) {
			continue
		}
		if record.EndsAt != nil && !record.EndsAt.After(now) {
			continue
		}
		return fmt.Errorf("%w: environment %s is suppressed", adapters.ErrDenied, environment)
	}
	return nil
}

func desiredState(request domain.HandoffRequest) (controlplane.DesiredState, error) {
	if !strings.HasPrefix(request.ArtifactDigest, "sha256:") || len(request.ArtifactDigest) != len("sha256:")+64 {
		return controlplane.DesiredState{}, fmt.Errorf("%w: artifact digest is not a sha256 digest", domain.ErrIdentity)
	}
	environment, entity := splitTarget(request.TargetPlanRef)
	if request.EnvironmentID != "" {
		environment = request.EnvironmentID
	}
	if request.EntityName != "" {
		entity = request.EntityName
	}
	generation := request.Generation
	if generation == "" {
		generation = strings.TrimPrefix(request.ArtifactDigest, "sha256:")[:16]
	}
	product := request.Product
	if product == "" {
		product = "app.example:sample-app"
	}
	version := request.Version
	if version == "" {
		version = "0.1.0"
	}
	namespace := request.Namespace
	if namespace == "" {
		namespace = "example"
	}
	chart := request.ChartRepository
	if chart == "" {
		chart = "registry.example/sample-app"
	}
	image := request.ImageRepository
	if image == "" {
		image = chart
	}
	return controlplane.DesiredState{
		APIVersion: "platform.delivery/v1alpha1",
		Kind:       "EnvironmentDeployment",
		Environment: controlplane.Environment{
			ID:         environment,
			Generation: generation,
		},
		Releases: []controlplane.Release{{
			Product: product,
			Version: version,
			Chart:   controlplane.Artifact{Repository: chart, Tag: version, Digest: request.ArtifactDigest},
			Images:  []controlplane.Artifact{{Repository: image, Tag: version, Digest: request.ArtifactDigest}},
		}},
		Entities: []controlplane.Entity{{
			Name: entity, Product: product, Version: version, Namespace: namespace,
		}},
	}, nil
}

func splitTarget(target string) (string, string) {
	environment, entity, ok := strings.Cut(target, "/")
	if !ok || environment == "" || entity == "" {
		return "dev-use2", "sample-app"
	}
	return environment, entity
}
