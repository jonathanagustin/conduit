package uds_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/agent/adapters/uds"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

type nativeBundleRunner struct {
	*fakeRunner
	t    *testing.T
	path string
}

func (r *nativeBundleRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "uds" && hasPrefix(args, "--features", "NextMode", "bundle", "deploy") {
		if !hasPrefix(r.calls[0], "uds", "--features", "NextMode", "bundle", "verify") {
			r.t.Fatal("native deploy preceded authentication")
		}
		expected := []string{"uds", "--features", "NextMode", "bundle", "verify", source(desiredDigest), "--public-key", "/publisher.pub", "--architecture", "amd64"}
		if !reflect.DeepEqual(r.calls[0], expected) {
			r.t.Fatalf("native authentication policy changed: %q", r.calls[0])
		}
		if !hasPrefix(args, "--features", "NextMode", "bundle", "deploy", source(desiredDigest), "--public-key", "/publisher.pub") {
			r.t.Fatalf("native deploy lost its verification policy: %q", args)
		}
		for i, arg := range args {
			if strings.HasPrefix(arg, "--skip-") || arg == "--set" {
				r.t.Fatal("native bundle bypassed verification or inferred variable types")
			}
			if arg == "--config" && i+1 < len(args) {
				r.path = args[i+1]
			}
		}
		text, err := os.ReadFile(r.path)
		if err != nil {
			r.t.Fatal(err)
		}
		for _, expected := range []string{`"TEXT" = "0012"`, `"BOOL" = false`, `"COUNT" = 12`, `"LITERAL" = "$${1+1} %%{literal}"`} {
			if !strings.Contains(string(text), expected) {
				r.t.Fatalf("native config lost literal/type %s: %s", expected, text)
			}
		}
		for path, mode := range map[string]os.FileMode{r.path: 0o600, filepath.Dir(r.path): 0o700} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != mode {
				r.t.Fatalf("private native config: %v %v", info, err)
			}
		}
	}
	return r.fakeRunner.Run(ctx, name, args...)
}

func TestNativeBundleUsesVerifiedTypedConfiguration(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "deployment failure"}[failure], func(t *testing.T) {
			base := initialised("")
			if failure {
				base.on("failed", errors.New("deploy failed"), "uds", "--features", "NextMode", "bundle", "deploy")
			}
			runner := &nativeBundleRunner{fakeRunner: base, t: t}
			exec := uds.Executor{Runner: runner, Architecture: "amd64", VerificationKeyPath: "/publisher.pub", NativeBundles: true}
			step := bundleStep()
			step.Values = []byte(`{"TEXT":"0012","BOOL":false,"COUNT":12,"LITERAL":"${1+1} %{literal}"}`)
			_, err := exec.Apply(t.Context(), controlplane.Plan{}, step)
			if (err != nil) != failure {
				t.Fatalf("Apply: %v", err)
			}
			if !filepath.IsAbs(runner.path) {
				t.Fatalf("nonabsolute native config: %s", runner.path)
			}
			if _, err := os.Stat(filepath.Dir(runner.path)); !os.IsNotExist(err) {
				t.Fatalf("retained native config: %v", err)
			}
		})
	}
}

func TestNativeBundleRefusesLegacyScopedVariablesBeforeAuthentication(t *testing.T) {
	runner := initialised("")
	exec := uds.Executor{Runner: runner, Architecture: "amd64", VerificationKeyPath: "/publisher.pub", NativeBundles: true}
	step := bundleStep()
	step.Values = []byte(`{"runtime.ENVIRONMENT_ID":"fixture"}`)
	_, err := exec.Apply(t.Context(), controlplane.Plan{}, step)
	if !errors.Is(err, uds.ErrRefused) || len(runner.calls) != 0 {
		t.Fatalf("silently admitted scoped native setting: %v, %q", err, runner.calls)
	}
}

func TestNativeBundleVerificationFailurePrecedesClusterAccess(t *testing.T) {
	runner := initialised("").on("signature rejected", errors.New("invalid signature"), "uds", "--features", "NextMode", "bundle", "verify")
	exec := uds.Executor{Runner: runner, Architecture: "amd64", VerificationKeyPath: "/publisher.pub", NativeBundles: true}
	_, err := exec.Apply(t.Context(), controlplane.Plan{}, bundleStep())
	if err == nil || len(runner.calls) != 1 || !hasPrefix(runner.calls[0], "uds", "--features", "NextMode", "bundle", "verify") {
		t.Fatalf("native verification failure reached cluster: %v %q", err, runner.calls)
	}
}
