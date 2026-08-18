package registryauth

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerConfigSuppliesEncodedAndExplicitCredentials(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("AWS:rotating-token:with-colons"))
	config := loadFixture(t, `{"auths":{`+
		`"https://registry.example/v1/":{"auth":"`+encoded+`"},`+
		`"ghcr.io":{"username":"publisher","password":"secret"}}}`)

	for host, want := range map[string][2]string{
		"registry.example": {"AWS", "rotating-token:with-colons"},
		"ghcr.io":          {"publisher", "secret"},
	} {
		username, password, err := config.Basic(context.Background(), host)
		if err != nil {
			t.Fatalf("credentials for %s: %v", host, err)
		}
		if username != want[0] || password != want[1] {
			t.Errorf("credentials for %s = (%q, %q), want (%q, %q)",
				host, username, password, want[0], want[1])
		}
	}
}

func TestConfiguredDockerConfigNeverFallsBackToAnonymous(t *testing.T) {
	config := loadFixture(t, `{"auths":{"registry.example":{"auth":"`+
		base64.StdEncoding.EncodeToString([]byte("reader:token"))+`"}}}`)
	if _, _, err := config.Basic(context.Background(), "other.example"); err == nil {
		t.Fatal("a registry absent from the configured file fell back to anonymous")
	}
}

func TestDockerConfigRefusesUnusableDocuments(t *testing.T) {
	for name, body := range map[string]string{
		"invalid JSON":      `{`,
		"no auth entries":   `{"auths":{}}`,
		"helper-only entry": `{"auths":{"registry.example":{}},"credsStore":"desktop"}`,
		"invalid base64":    `{"auths":{"registry.example":{"auth":"%%%"}}}`,
		"no separator":      `{"auths":{"registry.example":{"auth":"` + base64.StdEncoding.EncodeToString([]byte("token")) + `"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("unusable Docker configuration was accepted")
			}
		})
	}
}

func TestDockerConfigErrorsDoNotDiscloseCredentialContents(t *testing.T) {
	secret := "credential-must-not-appear"
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"auths":{"registry.example":{"auth":"` +
		base64.StdEncoding.EncodeToString([]byte(secret)) + `"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("credential without a username separator was accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("credential content reached the error: %v", err)
	}
}

func loadFixture(t *testing.T, body string) *DockerConfig {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return config
}
