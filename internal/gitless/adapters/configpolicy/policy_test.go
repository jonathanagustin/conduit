package configpolicy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/configpolicy"
)

const good = `{"environments":{
  "prod":{"certificateIssuer":"^https://token\\.actions\\.githubusercontent\\.com$",
          "certificateIdentity":"^https://github\\.com/jonathanagustin/conduit/\\.github/workflows/platform-images\\.yml@refs/tags/v[0-9]+\\.[0-9]+\\.[0-9]+$"},
  "dev":{"certificateIssuer":"^https://token\\.actions\\.githubusercontent\\.com$",
         "certificateIdentity":"^https://github\\.com/jonathanagustin/conduit/\\.github/workflows/platform-images\\.yml@refs/heads/main$"}}}`

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policies.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEachEnvironmentGetsItsOwnPublisher(t *testing.T) {
	source, err := configpolicy.Load(write(t, good))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	prod, err := source.PolicyFor(t.Context(), "prod")
	if err != nil {
		t.Fatalf("prod: %v", err)
	}
	dev, err := source.PolicyFor(t.Context(), "dev")
	if err != nil {
		t.Fatalf("dev: %v", err)
	}
	// The whole point of per-environment policy: production requires a tag,
	// development accepts a branch build, and neither can be mistaken for the
	// other.
	if !strings.Contains(prod.CertificateIdentity, "refs/tags/") {
		t.Errorf("production does not require a tag: %q", prod.CertificateIdentity)
	}
	if !strings.Contains(dev.CertificateIdentity, "refs/heads/main") {
		t.Errorf("development policy is wrong: %q", dev.CertificateIdentity)
	}
	if got := strings.Join(source.Environments(), ","); got != "dev,prod" {
		t.Errorf("environments: %q", got)
	}
}

// A default publisher policy is one nobody reviewed, applied to the environment
// somebody forgot -- reliably the one that mattered.
func TestAnUndeclaredEnvironmentIsRefusedRatherThanDefaulted(t *testing.T) {
	source, err := configpolicy.Load(write(t, good))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	_, err = source.PolicyFor(t.Context(), "staging")
	if err == nil {
		t.Fatal("an undeclared environment received a policy")
	}
	if !strings.Contains(err.Error(), "staging") {
		t.Errorf("the refusal does not name the environment: %v", err)
	}
}

// A malformed policy must fail when the file is read, not during the deployment
// that needed it.
func TestAnUnanchoredPolicyFailsAtLoad(t *testing.T) {
	loose := strings.Replace(good, `"^https://token`, `"https://token`, 1)
	if _, err := configpolicy.Load(write(t, loose)); err == nil {
		t.Fatal("an unanchored policy loaded")
	}
}

// `certificateIdentiy` would otherwise leave the environment trusting an empty
// pattern, which is exactly what this file exists to prevent.
func TestATypoInAFieldNameIsRefused(t *testing.T) {
	typo := strings.Replace(good, "certificateIdentity", "certificateIdentiy", 1)
	if _, err := configpolicy.Load(write(t, typo)); err == nil {
		t.Fatal("a misspelled policy field was accepted")
	}
}

func TestAnEmptyFileIsRefused(t *testing.T) {
	if _, err := configpolicy.Load(write(t, `{"environments":{}}`)); err == nil {
		t.Fatal("a file declaring no environment loaded")
	}
}

func TestAMissingFileIsReported(t *testing.T) {
	if _, err := configpolicy.Load(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("a missing policy file loaded")
	}
}
