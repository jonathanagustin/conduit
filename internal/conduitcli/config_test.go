package conduitcli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConnectionConfigRequiresOneCompleteIdentityMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		configuration ConnectionConfig
		wantError     string
	}{
		{
			name: "development groups without subject",
			configuration: ConnectionConfig{
				Server: "https://hub.example.test", Timeout: time.Second, DevelopmentGroups: []string{"operators"},
			},
			wantError: "--development-groups requires --development-subject",
		},
		{
			// secureModes counts the identity modes configured. If it stopped
			// counting up, this case would be accepted: a client would be
			// handed both a client certificate and a SPIFFE source and would
			// silently use whichever the transport reached for first.
			name: "static mTLS and SPIFFE together",
			configuration: ConnectionConfig{
				Server: "https://hub.example.test", Timeout: time.Second,
				CAFile: "ca.pem", ClientCertFile: "client.crt", ClientKeyFile: "client.key",
				SPIFFEEndpoint: "unix:///run/spire.sock", HubSPIFFEID: "spiffe://platform.delivery/hub",
			},
			wantError: "static mTLS, SPIFFE, bearer-token, and OIDC identity are mutually exclusive",
		},
		{
			// Zero is not "unset" here -- validate is reached with whatever
			// connect resolved, so a zero that survives to this point is a
			// timeout of zero, and a client that times out immediately looks
			// exactly like a Hub that is refusing every call.
			name: "zero timeout",
			configuration: ConnectionConfig{
				Server: "https://hub.example.test", CAFile: "ca.pem",
				ClientCertFile: "client.crt", ClientKeyFile: "client.key",
			},
			wantError: "--timeout must be greater than zero",
		},
		{
			name: "negative timeout",
			configuration: ConnectionConfig{
				Server: "https://hub.example.test", Timeout: -time.Second, CAFile: "ca.pem",
				ClientCertFile: "client.crt", ClientKeyFile: "client.key",
			},
			wantError: "--timeout must be greater than zero",
		},
		{
			name:          "missing identity",
			configuration: ConnectionConfig{Server: "https://hub.example.test", Timeout: time.Second},
			wantError:     "configure static mTLS, SPIFFE, bearer-token, or OIDC identity",
		},
		{
			name: "partial static identity",
			configuration: ConnectionConfig{
				Server: "https://hub.example.test", Timeout: time.Second, CAFile: "ca.pem",
			},
			wantError: "configure static mTLS, SPIFFE, bearer-token, or OIDC identity",
		},
		{
			name: "partial client certificate",
			configuration: ConnectionConfig{
				Server: "https://hub.example.test", Timeout: time.Second, ClientCertFile: "client.crt",
			},
			wantError: "--client-cert-file and --client-key-file must be supplied together",
		},
		{
			name: "partial SPIFFE identity",
			configuration: ConnectionConfig{
				Server: "https://hub.example.test", Timeout: time.Second, SPIFFEEndpoint: "unix:///run/spire.sock",
			},
			wantError: "--spiffe-endpoint and --hub-spiffe-id must be supplied together",
		},
		{
			name: "remote development identity",
			configuration: ConnectionConfig{
				Server: "http://hub.example.test", Timeout: time.Second, DevelopmentSubject: "developer",
			},
			wantError: "--development-subject is allowed only with a loopback http server",
		},
		{
			name: "valid SPIFFE identity",
			configuration: ConnectionConfig{
				Server: "https://hub.example.test", Timeout: time.Second,
				SPIFFEEndpoint: "unix:///run/spire.sock", HubSPIFFEID: "spiffe://platform.delivery/hub",
			},
		},
		{
			name: "valid bearer identity",
			configuration: ConnectionConfig{
				Server: "https://hub.example.test", Timeout: time.Second, BearerTokenFile: "multipass.token",
			},
		},
		{
			name: "valid oidc identity",
			configuration: ConnectionConfig{
				Server: "https://hub.example.test", Timeout: time.Second,
				OIDCIssuer: "https://issuer.example.test/realms/main", OIDCClientID: "conduit",
				OIDCClientSecretFile: "client.secret",
			},
		},
		{
			name: "partial oidc identity",
			configuration: ConnectionConfig{
				Server: "https://hub.example.test", Timeout: time.Second, OIDCIssuer: "https://issuer.example.test",
			},
			wantError: "--oidc-issuer, --oidc-client-id, and --oidc-client-secret-file must be supplied together",
		},
		{
			name: "plaintext oidc issuer",
			configuration: ConnectionConfig{
				Server: "https://hub.example.test", Timeout: time.Second,
				OIDCIssuer: "http://issuer.example.test", OIDCClientID: "conduit", OIDCClientSecretFile: "client.secret",
			},
			wantError: "--oidc-issuer must be an absolute https URL without userinfo, query, or fragment",
		},
		{
			name: "oidc and bearer together",
			configuration: ConnectionConfig{
				Server: "https://hub.example.test", Timeout: time.Second, BearerTokenFile: "multipass.token",
				OIDCIssuer: "https://issuer.example.test", OIDCClientID: "conduit", OIDCClientSecretFile: "client.secret",
			},
			wantError: "static mTLS, SPIFFE, bearer-token, and OIDC identity are mutually exclusive",
		},
		{
			name: "bearer and SPIFFE together",
			configuration: ConnectionConfig{
				Server: "https://hub.example.test", Timeout: time.Second, BearerTokenFile: "multipass.token",
				SPIFFEEndpoint: "unix:///run/spire.sock", HubSPIFFEID: "spiffe://platform.delivery/hub",
			},
			wantError: "static mTLS, SPIFFE, bearer-token, and OIDC identity are mutually exclusive",
		},
		{
			name: "valid loopback development identity",
			configuration: ConnectionConfig{
				Server: "http://127.0.0.1:8081", Timeout: time.Second, DevelopmentSubject: "developer",
			},
		},
		{
			// Request paths are absolute, so a base path is dropped rather than
			// prefixed and every call would silently reach the wrong URL.
			name: "server carrying a path prefix",
			configuration: ConnectionConfig{
				Server: "https://hub.example.test/platform", Timeout: time.Second,
				SPIFFEEndpoint: "unix:///run/spire.sock", HubSPIFFEID: "spiffe://platform.delivery/hub",
			},
			wantError: "--server must name a scheme and host without a path, query, or fragment",
		},
		{
			name: "server with a query string",
			configuration: ConnectionConfig{
				Server: "https://hub.example.test/?tenant=a", Timeout: time.Second,
				SPIFFEEndpoint: "unix:///run/spire.sock", HubSPIFFEID: "spiffe://platform.delivery/hub",
			},
			wantError: "--server must name a scheme and host without a path, query, or fragment",
		},
		{
			name: "bare trailing slash remains valid",
			configuration: ConnectionConfig{
				Server: "https://hub.example.test/", Timeout: time.Second,
				SPIFFEEndpoint: "unix:///run/spire.sock", HubSPIFFEID: "spiffe://platform.delivery/hub",
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := test.configuration.validate()
			if test.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if test.wantError != "" && (err == nil || err.Error() != test.wantError) {
				t.Fatalf("error=%v, want %q", err, test.wantError)
			}
		})
	}
}

func TestBearerTokenFileAndTransportAreBoundedAndDoNotMutateRequests(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "multipass.token")
	if err := os.WriteFile(path, []byte("signed-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := readBearerTokenFile(path)
	if err != nil || token != "signed-token" {
		t.Fatalf("token=%q error=%v", token, err)
	}
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authorization = request.Header.Get("Authorization")
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	transport := bearerTokenTransport{base: http.DefaultTransport, token: token}
	request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if authorization != "Bearer signed-token" || request.Header.Get("Authorization") != "" {
		t.Fatalf("authorization=%q original=%q", authorization, request.Header.Get("Authorization"))
	}

	for name, contents := range map[string]string{
		"empty": "\n", "multiple values": "one two", "oversized": strings.Repeat("x", maximumBearerTokenBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			invalid := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(invalid, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readBearerTokenFile(invalid); err == nil {
				t.Fatal("invalid bearer token file accepted")
			}
		})
	}
}

func TestDevelopmentIdentityDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()
	destinationCalled := false
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		destinationCalled = true
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Platform-Subject") != "local-developer" {
			t.Fatal("source request omitted development identity")
		}
		http.Redirect(response, request, destination.URL, http.StatusFound)
	}))
	defer source.Close()
	configuration := ConnectionConfig{
		Server: source.URL, Timeout: time.Second, DevelopmentSubject: "local-developer",
	}
	connection, err := configuration.connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.cleanup()
	response, err := connection.client.Get(source.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusFound || destinationCalled {
		t.Fatalf("status=%d destinationCalled=%t", response.StatusCode, destinationCalled)
	}
}

func TestDevelopmentIdentityTransportAddsHeadersWithoutMutatingRequest(t *testing.T) {
	t.Parallel()
	var subject, groups string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		subject = request.Header.Get("X-Platform-Subject")
		groups = request.Header.Get("X-Platform-Groups")
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	configuration := ConnectionConfig{
		Server: server.URL, Timeout: time.Second, DevelopmentSubject: "local-developer",
		DevelopmentGroups: []string{"release-reviewers", " release-reviewers ", "security"},
	}
	connection, err := configuration.connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.cleanup()
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := connection.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if subject != "local-developer" || groups != "release-reviewers,security" {
		t.Fatalf("identity headers subject=%q groups=%q", subject, groups)
	}
	if request.Header.Get("X-Platform-Subject") != "" {
		t.Fatal("development transport mutated the caller request")
	}
}

// An unset timeout takes the default; a set one is left alone. Getting this
// backwards is not a crash -- every call simply runs on a timeout nobody
// chose, and a client that gives up early is indistinguishable from a Hub
// that is slow.
func TestConnectAppliesTheDefaultTimeoutOnlyWhenUnset(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	for name, test := range map[string]struct {
		configured time.Duration
		want       time.Duration
	}{
		"unset takes the default": {configured: 0, want: defaultTimeout},
		"a set timeout is kept":   {configured: 3 * time.Second, want: 3 * time.Second},
	} {
		configuration := ConnectionConfig{
			Server: server.URL, Timeout: test.configured, DevelopmentSubject: "local-developer",
		}
		connection, err := configuration.connect(context.Background())
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if connection.client.Timeout != test.want {
			t.Errorf("%s: client timeout = %v, want %v", name, connection.client.Timeout, test.want)
		}
		connection.cleanup()
	}
}
