package conduitcli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/oidcfake"
)

func TestRetainedLoginRefreshesAndRotatesWithoutLeakingCredentials(t *testing.T) {
	for _, kind := range []string{"id", "access"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newLoginFixture(t, oidcfake.Options{})
			if err := fixture.run(context.Background(), "login", "--token", kind, "--store-refresh-token"); err != nil {
				t.Fatal(err)
			}
			previous, err := readRefreshGrant(fixture.tokenFile)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				before := len(fixture.provider.TokenRequests())
				if err := fixture.run(context.Background(), "login", "--refresh"); err != nil {
					t.Fatal(err)
				}
				next, err := readRefreshGrant(fixture.tokenFile)
				if err != nil {
					t.Fatal(err)
				}
				if next.RefreshToken == previous.RefreshToken || next.TokenKind != kind || next.Subject != previous.Subject {
					t.Fatal("rotation or identity was not retained")
				}
				requests := fixture.provider.TokenRequests()
				if len(requests) != before+1 {
					t.Fatal("refresh repeated a credential exchange")
				}
				request := requests[len(requests)-1]
				if request.Form.Get("grant_type") != "refresh_token" || request.Form.Get("refresh_token") != previous.RefreshToken || request.Form.Get("client_id") != "conduit-cli" || request.Form.Has("client_secret") || request.BasicPresent {
					t.Fatal("incorrect public-client refresh exchange")
				}
				token, err := readBearerTokenFile(fixture.tokenFile)
				if err != nil || token != next.Token {
					t.Fatal("credential bundle is not usable by bearer transport", err)
				}
				output := fixture.stdout.String() + fixture.stderr.String()
				for _, secret := range []string{previous.Token, previous.RefreshToken, next.Token, next.RefreshToken} {
					if strings.Contains(output, secret) {
						t.Fatal("credential appeared in output")
					}
				}
				if runtime.GOOS != "windows" {
					info, err := os.Stat(fixture.tokenFile)
					if err != nil || info.Mode().Perm() != 0o600 {
						t.Fatal("credential permissions", err)
					}
				}
				previous = next
			}
			if err := fixture.run(context.Background(), "logout"); err != nil {
				t.Fatal(err)
			}
			fixture.requireNoToken()
		})
	}
}

func TestRefreshRefusesChangedIdentityAndPreservesTheOriginalFile(t *testing.T) {
	for _, claim := range []string{"sub", "iss", "aud", "azp"} {
		t.Run(claim, func(t *testing.T) {
			fixture := newLoginFixture(t, oidcfake.Options{})
			if err := fixture.run(context.Background(), "login", "--store-refresh-token"); err != nil {
				t.Fatal(err)
			}
			original := fixture.storedToken()
			fixture.provider.MutateClaims(func(claims map[string]any) { claims[claim] = "another-identity" })
			if err := fixture.run(context.Background(), "login", "--refresh"); err == nil {
				t.Fatal("refresh accepted a changed identity")
			}
			if fixture.storedToken() != original {
				t.Fatal("refused refresh changed stored credentials")
			}
			if _, err := os.Lstat(fixture.tokenFile + ".lock"); !os.IsNotExist(err) {
				t.Fatal("refresh left a writer lock")
			}
		})
	}
}

func TestRefreshRefusesMismatchedConfigurationBeforeSpendingTheGrant(t *testing.T) {
	for _, field := range []string{"issuer", "clientId"} {
		t.Run(field, func(t *testing.T) {
			fixture := newLoginFixture(t, oidcfake.Options{})
			if err := fixture.run(context.Background(), "login", "--store-refresh-token"); err != nil {
				t.Fatal(err)
			}
			var bundle map[string]any
			if err := json.Unmarshal([]byte(fixture.storedToken()), &bundle); err != nil {
				t.Fatal(err)
			}
			bundle[field] = "different-client"
			if field == "issuer" {
				bundle[field] = "https://other-issuer.example.test"
			}
			contents, err := json.Marshal(bundle)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fixture.tokenFile, contents, 0o600); err != nil {
				t.Fatal(err)
			}
			before := len(fixture.provider.TokenRequests())
			if err := fixture.run(context.Background(), "login", "--refresh"); err == nil || !strings.Contains(err.Error(), "does not match") {
				t.Fatal("missing issuer/client refusal", err)
			}
			if len(fixture.provider.TokenRequests()) != before {
				t.Fatal("spent grant despite configuration mismatch")
			}
			if fixture.storedToken() != string(contents) {
				t.Fatal("changed file on mismatch")
			}
		})
	}
}

func TestRefreshRefusesPlainCredentialsAndConcurrentWriters(t *testing.T) {
	fixture := newLoginFixture(t, oidcfake.Options{})
	if err := fixture.run(context.Background(), "login"); err != nil {
		t.Fatal(err)
	}
	original := fixture.storedToken()
	before := len(fixture.provider.TokenRequests())
	if err := fixture.run(context.Background(), "login", "--refresh"); err == nil || !strings.Contains(err.Error(), "no refresh grant") {
		t.Fatal("plain token was treated as refresh grant", err)
	}
	unlock, err := lockCredentialFile(fixture.tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	for _, command := range [][]string{{"login", "--store-refresh-token"}, {"logout"}, {"login", "--refresh"}} {
		if err := fixture.run(context.Background(), command...); err == nil || !strings.Contains(err.Error(), "locked") {
			t.Fatal("concurrent credential writer was not refused", err)
		}
	}
	if len(fixture.provider.TokenRequests()) != before || fixture.storedToken() != original {
		t.Fatal("refused operation spent a grant or changed credentials")
	}
}

func TestRefreshRefusesRevokedGrantWithoutPrintingIt(t *testing.T) {
	fixture := newLoginFixture(t, oidcfake.Options{})
	if err := fixture.run(context.Background(), "login", "--store-refresh-token"); err != nil {
		t.Fatal(err)
	}
	original := fixture.storedToken()
	if err := fixture.run(context.Background(), "login", "--refresh"); err != nil {
		t.Fatal(err)
	}
	// Restore the consumed credential deliberately: the fixture rotates and
	// rejects reuse, so this exercises an actual invalid_grant response.
	if err := os.WriteFile(fixture.tokenFile, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	err := fixture.run(context.Background(), "login", "--refresh")
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatal("revoked grant was not refused", err)
	}
	if fixture.storedToken() != original {
		t.Fatal("issuer refusal replaced credentials")
	}
	grant, err := decodeRefreshGrant(original)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fixture.stdout.String()+fixture.stderr.String(), grant.RefreshToken) {
		t.Fatal("refused grant leaked")
	}
}

func TestRefreshHandlesOmittedAndMalformedReplacementTokens(t *testing.T) {
	for _, replacement := range []string{"", "invalid replacement"} {
		t.Run(replacement, func(t *testing.T) {
			fixture := newLoginFixture(t, oidcfake.Options{})
			if err := fixture.run(context.Background(), "login", "--store-refresh-token"); err != nil {
				t.Fatal(err)
			}
			original := fixture.storedToken()
			grant, err := readRefreshGrant(fixture.tokenFile)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Form.Get("refresh_token") != grant.RefreshToken {
					t.Error("missing retained grant")
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tokenResponse{IDToken: grant.Token, RefreshToken: replacement})
			}))
			defer server.Close()
			_, err = refreshStoredGrant(t.Context(), server.Client(), oidcEndpoints{TokenEndpoint: server.URL}, grant, fixture.tokenFile, time.Second)
			if replacement != "" {
				if err == nil || fixture.storedToken() != original {
					t.Fatal("malformed rotated credential was committed", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				updated, err := readRefreshGrant(fixture.tokenFile)
				if err != nil || updated.RefreshToken != grant.RefreshToken {
					t.Fatal("omitted replacement lost the original grant", err)
				}
			}
		})
	}
}
