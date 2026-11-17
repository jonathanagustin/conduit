package packagingtools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/fixtures"
)

func TestZarfLintAndCreateOnARenderedPackage(t *testing.T) {
	bin := os.Getenv("ZARF_BIN")
	if bin == "" {
		if path, err := LookZarf(); err == nil {
			bin = path
		} else {
			t.Skip("zarf is not installed")
		}
	}
	source := []byte("name: static-web\nmemory: 128Mi\n")
	files, err := fixtures.Render(source, domain.DigestBytes(source), fixtures.TemplateVersion, fixtures.ToolVersions)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zarf.yaml"), files.Zarf, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "uds-package.yaml"), files.CR, 0o644); err != nil {
		t.Fatal(err)
	}
	runner := ZarfRunner{Binary: bin}
	if err := runner.Lint(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	archive, err := runner.Create(context.Background(), dir, filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(archive)
	if err != nil || info.Size() == 0 {
		t.Fatalf("archive = %s %v", archive, err)
	}
}

func TestFilteredZarfEnvDropsClusterCredentials(t *testing.T) {
	t.Setenv("KUBECONFIG", "/secret/kubeconfig")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	t.Setenv("KUBE_TOKEN", "nope")
	for _, entry := range filteredZarfEnv() {
		if entry == "KUBECONFIG=/secret/kubeconfig" || entry == "KUBERNETES_SERVICE_HOST=10.0.0.1" || entry == "KUBE_TOKEN=nope" {
			t.Fatalf("leaked cluster credential %q", entry)
		}
	}
}

func TestLookZarfDoesNotSelectAnUnrequestedDeveloperInstall(t *testing.T) {
	home := t.TempDir()
	installed := filepath.Join(home, ".local", "share", "mise", "installs", "zarf", "1.0.0", "zarf")
	if err := os.MkdirAll(filepath.Dir(installed), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("ZARF_BIN", "")
	if binary, err := LookZarf(); err == nil || binary != "" {
		t.Fatalf("implicitly selected developer install %q: %v", binary, err)
	}
}

func TestLookZarfPreservesExplicitSelectionAndFailsClosedOnBadOverride(t *testing.T) {
	dir := t.TempDir()
	selected := filepath.Join(dir, "zarf")
	if err := os.WriteFile(selected, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ZARF_BIN", "")
	if binary, err := LookZarf(); err != nil || binary != selected {
		t.Fatalf("PATH selection=%q err=%v", binary, err)
	}
	override := filepath.Join(t.TempDir(), "qualified-zarf")
	if err := os.WriteFile(override, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZARF_BIN", override)
	if binary, err := LookZarf(); err != nil || binary != override {
		t.Fatalf("override=%q err=%v", binary, err)
	}
	t.Setenv("ZARF_BIN", filepath.Join(dir, "missing"))
	if binary, err := LookZarf(); err == nil || binary != "" {
		t.Fatalf("invalid override fell through to PATH: %q err=%v", binary, err)
	}
}

func TestZarfCreateRefusesPreexistingOrUnownedOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	for _, name := range []string{"a-zarf-package.sha256", "zarf-package-output-probe-amd64-1.0.0.tar.zst", "unrelated.tar", "symlink.tar.zst"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			out := filepath.Join(dir, "out")
			if err := os.Mkdir(out, 0700); err != nil {
				t.Fatal(err)
			}
			writeFile(t, dir, "zarf.yaml", "kind: ZarfPackageConfig\nmetadata:\n  name: output-probe\n  version: 1.0.0\n")
			if name == "symlink.tar.zst" {
				outside := filepath.Join(t.TempDir(), "outside.tar.zst")
				if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(out, name)); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(filepath.Join(out, name), []byte("stale"), 0600); err != nil {
				t.Fatal(err)
			}
			fake := filepath.Join(dir, "fake-zarf")
			if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
				t.Fatal(err)
			}
			got, err := (ZarfRunner{Binary: fake}).Create(t.Context(), dir, out)
			if err == nil || got != "" {
				t.Fatalf("preexisting/unowned output accepted: %q %v", got, err)
			}
			if _, err := os.Lstat(filepath.Join(out, name)); err != nil {
				t.Fatalf("unowned output modified: %v", err)
			}
		})
	}
}

func TestZarfCreateRejectsInvalidNewArchives(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	bin, err := LookZarf()
	if err != nil {
		t.Skip("native Zarf is required for archive parsing")
	}
	dir := t.TempDir()
	const definition = "kind: ZarfPackageConfig\nmetadata:\n  name: output-probe\n  version: 1.0.0\ncomponents:\n  - name: payload\n    required: true\n    files:\n      - source: payload.txt\n        target: /tmp/output-probe.txt\n"
	writeFile(t, dir, "zarf.yaml", definition)
	writeFile(t, dir, "payload.txt", "archive content fixture\n")
	original, err := (ZarfRunner{Binary: bin}).Create(t.Context(), dir, filepath.Join(dir, "original"))
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	for _, mode := range []string{"missing", "checksum", "unrelated", "symlink", "garbage", "identity", "platform", "ambiguous", "process-failure"} {
		t.Run(mode, func(t *testing.T) {
			work := t.TempDir()
			requested := definition
			name := "output-probe"
			arch := runtime.GOARCH
			if mode == "identity" {
				name = "another-package"
				requested = strings.ReplaceAll(requested, "name: output-probe", "name: another-package")
			}
			if mode == "platform" {
				if arch == "amd64" {
					arch = "arm64"
				} else {
					arch = "amd64"
				}
				requested = strings.Replace(requested, "  version: 1.0.0", "  version: 1.0.0\n  architecture: "+arch, 1)
			}
			writeFile(t, work, "zarf.yaml", requested)
			filename := "zarf-package-" + name + "-" + arch + "-1.0.0.tar.zst"
			action := "cp " + quote(original) + " \"$out/" + filename + "\"\n"
			switch mode {
			case "missing":
				action = ":\n"
			case "checksum":
				action = "printf checksum > \"$out/a-zarf-package.sha256\"\n"
			case "unrelated":
				action = "cp " + quote(original) + " \"$out/unrelated.tar\"\n"
			case "symlink":
				action = "ln -s " + quote(original) + " \"$out/" + filename + "\"\n"
			case "garbage":
				action = "printf garbage > \"$out/" + filename + "\"\n"
			case "ambiguous":
				action += "cp " + quote(original) + " \"$out/" + strings.TrimSuffix(filename, ".zst") + "\"\n"
			case "process-failure":
				action += "exit 1\n"
			}
			script := fmt.Sprintf("#!/bin/sh\nset -eu\nif [ \"$2\" = inspect ]; then exec %s \"$@\"; fi\nout=\nprev=\nfor arg in \"$@\"; do if [ \"$prev\" = --output ]; then out=$arg; fi; prev=$arg; done\n%s", quote(bin), action)
			fake := filepath.Join(work, "fake-zarf")
			if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(work, "out")
			got, err := (ZarfRunner{Binary: fake}).Create(t.Context(), work, out)
			if err == nil || got != "" {
				t.Fatalf("%s output accepted: %q %v", mode, got, err)
			}
			want := map[string]string{
				"missing": "exactly one archive", "ambiguous": "exactly one archive",
				"checksum": "does not match requested package identity", "unrelated": "does not match requested package identity",
				"symlink": "nonempty regular archive", "garbage": "zarf package inspect definition",
				"identity": "archive content identity does not match", "platform": "archive content identity does not match",
				"process-failure": "exit status 1",
			}[mode]
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("%s failed for an unintended reason: %v", mode, err)
			}
			entries, err := os.ReadDir(out)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed job output retained: %v %v", entries, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := (ZarfRunner{Binary: bin}).Create(ctx, dir, filepath.Join(dir, "cancelled")); err == nil || got != "" {
		t.Fatalf("cancelled create accepted: %q %v", got, err)
	}
}
