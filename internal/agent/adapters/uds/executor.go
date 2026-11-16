// Package uds implements the Spoke agent.Executor port for Environments running
// UDS Core and Zarf (#427).
//
// It deploys Zarf packages and UDS bundles through the pinned zarf/uds CLIs,
// reads back the uds.dev/v1alpha1 Package CR status from the cluster, and maps
// that status into controlplane.ReportedEntity without leaking Zarf or UDS
// types into the domain packages.
package uds

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jonathanagustin/conduit/internal/agent"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/udsconverge"
)

// Executor implements agent.Executor for Zarf packages and UDS Core Package CRs.
type Executor struct {
	Runner              agent.CommandRunner
	ZarfBin             string
	UDSBin              string
	KubectlBin          string
	UseUDSBundle        bool
	PackageSource       func(step controlplane.PlanStep) string
	PackageManifestPath string
}

var _ agent.Executor = Executor{}

func (e Executor) runner() agent.CommandRunner {
	if e.Runner != nil {
		return e.Runner
	}
	return agent.ExecRunner{}
}

func (e Executor) zarfBin() string {
	if strings.TrimSpace(e.ZarfBin) != "" {
		return strings.TrimSpace(e.ZarfBin)
	}
	return "zarf"
}

func (e Executor) udsBin() string {
	if strings.TrimSpace(e.UDSBin) != "" {
		return strings.TrimSpace(e.UDSBin)
	}
	return "uds"
}

func (e Executor) kubectlBin() string {
	if strings.TrimSpace(e.KubectlBin) != "" {
		return strings.TrimSpace(e.KubectlBin)
	}
	return "kubectl"
}

func (e Executor) sourceFor(step controlplane.PlanStep) string {
	if e.PackageSource != nil {
		if src := strings.TrimSpace(e.PackageSource(step)); src != "" {
			return src
		}
	}
	if strings.TrimSpace(step.ReleaseDigest) != "" && strings.TrimSpace(step.Product) != "" {
		return fmt.Sprintf("oci://%s@%s", strings.TrimSpace(step.Product), strings.TrimSpace(step.ReleaseDigest))
	}
	return strings.TrimSpace(step.InstallationID)
}

// Apply deploys or removes a Zarf package or UDS bundle for step.
func (e Executor) Apply(ctx context.Context, _ controlplane.Plan, step controlplane.PlanStep) (agent.Receipt, error) {
	if strings.TrimSpace(step.InstallationID) == "" {
		return agent.Receipt{}, errors.New("installationId is required")
	}
	run := e.runner()
	if step.Action == "uninstall" {
		out, err := run.Run(ctx, e.zarfBin(), "package", "remove", step.InstallationID, "--confirm")
		if err != nil {
			return agent.Receipt{}, agent.DiagnosticError{
				Err:    err,
				Output: strings.TrimSpace(string(out)),
			}
		}
		return agent.Receipt{Step: step, PreviousRevision: 1}, nil
	}

	source := e.sourceFor(step)
	if source == "" {
		return agent.Receipt{}, errors.New("package source reference is required")
	}
	var (
		out []byte
		err error
	)
	if e.UseUDSBundle {
		out, err = run.Run(ctx, e.udsBin(), "deploy", source, "--confirm")
	} else {
		out, err = run.Run(ctx, e.zarfBin(), "package", "deploy", source, "--confirm")
	}
	if err != nil {
		return agent.Receipt{}, agent.DiagnosticError{
			Err:    err,
			Output: strings.TrimSpace(string(out)),
			Resources: []controlplane.ResourceDiagnostic{{
				Kind:      "Package",
				Name:      step.InstallationID,
				Namespace: step.Namespace,
				Message:   strings.TrimSpace(string(out)),
			}},
		}
	}
	if strings.TrimSpace(e.PackageManifestPath) != "" {
		ns := strings.TrimSpace(step.Namespace)
		if ns == "" {
			ns = "default"
		}
		applyOut, applyErr := run.Run(ctx, e.kubectlBin(), "apply", "-n", ns, "-f", strings.TrimSpace(e.PackageManifestPath))
		if applyErr != nil {
			return agent.Receipt{}, agent.DiagnosticError{
				Err:    applyErr,
				Output: strings.TrimSpace(string(applyOut)),
			}
		}
	}
	return agent.Receipt{Step: step, PreviousRevision: 0}, nil
}

// Observe reads the UDS Package CR status from the cluster and projects it into
// controlplane.ReportedEntity.
func (e Executor) Observe(ctx context.Context, step controlplane.PlanStep) (controlplane.ReportedEntity, error) {
	ns := strings.TrimSpace(step.Namespace)
	if ns == "" {
		ns = "default"
	}
	out, err := e.runner().Run(ctx, e.kubectlBin(), "get", "package.uds.dev", step.InstallationID, "-n", ns, "-o", "yaml")
	if err != nil {
		return controlplane.ReportedEntity{}, fmt.Errorf("observe UDS Package %s/%s: %w", ns, step.InstallationID, err)
	}
	obs, err := udsconverge.ParsePackageStatus(out)
	if err != nil {
		return controlplane.ReportedEntity{}, err
	}
	health := controlplane.HealthWarning
	rollout := controlplane.RolloutInGracePeriod
	if obs.Healthy && obs.Converged {
		health = controlplane.HealthHealthy
		rollout = controlplane.RolloutUpToDate
	} else if strings.EqualFold(obs.Phase, "Failed") || strings.EqualFold(obs.Phase, "Unhealthy") {
		health = controlplane.HealthTerminal
		rollout = controlplane.RolloutBlocked
	}
	releaseDigest := step.ReleaseDigest
	if obs.SourceDigest != "" {
		releaseDigest = obs.SourceDigest
	}
	return controlplane.ReportedEntity{
		InstallationID: step.InstallationID,
		Product:        step.Product,
		Version:        step.Version,
		ReleaseDigest:  releaseDigest,
		Namespace:      ns,
		ConfigDigest:   step.ConfigDigest,
		Health:         health,
		RolloutStatus:  rollout,
		ConfigStatus:   controlplane.ConfigApplied,
		Lifecycle:      controlplane.LifecycleInstalled,
	}, nil
}

// Rollback removes a failed Zarf package deployment when no prior revision
// existed, or re-deploys the prior package coordinate.
func (e Executor) Rollback(ctx context.Context, receipt agent.Receipt) error {
	if strings.TrimSpace(receipt.Step.InstallationID) == "" {
		return nil
	}
	_, err := e.runner().Run(ctx, e.zarfBin(), "package", "remove", receipt.Step.InstallationID, "--confirm")
	return err
}
