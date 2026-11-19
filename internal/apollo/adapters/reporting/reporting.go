// Package reporting translates compatibility input snapshots over an atomic
// native Environment projection boundary. It owns transport decoding and patches.
package reporting

import (
	"context"
	"encoding/json"
	"fmt"

	app "github.com/jonathanagustin/conduit/internal/apollo/application"
	domain "github.com/jonathanagustin/conduit/internal/environments/domain"
	ports "github.com/jonathanagustin/conduit/internal/environments/ports"
)

// Adapter retains the native read/write seam and adds compatibility translation.
type Adapter struct{ ports.AtomicReportingPort }

// New wraps an explicitly supplied atomic native projection port.
func New(port ports.AtomicReportingPort) Adapter { return Adapter{port} }

var _ app.ReportingPort = Adapter{}

// UpdateAgentReports replaces only non-nil snapshots, including explicit empties.
// Projection receives the complete transaction-local inputs and native facts.
func (adapter Adapter) UpdateAgentReports(ctx context.Context, environment string, patch app.AgentReportPatch, project func(app.AgentReportSnapshot) (*domain.Report, error)) error {
	return adapter.UpdateReportProjection(ctx, environment, func(native ports.ReportProjectionSnapshot) (ports.ReportProjectionInputs, *domain.Report, error) {
		raw := native.Inputs
		var err error
		if patch.Info != nil {
			raw.Info, err = json.Marshal(*patch.Info)
			if err != nil {
				return raw, nil, err
			}
		}
		if patch.Health != nil {
			raw.Health, err = json.Marshal(*patch.Health)
			if err != nil {
				return raw, nil, err
			}
		}
		snapshot := app.AgentReportSnapshot{Environment: native.Environment, Plan: native.Plan, Report: native.Report}
		if len(raw.Info) > 0 {
			if err := json.Unmarshal(raw.Info, &snapshot.Inputs.Info); err != nil {
				return raw, nil, fmt.Errorf("decode Apollo info: %w", err)
			}
		}
		if len(raw.Health) > 0 {
			if err := json.Unmarshal(raw.Health, &snapshot.Inputs.Health); err != nil {
				return raw, nil, fmt.Errorf("decode Apollo health: %w", err)
			}
		}
		report, err := project(snapshot)
		return raw, report, err
	})
}
