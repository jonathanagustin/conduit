package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/apollo/workloadmetadata"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"go.yaml.in/yaml/v4"
)

type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

// RegistryPreparer resolves externally owned credentials and ensures any
// namespace pull Secret without returning secret material to the Plan or Hub.
// Implementations must be idempotent and scoped to the immutable assignment
// revision carried by RegistryAccess.
type RegistryPreparer interface {
	Prepare(context.Context, controlplane.RegistryAccess, string) error
}

// ImagePreparer delegates node-level image caching to an Environment-owned
// implementation. It can use a native Kubernetes pre-puller, registry mirror,
// or provider facility without granting the Hub node credentials.
type ImagePreparer interface {
	PrepareImages(context.Context, []controlplane.Artifact, []controlplane.RegistryAccess, string) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

type HelmExecutor struct {
	Runner  CommandRunner
	Timeout time.Duration
	// IngressPollInterval controls only asynchronous external realization
	// observation. Zero uses the production two-second interval.
	IngressPollInterval time.Duration
	Now                 func() time.Time
	HelmMajor           int
	PlainHTTP           bool
	Registry            RegistryPreparer
	Images              ImagePreparer
	// Environment is the Apollo Environment ID installations belong to. It is
	// what makes the documented workload metadata computable; without it the
	// post-render step is skipped rather than guessed at.
	Environment string
	// PostRenderer is the executable Helm 3 invokes between rendering and
	// applying. It defaults to this binary, which answers the post-render
	// subcommand itself, so no second artifact ships.
	PostRenderer string
	// PostRendererPlugin is the Helm 4 plugin name for the same thing. Setting it
	// skips installation and uses an already-present plugin, which is how tests
	// avoid touching HELM_PLUGINS.
	PostRendererPlugin string
	// PluginRoot is where the Helm 4 plugin is installed. It defaults to a
	// directory under the system temporary path.
	PluginRoot string
	// Version labels the generated plugin manifest.
	Version string
}

func (executor HelmExecutor) Apply(
	ctx context.Context, _ controlplane.Plan, step controlplane.PlanStep,
) (Receipt, error) {
	if err := executor.checkMaintenance(ctx, step); err != nil {
		return Receipt{}, err
	}
	if step.Action == "uninstall" {
		previousRevision, err := executor.currentRevision(ctx, step)
		if err != nil {
			return Receipt{}, err
		}
		if previousRevision == 0 {
			return Receipt{}, fmt.Errorf("Helm release is absent before uninstall")
		}
		timeout := executor.Timeout
		if timeout == 0 {
			timeout = 5 * time.Minute
		}
		if err := executor.checkMaintenance(ctx, step); err != nil {
			return Receipt{}, err
		}
		_, err = executor.runner().Run(ctx, "helm", "uninstall", step.HelmRelease(), "--namespace", step.Namespace,
			"--keep-history", "--wait", "--timeout", timeout.String())
		if err != nil {
			return Receipt{}, fmt.Errorf("Helm uninstall: %w", err)
		}
		return executor.receipt(ctx, step, previousRevision), nil
	}
	if step.Action == "restart" {
		target := strings.ToLower(step.ResourceKind) + "/" + step.ResourceName
		if err := executor.checkMaintenance(ctx, step); err != nil {
			return Receipt{}, err
		}
		if output, err := executor.runner().Run(ctx, "kubectl", "rollout", "restart", target, "--namespace", step.Namespace); err != nil {
			return Receipt{}, DiagnosticError{Err: fmt.Errorf("restart Kubernetes resource: %w", err), Output: string(output), Resources: []controlplane.ResourceDiagnostic{{
				Kind: step.ResourceKind, Namespace: step.Namespace, Name: step.ResourceName, Reason: "RestartFailed",
			}}}
		}
		timeout := executor.Timeout
		if timeout == 0 {
			timeout = 5 * time.Minute
		}
		if output, err := executor.runner().Run(ctx, "kubectl", "rollout", "status", target, "--namespace", step.Namespace,
			"--timeout", timeout.String()); err != nil {
			return Receipt{}, DiagnosticError{Err: fmt.Errorf("wait for restarted Kubernetes resource: %w", err), Output: string(output), Resources: []controlplane.ResourceDiagnostic{{
				Kind: step.ResourceKind, Namespace: step.Namespace, Name: step.ResourceName, Reason: "RolloutFailed",
			}}}
		}
		return executor.receipt(ctx, step, 0), nil
	}
	for _, access := range step.RegistryAccess {
		if access.CredentialRef == "" && access.ImagePullSecretName == "" {
			continue
		}
		if executor.Registry == nil {
			return Receipt{}, fmt.Errorf("registry assignment %s requires an external credential preparer", access.AssignmentID)
		}
		if err := executor.checkMaintenance(ctx, step); err != nil {
			return Receipt{}, err
		}
		if err := executor.Registry.Prepare(ctx, access, step.Namespace); err != nil {
			return Receipt{}, fmt.Errorf("prepare registry assignment %s revision %d: %w", access.AssignmentID, access.Revision, err)
		}
	}
	runner := executor.runner()
	previousRevision, err := executor.currentRevision(ctx, step)
	if err != nil {
		return Receipt{}, err
	}
	helmMajor, err := executor.helmMajor(ctx)
	if err != nil {
		return Receipt{}, err
	}
	temporaryDirectory, err := os.MkdirTemp("", "platform-delivery-chart-*")
	if err != nil {
		return Receipt{}, fmt.Errorf("create chart staging directory: %w", err)
	}
	defer os.RemoveAll(temporaryDirectory)

	reference := strings.TrimPrefix(step.Chart.Repository, "oci://")
	pullArguments := []string{"pull", "oci://" + reference,
		"--version", step.Chart.Tag, "--destination", temporaryDirectory}
	if executor.PlainHTTP {
		pullArguments = append(pullArguments, "--plain-http")
	}
	pullOutput, err := runner.Run(ctx, "helm", pullArguments...)
	if err != nil {
		return Receipt{}, fmt.Errorf("pull chart: %w", err)
	}
	if !strings.Contains(string(pullOutput), "Digest: "+step.Chart.Digest) {
		return Receipt{}, fmt.Errorf("registry reported chart digest does not match %s", step.Chart.Digest)
	}
	packages, err := filepath.Glob(filepath.Join(temporaryDirectory, "*.tgz"))
	if err != nil || len(packages) != 1 {
		return Receipt{}, fmt.Errorf("chart pull produced %d packages", len(packages))
	}
	timeout := executor.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	description := executor.ownedReleaseDescription(ctx, step)
	valuesPath := filepath.Join(temporaryDirectory, "values.json")
	values := step.Values
	if len(values) == 0 {
		values = []byte("{}")
	}
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		return Receipt{}, fmt.Errorf("write resolved Helm values: %w", err)
	}
	templateArguments := []string{"template", step.HelmRelease(), packages[0],
		"--namespace", step.Namespace, "--values", valuesPath}
	// This render is what the declared-image check inspects, so it has to be the
	// render the upgrade will actually produce. helm template sets
	// .Release.IsInstall unless told otherwise, so a chart that keys any image
	// off .Release.IsUpgrade would be validated in install form and then deploy
	// its upgrade form unchecked.
	if previousRevision > 0 {
		templateArguments = append(templateArguments, "--is-upgrade")
	}
	rendered, err := runner.Run(ctx, "helm", templateArguments...)
	if err != nil {
		return Receipt{}, fmt.Errorf("render chart: %w", err)
	}
	if err := validateRenderedImages(rendered, step.Images); err != nil {
		return Receipt{}, err
	}
	if step.HelmExecution.ImagePrePull {
		if executor.Images == nil {
			return Receipt{}, fmt.Errorf("image pre-pull is required but no Environment image preparer is configured")
		}
		prePullTimeout := time.Duration(step.HelmExecution.ImagePrePullTimeoutSeconds) * time.Second
		if prePullTimeout == 0 {
			prePullTimeout = 15 * time.Minute
		}
		if err := executor.checkMaintenance(ctx, step); err != nil {
			return Receipt{}, err
		}
		prePullCtx, cancel := context.WithTimeout(ctx, prePullTimeout)
		err = executor.Images.PrepareImages(prePullCtx, step.Images, step.RegistryAccess, step.Namespace)
		cancel()
		if err != nil {
			return Receipt{}, fmt.Errorf("pre-pull immutable workload images: %w", err)
		}
	}
	operationTimeout := 5 * time.Minute
	arguments := []string{"upgrade", "--install", step.HelmRelease(), packages[0],
		"--namespace", step.Namespace,
		"--values", valuesPath,
		"--description", description}
	postRenderArguments, err := executor.postRenderArguments(step, temporaryDirectory, helmMajor)
	if err != nil {
		return Receipt{}, err
	}
	arguments = append(arguments, postRenderArguments...)
	if step.HelmExecution.RolloutStrategy == controlplane.RolloutApplyChangesNoWait {
		arguments = append(arguments, "--timeout", operationTimeout.String())
	} else {
		readinessTimeout := readinessTimeout(step, previousRevision, timeout)
		// Provenance (reviewed 2026-08-02): Helm 4 replaces --atomic with
		// --rollback-on-failure and introduces server-side apply selection.
		// https://helm.sh/docs/overview/
		if helmMajor == 4 {
			arguments = append(arguments, "--rollback-on-failure", "--wait=watcher", "--timeout", readinessTimeout.String())
		} else {
			arguments = append(arguments, "--atomic", "--wait", "--timeout", readinessTimeout.String())
		}
	}
	if helmMajor == 4 {
		// Keep the established Helm 3 client-side update semantics. A future
		// server-side migration needs its own explicit compatibility gate.
		arguments = append(arguments, "--server-side=false")
	}
	if err := executor.checkMaintenance(ctx, step); err != nil {
		return Receipt{}, err
	}
	applyOutput, err := runner.Run(ctx, "helm", arguments...)
	if err != nil {
		if step.HelmExecution.RolloutStrategy == controlplane.RolloutApplyChangesNoWait {
			rollbackCtx, cancel := context.WithTimeout(ctx, operationTimeout)
			rollbackErr := executor.Rollback(rollbackCtx, executor.receipt(ctx, step, previousRevision))
			cancel()
			if rollbackErr != nil {
				return Receipt{}, fmt.Errorf("Helm apply failed: %w; rollback: %v", err, rollbackErr)
			}
		}
		return Receipt{}, DiagnosticError{Err: fmt.Errorf("Helm reconcile: %w", err), Output: string(applyOutput), Resources: []controlplane.ResourceDiagnostic{{
			Kind: "HelmRelease", Namespace: step.Namespace, Name: step.HelmRelease(), Reason: "ReconcileFailed",
		}}}
	}
	return executor.receipt(ctx, step, previousRevision), nil
}

func (executor HelmExecutor) Observe(
	ctx context.Context, step controlplane.PlanStep,
) (controlplane.ReportedEntity, error) {
	if step.Action == "uninstall" {
		release, err := executor.currentDeployedRelease(ctx, step)
		if err != nil {
			return controlplane.ReportedEntity{}, err
		}
		if release != nil {
			return controlplane.ReportedEntity{}, fmt.Errorf("Helm release remains deployed after uninstall")
		}
		now := time.Now
		if executor.Now != nil {
			now = executor.Now
		}
		return controlplane.ReportedEntity{InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
			ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
			Health: controlplane.HealthUnknown, RolloutStatus: controlplane.RolloutUpToDate,
			ConfigStatus: controlplane.ConfigApplied, Lifecycle: controlplane.LifecycleUninstalled, ObservedAt: now().UTC()}, nil
	}
	output, err := executor.runner().Run(ctx, "helm", "status", step.HelmRelease(),
		"--namespace", step.Namespace, "--output", "json")
	if err != nil {
		return controlplane.ReportedEntity{}, DiagnosticError{Err: err, Output: string(output), Resources: []controlplane.ResourceDiagnostic{{
			Kind: "HelmRelease", Namespace: step.Namespace, Name: step.HelmRelease(), Reason: "ObservationFailed",
		}}}
	}
	var status struct {
		Info struct {
			Status      string `json:"status"`
			Description string `json:"description"`
		} `json:"info"`
	}
	if err := json.Unmarshal(output, &status); err != nil {
		return controlplane.ReportedEntity{}, fmt.Errorf("decode Helm status: %w", err)
	}
	if status.Info.Status != "deployed" {
		return controlplane.ReportedEntity{}, fmt.Errorf("Helm release status is %q", status.Info.Status)
	}
	if releaseIdentity(status.Info.Description) != releaseDescription(step) {
		return controlplane.ReportedEntity{}, fmt.Errorf("Helm release metadata does not match the leased plan")
	}
	release, err := executor.currentRelease(ctx, step)
	if err != nil {
		return controlplane.ReportedEntity{}, err
	}
	if release == nil {
		return controlplane.ReportedEntity{}, fmt.Errorf("Helm release is absent after successful reconciliation")
	}
	expectedVersionSuffix := "-" + step.Chart.Tag
	if !strings.HasSuffix(release.Chart, expectedVersionSuffix) {
		return controlplane.ReportedEntity{}, fmt.Errorf("deployed chart %q does not have version %q", release.Chart, step.Chart.Tag)
	}
	if step.HelmExecution.RolloutStrategy != controlplane.RolloutApplyChangesNoWait {
		if err := waitForIngressRealization(ctx, executor.runner(), step.HelmRelease(), step.Namespace, executor.Timeout, executor.IngressPollInterval); err != nil {
			return controlplane.ReportedEntity{}, err
		}
	}
	now := time.Now
	if executor.Now != nil {
		now = executor.Now
	}
	health, rollout := controlplane.HealthHealthy, controlplane.RolloutUpToDate
	if step.HelmExecution.RolloutStrategy == controlplane.RolloutApplyChangesNoWait {
		health, rollout = controlplane.HealthUnknown, controlplane.RolloutInGracePeriod
	}
	return controlplane.ReportedEntity{
		InstallationID: step.InstallationID,
		Product:        step.Product, Version: step.Version, Namespace: step.Namespace,
		ReleaseDigest: step.ReleaseDigest, ConfigDigest: step.ConfigDigest,
		Health: health, RolloutStatus: rollout,
		ConfigStatus: controlplane.ConfigApplied, Lifecycle: controlplane.LifecycleInstalled, ObservedAt: now().UTC(),
	}, nil
}

func readinessTimeout(step controlplane.PlanStep, previousRevision int, fallback time.Duration) time.Duration {
	seconds := step.HelmExecution.InstallResourceReadinessTimeoutSeconds
	if previousRevision > 0 {
		seconds = step.HelmExecution.UpgradeResourceReadinessTimeoutSeconds
	}
	if seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return fallback
}

func (executor HelmExecutor) receipt(ctx context.Context, step controlplane.PlanStep, previousRevision int) Receipt {
	receipt := Receipt{Step: step, PreviousRevision: previousRevision}
	if claim, ok := executionClaimFrom(ctx); ok {
		receipt.Fence = ReleaseFence{
			Generation: claim.Generation, Attempt: claim.Attempt,
			Namespace: step.Namespace, Name: step.HelmRelease(),
		}
	}
	return receipt
}

func (executor HelmExecutor) ownedReleaseDescription(ctx context.Context, step controlplane.PlanStep) string {
	description := releaseDescription(step)
	if claim, ok := executionClaimFrom(ctx); ok {
		description = fencedReleaseDescription(step, ReleaseFence{
			Generation: claim.Generation, Attempt: claim.Attempt,
			Namespace: step.Namespace, Name: step.HelmRelease(),
		})
	}
	return description
}

func (executor HelmExecutor) Rollback(ctx context.Context, receipt Receipt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timeout := executor.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	if receipt.Step.Action == "restart" {
		// A rollout restart changes Pod instances, not the immutable Helm release;
		// there is no truthful inverse operation.
		return nil
	}
	// The step records the release deployment changed. The entity name is a
	// different identifier and may name a release this attempt never touched.
	releaseName := receipt.Step.HelmRelease()
	if receipt.Fence.Attempt > 0 && receipt.Fence.Name != "" && receipt.Fence.Name != releaseName {
		return fmt.Errorf("rollback fence names %s but the step records %s", receipt.Fence.Name, releaseName)
	}
	if err := executor.confirmReleaseOwnership(ctx, receipt, releaseName); err != nil {
		return err
	}
	if receipt.PreviousRevision == 0 {
		output, err := executor.runner().Run(ctx, "helm", "uninstall", releaseName,
			"--namespace", receipt.Step.Namespace, "--wait", "--timeout", timeout.String())
		if err != nil {
			return DiagnosticError{Err: err, Output: string(output), Resources: []controlplane.ResourceDiagnostic{{
				Kind: "HelmRelease", Namespace: receipt.Step.Namespace, Name: releaseName, Reason: "RollbackFailed",
			}}}
		}
		return nil
	}
	output, err := executor.runner().Run(ctx, "helm", "rollback", releaseName,
		strconv.Itoa(receipt.PreviousRevision), "--namespace", receipt.Step.Namespace,
		"--wait", "--timeout", timeout.String())
	if err != nil {
		return DiagnosticError{Err: err, Output: string(output), Resources: []controlplane.ResourceDiagnostic{{
			Kind: "HelmRelease", Namespace: receipt.Step.Namespace, Name: releaseName, Reason: "RollbackFailed",
		}}}
	}
	return nil
}

// confirmReleaseOwnership reads the fence written at apply time. A newer
// attempt replaces that fence, and rollback must refuse the release rather
// than infer that the name still belongs to this execution.
func (executor HelmExecutor) confirmReleaseOwnership(ctx context.Context, receipt Receipt, releaseName string) error {
	if receipt.Fence.Attempt < 1 {
		return nil
	}
	output, err := executor.runner().Run(ctx, "helm", "status", releaseName,
		"--namespace", receipt.Step.Namespace, "--output", "json")
	if err != nil {
		return DiagnosticError{Err: err, Output: string(output), Resources: []controlplane.ResourceDiagnostic{{
			Kind: "HelmRelease", Namespace: receipt.Step.Namespace, Name: releaseName, Reason: "RollbackRefused",
		}}}
	}
	var status struct {
		Name string `json:"name"`
		Info struct {
			Description string `json:"description"`
		} `json:"info"`
	}
	if err := json.Unmarshal(output, &status); err != nil {
		return fmt.Errorf("decode Helm release ownership: %w", err)
	}
	if status.Name != "" && status.Name != releaseName {
		return fmt.Errorf("helm release status named %s, not %s", status.Name, releaseName)
	}
	live, ok := parseReleaseFence(status.Info.Description)
	if !ok {
		return nil
	}
	if !fenceMatches(receipt.Fence, live) {
		return fmt.Errorf("helm release %s/%s is owned by generation %s attempt %d", receipt.Step.Namespace, releaseName, live.Generation, live.Attempt)
	}
	return nil
}

func (executor HelmExecutor) currentRevision(ctx context.Context, step controlplane.PlanStep) (int, error) {
	release, err := executor.currentRelease(ctx, step)
	if err != nil || release == nil {
		return 0, err
	}
	revision, err := strconv.Atoi(release.Revision)
	if err != nil || revision < 1 {
		return 0, fmt.Errorf("Helm release has invalid revision %q", release.Revision)
	}
	return revision, nil
}

type helmReleaseSummary struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
	Chart    string `json:"chart"`
}

func (executor HelmExecutor) currentRelease(ctx context.Context, step controlplane.PlanStep) (*helmReleaseSummary, error) {
	return executor.lookupRelease(ctx, step, true)
}

func (executor HelmExecutor) currentDeployedRelease(ctx context.Context, step controlplane.PlanStep) (*helmReleaseSummary, error) {
	return executor.lookupRelease(ctx, step, false)
}

func (executor HelmExecutor) lookupRelease(ctx context.Context, step controlplane.PlanStep, includeAll bool) (*helmReleaseSummary, error) {
	arguments := []string{"list"}
	if includeAll {
		// Helm 4 removed the Helm 3 --all status shorthand because all
		// statuses became the default. Spell out the status set so the Spoke
		// can adopt and recover releases created by either supported major.
		arguments = append(arguments, "--deployed", "--failed", "--pending", "--uninstalled", "--superseded", "--uninstalling")
	}
	arguments = append(arguments, "--namespace", step.Namespace, "--filter", "^"+step.HelmRelease()+"$", "--output", "json")
	output, err := executor.runner().Run(ctx, "helm", arguments...)
	if err != nil {
		return nil, fmt.Errorf("inspect existing Helm release: %w", err)
	}
	var releases []helmReleaseSummary
	if err := json.Unmarshal(output, &releases); err != nil {
		return nil, fmt.Errorf("decode Helm release list: %w", err)
	}
	if len(releases) == 0 {
		return nil, nil
	}
	if len(releases) != 1 || releases[0].Name != step.HelmRelease() {
		return nil, fmt.Errorf("Helm release lookup was ambiguous")
	}
	return &releases[0], nil
}

func (executor HelmExecutor) runner() CommandRunner {
	if executor.Runner != nil {
		return executor.Runner
	}
	return ExecRunner{}
}

func (executor HelmExecutor) helmMajor(ctx context.Context) (int, error) {
	major := executor.HelmMajor
	if major == 0 {
		output, err := executor.runner().Run(ctx, "helm", "version", "--template", "{{.Version}}")
		if err != nil {
			return 0, fmt.Errorf("inspect Helm version: %w", err)
		}
		version := strings.TrimPrefix(strings.TrimSpace(string(output)), "v")
		component, _, _ := strings.Cut(version, ".")
		major, err = strconv.Atoi(component)
		if err != nil {
			return 0, fmt.Errorf("inspect Helm version: invalid version %q", strings.TrimSpace(string(output)))
		}
	}
	if major != 3 && major != 4 {
		return 0, fmt.Errorf("unsupported Helm major version %d; supported majors are 3 and 4", major)
	}
	return major, nil
}

func releaseDescription(step controlplane.PlanStep) string {
	return "platform-delivery/release=" + step.ReleaseDigest + ";config=" + step.ConfigDigest + ";chart=" + step.Chart.Digest
}

const releaseFenceMarker = ";fence="

func fencedReleaseDescription(step controlplane.PlanStep, fence ReleaseFence) string {
	return releaseDescription(step) + releaseFenceMarker + encodeReleaseFence(fence)
}

func releaseIdentity(description string) string {
	if index := strings.Index(description, releaseFenceMarker); index >= 0 {
		return description[:index]
	}
	return description
}

func encodeReleaseFence(fence ReleaseFence) string {
	return url.QueryEscape(fence.Generation) + "/" + strconv.Itoa(fence.Attempt) + "/" +
		url.QueryEscape(fence.Namespace) + "/" + url.QueryEscape(fence.Name)
}

func parseReleaseFence(description string) (ReleaseFence, bool) {
	index := strings.Index(description, releaseFenceMarker)
	if index < 0 {
		return ReleaseFence{}, false
	}
	parts := strings.Split(description[index+len(releaseFenceMarker):], "/")
	if len(parts) != 4 {
		return ReleaseFence{}, false
	}
	generation, err1 := url.QueryUnescape(parts[0])
	attempt, err2 := strconv.Atoi(parts[1])
	namespace, err3 := url.QueryUnescape(parts[2])
	name, err4 := url.QueryUnescape(parts[3])
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || attempt < 1 || name == "" {
		return ReleaseFence{}, false
	}
	return ReleaseFence{Generation: generation, Attempt: attempt, Namespace: namespace, Name: name}, true
}

func fenceMatches(recorded, live ReleaseFence) bool {
	return recorded.Attempt > 0 &&
		recorded.Generation == live.Generation &&
		recorded.Attempt == live.Attempt &&
		recorded.Namespace == live.Namespace &&
		recorded.Name == live.Name
}

func validateRenderedImages(rendered []byte, declared []controlplane.Artifact) error {
	allowed := make(map[string]struct{}, len(declared))
	seen := make(map[string]struct{}, len(declared))
	for _, artifact := range declared {
		allowed[artifact.Repository+":"+artifact.Tag+"@"+artifact.Digest] = struct{}{}
	}
	images, err := renderedImages(rendered)
	if err != nil {
		return fmt.Errorf("parse rendered workloads: %w", err)
	}
	for _, image := range images {
		if _, exists := allowed[image]; !exists {
			return fmt.Errorf("rendered workload uses undeclared or mutable image %q", image)
		}
		seen[image] = struct{}{}
	}
	for image := range allowed {
		if _, exists := seen[image]; !exists {
			return fmt.Errorf("declared image %q is absent from rendered workloads", image)
		}
	}
	return nil
}

func renderedImages(rendered []byte) ([]string, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(rendered))
	var images []string
	for {
		var document yaml.Node
		if err := decoder.Decode(&document); err != nil {
			if err == io.EOF {
				return images, nil
			}
			return nil, err
		}
		collectRenderedImages(&document, &images)
	}
}

func collectRenderedImages(node *yaml.Node, images *[]string) {
	if node.Kind == yaml.MappingNode {
		for index := 0; index+1 < len(node.Content); index += 2 {
			key, value := node.Content[index], node.Content[index+1]
			if key.Value == "image" {
				switch value.Kind {
				case yaml.ScalarNode:
					*images = append(*images, value.Value)
				case yaml.MappingNode:
					for child := 0; child+1 < len(value.Content); child += 2 {
						if value.Content[child].Value == "reference" && value.Content[child+1].Kind == yaml.ScalarNode {
							*images = append(*images, value.Content[child+1].Value)
						}
					}
				}
			}
			collectRenderedImages(value, images)
		}
		return
	}
	for _, child := range node.Content {
		collectRenderedImages(child, images)
	}
}

// postRenderArguments builds the flags that stamp Apollo workload metadata onto
// the objects this step installs.
//
// The spelling is version-dependent. Helm 3 takes an executable path; Helm 4
// resolves the value as the name of a postrenderer/v1 plugin, and passing it a
// path fails with "plugin: not found". Getting this wrong is not a graceful
// degradation -- it fails the install -- so the two are kept explicitly apart
// rather than sharing one flag value.
//
// It returns no arguments, rather than an error, when the metadata cannot be
// computed. Metadata is identification, not correctness: an installation that
// proceeds unlabelled is recoverable, while one that refuses because a label
// could not be derived is an outage caused by bookkeeping.
func (executor HelmExecutor) postRenderArguments(
	step controlplane.PlanStep, stagingDirectory string, helmMajor int,
) ([]string, error) {
	if strings.TrimSpace(executor.Environment) == "" {
		return nil, nil
	}
	metadata, err := workloadmetadata.ApolloWorkloadMetadataFor(
		executor.Environment, step.Entity, step.Product, step.Version)
	if err != nil {
		return nil, nil
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode Apollo workload metadata: %w", err)
	}
	metadataPath := filepath.Join(stagingDirectory, "apollo-workload-metadata.json")
	if err := os.WriteFile(metadataPath, encoded, 0o600); err != nil {
		return nil, fmt.Errorf("write Apollo workload metadata: %w", err)
	}
	if helmMajor >= 4 {
		plugin := executor.PostRendererPlugin
		if plugin == "" {
			plugin = PostRendererPluginName
		}
		// Installed here rather than at startup because the plugin must never
		// become visible to a Helm 3: that version rejects the postrenderer/v1
		// type and writes the failure to stdout, corrupting the JSON every
		// release lookup parses. By this point the major version is known.
		if executor.PostRendererPlugin == "" {
			root := executor.PluginRoot
			if root == "" {
				root = filepath.Join(os.TempDir(), "platform-helm-plugins")
			}
			if err := InstallPostRendererPlugin(root, executor.Version); err != nil {
				return nil, nil
			}
		}
		return []string{"--post-renderer", plugin, "--post-renderer-args", metadataPath}, nil
	}
	binary := executor.PostRenderer
	if binary == "" {
		resolved, err := os.Executable()
		if err != nil {
			return nil, nil
		}
		binary = resolved
	}
	return []string{
		"--post-renderer", binary,
		"--post-renderer-args", PostRenderCommand,
		"--post-renderer-args", metadataPath,
	}, nil
}
