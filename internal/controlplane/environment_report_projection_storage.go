package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
	environmentports "github.com/jonathanagustin/conduit/internal/environments/ports"
)

// Raw transport snapshots belong to the compatibility adapter, not the native
// Environment domain. They are committed with their normalized projection.
type persistedReportProjectionInputs = environmentports.ReportProjectionInputs

type reportProjectionOperation func(environmentReportReadRepository, persistedReportProjectionInputs) (persistedReportProjectionInputs, *EnvironmentReport, error)
type atomicReportProjectionStore interface {
	updateReportProjection(context.Context, string, reportProjectionOperation) error
}

// UpdateReportProjection supplies only native facts and opaque retained inputs.
// Compatibility decoding is performed by the adapter within this transaction.
func (port environmentReportingProjectionPort) UpdateReportProjection(ctx context.Context, environment string, project func(environmentports.ReportProjectionSnapshot) (environmentports.ReportProjectionInputs, *environments.Report, error)) error {
	if !dnsLabelPattern.MatchString(environment) {
		return ErrNotFound
	}
	store, ok := port.repository.(atomicReportProjectionStore)
	if !ok {
		return errors.New("repository does not support atomic report projections")
	}
	return store.updateReportProjection(ctx, environment, func(repository environmentReportReadRepository, raw persistedReportProjectionInputs) (persistedReportProjectionInputs, *EnvironmentReport, error) {
		snapshot := environmentports.ReportProjectionSnapshot{Environment: environment, Inputs: raw}
		var err error
		snapshot.Plan, err = latestEnvironmentPlanProjection(ctx, repository, environment)
		if err != nil {
			return raw, nil, err
		}
		snapshot.Report, err = repository.GetEnvironmentReport(ctx, environment)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return raw, nil, err
		}
		return project(snapshot)
	})
}

func cloneReportProjectionInputs(input map[string]persistedReportProjectionInputs) map[string]persistedReportProjectionInputs {
	result := make(map[string]persistedReportProjectionInputs, len(input))
	for key, value := range input {
		result[key] = persistedReportProjectionInputs{Info: append(json.RawMessage(nil), value.Info...), Health: append(json.RawMessage(nil), value.Health...)}
	}
	return result
}

func (repository *MemoryRepository) updateReportProjection(ctx context.Context, environment string, operation reportProjectionOperation) error {
	return repository.Transaction(ctx, func(view Repository) error {
		memory := view.(*MemoryRepository)
		raw, report, err := operation(memory, memory.reportProjectionInputs[environment])
		if err != nil {
			return err
		}
		if report != nil {
			if _, err := memory.PutEnvironmentReport(ctx, *report); err != nil {
				return err
			}
		}
		memory.reportProjectionInputs[environment] = raw
		return nil
	})
}

func (repository *PostgresRepository) updateReportProjection(ctx context.Context, environment string, operation reportProjectionOperation) error {
	// The metadata row lock serializes both raw inputs and projections, including
	// concurrent first reports where an environment's raw row does not yet exist.
	return repository.withStateTransaction(ctx, true, func(memory *MemoryRepository, transaction *sql.Tx) error {
		var encoded []byte
		var raw persistedReportProjectionInputs
		err := transaction.QueryRowContext(ctx, "SELECT inputs FROM conduit_apollo_agent_reports WHERE environment = $1", environment).Scan(&encoded)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if len(encoded) > 0 {
			if err := json.Unmarshal(encoded, &raw); err != nil {
				return err
			}
		}
		raw, report, err := operation(memory, raw)
		if err != nil {
			return err
		}
		if report != nil {
			if _, err := memory.PutEnvironmentReport(ctx, *report); err != nil {
				return err
			}
		}
		encoded, err = json.Marshal(raw)
		if err != nil {
			return err
		}
		_, err = transaction.ExecContext(ctx, `INSERT INTO conduit_apollo_agent_reports(environment, inputs) VALUES ($1, $2)
   ON CONFLICT (environment) DO UPDATE SET inputs = EXCLUDED.inputs`, environment, encoded)
		return err
	})
}

// A single file keeps raw input and projection atomic in single-process mode.
// Existing report files remain readable; native report writes preserve inputs.
type reportProjectionEnvelope struct {
	LiveOutbox nativeLiveOutbox `json:"_liveOutbox,omitempty"`
	EnvironmentReport
	ProjectionInputs  persistedReportProjectionInputs `json:"apolloAgentReports,omitempty"`
	ProjectionPending bool                            `json:"apolloProjectionPending,omitempty"`
}

func (repository *FileRepository) readReportProjectionEnvelope(environment string) (reportProjectionEnvelope, error) {
	if !dnsLabelPattern.MatchString(environment) {
		return reportProjectionEnvelope{}, ErrNotFound
	}
	var envelope reportProjectionEnvelope
	err := readJSON(repository.reportPath(environment), &envelope)
	return envelope, err
}

func (repository *FileRepository) updateReportProjection(ctx context.Context, environment string, operation reportProjectionOperation) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	envelope, err := repository.readReportProjectionEnvelope(environment)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if errors.Is(err, ErrNotFound) {
		envelope.Environment = environment
		envelope.ProjectionPending = true
	}
	memory := NewMemoryRepository()
	plans, err := repository.listPlansUnlocked(environment)
	if err != nil {
		return err
	}
	for _, plan := range plans {
		memory.plans[plan.ID] = plan
	}
	if !envelope.ProjectionPending {
		memory.reports[environment] = envelope.EnvironmentReport
	}
	var catalog ImportedCatalog
	if err := readJSON(repository.importedCatalogPath(environment), &catalog); err == nil {
		memory.importedCatalogs[environment] = catalog
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	raw, report, err := operation(memory, envelope.ProjectionInputs)
	if err != nil {
		return err
	}
	if report != nil {
		stored, err := memory.PutEnvironmentReport(ctx, *report)
		if err != nil {
			return err
		}
		envelope.EnvironmentReport = stored
		envelope.LiveOutbox = appendNativeInvalidation(envelope.LiveOutbox, "environment", environment, stored.Revision, "fleet", "overview", "kpi")
		envelope.ProjectionPending = false
	}
	envelope.ProjectionInputs = raw
	return atomicWriteJSON(repository.reportPath(environment), envelope)
}
