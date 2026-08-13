// Package agent is the Spoke: the half of Platform that runs inside an
// Environment's cluster. It leases work from the Hub over an outbound
// connection, renders and applies it with Helm, and reports back what is
// actually running. It never accepts an inbound connection, and the Hub never
// reaches into the cluster -- everything here is driven by the Spoke asking.
package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

type Receipt struct {
	Step             controlplane.PlanStep
	PreviousRevision int
}

type Executor interface {
	Apply(context.Context, controlplane.Plan, controlplane.PlanStep) (Receipt, error)
	Observe(context.Context, controlplane.PlanStep) (controlplane.ReportedEntity, error)
	Rollback(context.Context, Receipt) error
}

type SecretMutationExecutor interface {
	ApplySecretMutation(context.Context, controlplane.SecretMutation) error
	RollbackSecretMutation(context.Context, controlplane.SecretMutation) error
}

type Worker struct {
	ControlPlane  ControlPlane
	Executor      Executor
	Materials     SecretMutationExecutor
	Environment   string
	AgentID       string
	LeaseDuration time.Duration
	Now           func() time.Time
}

func (worker Worker) RunOnce(ctx context.Context) (bool, error) {
	if worker.ControlPlane == nil || worker.Executor == nil {
		return false, errors.New("control plane and executor are required")
	}
	if strings.TrimSpace(worker.Environment) == "" || strings.TrimSpace(worker.AgentID) == "" {
		return false, errors.New("environment and agent id are required")
	}
	leaseDuration := worker.LeaseDuration
	if leaseDuration == 0 {
		leaseDuration = 2 * time.Minute
	}
	if leaseDuration < 10*time.Second || leaseDuration > 15*time.Minute {
		return false, errors.New("lease duration must be between 10 seconds and 15 minutes")
	}
	record, err := worker.ControlPlane.Lease(ctx, worker.Environment, worker.AgentID, leaseDuration)
	if errors.Is(err, ErrNoPlan) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if record.Lease == nil || record.Lease.Token == "" {
		return false, errors.New("control plane returned a lease without an opaque token")
	}
	token := record.Lease.Token
	if err := worker.ControlPlane.Start(ctx, worker.Environment, worker.AgentID, record.ID, token); err != nil {
		return true, fmt.Errorf("start plan %s: %w", record.ID, err)
	}
	recorder := newExecutionRecorder(worker.Now)

	workCtx, cancelWork := context.WithCancel(ctx)
	heartbeatErrors := make(chan error, 1)
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		interval := leaseDuration / 3
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-workCtx.Done():
				return
			case <-ticker.C:
				if err := worker.ControlPlane.Renew(
					workCtx, worker.Environment, worker.AgentID, record.ID, token, leaseDuration,
				); err != nil {
					select {
					case heartbeatErrors <- err:
					default:
					}
					cancelWork()
					return
				}
			}
		}
	}()

	receipts := make([]Receipt, 0, len(record.Plan.Steps))
	materialReceipts := make([]controlplane.SecretMutation, 0, len(record.Plan.SecretMutations))
	postMaterialReceipts := make([]controlplane.SecretMutation, 0)
	reports := make([]controlplane.ReportedEntity, 0, len(record.Plan.Steps))
	var executionErr error
	failedInstallationID := ""
	validationStartedAt := recorder.started()
	if err := validateSecretMutationCoverage(record.Plan); err != nil {
		executionErr = err
	} else if len(record.Plan.SecretMutations) > 0 && worker.Materials == nil {
		executionErr = errors.New("plan contains secret mutations but no external material preparer is configured")
	}
	if executionErr != nil {
		recorder.add(controlplane.ExecutionPhaseValidate, controlplane.ExecutionTaskFailed, "", "", recorder.started(), executionErr)
	} else {
		recorder.add(controlplane.ExecutionPhaseValidate, controlplane.ExecutionTaskSucceeded, "", "", validationStartedAt, nil)
	}
	if executionErr == nil {
		for _, mutation := range record.Plan.SecretMutations {
			if mutation.Action == controlplane.SecretDelete {
				continue
			}
			startedAt := recorder.now()
			if applyErr := worker.Materials.ApplySecretMutation(workCtx, mutation); applyErr != nil {
				assignment := mutation.Current
				if assignment == nil {
					assignment = mutation.Previous
				}
				name := "unknown"
				if assignment != nil {
					name = assignment.Assignment
				}
				executionErr = fmt.Errorf("apply secret mutation %s in %s: %w", name, mutation.Namespace, applyErr)
				recorder.add(controlplane.ExecutionPhaseSecretMutation, controlplane.ExecutionTaskFailed, "", mutation.Namespace, startedAt, executionErr)
				break
			}
			recorder.add(controlplane.ExecutionPhaseSecretMutation, controlplane.ExecutionTaskSucceeded, "", mutation.Namespace, startedAt, nil)
			materialReceipts = append(materialReceipts, mutation)
		}
	}
	if executionErr == nil {
		for _, step := range record.Plan.Steps {
			startedAt := recorder.now()
			if step.Action != controlplane.PlanActionUnmanage {
				receipt, applyErr := worker.Executor.Apply(workCtx, record.Plan, step)
				if applyErr != nil {
					executionErr = fmt.Errorf("apply %s: %w", step.InstallationID, applyErr)
					failedInstallationID = step.InstallationID
					recorder.add(controlplane.ExecutionPhaseApply, controlplane.ExecutionTaskFailed, step.InstallationID, step.Namespace, startedAt, applyErr)
					break
				}
				receipts = append(receipts, receipt)
			}
			recorder.add(controlplane.ExecutionPhaseApply, controlplane.ExecutionTaskSucceeded, step.InstallationID, step.Namespace, startedAt, nil)
			startedAt = recorder.now()
			report, observeErr := worker.Executor.Observe(workCtx, step)
			if observeErr != nil {
				executionErr = fmt.Errorf("observe %s: %w", step.InstallationID, observeErr)
				failedInstallationID = step.InstallationID
				recorder.add(controlplane.ExecutionPhaseObserve, controlplane.ExecutionTaskFailed, step.InstallationID, step.Namespace, startedAt, observeErr)
				break
			}
			recorder.add(controlplane.ExecutionPhaseObserve, controlplane.ExecutionTaskSucceeded, step.InstallationID, step.Namespace, startedAt, nil)
			reports = append(reports, report)
		}
	}
	if executionErr == nil {
		for _, mutation := range record.Plan.SecretMutations {
			if mutation.Action != controlplane.SecretDelete {
				continue
			}
			startedAt := recorder.now()
			if applyErr := worker.Materials.ApplySecretMutation(workCtx, mutation); applyErr != nil {
				name := "unknown"
				if mutation.Previous != nil {
					name = mutation.Previous.Assignment
				}
				executionErr = fmt.Errorf("delete secret mutation %s in %s: %w", name, mutation.Namespace, applyErr)
				recorder.add(controlplane.ExecutionPhaseSecretMutation, controlplane.ExecutionTaskFailed, "", mutation.Namespace, startedAt, executionErr)
				break
			}
			recorder.add(controlplane.ExecutionPhaseSecretMutation, controlplane.ExecutionTaskSucceeded, "", mutation.Namespace, startedAt, nil)
			postMaterialReceipts = append(postMaterialReceipts, mutation)
		}
	}
	cancelWork()
	<-heartbeatDone
	planCancelled := false
	select {
	case heartbeatErr := <-heartbeatErrors:
		recorder.add(controlplane.ExecutionPhaseLease, controlplane.ExecutionTaskFailed, "", "", recorder.now(), heartbeatErr)
		if errors.Is(heartbeatErr, ErrPlanCancelled) {
			planCancelled = true
			executionErr = heartbeatErr
		} else if executionErr == nil {
			executionErr = fmt.Errorf("renew plan lease: %w", heartbeatErr)
		}
	default:
	}

	if executionErr != nil {
		rollbackErr := worker.rollback(ctx, receipts, materialReceipts, postMaterialReceipts, recorder)
		if planCancelled {
			if rollbackErr != nil {
				return true, fmt.Errorf("cancelled plan rollback: %w", rollbackErr)
			}
			return true, nil
		}
		failure := executionErr.Error()
		if rollbackErr != nil {
			failure += "; rollback: " + rollbackErr.Error()
		}
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer finishCancel()
		if err := worker.ControlPlane.Finish(
			finishCtx, worker.Environment, worker.AgentID, record.ID, token,
			false, failure, failedInstallationID, recorder.finish(), controlplane.EnvironmentReport{},
		); err != nil {
			return true, fmt.Errorf("%s; finish failed plan: %w", failure, err)
		}
		return true, executionErr
	}
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer finishCancel()
	if err := worker.ControlPlane.Finish(
		finishCtx, worker.Environment, worker.AgentID, record.ID, token, true, "", "", recorder.finish(), controlplane.EnvironmentReport{
			Entities: reports, Secrets: reportedSecrets(record.Plan.SecretMutations),
		},
	); errors.Is(err, ErrPlanCancelled) {
		if rollbackErr := worker.rollback(ctx, receipts, materialReceipts, postMaterialReceipts, recorder); rollbackErr != nil {
			return true, fmt.Errorf("cancelled plan rollback: %w", rollbackErr)
		}
		return true, nil
	} else if err != nil {
		return true, fmt.Errorf("finish successful plan: %w", err)
	}
	return true, nil
}

func validateSecretMutationCoverage(plan controlplane.Plan) error {
	covered := make(map[string]struct{}, len(plan.SecretMutations))
	for _, mutation := range plan.SecretMutations {
		if mutation.Current != nil {
			covered[mutation.Current.Assignment+"\x00"+mutation.Namespace] = struct{}{}
		}
	}
	for _, step := range plan.Steps {
		for _, access := range step.SecretAccess {
			if _, exists := covered[access.Assignment+"\x00"+step.Namespace]; !exists {
				return fmt.Errorf("secret assignment %s in %s has no lifecycle mutation", access.Assignment, step.Namespace)
			}
		}
	}
	return nil
}

func (worker Worker) rollback(
	ctx context.Context, receipts []Receipt, materialReceipts, postMaterialReceipts []controlplane.SecretMutation,
	recorder *executionRecorder,
) error {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
	defer cancel()
	failures := make([]string, 0)
	for index := len(postMaterialReceipts) - 1; index >= 0; index-- {
		mutation := postMaterialReceipts[index]
		startedAt := recorder.now()
		if err := worker.Materials.RollbackSecretMutation(rollbackCtx, mutation); err != nil {
			failures = append(failures, "secret "+mutation.Namespace+": "+err.Error())
			recorder.add(controlplane.ExecutionPhaseRollback, controlplane.ExecutionTaskRollbackFailed, "", mutation.Namespace, startedAt, err)
		} else {
			recorder.add(controlplane.ExecutionPhaseRollback, controlplane.ExecutionTaskRolledBack, "", mutation.Namespace, startedAt, nil)
		}
	}
	for index := len(receipts) - 1; index >= 0; index-- {
		startedAt := recorder.now()
		if err := worker.Executor.Rollback(rollbackCtx, receipts[index]); err != nil {
			failures = append(failures, receipts[index].Step.InstallationID+": "+err.Error())
			recorder.add(controlplane.ExecutionPhaseRollback, controlplane.ExecutionTaskRollbackFailed, receipts[index].Step.InstallationID, receipts[index].Step.Namespace, startedAt, err)
		} else {
			recorder.add(controlplane.ExecutionPhaseRollback, controlplane.ExecutionTaskRolledBack, receipts[index].Step.InstallationID, receipts[index].Step.Namespace, startedAt, nil)
		}
	}
	for index := len(materialReceipts) - 1; index >= 0; index-- {
		mutation := materialReceipts[index]
		if mutation.Action == controlplane.SecretVerify {
			continue
		}
		startedAt := recorder.now()
		if err := worker.Materials.RollbackSecretMutation(rollbackCtx, mutation); err != nil {
			failures = append(failures, "secret "+mutation.Namespace+": "+err.Error())
			recorder.add(controlplane.ExecutionPhaseRollback, controlplane.ExecutionTaskRollbackFailed, "", mutation.Namespace, startedAt, err)
		} else {
			recorder.add(controlplane.ExecutionPhaseRollback, controlplane.ExecutionTaskRolledBack, "", mutation.Namespace, startedAt, nil)
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func reportedSecrets(mutations []controlplane.SecretMutation) []controlplane.ReportedSecret {
	reported := make([]controlplane.ReportedSecret, 0, len(mutations))
	for _, mutation := range mutations {
		if mutation.Action == controlplane.SecretDelete || mutation.Current == nil {
			continue
		}
		access := mutation.Current
		reported = append(reported, controlplane.ReportedSecret{
			Assignment: access.Assignment, Namespace: mutation.Namespace, Revision: access.Revision,
			ProviderRef: access.ProviderRef, KubernetesSecretName: access.KubernetesSecretName,
			Keys: append([]string(nil), access.Keys...),
		})
	}
	return reported
}
