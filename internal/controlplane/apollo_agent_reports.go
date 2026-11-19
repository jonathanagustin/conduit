package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	apolloapp "github.com/jonathanagustin/conduit/internal/apollo/application"
	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
)

// Raw transport snapshots belong to the compatibility adapter, not the native
// Environment domain. They are committed with their normalized projection.
type persistedApolloAgentReports struct {
	Info   json.RawMessage `json:"info,omitempty"`
	Health json.RawMessage `json:"health,omitempty"`
}

type apolloAgentReportOperation func(Repository, persistedApolloAgentReports) (persistedApolloAgentReports, *EnvironmentReport, error)
type apolloAgentReportStore interface {
	updateApolloAgentReports(context.Context, string, apolloAgentReportOperation) error
}

func (port environmentReportingProjectionPort) UpdateAgentReports(ctx context.Context, environment string, patch apolloapp.AgentReportPatch, project func(apolloapp.AgentReportSnapshot) (*environments.Report, error)) error {
	if !dnsLabelPattern.MatchString(environment) {
		return ErrNotFound
	}
	store, ok := port.repository.(apolloAgentReportStore)
	if !ok {
		return errors.New("repository does not support atomic Apollo agent reports")
	}
	return store.updateApolloAgentReports(ctx, environment, func(repository Repository, raw persistedApolloAgentReports) (persistedApolloAgentReports, *EnvironmentReport, error) {
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
		snapshot := apolloapp.AgentReportSnapshot{Environment: environment}
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
		scoped := environmentReportingProjectionPort{repository: repository}
		snapshot.Plan, err = scoped.LatestPlan(ctx, environment)
		if err != nil {
			return raw, nil, err
		}
		snapshot.Report, err = scoped.GetReport(ctx, environment)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return raw, nil, err
		}
		report, err := project(snapshot)
		return raw, report, err
	})
}

func cloneApolloAgentReports(input map[string]persistedApolloAgentReports) map[string]persistedApolloAgentReports {
	result := make(map[string]persistedApolloAgentReports, len(input))
	for key, value := range input {
		result[key] = persistedApolloAgentReports{Info: append(json.RawMessage(nil), value.Info...), Health: append(json.RawMessage(nil), value.Health...)}
	}
	return result
}

func (repository *MemoryRepository) updateApolloAgentReports(ctx context.Context, environment string, operation apolloAgentReportOperation) error {
	return repository.Transaction(ctx, func(view Repository) error {
		memory := view.(*MemoryRepository)
		raw, report, err := operation(memory, memory.apolloAgentReports[environment])
		if err != nil {
			return err
		}
		if report != nil {
			if _, err := memory.PutEnvironmentReport(ctx, *report); err != nil {
				return err
			}
		}
		memory.apolloAgentReports[environment] = raw
		return nil
	})
}

func (repository *PostgresRepository) updateApolloAgentReports(ctx context.Context, environment string, operation apolloAgentReportOperation) error {
	// The metadata row lock serializes both raw inputs and projections, including
	// concurrent first reports where an environment's raw row does not yet exist.
	return repository.withStateTransaction(ctx, true, func(memory *MemoryRepository, transaction *sql.Tx) error {
		var encoded []byte
		var raw persistedApolloAgentReports
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
type apolloReportEnvelope struct {
	LiveOutbox nativeLiveOutbox `json:"_liveOutbox,omitempty"`
	EnvironmentReport
	ApolloAgentReports      persistedApolloAgentReports `json:"apolloAgentReports,omitempty"`
	ApolloProjectionPending bool                        `json:"apolloProjectionPending,omitempty"`
}

func (repository *FileRepository) readApolloReportEnvelope(environment string) (apolloReportEnvelope, error) {
	if !dnsLabelPattern.MatchString(environment) {
		return apolloReportEnvelope{}, ErrNotFound
	}
	var envelope apolloReportEnvelope
	err := readJSON(repository.reportPath(environment), &envelope)
	return envelope, err
}

func (repository *FileRepository) updateApolloAgentReports(ctx context.Context, environment string, operation apolloAgentReportOperation) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	envelope, err := repository.readApolloReportEnvelope(environment)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if errors.Is(err, ErrNotFound) {
		envelope.Environment = environment
		envelope.ApolloProjectionPending = true
	}
	memory := NewMemoryRepository()
	plans, err := repository.listPlansUnlocked(environment)
	if err != nil {
		return err
	}
	for _, plan := range plans {
		memory.plans[plan.ID] = plan
	}
	if !envelope.ApolloProjectionPending {
		memory.reports[environment] = envelope.EnvironmentReport
	}
	var catalog ImportedCatalog
	if err := readJSON(repository.importedCatalogPath(environment), &catalog); err == nil {
		memory.importedCatalogs[environment] = catalog
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	raw, report, err := operation(memory, envelope.ApolloAgentReports)
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
		envelope.ApolloProjectionPending = false
	}
	envelope.ApolloAgentReports = raw
	return atomicWriteJSON(repository.reportPath(environment), envelope)
}
