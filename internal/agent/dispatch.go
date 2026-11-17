package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// ErrExecutorBlocked marks a step whose artifact kind this Spoke has no
// executor for. Nothing was run and nothing changed.
var ErrExecutorBlocked = errors.New("no executor is configured for the step's artifact kind")

// KindExecutor chooses the executor for each step from the kind of artifact
// the step's Release declared. It is the only place that choice is made: a
// Spoke is not a "Helm Spoke" or a "Zarf Spoke", and one Plan may carry both.
//
// A Helm step -- every step whose artifact names no kind -- goes to Helm
// untouched: same executor value, same arguments, same results and errors.
//
// Package is nil unless the Spoke was explicitly configured to run Zarf and
// UDS. A package step on a Spoke without it fails with a blocked diagnostic
// that says what to configure, rather than being handed to Helm, which would
// try to pull a Zarf package as a chart.
type KindExecutor struct {
	Helm    Executor
	Package Executor
	// PackageHint tells an operator how to enable Package. It is appended to
	// the blocked diagnostic.
	PackageHint string
}

var _ Executor = KindExecutor{}

func (dispatch KindExecutor) executorFor(step controlplane.PlanStep) (Executor, error) {
	kind := step.Chart.EffectiveKind()
	switch {
	case kind == controlplane.ArtifactKindHelmChart:
		if dispatch.Helm != nil {
			return dispatch.Helm, nil
		}
	case step.Chart.IsPackage():
		if dispatch.Package != nil {
			return dispatch.Package, nil
		}
	default:
		return nil, blocked(step, fmt.Sprintf("artifact kind %q is not one this Spoke knows how to deploy", kind))
	}
	message := fmt.Sprintf("this Spoke is not configured to deploy a %s", kind)
	if step.Chart.IsPackage() && dispatch.PackageHint != "" {
		message += ": " + dispatch.PackageHint
	}
	return nil, blocked(step, message)
}

func blocked(step controlplane.PlanStep, message string) error {
	return DiagnosticError{
		Err: fmt.Errorf("%w: %s", ErrExecutorBlocked, message),
		Resources: []controlplane.ResourceDiagnostic{{
			Kind: "Package", Name: step.InstallationID, Namespace: step.Namespace, Reason: "Blocked", Message: message,
		}},
	}
}

// Apply runs the step on the executor for its artifact kind.
func (dispatch KindExecutor) Apply(ctx context.Context, plan controlplane.Plan, step controlplane.PlanStep) (Receipt, error) {
	executor, err := dispatch.executorFor(step)
	if err != nil {
		return Receipt{}, err
	}
	return executor.Apply(ctx, plan, step)
}

// Observe reads the step back with the executor that would have applied it.
func (dispatch KindExecutor) Observe(ctx context.Context, step controlplane.PlanStep) (controlplane.ReportedEntity, error) {
	executor, err := dispatch.executorFor(step)
	if err != nil {
		return controlplane.ReportedEntity{}, err
	}
	return executor.Observe(ctx, step)
}

// Rollback returns a receipt to the executor that issued it. The receipt
// carries its step, so the route is the one Apply took.
func (dispatch KindExecutor) Rollback(ctx context.Context, receipt Receipt) error {
	executor, err := dispatch.executorFor(receipt.Step)
	if err != nil {
		return err
	}
	return executor.Rollback(ctx, receipt)
}
