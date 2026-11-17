package uds_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/agent/adapters/uds"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

type nativeValuesRunner struct {
	*fakeRunner
	t        *testing.T
	path     string
	contents []byte
}

func (r *nativeValuesRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "zarf" && len(args) > 1 && args[0] == "package" && args[1] == "deploy" {
		for i, arg := range args {
			if arg == "--set-variables" || arg == "--set-values" {
				r.t.Fatal("native reviewed values passed through CLI type inference")
			}
			if arg != "--values" || i+1 == len(args) {
				continue
			}
			r.path = args[i+1]
			var err error
			r.contents, err = os.ReadFile(r.path)
			if err != nil {
				r.t.Fatal(err)
			}
			for path, mode := range map[string]os.FileMode{r.path: 0o600, filepath.Dir(r.path): 0o700} {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != mode {
					r.t.Fatalf("private values permissions: %s: %v, %v", path, info, err)
				}
			}
		}
	}
	return r.fakeRunner.Run(ctx, name, args...)
}

func TestNativePackageValuesPreserveReviewedTypesAndCleanup(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "deploy failure"}[fail], func(t *testing.T) {
			base := initialised("").on("apiVersion: zarf.dev/v1beta1\nkind: ZarfPackageConfig\nmetadata:\n  name: excalidraw\n", nil, verifiedRead(desiredDigest)...)
			if fail {
				base.on("native deploy failed", errors.New("exit status 1"), "zarf", "package", "deploy")
			}
			runner := &nativeValuesRunner{fakeRunner: base, t: t}
			exec := executor(base)
			exec.Runner = runner
			step := installStep()
			step.Values = json.RawMessage(`{"IDENTIFIER":"0012","TEXT_FALSE":"false","BOOL":false,"COUNT":12,"FLOAT":1.25,"ADMINS":"a,b","UNICODE":"東京"}`)
			_, err := exec.Apply(t.Context(), controlplane.Plan{}, step)
			if (err != nil) != fail {
				t.Fatalf("Apply: %v", err)
			}
			var got, want map[string]json.RawMessage
			if err := json.Unmarshal(runner.contents, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(step.Values, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("native values changed reviewed types: got %s, want %s", runner.contents, step.Values)
			}
			if !filepath.IsAbs(runner.path) || filepath.Ext(runner.path) != ".yaml" {
				t.Fatalf("values path cannot survive isolated CLI cwd: %q", runner.path)
			}
			if _, err := os.Stat(filepath.Dir(runner.path)); !os.IsNotExist(err) {
				t.Fatalf("private values directory was retained: %v", err)
			}
		})
	}
}

func TestPackageDefinitionRefusedBeforeClusterAccess(t *testing.T) {
	for name, definition := range map[string]string{
		"unsupported API":    "apiVersion: zarf.dev/v2\nkind: ZarfPackageConfig\nmetadata: {name: excalidraw}\n",
		"wrong identity":     "apiVersion: zarf.dev/v1beta1\nkind: ZarfPackageConfig\nmetadata: {name: other}\n",
		"wrong kind":         "apiVersion: zarf.dev/v1beta1\nkind: ConfigMap\nmetadata: {name: excalidraw}\n",
		"invalid":            "not a package definition",
		"multiple documents": "apiVersion: zarf.dev/v1beta1\nkind: ZarfPackageConfig\nmetadata: {name: excalidraw}\n---\n{}\n",
		"duplicate API":      "apiVersion: zarf.dev/v1alpha1\napiVersion: zarf.dev/v1beta1\nkind: ZarfPackageConfig\nmetadata: {name: excalidraw}\n",
		"oversized":          strings.Repeat(" ", (16<<20)+1),
	} {
		t.Run(name, func(t *testing.T) {
			runner := initialised("").on(definition, nil, verifiedRead(desiredDigest)...)
			_, err := executor(runner).Apply(t.Context(), controlplane.Plan{}, installStep())
			if !errors.Is(err, uds.ErrRefused) || len(runner.calls) != 1 {
				t.Fatalf("invalid authenticated definition reached cluster: %v; calls=%q", err, runner.calls)
			}
		})
	}
}

func TestNativeValuesCleanupWhenPreflightRefuses(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("TMPDIR", directory)
	runner := (&fakeRunner{}).on("apiVersion: zarf.dev/v1beta1\nkind: ZarfPackageConfig\nmetadata:\n  name: excalidraw\n", nil, verifiedRead(desiredDigest)...)
	runner.on("", nil, stateProbe...)
	_, err := executor(runner).Apply(t.Context(), controlplane.Plan{}, installStep())
	if !errors.Is(err, uds.ErrPreflightBlocked) {
		t.Fatalf("expected preflight refusal: %v", err)
	}
	files, err := os.ReadDir(directory)
	if err != nil || len(files) != 0 {
		t.Fatalf("preflight refusal retained private values: %v, %v", files, err)
	}
}
