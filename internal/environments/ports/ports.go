// Package ports declares what Environments needs from the rest of the system,
// and nothing more.
//
// Both ports here are outbound persistence boundaries rather than questions the
// rules ask. The rules — is this profile well formed, what health does this
// Entity resolve to, may this Hub issue Plans here — need nothing but their
// arguments, which is the point. What needs a store is remembering the answers.
package ports

import (
	"context"
	"encoding/json"

	"github.com/jonathanagustin/conduit/internal/environments/domain"
)

// ReportingPort is the Environment-owned boundary a compatibility context reads
// and writes observed state through. Implementations decide how plans and
// reports are stored; a caller cannot reach the control-plane repository
// directly, which is what stops a foreign API from acquiring one.
type ReportingPort interface {
	LatestPlan(ctx context.Context, environment string) (domain.PlanProjection, error)
	GetReport(ctx context.Context, environment string) (domain.Report, error)
	PutReport(ctx context.Context, report domain.Report) (domain.Report, error)
}

// ProfileRepository stores the profiles an operator authors, revision by
// revision. Revisions are kept rather than overwritten because a profile is
// evidence of what was declared when, and a change to it goes through an
// approval that has to be able to name what it changed from.
type ProfileRepository interface {
	PutEnvironmentProfile(ctx context.Context, profile domain.Profile) (domain.Profile, error)
	GetEnvironmentProfile(ctx context.Context, environment string) (domain.Profile, error)
	ListEnvironmentProfileRevisions(ctx context.Context, environment string) ([]domain.Profile, error)
}

// ReportProjectionInputs holds opaque independently replaced input snapshots.
// Decoding and transport-specific patch semantics belong to the edge adapter.
// JSON names preserve the existing persisted representation.
type ReportProjectionInputs struct {
	Info   json.RawMessage `json:"info,omitempty"`
	Health json.RawMessage `json:"health,omitempty"`
}

// ReportProjectionSnapshot contains facts captured inside one storage transaction.
type ReportProjectionSnapshot struct {
	Environment string
	Inputs      ReportProjectionInputs
	Plan        domain.PlanProjection
	Report      domain.Report
}

// AtomicReportingPort commits raw inputs and their normalized native projection
// together. Unsupported stores must refuse rather than fall back to split writes.
type AtomicReportingPort interface {
	ReportingPort
	UpdateReportProjection(context.Context, string, func(ReportProjectionSnapshot) (ReportProjectionInputs, *domain.Report, error)) error
}
