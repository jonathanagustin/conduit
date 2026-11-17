// Package uds implements the Spoke agent.Executor port for Environments running
// UDS Core and Zarf (#427).
//
// It deploys Zarf packages and UDS bundles through the pinned zarf/uds CLIs and
// reports what the cluster holds, read from Zarf's own deployed-package record,
// without leaking Zarf or UDS types into the domain packages.
//
// The executor fails closed. It runs nothing unless it can express signature
// verification for the artifact, it refuses reviewed inputs it cannot carry to
// the CLI rather than dropping them, and it never reports a desired value as an
// observed one.
//
// Everything about the artifact comes from the step: its kind (a Zarf package
// or a UDS bundle), its OCI repository and digest, the architectures it was
// built for, and the Zarf package name it deploys as (#833). The source is
// always oci://<repository>@<digest>. What was applied -- the Release, the
// values, and for a bundle the bundle itself -- is reported from the record
// described in applied.go, never from the step.
//
// CLI contracts (reviewed 2026-11-16). "VERIFIED" means read from the pinned
// binary's --help or the pinned release's source; "ASSUMED" means the flag
// exists but the behaviour was not exercised against a cluster.
//
//   - zarf v0.82.0 `package deploy SOURCE --confirm --verify=always
//     [--key PATH | --certificate-identity-regexp R --certificate-oidc-issuer-regexp R]
//     [--set-variables KEY=value]...`: VERIFIED (--help).
//   - zarf v0.82.0 `package verify SOURCE` accepts the same key/identity/issuer
//     flags without --verify; a supplied key rejects an unsigned package:
//     VERIFIED (--help and TestPinnedCLIRejectsUntrustedBundleAndIsolatesConfiguration).
//   - zarf v0.82.0 `package remove NAME --confirm`, NAME as recorded in the
//     cluster: VERIFIED (--help).
//   - zarf v0.82.0 records each deployment as Secret `zarf-package-<name>` in
//     namespace `zarf`, key `data`, JSON with `digest`, `generation`,
//     `data.metadata.version`, `deployedComponents[].status`: VERIFIED
//     (src/pkg/state/state.go, src/pkg/cluster/zarf.go). `zarf package list -o
//     json` omits the digest (src/cmd/package.go), so it is not used.
//   - The `digest` zarf v0.82.0 records is the OCI manifest digest it resolved
//     for the cluster's platform, and `generation` is the previous generation
//     plus one on every deploy: VERIFIED (src/pkg/packager/pull.go
//     SetRegistryDigest, src/pkg/packager/deploy.go, read 2026-11-17).
//   - zarf v0.82.0 `package deploy oci://REPOSITORY@sha256:DIGEST`, a reference
//     with a digest and no tag, resolves and deploys that manifest: ASSUMED.
//   - zarf init creates Secret `zarf-state` in namespace `zarf`: VERIFIED
//     (src/pkg/state/state.go constants).
//   - `kubectl create configmap NAME --from-literal=KEY=VALUE` stores VALUE
//     verbatim, splitting on the first "=" only, and `kubectl delete configmap
//     NAME --ignore-not-found` exits zero when it is absent: ASSUMED (standard
//     kubectl, not exercised here).
//   - uds v0.41.0 `deploy SOURCE --confirm [--set KEY=value]...` and
//     `remove SOURCE --confirm`: VERIFIED (--help). `uds deploy` has no
//     verification flags at all.
//   - uds v0.41.0 `inspect SOURCE --key PATH` accepts a matching ECDSA signature
//     and rejects a wrong key or unsigned bundle: VERIFIED on a local generated
//     archive (TestPinnedCLIRejectsUntrustedBundleAndIsolatesConfiguration).
//   - `kubectl get ... --ignore-not-found` prints nothing and exits zero for an
//     absent object: ASSUMED (standard kubectl, not exercised here).
//   - UDS Core's Package CRD is named `packages.uds.dev`: ASSUMED from the
//     upstream file name cited in docs/packaging-assessment.md.
package uds

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/agent"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/udsconverge"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

var (
	// ErrRefused marks a step the executor declined before running anything.
	ErrRefused = errors.New("uds executor refused the step")
	// ErrPreflightBlocked marks a cluster that is not ready for Zarf or UDS
	// work. It wraps one of the two more specific errors below.
	ErrPreflightBlocked = errors.New("uds executor preflight blocked")
	// ErrZarfNotInitialised means the cluster carries no Zarf state.
	ErrZarfNotInitialised = errors.New("zarf is not initialised in this cluster")
	// ErrUDSCRDsAbsent means the UDS Core Package CRD is not installed.
	ErrUDSCRDsAbsent = errors.New("UDS Core CRDs are absent from this cluster")
)

const (
	zarfNamespace     = "zarf"
	zarfStateSecret   = "zarf-state"
	zarfPackagePrefix = "zarf-package-"
	udsPackageCRD     = "packages.uds.dev"
	actionUninstall   = "uninstall"
)

// Zarf's own rule for a deployed package name (state.DeployedPackageNameRegex).
var zarfPackageName = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]*$`)

var ociDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// Zarf upper-cases variable names, so two keys differing only in case would
// collapse into one. Only the canonical form is accepted.
var zarfVariableName = regexp.MustCompile(`^[A-Z0-9_]+$`)

// A bundle variable may be scoped to one package of the bundle.
var bundleVariableName = regexp.MustCompile(`^([a-z0-9][a-z0-9\-]*\.)?[A-Z0-9_]+$`)

// Executor implements agent.Executor for Zarf packages and UDS bundles.
type Executor struct {
	// Runner is an explicitly trusted command port. The default runner isolates
	// CLI configuration and credentials; injected implementations must preserve
	// that boundary themselves.
	Runner     agent.CommandRunner
	ZarfBin    string
	UDSBin     string
	KubectlBin string
	// Architecture is the cluster architecture this Spoke deploys onto. It
	// defaults to the architecture of the Spoke's own binary, which is the
	// architecture of the node it runs on. A step whose artifact was not built
	// for it is refused before anything runs.
	Architecture string
	// Publisher is the admitted keyless publisher policy. Its fields are
	// anchored regular expressions, so they map to Zarf's *-regexp flags.
	Publisher domain.PublisherPolicy
	// VerificationKeyPath is a public key file, the alternative to Publisher.
	// Exactly one of the two must be configured.
	VerificationKeyPath string
	// PackageManifestPath is an optional UDS Package CR applied after deploy.
	PackageManifestPath string
	// UDSPackageName names that CR; it defaults to the Zarf package name.
	UDSPackageName string
	Now            func() time.Time
}

var _ agent.Executor = Executor{}

func (e Executor) runner() agent.CommandRunner {
	if e.Runner != nil {
		return e.Runner
	}
	return scopedRunner{}
}

func binOr(configured, fallback string) string {
	if trimmed := strings.TrimSpace(configured); trimmed != "" {
		return trimmed
	}
	return fallback
}

func (e Executor) zarfBin() string    { return binOr(e.ZarfBin, "zarf") }
func (e Executor) udsBin() string     { return binOr(e.UDSBin, "uds") }
func (e Executor) kubectlBin() string { return binOr(e.KubectlBin, "kubectl") }

func (e Executor) managesUDSPackage() bool { return strings.TrimSpace(e.PackageManifestPath) != "" }

func (e Executor) udsPackageName(zarfName string) string {
	return binOr(e.UDSPackageName, zarfName)
}

func refuse(step controlplane.PlanStep, format string, args ...any) error {
	err := fmt.Errorf("%w: %s", ErrRefused, fmt.Sprintf(format, args...))
	return agent.DiagnosticError{Err: err, Resources: []controlplane.ResourceDiagnostic{{
		Kind: "Package", Name: step.InstallationID, Namespace: step.Namespace, Reason: "Refused", Message: err.Error(),
	}}}
}

func (e Executor) architecture() string { return binOr(e.Architecture, runtime.GOARCH) }

// isBundle reports whether the step's artifact is a UDS bundle rather than a
// Zarf package. The Release declares it; the Spoke is not configured for one
// or the other.
func isBundle(step controlplane.PlanStep) bool {
	return step.Chart.EffectiveKind() == controlplane.ArtifactKindUDSBundle
}

// admit checks the step's own artifact declaration. The Hub validates the same
// rules at proposal, but a Plan is data that crossed a network, so the mutable
// reference and the wrong architecture are refused here again, where refusing
// is the last thing that happens before a command runs.
func (e Executor) admit(step controlplane.PlanStep) error {
	artifact := step.Chart
	if !artifact.IsPackage() {
		return refuse(step, "the step's artifact is a %s, which this executor does not deploy", artifact.EffectiveKind())
	}
	if problems := artifact.PackageProblems(); len(problems) > 0 {
		return refuse(step, "artifact %s %s", problems[0].Field, problems[0].Message)
	}
	if strings.TrimSpace(artifact.Repository) == "" {
		return refuse(step, "the artifact names no OCI repository")
	}
	return nil
}

// admitArchitecture refuses an artifact that was not built for this cluster.
func (e Executor) admitArchitecture(step controlplane.PlanStep) error {
	if !step.Chart.SupportsArchitecture(e.architecture()) {
		return refuse(step, "the %s was built for %s, and this Spoke deploys onto %s",
			step.Chart.Kind, strings.Join(step.Chart.Architectures, ", "), e.architecture())
	}
	return nil
}

// sourceAt is the digest-pinned reference for the step's repository. It never
// carries a tag: the digest is the only part of the reference the registry
// cannot change underneath a reviewed Plan.
func sourceAt(step controlplane.PlanStep, digest string) string {
	repository := strings.TrimPrefix(strings.TrimSpace(step.Chart.Repository), "oci://")
	return "oci://" + repository + "@" + strings.TrimSpace(digest)
}

func sourceFor(step controlplane.PlanStep) string { return sourceAt(step, step.Chart.Digest) }

// packageName is the Zarf package name the Release declared for its artifact.
// The Conduit InstallationID is not one, and guessing would observe or remove
// the wrong package.
func (e Executor) packageName(step controlplane.PlanStep) (string, error) {
	if err := e.admit(step); err != nil {
		return "", err
	}
	return step.Chart.PackageName, nil
}

// zarfVerifyFlags returns the flags that make Zarf enforce the signature, or a
// refusal when no usable verification material is configured.
func (e Executor) zarfVerifyFlags(step controlplane.PlanStep) ([]string, error) {
	key := strings.TrimSpace(e.VerificationKeyPath)
	keyless := e.Publisher != (domain.PublisherPolicy{})
	switch {
	case key != "" && keyless:
		return nil, refuse(step, "both a verification key and a publisher policy are configured; exactly one is required")
	case key != "":
		return []string{"--verify=always", "--key", key}, nil
	case keyless:
		if err := e.Publisher.Validate(); err != nil {
			return nil, refuse(step, "%v", err)
		}
		return []string{
			"--verify=always",
			"--certificate-identity-regexp", e.Publisher.CertificateIdentity,
			"--certificate-oidc-issuer-regexp", e.Publisher.CertificateIssuer,
		}, nil
	default:
		return nil, refuse(step, "no signature verification material is configured (publisher issuer and identity, or a key)")
	}
}

// setFlags maps reviewed values onto repeated `flag KEY=value` arguments in
// key order. Anything it cannot represent exactly is refused.
func setFlags(step controlplane.PlanStep, flag string, name *regexp.Regexp) ([]string, error) {
	raw := bytes.TrimSpace(step.Values)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, refuse(step, "values must be one JSON object")
	}
	values := make(map[string]any)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, refuse(step, "values contain an invalid variable name")
		}
		if _, exists := values[key]; exists {
			return nil, refuse(step, "value %q is repeated", key)
		}
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, refuse(step, "values contain invalid JSON")
		}
		values[key] = value
	}
	if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
		return nil, refuse(step, "values contain an incomplete object")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, refuse(step, "values must contain exactly one JSON object")
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var flags []string
	for _, key := range keys {
		if !name.MatchString(key) {
			return nil, refuse(step, "value %q is not a deployment variable name (%s)", key, name)
		}
		// UDS applies its --set map in map iteration order. A global variable
		// and a package-scoped spelling of it could otherwise overwrite each
		// other in either order, despite a deterministic argv.
		if _, variable, scoped := strings.Cut(key, "."); scoped {
			if _, global := values[variable]; global {
				return nil, refuse(step, "value %q conflicts with the global variable %q", key, variable)
			}
		}
		var text string
		switch value := values[key].(type) {
		case string:
			text = value
		case json.Number:
			text = value.String()
		case bool:
			text = fmt.Sprintf("%t", value)
		default:
			return nil, refuse(step, "value %q is not a scalar and cannot be passed as a deployment variable", key)
		}
		// pflag's stringToString takes an argument with exactly one "=" whole,
		// re-parses any other as CSV, and trims surrounding quotes.
		if strings.ContainsAny(text, "\x00=\"\r\n") {
			return nil, refuse(step, "value %q contains a character the CLI would re-interpret", key)
		}
		flags = append(flags, flag, key+"="+text)
	}
	return flags, nil
}

// deployedPackage is the part of Zarf's state.DeployedPackage this executor reads.
type deployedPackage struct {
	Name       string `json:"name"`
	Digest     string `json:"digest"`
	Generation int    `json:"generation"`
	Data       struct {
		Metadata struct {
			Version string `json:"version"`
		} `json:"metadata"`
	} `json:"data"`
	DeployedComponents []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"deployedComponents"`
}

// parseDeployedPackage decodes the base64 `data` key of a zarf-package-<name>
// Secret, as printed by kubectl's jsonpath. Empty input means no deployment.
func parseDeployedPackage(encoded []byte) (*deployedPackage, error) {
	trimmed := strings.Trim(strings.TrimSpace(string(encoded)), `'"`)
	if trimmed == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("decode Zarf deployed-package record: %w", err)
	}
	// Zarf 1.x stores gzip JSON; older deployments retain plain JSON. Bound
	// expansion before parsing so a compressed Secret cannot exhaust the worker.
	const maxRecordBytes = 16 << 20
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("decode compressed Zarf state: %w", err)
		}
		defer reader.Close()
		raw, err = io.ReadAll(io.LimitReader(reader, maxRecordBytes+1))
		if err != nil {
			return nil, fmt.Errorf("read compressed Zarf state: %w", err)
		}
	}
	if len(raw) > maxRecordBytes {
		return nil, fmt.Errorf("Zarf deployed-package record exceeds size limit")
	}
	var pkg deployedPackage
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return nil, fmt.Errorf("parse Zarf deployed-package record: %w", err)
	}
	return &pkg, nil
}

func (e Executor) deployedState(ctx context.Context, name string) (*deployedPackage, error) {
	out, err := e.runner().Run(ctx, e.kubectlBin(), "get", "secret", zarfPackagePrefix+name,
		"-n", zarfNamespace, "--ignore-not-found", "-o", "jsonpath={.data.data}")
	if err != nil {
		return nil, agent.DiagnosticError{
			Err:    fmt.Errorf("read Zarf deployed-package record %s: %w", name, err),
			Output: strings.TrimSpace(string(out)),
		}
	}
	return parseDeployedPackage(out)
}

// preflight confirms the cluster can take Zarf (and, when needed, UDS) work.
func (e Executor) preflight(ctx context.Context, needUDS bool) error {
	probe := func(specific error, args ...string) error {
		out, err := e.runner().Run(ctx, e.kubectlBin(), args...)
		if err != nil {
			return agent.DiagnosticError{Err: fmt.Errorf("preflight: %w", err), Output: strings.TrimSpace(string(out))}
		}
		if strings.TrimSpace(string(out)) == "" {
			return agent.DiagnosticError{Err: fmt.Errorf("%w: %w", ErrPreflightBlocked, specific)}
		}
		return nil
	}
	if err := probe(ErrZarfNotInitialised, "get", "secret", zarfStateSecret, "-n", zarfNamespace, "--ignore-not-found", "-o", "name"); err != nil {
		return err
	}
	if needUDS {
		return probe(ErrUDSCRDsAbsent, "get", "crd", udsPackageCRD, "--ignore-not-found", "-o", "name")
	}
	return nil
}

// receiptFor records what the cluster held before the step changed it.
// PreviousRevision is the prior Zarf generation, or zero when nothing was
// deployed. Fence.Name is the Zarf package name and Fence.Generation the prior
// digest: Receipt has no typed field for a prior package coordinate.
func receiptFor(step controlplane.PlanStep, name string, prior *deployedPackage) agent.Receipt {
	receipt := agent.Receipt{Step: step, Fence: agent.ReleaseFence{Name: name}}
	if prior != nil {
		receipt.PreviousRevision = max(prior.Generation, 1)
		receipt.Fence.Generation = strings.TrimSpace(prior.Digest)
	}
	return receipt
}

// Apply deploys or removes a Zarf package or UDS bundle for step.
func (e Executor) Apply(ctx context.Context, _ controlplane.Plan, step controlplane.PlanStep) (agent.Receipt, error) {
	if strings.TrimSpace(step.InstallationID) == "" {
		return agent.Receipt{}, errors.New("installationId is required")
	}
	name, err := e.packageName(step)
	if err != nil {
		return agent.Receipt{}, err
	}
	if step.Action == actionUninstall {
		return e.uninstall(ctx, step, name)
	}

	if err := e.admitArchitecture(step); err != nil {
		return agent.Receipt{}, err
	}
	source := sourceFor(step)
	if len(step.RegistryAccess) > 0 || len(step.SecretAccess) > 0 {
		return agent.Receipt{}, refuse(step, "the step carries registry or secret access, which this executor cannot prepare")
	}
	namespace := strings.TrimSpace(step.Namespace)
	if namespace == "" {
		return agent.Receipt{}, refuse(step, "a namespace is required: the applied record is kept there")
	}
	deploy, err := e.deployCommands(step, source, true)
	if err != nil {
		return agent.Receipt{}, err
	}
	pairs, err := valuePairs(step)
	if err != nil {
		return agent.Receipt{}, err
	}
	// Authenticate before even probing the cluster. A rejected signature is
	// not a failed deployment and must never enter rollback or removal.
	if out, err := e.runAll(ctx, deploy[:len(deploy)-1]); err != nil {
		return agent.Receipt{}, agent.DiagnosticError{Err: fmt.Errorf("verify: %w", err), Output: strings.TrimSpace(string(out))}
	}
	if err := e.preflight(ctx, isBundle(step) || e.managesUDSPackage()); err != nil {
		return agent.Receipt{}, err
	}
	prior, err := e.deployedState(ctx, name)
	if err != nil {
		return agent.Receipt{}, err
	}
	receipt := receiptFor(step, name, prior)

	if out, err := e.runAll(ctx, deploy[len(deploy)-1:]); err != nil {
		return agent.Receipt{}, e.failed(ctx, receipt, "deploy", err, out)
	}
	if e.managesUDSPackage() {
		out, err := e.runner().Run(ctx, e.kubectlBin(), "apply", "-n", namespace, "-f", strings.TrimSpace(e.PackageManifestPath))
		if err != nil {
			return agent.Receipt{}, e.failed(ctx, receipt, "apply UDS Package manifest", err, out)
		}
	}

	// The deploy succeeded. Read back what Zarf recorded for it and only then
	// write down what was applied, bound to that exact deployment. Failing to
	// do either is a failed step: a deployment Conduit cannot later account
	// for must not be left looking like a success.
	landed, err := e.deployedState(ctx, name)
	if err != nil {
		return agent.Receipt{}, e.failed(ctx, receipt, "read back the deployment", err, nil)
	}
	if landed == nil {
		return agent.Receipt{}, e.failed(ctx, receipt, "read back the deployment",
			fmt.Errorf("the deploy reported success but Zarf holds no record of package %s", name), nil)
	}
	// A Zarf package is deployed by its own digest, so Zarf must have recorded
	// that digest. It records the manifest it resolved for the cluster's
	// architecture, so a multi-architecture index digest fails here. A bundle's
	// digest is not the digest of the package inside it and cannot be compared.
	if !isBundle(step) && strings.TrimSpace(landed.Digest) != step.Chart.Digest {
		return agent.Receipt{}, e.failed(ctx, receipt, "read back the deployment",
			fmt.Errorf("Zarf recorded digest %q for package %s, not the artifact digest %s", landed.Digest, name, step.Chart.Digest), nil)
	}
	if err := e.writeApplied(ctx, namespace, appliedRecord{
		InstallationID: step.InstallationID, ReleaseDigest: step.ReleaseDigest, Version: step.Version,
		ConfigDigest: step.ConfigDigest, ValuesDigest: valuesDigest(pairs),
		ArtifactKind: step.Chart.Kind, ArtifactDigest: step.Chart.Digest,
		ZarfPackage: name, ZarfDigest: strings.TrimSpace(landed.Digest), ZarfGeneration: landed.Generation,
	}); err != nil {
		return agent.Receipt{}, e.failed(ctx, receipt, "record what was applied", err, nil)
	}
	return receipt, nil
}

// failed restores the prior state after a step broke part-way. The worker only
// rolls back steps that returned a receipt, and neither CLI is atomic.
func (e Executor) failed(ctx context.Context, receipt agent.Receipt, what string, cause error, out []byte) error {
	err := fmt.Errorf("%s: %w", what, cause)
	if rollbackErr := e.Rollback(ctx, receipt); rollbackErr != nil {
		err = fmt.Errorf("%w; rollback: %v", err, rollbackErr)
	}
	output := strings.TrimSpace(string(out))
	return agent.DiagnosticError{Err: err, Output: output, Resources: []controlplane.ResourceDiagnostic{{
		Kind: "Package", Name: receipt.Step.InstallationID, Namespace: receipt.Step.Namespace, Message: output,
	}}}
}

func (e Executor) runAll(ctx context.Context, commands [][]string) ([]byte, error) {
	for _, command := range commands {
		if out, err := e.runner().Run(ctx, command[0], command[1:]...); err != nil {
			return out, err
		}
	}
	return nil, nil
}

// deployCommands builds the verified deploy of source. A bundle is verified by
// a separate `uds inspect --key` because `uds deploy` cannot verify; keyless
// material therefore cannot authorise a bundle.
func (e Executor) deployCommands(step controlplane.PlanStep, source string, withValues bool) ([][]string, error) {
	if isBundle(step) {
		key := strings.TrimSpace(e.VerificationKeyPath)
		if key == "" || e.Publisher != (domain.PublisherPolicy{}) {
			return nil, refuse(step, "a UDS bundle can only be verified with a public key, and exactly that must be configured")
		}
		deploy := []string{e.udsBin(), "deploy", source, "--confirm"}
		if withValues {
			sets, err := setFlags(step, "--set", bundleVariableName)
			if err != nil {
				return nil, err
			}
			deploy = append(deploy, sets...)
		}
		return [][]string{{e.udsBin(), "inspect", source, "--key", key}, deploy}, nil
	}
	verify, err := e.zarfVerifyFlags(step)
	if err != nil {
		return nil, err
	}
	deploy := append([]string{e.zarfBin(), "package", "deploy", source, "--confirm"}, verify...)
	if withValues {
		sets, err := setFlags(step, "--set-variables", zarfVariableName)
		if err != nil {
			return nil, err
		}
		deploy = append(deploy, sets...)
	}
	// `package verify` always verifies signatures when trust material is
	// supplied; unlike deploy, it has no --verify mode flag. The deploy still
	// enforces --verify=always against the same immutable source.
	check := append([]string{e.zarfBin(), "package", "verify", source}, verify[1:]...)
	return [][]string{check, deploy}, nil
}

func (e Executor) removeCommand(step controlplane.PlanStep, name string) ([]string, error) {
	if !isBundle(step) {
		return []string{e.zarfBin(), "package", "remove", name, "--confirm"}, nil
	}
	return []string{e.udsBin(), "remove", sourceFor(step), "--confirm"}, nil
}

func (e Executor) uninstall(ctx context.Context, step controlplane.PlanStep, name string) (agent.Receipt, error) {
	remove, err := e.removeCommand(step, name)
	if err != nil {
		return agent.Receipt{}, err
	}
	if err := e.preflight(ctx, false); err != nil {
		return agent.Receipt{}, err
	}
	prior, err := e.deployedState(ctx, name)
	if err != nil {
		return agent.Receipt{}, err
	}
	receipt := receiptFor(step, name, prior)
	if prior == nil {
		return receipt, nil
	}
	// The record goes first, while its namespace certainly still exists. If
	// the removal then fails, the package is left with no record and reports
	// INDETERMINATE, which is true.
	if namespace := strings.TrimSpace(step.Namespace); namespace != "" {
		if err := e.deleteApplied(ctx, namespace, name); err != nil {
			return agent.Receipt{}, err
		}
	}
	if out, err := e.runner().Run(ctx, remove[0], remove[1:]...); err != nil {
		return agent.Receipt{}, agent.DiagnosticError{Err: err, Output: strings.TrimSpace(string(out))}
	}
	return receipt, nil
}

func (e Executor) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

// Observe reports what Zarf recorded as deployed, not what the Plan asked for.
func (e Executor) Observe(ctx context.Context, step controlplane.PlanStep) (controlplane.ReportedEntity, error) {
	name, err := e.packageName(step)
	if err != nil {
		return controlplane.ReportedEntity{}, err
	}
	deployed, err := e.deployedState(ctx, name)
	if err != nil {
		return controlplane.ReportedEntity{}, err
	}
	if step.Action == actionUninstall {
		if deployed != nil {
			return controlplane.ReportedEntity{}, fmt.Errorf("Zarf package %s remains deployed after uninstall", name)
		}
		// Absence was read back, so this mirrors the Helm executor's
		// uninstalled report: the digests identify what was removed.
		return controlplane.ReportedEntity{
			InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
			ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
			Health: controlplane.HealthUnknown, RolloutStatus: controlplane.RolloutUpToDate,
			ConfigStatus: controlplane.ConfigApplied, Lifecycle: controlplane.LifecycleUninstalled, ObservedAt: e.now(),
		}, nil
	}
	if deployed == nil {
		return controlplane.ReportedEntity{}, agent.DiagnosticError{
			Err: fmt.Errorf("Zarf package %s is not deployed", name),
			Resources: []controlplane.ResourceDiagnostic{{
				Kind: "Package", Name: step.InstallationID, Namespace: step.Namespace, Reason: "NotDeployed",
			}},
		}
	}

	health, rollout := componentHealth(deployed)
	if e.managesUDSPackage() {
		ns := strings.TrimSpace(step.Namespace)
		if ns == "" {
			return controlplane.ReportedEntity{}, refuse(step, "a namespace is required to read the UDS Package")
		}
		crName := e.udsPackageName(name)
		out, err := e.runner().Run(ctx, e.kubectlBin(), "get", "package.uds.dev", crName, "-n", ns, "-o", "yaml")
		if err != nil {
			return controlplane.ReportedEntity{}, fmt.Errorf("observe UDS Package %s/%s: %w", ns, crName, err)
		}
		obs, err := udsconverge.ParsePackageStatus(out)
		if err != nil {
			return controlplane.ReportedEntity{}, err
		}
		switch {
		case strings.EqualFold(obs.Phase, "Failed") || strings.EqualFold(obs.Phase, "Unhealthy"):
			health, rollout = controlplane.HealthTerminal, controlplane.RolloutBlocked
		case !(obs.Healthy && obs.Converged) && health == controlplane.HealthHealthy:
			health, rollout = controlplane.HealthWarning, controlplane.RolloutInGracePeriod
		}
	}

	// Zarf does not record the Release, the variables, or the bundle a
	// deployment came from. Those are reported only from the applied record,
	// and only while that record still describes the deployment Zarf holds
	// (see applied.go). Nothing below is filled in from the step: where the
	// record is missing, stale, or another installation's, the Release and
	// config digests stay empty and the configuration is INDETERMINATE, which
	// the Hub refuses as a success.
	report := controlplane.ReportedEntity{
		InstallationID: step.InstallationID,
		Product:        step.Product,
		Version:        strings.TrimSpace(deployed.Data.Metadata.Version),
		Namespace:      step.Namespace,
		ConfigStatus:   controlplane.ConfigIndeterminate,
		Health:         health,
		Lifecycle:      controlplane.LifecycleInstalled,
		ObservedAt:     e.now(),
	}
	var record *appliedRecord
	if namespace := strings.TrimSpace(step.Namespace); namespace != "" {
		if record, err = e.readApplied(ctx, namespace, name); err != nil {
			return controlplane.ReportedEntity{}, err
		}
	}
	bound := record != nil && record.InstallationID == step.InstallationID && record.describes(name, deployed)
	if bound {
		report.ReleaseDigest = record.ReleaseDigest
		report.ConfigDigest = record.ConfigDigest
		report.ConfigStatus = controlplane.ConfigApplied
		// The values Conduit last applied are compared with the ones this step
		// carries, independently of the Hub's own digest comparison.
		pairs, pairsErr := valuePairs(step)
		if pairsErr != nil || record.ConfigDigest != step.ConfigDigest || record.ValuesDigest != valuesDigest(pairs) {
			report.ConfigStatus = controlplane.ConfigDrifted
		}
		// The version Zarf holds for a bundle is the bundled package's, not
		// the Release's, so a bundle's version comes from the record too.
		if isBundle(step) {
			report.Version = record.Version
		}
	}
	current := bound && record.ReleaseDigest == step.ReleaseDigest && record.ArtifactDigest == step.Chart.Digest &&
		(strings.TrimSpace(step.Version) == "" || report.Version == strings.TrimSpace(step.Version))
	if rollout == controlplane.RolloutUpToDate && !current {
		rollout = controlplane.RolloutOutOfDate
	}
	report.RolloutStatus = rollout
	return report, nil
}

func componentHealth(deployed *deployedPackage) (health, rollout string) {
	health, rollout = controlplane.HealthHealthy, controlplane.RolloutUpToDate
	if len(deployed.DeployedComponents) == 0 {
		return controlplane.HealthWarning, controlplane.RolloutInGracePeriod
	}
	for _, component := range deployed.DeployedComponents {
		switch component.Status {
		case "Succeeded":
		case "Failed":
			return controlplane.HealthTerminal, controlplane.RolloutBlocked
		default:
			health, rollout = controlplane.HealthWarning, controlplane.RolloutInGracePeriod
		}
	}
	return health, rollout
}

// Rollback restores what the receipt says the cluster held before the step.
//
// With a prior deployment it re-deploys the prior digest, verified, and never
// removes. Only a step that installed onto nothing is removed. The prior
// deployment's variables are not recorded anywhere, so the re-deploy runs with
// the package's own defaults.
func (e Executor) Rollback(ctx context.Context, receipt agent.Receipt) error {
	step := receipt.Step
	if strings.TrimSpace(step.InstallationID) == "" {
		return nil
	}
	name := strings.TrimSpace(receipt.Fence.Name)
	if !zarfPackageName.MatchString(name) {
		return refuse(step, "the receipt does not name a Zarf package, so there is nothing safe to roll back")
	}
	if receipt.PreviousRevision == 0 {
		if step.Action == actionUninstall {
			return nil
		}
		remove, err := e.removeCommand(step, name)
		if err != nil {
			return err
		}
		// A deploy that failed verification or never started recorded nothing.
		if current, err := e.deployedState(ctx, name); err != nil || current == nil {
			return err
		}
		if namespace := strings.TrimSpace(step.Namespace); namespace != "" {
			if err := e.deleteApplied(ctx, namespace, name); err != nil {
				return err
			}
		}
		if out, err := e.runner().Run(ctx, remove[0], remove[1:]...); err != nil {
			return agent.DiagnosticError{Err: fmt.Errorf("remove failed first deployment: %w", err), Output: strings.TrimSpace(string(out))}
		}
		return nil
	}
	if isBundle(step) {
		return refuse(step, "a prior deployment exists but its bundle coordinate cannot be read back; leaving it in place")
	}
	priorDigest := strings.TrimSpace(receipt.Fence.Generation)
	if !ociDigest.MatchString(priorDigest) {
		return refuse(step, "a prior deployment exists but recorded no usable digest; leaving it in place")
	}
	// The prior package is assumed to live in the repository this step's
	// Release names. Zarf records no source repository, so if it came from
	// elsewhere the verified pull below fails and the deployment stays as it is.
	deploy, err := e.deployCommands(step, sourceAt(step, priorDigest), false)
	if err != nil {
		return err
	}
	if out, err := e.runAll(ctx, deploy); err != nil {
		return agent.DiagnosticError{Err: fmt.Errorf("re-deploy prior package %s: %w", priorDigest, err), Output: strings.TrimSpace(string(out))}
	}
	return nil
}
