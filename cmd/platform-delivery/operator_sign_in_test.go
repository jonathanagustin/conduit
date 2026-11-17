package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	identitycomposition "github.com/jonathanagustin/conduit/internal/identity/composition"
	"github.com/jonathanagustin/conduit/internal/identity/oidcfake"
)

func signInEnvironment(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

// loadWithMemorySessions is a Hub on the file repository: configureReconciliation
// opened no durable session store, so none is handed over.
func loadWithMemorySessions(ctx context.Context, getenv func(string) string) (identitycomposition.OperatorIdentity, func(), error) {
	return loadOperatorIdentity(ctx, getenv, nil)
}

func TestSignInIsOffUnlessBothAnIssuerAndAPublicAddressAreConfigured(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	resolved, closer, err := loadWithMemorySessions(ctx, signInEnvironment(nil))
	closer()
	if err != nil || resolved.Resolver != nil || resolved.SignIn != nil {
		t.Fatalf("an unconfigured Hub gave %#v %v", resolved, err)
	}
	// A deployment may state its address without running an issuer.
	resolved, closer, err = loadWithMemorySessions(ctx, signInEnvironment(map[string]string{
		"CONSOLE_PUBLIC_URL": "https://conduit.example.test",
	}))
	closer()
	if err != nil || resolved.Resolver != nil || resolved.SignIn != nil {
		t.Fatalf("a public address with no issuer gave %#v %v", resolved, err)
	}
	// An issuer with no public address verifies bearer tokens and nothing more.
	resolved, closer, err = loadWithMemorySessions(ctx, signInEnvironment(map[string]string{
		"OPERATOR_OIDC_ISSUER": "https://identity.example.test", "OPERATOR_OIDC_CLIENT_ID": "conduit",
		"OPERATOR_OIDC_JWKS_URL": "https://identity.example.test/jwks.json",
	}))
	closer()
	if err != nil || resolved.Resolver == nil || resolved.SignIn != nil {
		t.Fatalf("an issuer with no public address gave %#v %v", resolved, err)
	}
}

func TestHalfConfiguredSignInStopsTheHub(t *testing.T) {
	t.Parallel()
	provider, err := oidcfake.Start(oidcfake.Options{ClientID: "conduit"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(provider.Close)
	directory := t.TempDir()
	write := func(name, contents string) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	caFile := write("ca.pem", string(provider.CertificatePEM()))
	secretFile := write("secret", "s3cret\n")
	base := func(extra map[string]string) map[string]string {
		values := map[string]string{
			"OPERATOR_OIDC_ISSUER": provider.Issuer(), "OPERATOR_OIDC_CLIENT_ID": "conduit",
			"OPERATOR_OIDC_CA_FILE": caFile, "CONSOLE_PUBLIC_URL": "https://conduit.example.test",
		}
		for name, value := range extra {
			if value == "" {
				delete(values, name)
			} else {
				values[name] = value
			}
		}
		return values
	}
	refused := map[string]struct {
		values map[string]string
		want   string
	}{
		"a session lifetime with no issuer": {
			map[string]string{"OPERATOR_SESSION_MAX_LIFETIME": "8h"}, "OPERATOR_OIDC_ISSUER"},
		"a client secret with no issuer": {
			map[string]string{"OPERATOR_OIDC_CLIENT_SECRET_FILE": secretFile}, "OPERATOR_OIDC_ISSUER"},
		"a client secret with no public address": {
			base(map[string]string{"CONSOLE_PUBLIC_URL": "", "OPERATOR_OIDC_CLIENT_SECRET_FILE": secretFile}), "CONSOLE_PUBLIC_URL"},
		"a session lifetime with no public address": {
			base(map[string]string{"CONSOLE_PUBLIC_URL": "", "OPERATOR_SESSION_MAX_LIFETIME": "8h"}), "CONSOLE_PUBLIC_URL"},
		"a public address over http": {
			base(map[string]string{"CONSOLE_PUBLIC_URL": "http://conduit.example.test"}), "https origin"},
		"a public address with a path": {
			base(map[string]string{"CONSOLE_PUBLIC_URL": "https://conduit.example.test/console/"}), "https origin"},
		"a session lifetime below the minimum": {
			base(map[string]string{"OPERATOR_SESSION_MAX_LIFETIME": "1m"}), "OPERATOR_SESSION_MAX_LIFETIME"},
		"a session lifetime above the maximum": {
			base(map[string]string{"OPERATOR_SESSION_MAX_LIFETIME": "48h"}), "OPERATOR_SESSION_MAX_LIFETIME"},
		"a session lifetime of zero": {
			base(map[string]string{"OPERATOR_SESSION_MAX_LIFETIME": "0"}), "OPERATOR_SESSION_MAX_LIFETIME"},
		"a session lifetime that is not a duration": {
			base(map[string]string{"OPERATOR_SESSION_MAX_LIFETIME": "eight hours"}), "OPERATOR_SESSION_MAX_LIFETIME"},
		"a client secret file that does not exist": {
			base(map[string]string{"OPERATOR_OIDC_CLIENT_SECRET_FILE": filepath.Join(directory, "absent")}), "OPERATOR_OIDC_CLIENT_SECRET_FILE"},
		"a client secret file of two lines": {
			base(map[string]string{"OPERATOR_OIDC_CLIENT_SECRET_FILE": write("two", "a\nb\n")}), "OPERATOR_OIDC_CLIENT_SECRET_FILE"},
		"an unknown repository kind": {
			base(map[string]string{"REPOSITORY_KIND": "sqlite"}), "REPOSITORY_KIND"},
		// Replicas must share sessions; memory is never substituted.
		"PostgreSQL with no durable session store": {
			base(map[string]string{"REPOSITORY_KIND": "postgres"}), "PostgreSQL operator session store"},
	}
	for name, test := range refused {
		resolved, closer, err := loadWithMemorySessions(context.Background(), signInEnvironment(test.values))
		closer()
		if err == nil || !strings.Contains(err.Error(), test.want) || resolved.Resolver != nil || resolved.SignIn != nil {
			t.Errorf("%s gave %#v %v, want an error naming %s", name, resolved, err, test.want)
		}
		if err != nil && strings.Contains(err.Error(), "s3cret") {
			t.Errorf("%s: the error carries the client secret", name)
		}
	}

	resolved, closer, err := loadWithMemorySessions(context.Background(), signInEnvironment(base(map[string]string{
		"OPERATOR_OIDC_CLIENT_SECRET_FILE": secretFile, "OPERATOR_SESSION_MAX_LIFETIME": "30m",
	})))
	defer closer()
	if err != nil || resolved.Resolver == nil || resolved.SignIn == nil ||
		resolved.SignIn.RedirectURI() != "https://conduit.example.test/v1/auth/callback" ||
		resolved.PublicOrigin != "https://conduit.example.test" {
		t.Fatalf("a fully configured Hub gave %#v %v", resolved, err)
	}
}
