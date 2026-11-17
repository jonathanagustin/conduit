package uds_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/agent"
	"github.com/jonathanagustin/conduit/internal/agent/adapters/uds"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

const (
	issuer   = `^https://token\.actions\.githubusercontent\.com$`
	identity = `^https://github\.com/example/app/\.github/workflows/release\.yml@refs/tags/v.*$`
)

var (
	desiredDigest = "sha256:" + strings.Repeat("c", 64)
	priorDigest   = "sha256:" + strings.Repeat("a", 64)
	configDigest  = "sha256:" + strings.Repeat("d", 64)
	// releaseDigest is the Hub's digest of the Release document. It is not an
	// OCI digest and never appears in a registry reference.
	releaseDigest = "sha256:" + strings.Repeat("e", 64)
)

// fakeRunner answers the first rule whose argv prefix matches, and records
// every call. Unmatched commands succeed with "ok".
type fakeRunner struct {
	calls [][]string
	rules []rule
}

type rule struct {
	prefix []string
	out    string
	err    error
}

func (f *fakeRunner) on(out string, err error, prefix ...string) *fakeRunner {
	f.rules = append(f.rules, rule{prefix: prefix, out: out, err: err})
	return f
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	for _, candidate := range f.rules {
		if len(call) >= len(candidate.prefix) && reflect.DeepEqual(call[:len(candidate.prefix)], candidate.prefix) {
			return []byte(candidate.out), candidate.err
		}
	}
	return []byte("ok"), nil
}

var (
	stateProbe = []string{"kubectl", "get", "secret", "zarf-state", "-n", "zarf", "--ignore-not-found", "-o", "name"}
	crdProbe   = []string{"kubectl", "get", "crd", "packages.uds.dev", "--ignore-not-found", "-o", "name"}
	recordRead = []string{"kubectl", "get", "secret", "zarf-package-excalidraw", "-n", "zarf", "--ignore-not-found", "-o", "jsonpath={.data.data}"}
)

// record renders the Secret payload zarf v0.82.0 writes for a deployed package.
func record(digest, version string, generation int, statuses ...string) string {
	components := []map[string]any{}
	for index, status := range statuses {
		components = append(components, map[string]any{"name": "c" + string(rune('0'+index)), "status": status})
	}
	raw, err := json.Marshal(map[string]any{
		"name": "excalidraw", "digest": digest, "generation": generation,
		"data":               map[string]any{"metadata": map[string]any{"name": "excalidraw", "version": version}},
		"deployedComponents": components,
	})
	if err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func initialised(prior string) *fakeRunner {
	return (&fakeRunner{}).on("secret/zarf-state", nil, stateProbe...).on(prior, nil, recordRead...)
}

func executor(runner *fakeRunner) uds.Executor {
	return uds.Executor{
		Runner:       runner,
		Architecture: "amd64",
		Publisher:    domain.PublisherPolicy{CertificateIssuer: issuer, CertificateIdentity: identity},
	}
}

func installStep() controlplane.PlanStep {
	return controlplane.PlanStep{
		Action:         "install",
		InstallationID: "inst-7f3a",
		Product:        "example:excalidraw",
		Version:        "0.2.0",
		ReleaseDigest:  releaseDigest,
		Namespace:      "excalidraw",
		ConfigDigest:   configDigest,
		Chart: controlplane.Artifact{
			Kind: controlplane.ArtifactKindZarfPackage, Repository: "ghcr.io/example/excalidraw",
			Digest: desiredDigest, Architectures: []string{"amd64", "arm64"}, PackageName: "excalidraw",
		},
	}
}

func bundleStep() controlplane.PlanStep {
	step := installStep()
	step.Chart.Kind = controlplane.ArtifactKindUDSBundle
	return step
}

func source(digest string) string { return "oci://ghcr.io/example/excalidraw@" + digest }

func verifiedDeploy(digest string, extra ...string) []string {
	return append([]string{
		"zarf", "package", "deploy", source(digest), "--confirm", "--verify=always",
		"--certificate-identity-regexp", identity, "--certificate-oidc-issuer-regexp", issuer,
	}, extra...)
}

func assertCalls(t *testing.T, runner *fakeRunner, want ...[]string) {
	t.Helper()
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("commands:\n got %q\nwant %q", runner.calls, want)
	}
}

func TestApplyDeploysWithVerificationAndReviewedValues(t *testing.T) {
	t.Parallel()
	runner := initialised("")
	step := installStep()
	step.Values = json.RawMessage(`{"REPLICAS":3,"DOMAIN":"uds.dev","DEBUG":false,"ADMINS":"a,b"}`)

	receipt, err := executor(runner).Apply(context.Background(), controlplane.Plan{}, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	assertCalls(t, runner, stateProbe, recordRead, verifiedDeploy(desiredDigest,
		"--set-variables", "ADMINS=a,b", "--set-variables", "DEBUG=false",
		"--set-variables", "DOMAIN=uds.dev", "--set-variables", "REPLICAS=3"))
	if receipt.PreviousRevision != 0 || receipt.Fence.Name != "excalidraw" || receipt.Fence.Generation != "" {
		t.Fatalf("receipt for a first install = %#v", receipt)
	}
}

func TestApplyDeploysWithKey(t *testing.T) {
	t.Parallel()
	runner := initialised("")
	exec := executor(runner)
	exec.Publisher = domain.PublisherPolicy{}
	exec.VerificationKeyPath = "/etc/conduit/cosign.pub"
	if _, err := exec.Apply(context.Background(), controlplane.Plan{}, installStep()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	assertCalls(t, runner, stateProbe, recordRead, []string{
		"zarf", "package", "deploy", source(desiredDigest), "--confirm", "--verify=always", "--key", "/etc/conduit/cosign.pub",
	})
}

func TestApplyRefusesBeforeRunningAnything(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*uds.Executor, *controlplane.PlanStep){
		"no verification material": func(e *uds.Executor, _ *controlplane.PlanStep) { e.Publisher = domain.PublisherPolicy{} },
		"issuer without identity":  func(e *uds.Executor, _ *controlplane.PlanStep) { e.Publisher.CertificateIdentity = "" },
		"key and policy together":  func(e *uds.Executor, _ *controlplane.PlanStep) { e.VerificationKeyPath = "/k.pub" },
		"nested value": func(_ *uds.Executor, s *controlplane.PlanStep) {
			s.Values = json.RawMessage(`{"INGRESS":{"host":"a"}}`)
		},
		"list value":            func(_ *uds.Executor, s *controlplane.PlanStep) { s.Values = json.RawMessage(`{"HOSTS":["a"]}`) },
		"null value":            func(_ *uds.Executor, s *controlplane.PlanStep) { s.Values = json.RawMessage(`{"HOST":null}`) },
		"non-variable key":      func(_ *uds.Executor, s *controlplane.PlanStep) { s.Values = json.RawMessage(`{"image.tag":"1"}`) },
		"value the CLI resplit": func(_ *uds.Executor, s *controlplane.PlanStep) { s.Values = json.RawMessage(`{"OPTS":"a=b,c=d"}`) },
		"values not an object":  func(_ *uds.Executor, s *controlplane.PlanStep) { s.Values = json.RawMessage(`[1]`) },
		"registry access": func(_ *uds.Executor, s *controlplane.PlanStep) {
			s.RegistryAccess = make([]controlplane.RegistryAccess, 1)
		},
		"no package name":      func(_ *uds.Executor, s *controlplane.PlanStep) { s.Chart.PackageName = "" },
		"invalid package name": func(_ *uds.Executor, s *controlplane.PlanStep) { s.Chart.PackageName = "Inst_7F3A" },
		"a Helm chart":         func(_ *uds.Executor, s *controlplane.PlanStep) { s.Chart.Kind = "" },
		"an unknown kind":      func(_ *uds.Executor, s *controlplane.PlanStep) { s.Chart.Kind = "oci-thing" },
		"tag-only reference": func(_ *uds.Executor, s *controlplane.PlanStep) {
			s.Chart.Digest, s.Chart.Tag = "", "0.2.0"
		},
		"tag beside the digest":     func(_ *uds.Executor, s *controlplane.PlanStep) { s.Chart.Tag = "0.2.0" },
		"tag inside the repository": func(_ *uds.Executor, s *controlplane.PlanStep) { s.Chart.Repository += ":0.2.0" },
		"no repository":             func(_ *uds.Executor, s *controlplane.PlanStep) { s.Chart.Repository = " " },
		"architecture not built": func(_ *uds.Executor, s *controlplane.PlanStep) {
			s.Chart.Architectures = []string{"arm64"}
		},
		"bundle with keyless policy only": func(_ *uds.Executor, s *controlplane.PlanStep) {
			s.Chart.Kind = controlplane.ArtifactKindUDSBundle
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runner := initialised("")
			exec, step := executor(runner), installStep()
			mutate(&exec, &step)
			_, err := exec.Apply(context.Background(), controlplane.Plan{}, step)
			var diagnostic agent.DiagnosticError
			if !errors.Is(err, uds.ErrRefused) || !errors.As(err, &diagnostic) {
				t.Fatalf("Apply error = %v, want a diagnostic refusal", err)
			}
			assertCalls(t, runner)
		})
	}
}

func TestApplyPreflightBlocks(t *testing.T) {
	t.Parallel()
	t.Run("zarf not initialised", func(t *testing.T) {
		t.Parallel()
		runner := (&fakeRunner{}).on("", nil, stateProbe...)
		_, err := executor(runner).Apply(context.Background(), controlplane.Plan{}, installStep())
		if !errors.Is(err, uds.ErrPreflightBlocked) || !errors.Is(err, uds.ErrZarfNotInitialised) || errors.Is(err, uds.ErrUDSCRDsAbsent) {
			t.Fatalf("Apply error = %v", err)
		}
		assertCalls(t, runner, stateProbe)
	})
	t.Run("UDS CRDs absent", func(t *testing.T) {
		t.Parallel()
		runner := initialised("").on("", nil, crdProbe...)
		exec := executor(runner)
		exec.PackageManifestPath = "/tmp/uds-package.yaml"
		_, err := exec.Apply(context.Background(), controlplane.Plan{}, installStep())
		if !errors.Is(err, uds.ErrPreflightBlocked) || !errors.Is(err, uds.ErrUDSCRDsAbsent) || errors.Is(err, uds.ErrZarfNotInitialised) {
			t.Fatalf("Apply error = %v", err)
		}
		assertCalls(t, runner, stateProbe, crdProbe)
	})
}

func TestApplyRecordsPriorDeploymentAndRollbackRedeploysIt(t *testing.T) {
	t.Parallel()
	runner := initialised(record(priorDigest, "0.1.0", 4, "Succeeded"))
	exec := executor(runner)
	step := installStep()
	step.Values = json.RawMessage(`{"REPLICAS":3}`)
	receipt, err := exec.Apply(context.Background(), controlplane.Plan{}, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if receipt.PreviousRevision != 4 || receipt.Fence.Generation != priorDigest {
		t.Fatalf("receipt = %#v, want the prior generation and digest", receipt)
	}

	runner.calls = nil
	if err := exec.Rollback(context.Background(), receipt); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	assertCalls(t, runner, verifiedDeploy(priorDigest))
}

func TestRollbackWithoutPriorRemovesByPackageName(t *testing.T) {
	t.Parallel()
	runner := initialised("")
	exec := executor(runner)
	receipt, err := exec.Apply(context.Background(), controlplane.Plan{}, installStep())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// The deploy landed, so the record now exists.
	runner.rules = nil
	runner.on(record(desiredDigest, "0.2.0", 1, "Succeeded"), nil, recordRead...)
	runner.calls = nil
	if err := exec.Rollback(context.Background(), receipt); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	assertCalls(t, runner, recordRead, []string{"zarf", "package", "remove", "excalidraw", "--confirm"})
}

func TestRollbackNeverRemovesWhatItCannotRestore(t *testing.T) {
	t.Parallel()
	cases := map[string]agent.Receipt{
		"prior without digest": {Step: installStep(), PreviousRevision: 2, Fence: agent.ReleaseFence{Name: "excalidraw"}},
		"foreign receipt":      {Step: installStep(), PreviousRevision: 0},
	}
	for name, receipt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runner := initialised(record(priorDigest, "0.1.0", 2, "Succeeded"))
			if err := executor(runner).Rollback(context.Background(), receipt); !errors.Is(err, uds.ErrRefused) {
				t.Fatalf("Rollback error = %v, want a refusal", err)
			}
			assertCalls(t, runner)
		})
	}
}

func TestFailedUpgradeRedeploysPriorInsteadOfRemoving(t *testing.T) {
	t.Parallel()
	runner := initialised(record(priorDigest, "0.1.0", 4, "Succeeded")).
		on("helm: timed out", errors.New("exit status 1"), "zarf", "package", "deploy", source(desiredDigest))
	_, err := executor(runner).Apply(context.Background(), controlplane.Plan{}, installStep())
	var diagnostic agent.DiagnosticError
	if !errors.As(err, &diagnostic) || diagnostic.Output != "helm: timed out" {
		t.Fatalf("Apply error = %v", err)
	}
	assertCalls(t, runner, stateProbe, recordRead, verifiedDeploy(desiredDigest), verifiedDeploy(priorDigest))
}

func TestObserveReportsDeployedNotDesired(t *testing.T) {
	t.Parallel()
	runner := initialised(record(priorDigest, "0.1.0", 4, "Succeeded"))
	entity, err := executor(runner).Observe(context.Background(), installStep())
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertCalls(t, runner, recordRead)
	// A different artifact is deployed, so no Release digest is claimed for it.
	if entity.ReleaseDigest != "" || entity.Version != "0.1.0" {
		t.Fatalf("reported %q %s, want no Release digest and the deployed version 0.1.0", entity.ReleaseDigest, entity.Version)
	}
	if entity.RolloutStatus != controlplane.RolloutOutOfDate || entity.Health != controlplane.HealthHealthy {
		t.Fatalf("rollout=%s health=%s", entity.RolloutStatus, entity.Health)
	}
	if entity.ConfigStatus != controlplane.ConfigIndeterminate || entity.ConfigDigest != "" {
		t.Fatalf("config=%s digest=%q: configuration was never read back", entity.ConfigStatus, entity.ConfigDigest)
	}
}

func TestObserveMatchingDeployment(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		statuses        []string
		health, rollout string
	}{
		"succeeded": {[]string{"Succeeded", "Succeeded"}, controlplane.HealthHealthy, controlplane.RolloutUpToDate},
		"deploying": {[]string{"Succeeded", "Deploying"}, controlplane.HealthWarning, controlplane.RolloutInGracePeriod},
		"failed":    {[]string{"Succeeded", "Failed"}, controlplane.HealthTerminal, controlplane.RolloutBlocked},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runner := initialised(record(desiredDigest, "0.2.0", 5, tc.statuses...))
			entity, err := executor(runner).Observe(context.Background(), installStep())
			if err != nil {
				t.Fatalf("Observe: %v", err)
			}
			if entity.Health != tc.health || entity.RolloutStatus != tc.rollout || entity.ReleaseDigest != releaseDigest ||
				entity.Lifecycle != controlplane.LifecycleInstalled || entity.ConfigStatus != controlplane.ConfigIndeterminate {
				t.Fatalf("entity = %#v", entity)
			}
		})
	}
}

func TestObserveReadsUDSPackageByNameNotInstallationID(t *testing.T) {
	t.Parallel()
	crRead := []string{"kubectl", "get", "package.uds.dev", "excalidraw", "-n", "excalidraw", "-o", "yaml"}
	runner := initialised(record(desiredDigest, "0.2.0", 5, "Succeeded")).on(`apiVersion: uds.dev/v1alpha1
kind: Package
metadata:
  name: excalidraw
  namespace: excalidraw
  generation: 2
status:
  phase: Pending
  observedGeneration: 1
`, nil, crRead...)
	exec := executor(runner)
	exec.PackageManifestPath = "/tmp/uds-package.yaml"
	entity, err := exec.Observe(context.Background(), installStep())
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertCalls(t, runner, recordRead, crRead)
	if entity.Health != controlplane.HealthWarning || entity.RolloutStatus != controlplane.RolloutInGracePeriod {
		t.Fatalf("health=%s rollout=%s for an unreconciled UDS Package", entity.Health, entity.RolloutStatus)
	}
}

func TestObserveAbsentPackage(t *testing.T) {
	t.Parallel()
	if _, err := executor(initialised("")).Observe(context.Background(), installStep()); err == nil {
		t.Fatal("Observe reported an entity for a package Zarf has no record of")
	}
	step := installStep()
	step.Action = "uninstall"
	entity, err := executor(initialised("")).Observe(context.Background(), step)
	if err != nil || entity.Lifecycle != controlplane.LifecycleUninstalled {
		t.Fatalf("Observe after uninstall = %#v, %v", entity, err)
	}
	if _, err := executor(initialised(record(priorDigest, "0.1.0", 4, "Succeeded"))).Observe(context.Background(), step); err == nil {
		t.Fatal("Observe reported an uninstall while the package is still deployed")
	}
}

func TestUninstallRemovesZarfPackageByName(t *testing.T) {
	t.Parallel()
	step := installStep()
	step.Action = "uninstall"
	runner := initialised(record(priorDigest, "0.1.0", 4, "Succeeded"))
	exec := executor(runner)
	receipt, err := exec.Apply(context.Background(), controlplane.Plan{}, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	assertCalls(t, runner, stateProbe, recordRead, []string{"zarf", "package", "remove", "excalidraw", "--confirm"})

	// Rolling an uninstall back restores the package that was removed.
	runner.calls = nil
	if err := exec.Rollback(context.Background(), receipt); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	assertCalls(t, runner, verifiedDeploy(priorDigest))
}

func bundleExecutor(runner *fakeRunner) uds.Executor {
	exec := executor(runner)
	exec.Publisher = domain.PublisherPolicy{}
	exec.VerificationKeyPath = "/etc/conduit/bundle.pub"
	return exec
}

func TestBundleDeployVerifiesThenDeploys(t *testing.T) {
	t.Parallel()
	inspect := []string{"uds", "inspect", source(desiredDigest), "--key", "/etc/conduit/bundle.pub"}
	step := bundleStep()
	step.Values = json.RawMessage(`{"excalidraw.DOMAIN":"uds.dev","REPLICAS":2}`)

	runner := initialised("").on("packages.uds.dev", nil, crdProbe...)
	if _, err := bundleExecutor(runner).Apply(context.Background(), controlplane.Plan{}, step); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	assertCalls(t, runner, stateProbe, crdProbe, recordRead, inspect, []string{
		"uds", "deploy", source(desiredDigest), "--confirm", "--set", "REPLICAS=2", "--set", "excalidraw.DOMAIN=uds.dev",
	})

	rejected := initialised("").on("packages.uds.dev", nil, crdProbe...).on("bad signature", errors.New("exit status 1"), inspect...)
	if _, err := bundleExecutor(rejected).Apply(context.Background(), controlplane.Plan{}, step); err == nil {
		t.Fatal("Apply deployed a bundle whose signature did not verify")
	}
	assertCalls(t, rejected, stateProbe, crdProbe, recordRead, inspect)
}

func TestBundleUninstallUsesUDSRemove(t *testing.T) {
	t.Parallel()
	step := bundleStep()
	step.Action = "uninstall"
	runner := initialised(record(priorDigest, "0.1.0", 4, "Succeeded"))
	if _, err := bundleExecutor(runner).Apply(context.Background(), controlplane.Plan{}, step); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	assertCalls(t, runner, stateProbe, recordRead, []string{"uds", "remove", source(desiredDigest), "--confirm"})
}

func TestBundleObserveDoesNotBorrowThePackageDigest(t *testing.T) {
	t.Parallel()
	runner := initialised(record(desiredDigest, "0.2.0", 5, "Succeeded"))
	entity, err := bundleExecutor(runner).Observe(context.Background(), bundleStep())
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if entity.ReleaseDigest != "" || entity.RolloutStatus != controlplane.RolloutOutOfDate {
		t.Fatalf("digest=%q rollout=%s: a bundle digest cannot be read back", entity.ReleaseDigest, entity.RolloutStatus)
	}
}

func TestBundleRollbackLeavesPriorDeploymentInPlace(t *testing.T) {
	t.Parallel()
	runner := initialised(record(priorDigest, "0.1.0", 4, "Succeeded")).on("packages.uds.dev", nil, crdProbe...)
	exec := bundleExecutor(runner)
	receipt, err := exec.Apply(context.Background(), controlplane.Plan{}, bundleStep())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	runner.calls = nil
	if err := exec.Rollback(context.Background(), receipt); !errors.Is(err, uds.ErrRefused) {
		t.Fatalf("Rollback error = %v, want a refusal", err)
	}
	assertCalls(t, runner)
}
