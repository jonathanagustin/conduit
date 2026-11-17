package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/agent"
	"github.com/jonathanagustin/conduit/internal/agent/adapters/uds"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func environmentOf(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

const (
	testIssuer   = `^https://token\.actions\.githubusercontent\.com$`
	testIdentity = `^https://github\.com/example/app/\.github/workflows/release\.yml@refs/tags/v.*$`
)

func TestPackageExecutorIsOffUnlessExplicitlyEnabled(t *testing.T) {
	for name, environment := range map[string]map[string]string{
		"nothing set":      {},
		"explicitly false": {"PACKAGE_EXECUTOR_ENABLED": "false"},
		// kubectl is the Helm path's tool too; naming it enables nothing.
		"only kubectl named": {"KUBECTL_BIN": "/usr/local/bin/kubectl"},
	} {
		executor, err := packageExecutorFromEnvironment(environmentOf(environment))
		if err != nil || executor != nil {
			t.Fatalf("%s: executor=%v err=%v, want none", name, executor, err)
		}
	}
}

func TestPackageExecutorConfigurationMapsOntoTheExecutor(t *testing.T) {
	key := filepath.Join(t.TempDir(), "cosign.pub")
	if err := os.WriteFile(key, []byte("-----BEGIN PUBLIC KEY-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keyed, err := packageExecutorFromEnvironment(environmentOf(map[string]string{
		"PACKAGE_EXECUTOR_ENABLED": "true", "PACKAGE_VERIFICATION_KEY_PATH": key,
		"ZARF_BIN": "/opt/zarf", "UDS_BIN": "/opt/uds", "KUBECTL_BIN": "/opt/kubectl", "PACKAGE_ARCHITECTURE": "arm64",
	}))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := keyed.(uds.Executor)
	if !ok || got.VerificationKeyPath != key || got.ZarfBin != "/opt/zarf" || got.UDSBin != "/opt/uds" ||
		got.KubectlBin != "/opt/kubectl" || got.Architecture != "arm64" {
		t.Fatalf("executor = %#v", keyed)
	}

	keyless, err := packageExecutorFromEnvironment(environmentOf(map[string]string{
		"PACKAGE_EXECUTOR_ENABLED":             "true",
		"PACKAGE_PUBLISHER_CERTIFICATE_ISSUER": testIssuer, "PACKAGE_PUBLISHER_CERTIFICATE_IDENTITY": testIdentity,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := keyless.(uds.Executor); got.Publisher.CertificateIssuer != testIssuer || got.Publisher.CertificateIdentity != testIdentity {
		t.Fatalf("publisher = %#v", got.Publisher)
	}
}

func TestPackageExecutorMisconfigurationIsRefusedAtStartup(t *testing.T) {
	for name, environment := range map[string]map[string]string{
		"enabled with nothing to verify against": {"PACKAGE_EXECUTOR_ENABLED": "true"},
		"key and publisher together": {
			"PACKAGE_EXECUTOR_ENABLED": "true", "PACKAGE_VERIFICATION_KEY_PATH": "/k.pub",
			"PACKAGE_PUBLISHER_CERTIFICATE_ISSUER": testIssuer, "PACKAGE_PUBLISHER_CERTIFICATE_IDENTITY": testIdentity,
		},
		"issuer without identity": {"PACKAGE_EXECUTOR_ENABLED": "true", "PACKAGE_PUBLISHER_CERTIFICATE_ISSUER": testIssuer},
		"unanchored publisher": {
			"PACKAGE_EXECUTOR_ENABLED":             "true",
			"PACKAGE_PUBLISHER_CERTIFICATE_ISSUER": "token.actions.githubusercontent.com", "PACKAGE_PUBLISHER_CERTIFICATE_IDENTITY": testIdentity,
		},
		"key file that is not there": {"PACKAGE_EXECUTOR_ENABLED": "true", "PACKAGE_VERIFICATION_KEY_PATH": "/nonexistent/cosign.pub"},
		"relative key path":          {"PACKAGE_EXECUTOR_ENABLED": "true", "PACKAGE_VERIFICATION_KEY_PATH": "cosign.pub"},
		"settings without the switch": {
			"PACKAGE_PUBLISHER_CERTIFICATE_ISSUER": testIssuer, "PACKAGE_PUBLISHER_CERTIFICATE_IDENTITY": testIdentity,
		},
		"a binary without the switch": {"ZARF_BIN": "/opt/zarf"},
		"a switch that is not a boolean": {
			"PACKAGE_EXECUTOR_ENABLED": "yes", "PACKAGE_PUBLISHER_CERTIFICATE_ISSUER": testIssuer,
			"PACKAGE_PUBLISHER_CERTIFICATE_IDENTITY": testIdentity,
		},
		"unknown architecture": {
			"PACKAGE_EXECUTOR_ENABLED": "true", "PACKAGE_ARCHITECTURE": "sparc",
			"PACKAGE_PUBLISHER_CERTIFICATE_ISSUER": testIssuer, "PACKAGE_PUBLISHER_CERTIFICATE_IDENTITY": testIdentity,
		},
	} {
		if executor, err := packageExecutorFromEnvironment(environmentOf(environment)); err == nil || executor != nil {
			t.Errorf("%s: accepted as %v", name, executor)
		}
	}
}

type helmStandIn struct{ applied int }

func (helm *helmStandIn) Apply(context.Context, controlplane.Plan, controlplane.PlanStep) (agent.Receipt, error) {
	helm.applied++
	return agent.Receipt{}, nil
}

func (helm *helmStandIn) Observe(context.Context, controlplane.PlanStep) (controlplane.ReportedEntity, error) {
	return controlplane.ReportedEntity{}, nil
}

func (helm *helmStandIn) Rollback(context.Context, agent.Receipt) error { return nil }

// The wiring the binary uses: an unconfigured Spoke still runs Helm steps, and
// answers a package step with a diagnostic that names the switch.
func TestUnconfiguredSpokeRunsHelmAndBlocksPackages(t *testing.T) {
	packages, err := packageExecutorFromEnvironment(environmentOf(nil))
	if err != nil {
		t.Fatal(err)
	}
	helm := &helmStandIn{}
	executor := stepExecutor(helm, packages)
	if _, err := executor.Apply(context.Background(), controlplane.Plan{}, controlplane.PlanStep{InstallationID: "web"}); err != nil || helm.applied != 1 {
		t.Fatalf("Helm step: applied=%d err=%v", helm.applied, err)
	}
	_, err = executor.Apply(context.Background(), controlplane.Plan{}, controlplane.PlanStep{
		InstallationID: "pkg", Chart: controlplane.Artifact{Kind: controlplane.ArtifactKindZarfPackage},
	})
	var diagnostic agent.DiagnosticError
	if !errors.Is(err, agent.ErrExecutorBlocked) || !errors.As(err, &diagnostic) ||
		!strings.Contains(err.Error(), packageExecutorEnabledVariable) || helm.applied != 1 {
		t.Fatalf("package step on an unconfigured Spoke: %v (helm applied %d)", err, helm.applied)
	}
}
