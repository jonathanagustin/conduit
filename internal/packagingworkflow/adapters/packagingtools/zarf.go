package packagingtools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const defaultZarfTimeout = 2 * time.Minute

// ZarfRunner runs zarf package checks against a work directory.
type ZarfRunner struct {
	Binary  string
	Timeout time.Duration
}

// LookZarf finds the zarf binary. ZARF_BIN wins, then PATH, then pinned mise locations.
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
	candidates := []string{"/home/box/.local/share/mise/installs/zarf/1.0.0/zarf"}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidates = append([]string{filepath.Join(home, ".local/share/mise/installs/zarf/1.0.0/zarf")}, candidates...)
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
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

// Create runs `zarf package create` in dir and writes the archive to outDir.
func (runner ZarfRunner) Create(ctx context.Context, dir, outDir string) (string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	if err := runner.run(ctx, dir, "package", "create", ".", "--confirm", "--skip-sbom", "--output", outDir); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && (strings.HasSuffix(name, ".tar.zst") || strings.HasSuffix(name, ".tar") || strings.Contains(name, "zarf-package")) {
			return filepath.Join(outDir, name), nil
		}
	}
	return "", fmt.Errorf("zarf package create did not write an archive")
}

func (runner ZarfRunner) run(ctx context.Context, dir string, args ...string) error {
	bin, err := runner.binary()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, runner.timeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = filteredZarfEnv()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("zarf %s: %w\n%s", strings.Join(args, " "), err, firstLines(out.String(), 40))
	}
	return nil
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
