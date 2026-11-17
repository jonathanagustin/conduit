package packagingtools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"

	"gopkg.in/yaml.v3"
	"strings"
	"time"
)

const defaultZarfTimeout = 2 * time.Minute

// ZarfRunner runs zarf package checks against a work directory.
type ZarfRunner struct {
	Binary  string
	Timeout time.Duration
}

// LookZarf finds the operator-selected zarf binary. ZARF_BIN wins, then PATH.
// Installed developer tools outside PATH are not implicit runtime authority.
func LookZarf() (string, error) {
	if path := strings.TrimSpace(os.Getenv("ZARF_BIN")); path != "" {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
		return "", fmt.Errorf("ZARF_BIN is not an executable file")
	}
	if path, err := exec.LookPath("zarf"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("zarf is not installed")
}

func (runner ZarfRunner) binary() (string, error) {
	if strings.TrimSpace(runner.Binary) != "" {
		return runner.Binary, nil
	}
	return LookZarf()
}

func (runner ZarfRunner) timeout() time.Duration {
	if runner.Timeout > 0 {
		return runner.Timeout
	}
	return defaultZarfTimeout
}

// Lint runs `zarf dev lint` on a package directory.
func (runner ZarfRunner) Lint(ctx context.Context, dir string) error {
	return runner.run(ctx, dir, "dev", "lint", ".")
}

// Create admits one newly generated, regular native Zarf archive. The returned
// path is local build output, not a durable artifact receipt or an OCI digest.
func (runner ZarfRunner) Create(ctx context.Context, dir, outDir string) (archive string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "zarf.yaml"))
	if err != nil {
		return "", err
	}
	expected, err := archiveIdentity(raw)
	if err != nil {
		return "", err
	}
	arch := expected.Metadata.Architecture
	if arch == "" {
		arch = runtime.GOARCH
	}
	if arch != "amd64" && arch != "arm64" {
		return "", fmt.Errorf("unsupported package architecture")
	}
	outDir, err = filepath.Abs(outDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(outDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("archive output must be a real directory")
	}
	job, err := os.MkdirTemp(outDir, "create-")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(job)
		}
	}()
	if err := runner.run(ctx, dir, "package", "create", ".", "--confirm", "--skip-sbom", "--architecture", arch, "--output", job); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(job)
	if err != nil {
		return "", err
	}
	if len(entries) != 1 {
		return "", fmt.Errorf("zarf package create must write exactly one archive")
	}
	name := entries[0].Name()
	stem := "zarf-package-" + expected.Metadata.Name + "-" + arch + "-" + expected.Metadata.Version
	if name != stem+".tar.zst" && name != stem+".tar" {
		return "", fmt.Errorf("zarf output does not match requested package identity")
	}
	path := filepath.Join(job, name)
	info, err = os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return "", fmt.Errorf("zarf output must be a nonempty regular archive")
	}
	definition, err := runner.output(ctx, dir, "package", "inspect", "definition", path, "--no-color", "--log-level", "warn")
	if err != nil {
		return "", err
	}
	actual, err := archiveIdentity(definition)
	if err != nil {
		return "", fmt.Errorf("invalid archive definition: %w", err)
	}
	if actual.Metadata.Name != expected.Metadata.Name || actual.Metadata.Version != expected.Metadata.Version || actual.Metadata.Architecture != arch {
		return "", fmt.Errorf("archive content identity does not match requested package")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return path, nil
}

type zarfArchiveIdentity struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name         string `yaml:"name"`
		Version      string `yaml:"version"`
		Architecture string `yaml:"architecture"`
	} `yaml:"metadata"`
}

var archiveName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
var archiveVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]*$`)

func archiveIdentity(raw []byte) (zarfArchiveIdentity, error) {
	var identity zarfArchiveIdentity
	if err := yaml.Unmarshal(raw, &identity); err != nil {
		return identity, err
	}
	if identity.Kind != "ZarfPackageConfig" || !archiveName.MatchString(identity.Metadata.Name) || !archiveVersion.MatchString(identity.Metadata.Version) {
		return identity, fmt.Errorf("package requires a native name and version")
	}
	return identity, nil
}

func (runner ZarfRunner) run(ctx context.Context, dir string, args ...string) error {
	_, err := runner.output(ctx, dir, args...)
	return err
}

func (runner ZarfRunner) output(ctx context.Context, dir string, args ...string) ([]byte, error) {
	bin, err := runner.binary()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, runner.timeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = filteredZarfEnv()
	var out, diagnostic bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &diagnostic
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("zarf %s: %w\n%s", strings.Join(args, " "), err, firstLines(out.String()+"\n"+diagnostic.String(), 40))
	}
	return out.Bytes(), nil
}

func filteredZarfEnv() []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if strings.Contains(upper, "KUBE") || strings.Contains(upper, "KUBERNETES") || upper == "KUBECONFIG" {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "KUBECONFIG="+os.DevNull)
	return env
}

func firstLines(text string, n int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
