package uds_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	// bundledDigest is the digest Zarf records for the package inside a
	// bundle. It is not the bundle's digest.
	bundledDigest = "sha256:" + strings.Repeat("b", 64)
)

// fakeRunner answers the first rule whose argv prefix matches, and records
// every call. Commands no rule answers act on a small model of the two things
// the executor reads back: Zarf's deployed-package Secret and the applied
// record ConfigMap. Anything else succeeds with "ok".
type fakeRunner struct {
	calls [][]string
	rules []rule
	// zarf is the base64 deployed-package record, empty when nothing is deployed.
	zarf string
	// applied is the applied record's JSON, empty when there is none.
	applied string
	// deployedVersion is the version a successful deploy records.
	deployedVersion string
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

func hasPrefix(call []string, prefix ...string) bool {
	return len(call) >= len(prefix) && reflect.DeepEqual(call[:len(prefix)], prefix)
}

func (f *fakeRunner) generation() int {
	if f.zarf == "" {
		return 0
	}
	raw, _ := base64.StdEncoding.DecodeString(f.zarf)
	var decoded struct {
		Generation int `json:"generation"`
	}
	_ = json.Unmarshal(raw, &decoded)
	return decoded.Generation
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	for _, candidate := range f.rules {
		if hasPrefix(call, candidate.prefix...) {
			return []byte(candidate.out), candidate.err
		}
	}
	switch {
	case hasPrefix(call, recordRead...):
		return []byte(f.zarf), nil
	case hasPrefix(call, "zarf", "package", "deploy"):
		// Zarf records the digest it deployed and adds one to the generation.
		digest := call[3][strings.LastIndex(call[3], "@")+1:]
		f.zarf = record(digest, f.deployedVersion, f.generation()+1, "Succeeded")
	case hasPrefix(call, "uds", "deploy"):
		f.zarf = record(bundledDigest, "9.9.9", f.generation()+1, "Succeeded")
	case hasPrefix(call, "zarf", "package", "remove"), hasPrefix(call, "uds", "remove"):
		f.zarf = ""
	case hasPrefix(call, appliedDelete...):
		f.applied = ""
	case hasPrefix(call, appliedCreatePrefix...):
		f.applied = strings.TrimPrefix(call[len(call)-1], "--from-literal=record=")
	case hasPrefix(call, appliedRead...):
		return []byte(f.applied), nil
	}
	return []byte("ok"), nil
}

var (
	stateProbe          = []string{"kubectl", "get", "secret", "zarf-state", "-n", "zarf", "--ignore-not-found", "-o", "name"}
	crdProbe            = []string{"kubectl", "get", "crd", "packages.uds.dev", "--ignore-not-found", "-o", "name"}
	recordRead          = []string{"kubectl", "get", "secret", "zarf-package-excalidraw", "-n", "zarf", "--ignore-not-found", "-o", "jsonpath={.data.data}"}
	appliedRead         = []string{"kubectl", "get", "configmap", "conduit-applied-excalidraw", "-n", "excalidraw", "--ignore-not-found", "-o", "jsonpath={.data.record}"}
	appliedDelete       = []string{"kubectl", "delete", "configmap", "conduit-applied-excalidraw", "-n", "excalidraw", "--ignore-not-found"}
	appliedCreatePrefix = []string{"kubectl", "create", "configmap", "conduit-applied-excalidraw", "-n", "excalidraw"}
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

// applied mirrors the executor's applied record, field for field and in order,
// so a test can state the exact document it expects to be written or read.
type applied struct {
	InstallationID string `json:"installationId"`
	ReleaseDigest  string `json:"releaseDigest"`
	Version        string `json:"version"`
	ConfigDigest   string `json:"configDigest"`
	ValuesDigest   string `json:"valuesDigest"`
	ArtifactKind   string `json:"artifactKind"`
	ArtifactDigest string `json:"artifactDigest"`
	ZarfPackage    string `json:"zarfPackage"`
	ZarfDigest     string `json:"zarfDigest"`
	ZarfGeneration int    `json:"zarfGeneration"`
}

func (a applied) json() string {
	raw, err := json.Marshal(a)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func valuesDigest(pairs ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(pairs, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// appliedFor is the record a successful deploy of step writes.
func appliedFor(step controlplane.PlanStep, zarfDigest string, generation int, pairs ...string) applied {
	return applied{
		InstallationID: step.InstallationID, ReleaseDigest: step.ReleaseDigest, Version: step.Version,
		ConfigDigest: step.ConfigDigest, ValuesDigest: valuesDigest(pairs...),
		ArtifactKind: step.Chart.Kind, ArtifactDigest: step.Chart.Digest,
		ZarfPackage: "excalidraw", ZarfDigest: zarfDigest, ZarfGeneration: generation,
	}
}

func appliedCreate(a applied) []string {
	return append(append([]string{}, appliedCreatePrefix...), "--from-literal=record="+a.json())
}

// initialised is a cluster with Zarf state and the given deployed package.
func initialised(prior string) *fakeRunner {
	return (&fakeRunner{zarf: prior, deployedVersion: "0.2.0"}).on("secret/zarf-state", nil, stateProbe...)
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
	// The source is the Release's repository at its digest: never the Product
	// name, never the Hub's Release digest, never a tag.
	assertCalls(t, runner, stateProbe, recordRead, verifiedDeploy(desiredDigest,
		"--set-variables", "ADMINS=a,b", "--set-variables", "DEBUG=false",
		"--set-variables", "DOMAIN=uds.dev", "--set-variables", "REPLICAS=3"),
		// Only after the deploy: read back what Zarf recorded, then record
		// what was applied, bound to that deployment.
		recordRead, appliedDelete,
		appliedCreate(appliedFor(step, desiredDigest, 1, "ADMINS=a,b", "DEBUG=false", "DOMAIN=uds.dev", "REPLICAS=3")))
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
	if deploy := runner.calls[2]; !reflect.DeepEqual(deploy, []string{
		"zarf", "package", "deploy", source(desiredDigest), "--confirm", "--verify=always", "--key", "/etc/conduit/cosign.pub",
	}) {
		t.Fatalf("deploy = %q", deploy)
	}
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
		"no namespace":         func(_ *uds.Executor, s *controlplane.PlanStep) { s.Namespace = "" },
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

// The refusal names both sides, and the same step is accepted once the
// artifact lists the architecture the Spoke deploys onto.
func TestApplyRefusesAnArtifactBuiltForAnotherArchitecture(t *testing.T) {
	t.Parallel()
	runner := initialised("")
	exec, step := executor(runner), installStep()
	exec.Architecture = "arm64"
	step.Chart.Architectures = []string{"amd64"}
	_, err := exec.Apply(context.Background(), controlplane.Plan{}, step)
	if !errors.Is(err, uds.ErrRefused) || !strings.Contains(err.Error(), "amd64") || !strings.Contains(err.Error(), "arm64") {
		t.Fatalf("Apply error = %v", err)
	}
	assertCalls(t, runner)

	step.Chart.Architectures = []string{"amd64", "arm64"}
	if _, err := exec.Apply(context.Background(), controlplane.Plan{}, step); err != nil {
		t.Fatalf("Apply for a supported architecture: %v", err)
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

	// The rollback was a deploy, so Zarf's generation moved past the one the
	// applied record describes. The record is now about nothing current.
	entity, err := exec.Observe(context.Background(), step)
	if err != nil {
		t.Fatalf("Observe after rollback: %v", err)
	}
	if entity.ConfigStatus != controlplane.ConfigIndeterminate || entity.ReleaseDigest != "" || entity.ConfigDigest != "" {
		t.Fatalf("after a rollback the stale record was still believed: %#v", entity)
	}
}

func TestRollbackWithoutPriorRemovesByPackageName(t *testing.T) {
	t.Parallel()
	runner := initialised("")
	exec := executor(runner)
	receipt, err := exec.Apply(context.Background(), controlplane.Plan{}, installStep())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	runner.calls = nil
	if err := exec.Rollback(context.Background(), receipt); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	assertCalls(t, runner, recordRead, appliedDelete, []string{"zarf", "package", "remove", "excalidraw", "--confirm"})
	if runner.applied != "" {
		t.Fatal("the applied record outlived the deployment it described")
	}
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
	if runner.applied != "" {
		t.Fatal("a failed deploy wrote an applied record")
	}
}

// A deploy that "succeeds" while Zarf records some other digest -- the
// platform manifest behind a multi-architecture index, say -- did not deploy
// the artifact the Plan reviewed.
func TestApplyFailsWhenZarfRecordsADifferentDigest(t *testing.T) {
	t.Parallel()
	runner := initialised("")
	runner.on(record(priorDigest, "0.2.0", 1, "Succeeded"), nil, recordRead...)
	_, err := executor(runner).Apply(context.Background(), controlplane.Plan{}, installStep())
	if err == nil || !strings.Contains(err.Error(), "not the artifact digest") {
		t.Fatalf("Apply error = %v", err)
	}
	if runner.applied != "" {
		t.Fatal("an applied record was written for a digest the Plan never named")
	}
}

// If the record cannot be written the step fails and is rolled back: a
// deployment Conduit cannot account for must not look like a success.
func TestApplyFailsAndRollsBackWhenTheRecordCannotBeWritten(t *testing.T) {
	t.Parallel()
	runner := initialised("").on("forbidden", errors.New("exit status 1"), appliedCreatePrefix...)
	_, err := executor(runner).Apply(context.Background(), controlplane.Plan{}, installStep())
	if err == nil || !strings.Contains(err.Error(), "record what was applied") {
		t.Fatalf("Apply error = %v", err)
	}
	if runner.zarf != "" {
		t.Fatal("the deployment was left in place without a record")
	}
}

func TestObserveReportsDeployedNotDesired(t *testing.T) {
	t.Parallel()
	runner := initialised(record(priorDigest, "0.1.0", 4, "Succeeded"))
	entity, err := executor(runner).Observe(context.Background(), installStep())
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	assertCalls(t, runner, recordRead, appliedRead)
	// A package Conduit has no record of deploying: nothing is claimed for it.
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

// The property the Hub's success check rests on: after a real Apply, Observe
// reports the Release and configuration from the record Apply wrote.
func TestObserveAfterApplyReportsWhatWasApplied(t *testing.T) {
	t.Parallel()
	runner := initialised("")
	exec, step := executor(runner), installStep()
	step.Values = json.RawMessage(`{"REPLICAS":3}`)
	if _, err := exec.Apply(context.Background(), controlplane.Plan{}, step); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	entity, err := exec.Observe(context.Background(), step)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	want := controlplane.ReportedEntity{
		InstallationID: step.InstallationID, Product: step.Product, Version: "0.2.0",
		ReleaseDigest: releaseDigest, Namespace: "excalidraw", ConfigDigest: configDigest,
		Health: controlplane.HealthHealthy, RolloutStatus: controlplane.RolloutUpToDate,
		ConfigStatus: controlplane.ConfigApplied, Lifecycle: controlplane.LifecycleInstalled, ObservedAt: entity.ObservedAt,
	}
	if !reflect.DeepEqual(entity, want) {
		t.Fatalf("entity:\n got %#v\nwant %#v", entity, want)
	}
}

// APPLIED is a read of the record, never an echo of the step. Each case leaves
// a correctly deployed package and a step that asks for exactly what is
// deployed, and changes only the record. An executor that echoed the step
// would report APPLIED, UP_TO_DATE and the step's own digests in every one.
func TestObserveNeverEchoesTheStep(t *testing.T) {
	t.Parallel()
	step := installStep()
	good := appliedFor(step, desiredDigest, 5)
	cases := map[string]struct {
		record                        func(applied) string
		config, release, configDigest string
	}{
		"no record": {func(applied) string { return "" }, controlplane.ConfigIndeterminate, "", ""},
		"record of an earlier generation": {
			func(a applied) string { a.ZarfGeneration = 4; return a.json() }, controlplane.ConfigIndeterminate, "", "",
		},
		"record of another digest": {
			func(a applied) string { a.ZarfDigest = priorDigest; return a.json() }, controlplane.ConfigIndeterminate, "", "",
		},
		"record of another package": {
			func(a applied) string { a.ZarfPackage = "other"; return a.json() }, controlplane.ConfigIndeterminate, "", "",
		},
		"another installation's record": {
			func(a applied) string { a.InstallationID = "inst-other"; return a.json() }, controlplane.ConfigIndeterminate, "", "",
		},
		// The record is current, so its contents are reported -- and they are
		// the record's, not the step's.
		"record of other values": {
			func(a applied) string { a.ValuesDigest = valuesDigest("REPLICAS=9"); return a.json() },
			controlplane.ConfigDrifted, releaseDigest, configDigest,
		},
		"record of another configuration": {
			func(a applied) string { a.ConfigDigest = priorDigest; return a.json() },
			controlplane.ConfigDrifted, releaseDigest, priorDigest,
		},
		"record of another Release": {
			func(a applied) string { a.ReleaseDigest = priorDigest; return a.json() },
			controlplane.ConfigApplied, priorDigest, configDigest,
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runner := initialised(record(desiredDigest, "0.2.0", 5, "Succeeded"))
			runner.applied = testCase.record(good)
			entity, err := executor(runner).Observe(context.Background(), step)
			if err != nil {
				t.Fatalf("Observe: %v", err)
			}
			if entity.ConfigStatus != testCase.config || entity.ReleaseDigest != testCase.release || entity.ConfigDigest != testCase.configDigest {
				t.Fatalf("config=%s release=%q configDigest=%q, want %s %q %q",
					entity.ConfigStatus, entity.ReleaseDigest, entity.ConfigDigest, testCase.config, testCase.release, testCase.configDigest)
			}
			// What the Hub accepts as success is APPLIED, UP_TO_DATE and both
			// digests equal to the step's. No case here may produce all four.
			if entity.ConfigStatus == controlplane.ConfigApplied && entity.RolloutStatus == controlplane.RolloutUpToDate &&
				entity.ReleaseDigest == step.ReleaseDigest && entity.ConfigDigest == step.ConfigDigest {
				t.Fatalf("a record that does not say so produced a reportable success: %#v", entity)
			}
		})
	}

	t.Run("an unreadable record is an error, not an absence", func(t *testing.T) {
		t.Parallel()
		runner := initialised(record(desiredDigest, "0.2.0", 5, "Succeeded"))
		runner.applied = "{not json"
		if _, err := executor(runner).Observe(context.Background(), step); err == nil {
			t.Fatal("Observe reported an entity from a record it could not parse")
		}
	})
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
			step := installStep()
			runner := initialised(record(desiredDigest, "0.2.0", 5, tc.statuses...))
			runner.applied = appliedFor(step, desiredDigest, 5).json()
			entity, err := executor(runner).Observe(context.Background(), step)
			if err != nil {
				t.Fatalf("Observe: %v", err)
			}
			if entity.Health != tc.health || entity.RolloutStatus != tc.rollout || entity.ReleaseDigest != releaseDigest ||
				entity.Lifecycle != controlplane.LifecycleInstalled || entity.ConfigStatus != controlplane.ConfigApplied {
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
	assertCalls(t, runner, recordRead, crRead, appliedRead)
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
	runner.applied = appliedFor(step, priorDigest, 4).json()
	exec := executor(runner)
	receipt, err := exec.Apply(context.Background(), controlplane.Plan{}, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	assertCalls(t, runner, stateProbe, recordRead, appliedDelete, []string{"zarf", "package", "remove", "excalidraw", "--confirm"})
	if runner.applied != "" {
		t.Fatal("the applied record outlived the package it described")
	}

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
	exec := bundleExecutor(runner)
	if _, err := exec.Apply(context.Background(), controlplane.Plan{}, step); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// The kind on the step selected `uds`; nothing on the executor did.
	assertCalls(t, runner, stateProbe, crdProbe, recordRead, inspect, []string{
		"uds", "deploy", source(desiredDigest), "--confirm", "--set", "REPLICAS=2", "--set", "excalidraw.DOMAIN=uds.dev",
	}, recordRead, appliedDelete,
		// The record keeps the bundle's digest beside the digest Zarf recorded
		// for the bundled package. They are different things and stay so.
		appliedCreate(appliedFor(step, bundledDigest, 1, "REPLICAS=2", "excalidraw.DOMAIN=uds.dev")))

	// A bundle's digest cannot be read back from the cluster. It is reported
	// from the record of the deploy, while the bundled package is unchanged.
	entity, err := exec.Observe(context.Background(), step)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if entity.ReleaseDigest != releaseDigest || entity.Version != "0.2.0" || entity.ConfigDigest != configDigest ||
		entity.ConfigStatus != controlplane.ConfigApplied || entity.RolloutStatus != controlplane.RolloutUpToDate {
		t.Fatalf("bundle entity = %#v", entity)
	}

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
	assertCalls(t, runner, stateProbe, recordRead, appliedDelete, []string{"uds", "remove", source(desiredDigest), "--confirm"})
}

func TestBundleObserveDoesNotBorrowThePackageDigest(t *testing.T) {
	t.Parallel()
	// The bundled package happens to carry the very digest the step names.
	// With no applied record that coincidence must count for nothing.
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
