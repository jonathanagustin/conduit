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

// --- the directory Cosign is told about -------------------------------------
//
// Cosign has no flag for a credential file. It reads the DIRECTORY named by
// DOCKER_CONFIG and joins config.json onto it, and a directory with no
// config.json is not an error to Cosign: it authenticates anonymously. That
// failure has no symptom until a private registry answers 401, so the name is
// checked where the operator can still act on it.

func TestTheDirectoryHandedToCosignIsTheOneHoldingConfigJson(t *testing.T) {
	directory := t.TempDir()
	got, err := DockerConfigDirectory(filepath.Join(directory, "config.json"))
	if err != nil {
		t.Fatalf("a conventionally named credential file was refused: %v", err)
	}
	if got != directory {
		t.Fatalf("DOCKER_CONFIG would be %q, want the directory holding the file, %q", got, directory)
	}
}

func TestACredentialFileCosignWouldNotReadIsRefused(t *testing.T) {
	for name, path := range map[string]string{
		// The key a Kubernetes dockerconfigjson Secret projects by default, and
		// the most likely way to get this silently wrong.
		"a dockerconfigjson projection": "/etc/gitless/registry/.dockerconfigjson",
		"some other name":               "/etc/gitless/registry/registry.json",
		"a directory":                   "/etc/gitless/registry",
		"empty":                         "   ",
	} {
		got, err := DockerConfigDirectory(path)
		if err == nil {
			t.Errorf("%s was accepted, and would have produced DOCKER_CONFIG=%q", name, got)
		}
	}
}

func TestTheRefusalSaysWhatToRename(t *testing.T) {
	_, err := DockerConfigDirectory("/etc/gitless/registry/.dockerconfigjson")
	if err == nil {
		t.Fatal("a file Cosign cannot read was accepted")
	}
	for _, want := range []string{"config.json", "DOCKER_CONFIG"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
}

func TestTheDirectoryIsAbsoluteSoTheChildsWorkingDirectoryCannotChangeIt(t *testing.T) {
	got, err := DockerConfigDirectory("relative/config.json")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("DOCKER_CONFIG would be the relative %q", got)
	}
}
