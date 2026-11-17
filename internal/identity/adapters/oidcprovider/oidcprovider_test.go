package oidcprovider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jonathanagustin/conduit/internal/identity/adapters/oidcprovider"
)

// issuer is a TLS server that publishes whatever discovery document a test
// asks for, with its own address as the issuer.
type issuer struct {
	server *httptest.Server
	// elsewhere counts requests to the address a token endpoint redirects to.
	elsewhere atomic.Int32
	tokenHits atomic.Int32
}

func startIssuer(t *testing.T, edit func(base string, document map[string]any), token http.HandlerFunc) *issuer {
	t.Helper()
	started := &issuer{}
	mux := http.NewServeMux()
	started.server = httptest.NewTLSServer(mux)
	t.Cleanup(started.server.Close)
	base := started.server.URL
	mux.HandleFunc("GET /.well-known/openid-configuration", func(writer http.ResponseWriter, _ *http.Request) {
		document := map[string]any{
			"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token",
			"jwks_uri": base + "/jwks", "code_challenge_methods_supported": []string{"S256"},
		}
		if edit != nil {
			edit(base, document)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(document)
	})
	if token == nil {
		token = func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"id_token":"header.payload.signature"}`))
		}
	}
	mux.HandleFunc("POST /token", func(writer http.ResponseWriter, request *http.Request) {
		started.tokenHits.Add(1)
		token(writer, request)
	})
	// Answers as a token endpoint would, so that a client which followed a
	// redirect here would come away with a token.
	mux.HandleFunc("/elsewhere", func(writer http.ResponseWriter, _ *http.Request) {
		started.elsewhere.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id_token":"header.payload.signature"}`))
	})
	return started
}

func (started *issuer) provider(t *testing.T, secret string) (*oidcprovider.Provider, error) {
	t.Helper()
	return oidcprovider.New(context.Background(), oidcprovider.Config{
		Issuer: started.server.URL, ClientID: "conduit", ClientSecret: secret, HTTPClient: started.server.Client(),
	})
}

func TestExchangeReturnsTheIDTokenAndSendsTheSecretOnlyInTheBasicHeader(t *testing.T) {
	t.Parallel()
	var form map[string][]string
	var user, password string
	var basic bool
	started := startIssuer(t, nil, func(writer http.ResponseWriter, request *http.Request) {
		_ = request.ParseForm()
		form = request.PostForm
		user, password, basic = request.BasicAuth()
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id_token":"header.payload.signature","access_token":"unused"}`))
	})
	provider, err := started.provider(t, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	token, err := provider.Exchange(context.Background(), "the-code", "the-verifier", "https://conduit.example.test/v1/auth/callback")
	if err != nil || token != "header.payload.signature" {
		t.Fatalf("exchange gave %q %v", token, err)
	}
	if !basic || user != "conduit" || password != "s3cret" {
		t.Fatalf("the client did not authenticate with client_secret_basic: %q %v", user, basic)
	}
	if _, inBody := form["client_secret"]; inBody {
		t.Fatal("the client secret was also sent in the body")
	}
	for name, want := range map[string]string{
		"grant_type": "authorization_code", "code": "the-code", "code_verifier": "the-verifier",
		"redirect_uri": "https://conduit.example.test/v1/auth/callback",
	} {
		if got := form[name]; len(got) != 1 || got[0] != want {
			t.Errorf("%s was sent as %q, want %q", name, got, want)
		}
	}
}

// Following a redirect from the token endpoint would carry the authorization
// code, the PKCE verifier and the client secret to wherever it points.
func TestExchangeDoesNotFollowARedirectFromTheTokenEndpoint(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect, http.StatusFound} {
		var started *issuer
		started = startIssuer(t, nil, func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(writer, request, started.server.URL+"/elsewhere", status)
		})
		provider, err := started.provider(t, "s3cret")
		if err != nil {
			t.Fatal(err)
		}
		token, err := provider.Exchange(context.Background(), "the-code", "the-verifier", "https://conduit.example.test/v1/auth/callback")
		if err == nil || token != "" {
			t.Errorf("%d: exchange gave %q %v, want a refusal", status, token, err)
		}
		if started.elsewhere.Load() != 0 {
			t.Errorf("%d: the redirect was followed", status)
		}
		if started.tokenHits.Load() != 1 {
			t.Errorf("%d: the token endpoint was called %d times", status, started.tokenHits.Load())
		}
	}
}

func TestExchangeErrorsCarryNothingTheProviderSent(t *testing.T) {
	t.Parallel()
	for name, answer := range map[string]http.HandlerFunc{
		"a refusal": func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":"invalid_grant","error_description":"LEAKED-DETAIL"}`))
		},
		// A status that is not 200 is a refusal whatever rides in its body.
		"a refusal carrying a token": func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"id_token":"LEAKED-DETAIL.payload.signature"}`))
		},
		"no ID token": func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte(`{"access_token":"LEAKED-DETAIL"}`))
		},
		"not JSON": func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte(`LEAKED-DETAIL`))
		},
	} {
		started := startIssuer(t, nil, answer)
		provider, err := started.provider(t, "s3cret")
		if err != nil {
			t.Fatal(err)
		}
		token, err := provider.Exchange(context.Background(), "the-code", "the-verifier", "https://conduit.example.test/v1/auth/callback")
		if err == nil || token != "" {
			t.Errorf("%s: exchange gave %q %v, want a refusal", name, token, err)
			continue
		}
		for _, secret := range []string{"LEAKED-DETAIL", "the-code", "the-verifier", "s3cret"} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%s: the error carries %q: %v", name, secret, err)
			}
		}
	}
}

func TestAnIssuerABrowserSignInCannotSafelyUseIsRefusedAtStart(t *testing.T) {
	t.Parallel()
	for name, edit := range map[string]func(base string, document map[string]any){
		"PKCE without S256": func(_ string, document map[string]any) {
			document["code_challenge_methods_supported"] = []string{"plain"}
		},
		"a plain-http authorization endpoint": func(base string, document map[string]any) {
			document["authorization_endpoint"] = "http" + strings.TrimPrefix(base, "https") + "/authorize"
		},
		"a plain-http token endpoint": func(base string, document map[string]any) {
			document["token_endpoint"] = "http" + strings.TrimPrefix(base, "https") + "/token"
		},
		"no token endpoint": func(_ string, document map[string]any) { delete(document, "token_endpoint") },
		"a plain-http end-session endpoint": func(base string, document map[string]any) {
			document["end_session_endpoint"] = "http" + strings.TrimPrefix(base, "https") + "/logout"
		},
		"an authorization endpoint with userinfo": func(base string, document map[string]any) {
			document["authorization_endpoint"] = "https://user:pass@" + strings.TrimPrefix(base, "https://") + "/authorize"
		},
	} {
		started := startIssuer(t, edit, nil)
		if provider, err := started.provider(t, ""); err == nil {
			t.Errorf("%s: accepted, with authorization endpoint %s", name, provider.AuthorizationEndpoint())
		}
	}
	// The same server, unedited, is accepted: the refusals above are about the
	// edits and not about the fixture.
	started := startIssuer(t, func(base string, document map[string]any) {
		document["end_session_endpoint"] = base + "/logout"
	}, nil)
	provider, err := started.provider(t, "")
	if err != nil || provider.EndSessionEndpoint() != started.server.URL+"/logout" {
		t.Fatalf("a sound issuer gave %v", err)
	}
	if _, err := oidcprovider.New(context.Background(), oidcprovider.Config{
		Issuer: started.server.URL, ClientID: " ", HTTPClient: started.server.Client(),
	}); err == nil {
		t.Fatal("a provider with no client ID was accepted")
	}
}

// A public client names itself in the body and sends no secret anywhere.
func TestAPublicClientSendsItsIDAndNoSecret(t *testing.T) {
	t.Parallel()
	var form map[string][]string
	var basic bool
	started := startIssuer(t, nil, func(writer http.ResponseWriter, request *http.Request) {
		_ = request.ParseForm()
		form = request.PostForm
		_, _, basic = request.BasicAuth()
		_, _ = writer.Write([]byte(`{"id_token":"header.payload.signature"}`))
	})
	provider, err := started.provider(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Exchange(context.Background(), "the-code", "the-verifier", "https://conduit.example.test/v1/auth/callback"); err != nil {
		t.Fatal(err)
	}
	if basic || len(form["client_secret"]) != 0 || len(form["client_id"]) != 1 || form["client_id"][0] != "conduit" {
		t.Fatalf("a public client sent basic=%v form=%v", basic, form)
	}
}
