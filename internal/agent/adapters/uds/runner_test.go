package uds

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type commandEnvironment struct {
	Environment map[string]string
	Directory   string
	Config      map[string]string
	Arguments   []string
}

// This is a real child process, so the assertions cover what exec supplies to
// a CLI rather than a second implementation of the environment filter.
func TestScopedCommandProcess(t *testing.T) {
	if os.Args[len(os.Args)-1] != "uds-environment-probe" {
		return
	}
	result := commandEnvironment{Environment: map[string]string{}, Config: map[string]string{}, Arguments: os.Args}
	for _, pair := range os.Environ() {
		key, value, _ := strings.Cut(pair, "=")
		// A deliberately broken runner may inherit real parent credentials.
		// Report their presence without putting their values in a test log.
		result.Environment[key] = "<present>"
		switch key {
		case "HOME", "KUBECONFIG", "DOCKER_CONFIG", "SSL_CERT_FILE", "KUBERNETES_SERVICE_HOST", "KUBERNETES_SERVICE_PORT":
			result.Environment[key] = value // Explicit synthetic paths/endpoints below.
		}
	}
	result.Directory, _ = os.Getwd()
	for _, key := range []string{"uds-config.yaml", "zarf-config.yaml"} {
		content, err := os.ReadFile(filepath.Join(result.Directory, key))
		if err != nil {
			os.Exit(2)
		}
		result.Config[key] = string(content)
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
	os.Exit(0)
}

func TestDefaultRunnerIsolatesConfigurationAndPreservesExplicitAccess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, key := range []string{"UDS_CONFIG", "ZARF_CONFIG", "CLI_FEATURES", "UDS_UNREVIEWED", "UDS_INSECURE", "UDS_SKIP_SIGNATURE_VALIDATION", "ZARF_VAR_UNREVIEWED", "ZARF_PACKAGE_DEPLOY_SET", "COSIGN_PASSWORD", "AWS_SECRET_ACCESS_KEY", "UNRELATED_TOKEN"} {
		t.Setenv(key, "unreviewed-value")
	}
	t.Setenv("KUBECONFIG", "config-one"+string(os.PathListSeparator)+"config-two")
	t.Setenv("DOCKER_CONFIG", "registry-config")
	t.Setenv("SSL_CERT_FILE", "registry-ca.pem")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")
	keyPath, _ := filepath.Abs("publisher.pub")
	manifestPath, _ := filepath.Abs("package.yaml")
	out, err := (Executor{}).runner().Run(t.Context(), os.Args[0], "-test.run=^TestScopedCommandProcess$", "--", "--key", "publisher.pub", "-f", "package.yaml", "uds-environment-probe")
	if err != nil {
		t.Fatalf("child command: %v\n%s", err, out)
	}
	var observed commandEnvironment
	if err := json.Unmarshal(out, &observed); err != nil {
		t.Fatalf("child response: %v\n%s", err, out)
	}
	for key := range observed.Environment {
		if strings.HasPrefix(key, "UDS_") || strings.HasPrefix(key, "ZARF_") {
			t.Errorf("child inherited CLI variable %s", key)
		}
	}
	for _, key := range []string{"UDS_CONFIG", "ZARF_CONFIG", "CLI_FEATURES", "UDS_UNREVIEWED", "UDS_INSECURE", "UDS_SKIP_SIGNATURE_VALIDATION", "ZARF_VAR_UNREVIEWED", "ZARF_PACKAGE_DEPLOY_SET", "COSIGN_PASSWORD", "AWS_SECRET_ACCESS_KEY", "UNRELATED_TOKEN"} {
		if _, exists := observed.Environment[key]; exists {
			t.Errorf("child inherited %s", key)
		}
	}
	for key, suffixes := range map[string][]string{"KUBECONFIG": {"config-one", "config-two"}, "DOCKER_CONFIG": {"registry-config"}, "SSL_CERT_FILE": {"registry-ca.pem"}} {
		var expected []string
		for _, suffix := range suffixes {
			path, _ := filepath.Abs(suffix)
			expected = append(expected, path)
		}
		if observed.Environment[key] != strings.Join(expected, string(os.PathListSeparator)) {
			t.Errorf("explicit %s path changed: %q", key, observed.Environment[key])
		}
	}
	if observed.Environment["KUBERNETES_SERVICE_HOST"] != "10.0.0.1" || observed.Environment["KUBERNETES_SERVICE_PORT"] != "443" {
		t.Fatal("in-cluster Kubernetes endpoint lost")
	}
	for _, key := range []string{"uds-config.yaml", "zarf-config.yaml"} {
		if observed.Config[key] != "{}\n" {
			t.Fatalf("%s was not a private empty config: %+v", key, observed)
		}
	}
	if observed.Environment["HOME"] != observed.Directory || observed.Directory == os.Getenv("HOME") {
		t.Fatalf("child used ambient home: %+v", observed)
	}
	if strings.Join(observed.Arguments, "\x00") != strings.Join([]string{os.Args[0], "-test.run=^TestScopedCommandProcess$", "--", "--key", keyPath, "-f", manifestPath, "uds-environment-probe"}, "\x00") {
		t.Fatalf("explicit file arguments changed meaning: %q", observed.Arguments)
	}
	if _, err := os.Stat(observed.Directory); !os.IsNotExist(err) {
		t.Fatalf("private command directory not removed: %v", err)
	}
}
