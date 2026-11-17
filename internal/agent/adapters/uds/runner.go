package uds

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// scopedRunner prevents local CLI defaults from adding unreviewed variables or
// changing verification. This is configuration isolation, not an OS sandbox.
// Kubernetes uses an explicit KUBECONFIG or its standard service-account mount;
// registry credentials require an explicit DOCKER_CONFIG directory. Neither
// CLI gets the Spoke user's ambient home configuration or unrelated tokens.
type scopedRunner struct{}

func (scopedRunner) Run(ctx context.Context, name string, arguments ...string) ([]byte, error) {
	executable, err := exec.LookPath(name)
	if err != nil {
		return nil, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, err
	}
	args := append([]string(nil), arguments...)
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--key" || args[i] == "-f" {
			args[i+1], err = filepath.Abs(args[i+1])
			if err != nil {
				return nil, err
			}
			i++
		}
	}
	directory, err := os.MkdirTemp("", "conduit-package-command-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, err
	}
	// UDS turns every UDS_* environment entry into a package variable,
	// including UDS_CONFIG. Use its default filename in the private directory
	// instead of adding a CONFIG variable ourselves. Zarf uses the same
	// current-directory-first lookup, with its own default filename.
	for _, name := range []string{"uds-config.yaml", "zarf-config.yaml"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("{}\n"), 0o600); err != nil {
			return nil, err
		}
	}
	environment := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + directory, "USERPROFILE=" + directory,
		"XDG_CONFIG_HOME=" + directory, "XDG_CACHE_HOME=" + directory}
	for _, key := range []string{"KUBECONFIG", "DOCKER_CONFIG", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value := os.Getenv(key); value != "" {
			paths := []string{value}
			if key == "KUBECONFIG" || key == "SSL_CERT_DIR" {
				paths = filepath.SplitList(value)
			}
			for i, path := range paths {
				if path != "" {
					paths[i], err = filepath.Abs(path)
					if err != nil {
						return nil, err
					}
				}
			}
			environment = append(environment, key+"="+strings.Join(paths, string(os.PathListSeparator)))
		}
	}
	for _, key := range []string{"KUBERNETES_SERVICE_HOST", "KUBERNETES_SERVICE_PORT", "SYSTEMROOT"} {
		if value := os.Getenv(key); value != "" {
			environment = append(environment, key+"="+value)
		}
	}
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir, command.Env = directory, environment
	output, err := command.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return output, nil
}
