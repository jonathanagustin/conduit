package packagingtools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReadFileDeniesSecretBearingPaths(t *testing.T) {
	root := t.TempDir()
	denied := []string{
		".env", ".env.production", ".ENV.local", "config/prod.env",
		"certs/server.pem", "certs/TLS.KEY", "id_rsa", "deploy/id_ed25519",
		".git/config", ".netrc", ".npmrc", "kubeconfig", "ops/admin.kubeconfig",
		".kube/config", "infra/terraform.tfstate", "infra/terraform.tfstate.backup",
		".aws/credentials", ".ssh/known_hosts", ".git-credentials",
	}
	for _, path := range denied {
		writeFile(t, root, path, "harmless text\n")
	}
	allowed := []string{"README.md", "src/env.go", "id_rsa.pub", "environment.md", "keys.txt", "config/gitconfig.md"}
	for _, path := range allowed {
		writeFile(t, root, path, "harmless text\n")
	}
	service := New(WithRepoRoot(root))
	for _, path := range denied {
		_, err := service.ReadFile(context.Background(), path)
		if !errors.Is(err, ErrSecretPath) {
			t.Errorf("%s: err = %v, want ErrSecretPath", path, err)
		}
	}
	if ErrSecretPath.Error() != "denied: secret-bearing path" {
		t.Fatalf("error text = %q", ErrSecretPath)
	}
	for _, path := range allowed {
		got, err := service.ReadFile(context.Background(), path)
		if err != nil || got.Content != "harmless text\n" {
			t.Errorf("%s: %#v %v", path, got, err)
		}
	}
}

func TestReadFileDeniesASymlinkToASecretPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture")
	}
	root := t.TempDir()
	writeFile(t, root, ".env", "API_TOKEN=abc\n")
	if err := os.Symlink(filepath.Join(root, ".env"), filepath.Join(root, "notes.txt")); err != nil {
		t.Fatal(err)
	}
	_, err := New(WithRepoRoot(root)).ReadFile(context.Background(), "notes.txt")
	if !errors.Is(err, ErrSecretPath) {
		t.Fatalf("err = %v, want ErrSecretPath", err)
	}
}

func TestReadFileRedactsCredentialPatterns(t *testing.T) {
	root := t.TempDir()
	content := strings.Join([]string{
		"# settings",
		"-----BEGIN RSA PRIVATE KEY-----",
		"MIIEpAIBAAKCAQEAprivatekeymaterial",
		"-----END RSA PRIVATE KEY-----",
		"aws_key_id: AKIAIOSFODNN7EXAMPLE",
		"curl -H 'Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig'",
		"password=hunter2hunter2",
		`"api_key": "s3cr3t-value"`,
		"DB_TOKEN = 'tok-12345'",
		"url: postgres://app:dbpassw0rd@db.internal/app",
		"password: ${DB_PASSWORD}",
		"tokenizer: bert",
		"port: 8080",
	}, "\n")
	writeFile(t, root, "settings.yaml", content)
	got, err := New(WithRepoRoot(root)).ReadFile(context.Background(), "settings.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"privatekeymaterial", "PRIVATE KEY", "AKIAIOSFODNN7EXAMPLE", "eyJhbGciOiJIUzI1NiJ9", "hunter2", "s3cr3t-value", "tok-12345", "dbpassw0rd"} {
		if strings.Contains(got.Content, secret) {
			t.Errorf("content still contains %q:\n%s", secret, got.Content)
		}
	}
	for _, kept := range []string{"# settings", "aws_key_id: [REDACTED]", "Bearer [REDACTED]", "password=[REDACTED]", `"api_key": [REDACTED]`, "DB_TOKEN = [REDACTED]", "postgres://app:[REDACTED]@db.internal/app", "password: ${DB_PASSWORD}", "tokenizer: bert", "port: 8080"} {
		if !strings.Contains(got.Content, kept) {
			t.Errorf("content lost %q:\n%s", kept, got.Content)
		}
	}
}

func TestRedactSecretsRemovesAnUnterminatedPrivateKey(t *testing.T) {
	got := redactSecrets("intro\n-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXk\n")
	if got != "intro\n[REDACTED]" {
		t.Fatalf("got %q", got)
	}
}
