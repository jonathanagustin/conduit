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
	observability "github.com/jonathanagustin/conduit/internal/observability/domain"
)

type Receipt struct {
	Step             controlplane.PlanStep
	PreviousRevision int
	// Fence is the execution that created or changed the release. Rollback
	// refuses a live release that carries a different one.
	Fence ReleaseFence
}

// ReleaseFence binds a Helm release to one runtime generation, one lease
// attempt, and the release name the step recorded. A later attempt replaces
// it, which is how an old worker can see that the release is no longer its own.
type ReleaseFence struct {
	Generation string
	Attempt    int
	Namespace  string
	Name       string
}

type executionClaim struct {
	Generation string
	Attempt    int
}

type executionClaimKey struct{}

func withExecutionClaim(ctx context.Context, claim executionClaim) context.Context {
	return context.WithValue(ctx, executionClaimKey{}, claim)
}

func executionClaimFrom(ctx context.Context) (executionClaim, bool) {
	claim, ok := ctx.Value(executionClaimKey{}).(executionClaim)
	if !ok || claim.Attempt < 1 || strings.TrimSpace(claim.Generation) == "" {
		return executionClaim{}, false
	}
	return claim, true
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
	// One trace per cycle, so that the Hub's access-log lines for this cycle's
	// lease, start, renewals and report share a trace_id. It rides the
	// traceparent request header; no protocol document changes.
	if _, traced := observability.TraceFromContext(ctx); !traced {
		ctx = observability.ContextWithTrace(ctx, observability.NewTraceContext())
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
	attempt := record.Lease.Attempt
	if err := worker.ControlPlane.Start(ctx, worker.Environment, worker.AgentID, record.ID, token); err != nil {
		return true, fmt.Errorf("start plan %s: %w", record.ID, err)
	}
	recorder := newExecutionRecorder(worker.Now)

	workCtx, cancelWork := context.WithCancel(ctx)
	if record.Command != nil {
		workCtx = context.WithValue(workCtx, imperativeMaintenanceKey{}, true)
	}
	if attempt > 0 && strings.TrimSpace(record.Plan.Generation) != "" {
		workCtx = withExecutionClaim(workCtx, executionClaim{
			Generation: record.Plan.Generation, Attempt: attempt,
		})
	}
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
			if err := maintenanceAllowsPlan(workCtx, record.Plan, recorder.now()); err != nil {
				executionErr = err
				recorder.add(controlplane.ExecutionPhaseSecretMutation, controlplane.ExecutionTaskFailed, "", mutation.Namespace, startedAt, err)
				break
			}
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
			if err := maintenanceAllowsStep(workCtx, step, recorder.now()); err != nil {
				executionErr = err
				failedInstallationID = step.InstallationID
				recorder.add(controlplane.ExecutionPhaseApply, controlplane.ExecutionTaskFailed, step.InstallationID, step.Namespace, startedAt, err)
				break
			}
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
			if err := maintenanceAllowsPlan(workCtx, record.Plan, recorder.now()); err != nil {
				executionErr = err
				recorder.add(controlplane.ExecutionPhaseSecretMutation, controlplane.ExecutionTaskFailed, "", mutation.Namespace, startedAt, err)
				break
			}
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
	leaseLost := false
	var heartbeatErr error
	select {
	case heartbeatErr = <-heartbeatErrors:
		recorder.add(controlplane.ExecutionPhaseLease, controlplane.ExecutionTaskFailed, "", "", recorder.now(), heartbeatErr)
		if errors.Is(heartbeatErr, ErrPlanCancelled) {
			planCancelled = true
			executionErr = heartbeatErr
		} else {
			// The lease was not renewed. A newer execution may already own the
			// releases, so rollback must not run on this attempt's authority.
			leaseLost = true
			if executionErr == nil {
				executionErr = fmt.Errorf("renew plan lease: %w", heartbeatErr)
			}
		}
	default:
	}

	if executionErr != nil {
		var rollbackErr error
		if !leaseLost {
			rollbackErr = worker.rollback(ctx, record.ID, token, leaseDuration, receipts, materialReceipts, postMaterialReceipts, recorder)
		}
		if planCancelled {
			if rollbackErr != nil {
				return true, fmt.Errorf("cancelled plan rollback: %w", rollbackErr)
			}
			return true, nil
		}
		failure := executionErr.Error()
		if leaseLost && heartbeatErr != nil && !strings.Contains(failure, heartbeatErr.Error()) {
			failure += "; lease lost: " + heartbeatErr.Error()
		}
		if rollbackErr != nil {
			failure += "; rollback: " + rollbackErr.Error()
		}
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer finishCancel()
		if err := worker.ControlPlane.Finish(
			finishCtx, worker.Environment, worker.AgentID, record.ID, token,
			false, failure, failedInstallationID, recorder.finish(attempt), controlplane.EnvironmentReport{},
		); err != nil {
			return true, fmt.Errorf("%s; finish failed plan: %w", failure, err)
		}
		return true, executionErr
	}
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer finishCancel()
	if err := worker.ControlPlane.Finish(
		finishCtx, worker.Environment, worker.AgentID, record.ID, token, true, "", "", recorder.finish(attempt), controlplane.EnvironmentReport{
			Entities: reports, Secrets: reportedSecrets(record.Plan.SecretMutations),
		},
	); errors.Is(err, ErrPlanCancelled) {
		if rollbackErr := worker.rollback(ctx, record.ID, token, leaseDuration, receipts, materialReceipts, postMaterialReceipts, recorder); rollbackErr != nil {
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

// renewForRollback keeps this attempt's lease while it unwinds work. A
// cancelled plan is not handed to another execution, so that refusal does not
// stop the unwind. Every other renewal failure means the lease has expired or
// moved, and rollback must stop before it can undo a successor.
func (worker Worker) renewForRollback(ctx context.Context, planID, token string, leaseDuration time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if leaseDuration <= 0 {
		leaseDuration = 2 * time.Minute
	}
	err := worker.ControlPlane.Renew(ctx, worker.Environment, worker.AgentID, planID, token, leaseDuration)
	if err == nil || errors.Is(err, ErrPlanCancelled) {
		return nil
	}
	return fmt.Errorf("lease is no longer held: %w", err)
}

func (worker Worker) rollback(
	ctx context.Context, planID, token string, leaseDuration time.Duration,
	receipts []Receipt, materialReceipts, postMaterialReceipts []controlplane.SecretMutation,
	recorder *executionRecorder,
) error {
	// The caller's context is the cancellation signal. Rollback used to detach
	// from it for ten minutes after renewal had already stopped, which left an
	// expired attempt free to undo the execution that had taken the lease.
	if err := worker.renewForRollback(ctx, planID, token, leaseDuration); err != nil {
		return err
	}
	rollbackCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	renewErrors := make(chan error, 1)
	go func() {
		interval := leaseDuration / 3
		if interval <= 0 {
			interval = time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-rollbackCtx.Done():
				return
			case <-ticker.C:
				if err := worker.renewForRollback(rollbackCtx, planID, token, leaseDuration); err != nil {
					select {
					case renewErrors <- err:
					default:
					}
					cancel()
					return
				}
			}
		}
	}()
	stop := func() error {
		select {
		case err := <-renewErrors:
			return err
		default:
		}
		return rollbackCtx.Err()
	}
	failures := make([]string, 0)
	stopped := false
	halt := func() bool {
		if stopped {
			return true
		}
		if err := stop(); err != nil {
			failures = append(failures, err.Error())
			stopped = true
		}
		return stopped
	}
	for index := len(postMaterialReceipts) - 1; index >= 0 && !halt(); index-- {
		mutation := postMaterialReceipts[index]
		startedAt := recorder.now()
		if err := worker.Materials.RollbackSecretMutation(rollbackCtx, mutation); err != nil {
			failures = append(failures, "secret "+mutation.Namespace+": "+err.Error())
			recorder.add(controlplane.ExecutionPhaseRollback, controlplane.ExecutionTaskRollbackFailed, "", mutation.Namespace, startedAt, err)
		} else {
			recorder.add(controlplane.ExecutionPhaseRollback, controlplane.ExecutionTaskRolledBack, "", mutation.Namespace, startedAt, nil)
		}
	}
	for index := len(receipts) - 1; index >= 0 && !halt(); index-- {
		startedAt := recorder.now()
		if err := worker.Executor.Rollback(rollbackCtx, receipts[index]); err != nil {
			failures = append(failures, receipts[index].Step.InstallationID+": "+err.Error())
			recorder.add(controlplane.ExecutionPhaseRollback, controlplane.ExecutionTaskRollbackFailed, receipts[index].Step.InstallationID, receipts[index].Step.Namespace, startedAt, err)
		} else {
			recorder.add(controlplane.ExecutionPhaseRollback, controlplane.ExecutionTaskRolledBack, receipts[index].Step.InstallationID, receipts[index].Step.Namespace, startedAt, nil)
		}
	}
	for index := len(materialReceipts) - 1; index >= 0 && !halt(); index-- {
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
