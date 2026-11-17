package localcmd

import (
	"os"
	"strings"
	"testing"
)

func TestLegacyEnvironmentFilterRetainsOnlyAdmittedVariables(t *testing.T) {
	t.Setenv("CONDUIT_DATABASE_DSN", "postgres://hub:hunter2@db/conduit")
	t.Setenv("BUNDLE_SIGNING_KEY", "signing-secret")
	t.Setenv("DATABASE_URL", "postgres://db-secret")
	t.Setenv("OIDC_CLIENT_SECRET", "oidc-secret")
	t.Setenv("NOTIFICATION_SMTP_PASSWORD", "smtp-secret")
	t.Setenv("ANTHROPIC_API_KEY", "provider-key")
	t.Setenv("HTTPS_PROXY", "http://proxy.internal:3128")
	t.Setenv("LC_ALL", "C")
	t.Setenv("CONDUIT_PROVIDER_CLAUDE_CMD", "claude -p")
	t.Setenv("OPERATOR_EXTRA", "kept")
	t.Setenv("OPERATOR_OTHER", "dropped")
	t.Setenv(EnvAllowVar, " OPERATOR_EXTRA , AWS_PROFILE,")
	out := []byte(strings.Join(ChildEnv(os.Environ()), "\n"))
	got := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			got[key] = value
		}
	}
	for _, name := range []string{"CONDUIT_DATABASE_DSN", "BUNDLE_SIGNING_KEY", "DATABASE_URL", "OIDC_CLIENT_SECRET", "NOTIFICATION_SMTP_PASSWORD", "OPERATOR_OTHER"} {
		if _, ok := got[name]; ok {
			t.Errorf("child saw %s", name)
		}
	}
	for _, secret := range []string{"hunter2", "signing-secret", "db-secret", "oidc-secret", "smtp-secret"} {
		if strings.Contains(string(out), secret) {
			t.Errorf("child environment contains %q", secret)
		}
	}
	if got["PATH"] == "" || got["PATH"] != os.Getenv("PATH") {
		t.Errorf("PATH = %q", got["PATH"])
	}
	for name, want := range map[string]string{
		"ANTHROPIC_API_KEY": "provider-key", "HTTPS_PROXY": "http://proxy.internal:3128",
		"LC_ALL": "C", "CONDUIT_PROVIDER_CLAUDE_CMD": "claude -p", "OPERATOR_EXTRA": "kept",
		EnvAllowVar: " OPERATOR_EXTRA , AWS_PROFILE,",
	} {
		if got[name] != want {
			t.Errorf("%s = %q, want %q", name, got[name], want)
		}
	}
}

func TestEnvironmentPolicySurvivesNestedModelDelegation(t *testing.T) {
	parent := []string{
		"PATH=/bin", EnvAllowVar + "=AWS_PROFILE,AWS_REGION", "AWS_PROFILE=bedrock", "AWS_REGION=us-east-1",
		"DATABASE_URL=hub-secret", "OIDC_CLIENT_SECRET=other-hub-secret",
	}
	provider := ChildEnv(ChildEnv(parent))
	got := strings.Join(provider, "|")
	for _, want := range []string{"AWS_PROFILE=bedrock", "AWS_REGION=us-east-1", EnvAllowVar + "=AWS_PROFILE,AWS_REGION"} {
		if !strings.Contains(got, want) {
			t.Fatalf("nested provider lost %q: %q", want, got)
		}
	}
	if strings.Contains(got, "hub-secret") {
		t.Fatal("nested provider inherited unrelated Hub credentials")
	}
}

func TestChildEnvWithoutOperatorExtensionDropsUnknownNames(t *testing.T) {
	env := ChildEnv([]string{"PATH=/bin", "https_proxy=http://p", "OPERATOR_EXTRA=x", "HUB_SESSION_SECRET=y", "malformed"})
	if strings.Join(env, "|") != "PATH=/bin|https_proxy=http://p" {
		t.Fatalf("env = %#v", env)
	}
}
