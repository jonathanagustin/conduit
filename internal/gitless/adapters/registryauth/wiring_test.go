package registryauth_test

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/ocipublish"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/ociresolve"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/registryauth"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

const acceptanceDigest = "sha256:bb22" +
	"000000000000000000000000000000000000000000000000000000000000"

func TestDockerConfigCredentialsReachAChallengedSourceRegistry(t *testing.T) {
	credentials := config(t, "registry.example", "reader", "rotating-token")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v2/team/foundry/manifests/release":
			username, password, ok := request.BasicAuth()
			if !ok {
				writer.Header().Set("WWW-Authenticate", `Basic realm="registry.example"`)
				writer.WriteHeader(http.StatusUnauthorized)
				return
			}
			if username != "reader" || password != "rotating-token" {
				writer.WriteHeader(http.StatusForbidden)
				return
			}
			writer.Header().Set("Docker-Content-Digest", acceptanceDigest)
			writer.WriteHeader(http.StatusOK)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	resolver := ociresolve.New(&http.Client{Transport: rewrite{host: target.Host}}, credentials)
	pin, err := resolver.Resolve(t.Context(), domain.Selector{
		Kind: domain.ByChannel, Repository: "registry.example/team/foundry", Channel: "release",
	})
	if err != nil {
		t.Fatalf("resolve private source: %v", err)
	}
	if pin.Digest != acceptanceDigest {
		t.Fatalf("resolved %s, want %s", pin.Digest, acceptanceDigest)
	}
}

func TestPrivateRegistryPublicationAcceptance(t *testing.T) {
	repository := os.Getenv("GITLESS_REGISTRY_ACCEPTANCE_REPOSITORY")
	configPath := os.Getenv("GITLESS_REGISTRY_CONFIG")
	if repository == "" || configPath == "" {
		t.Skip("set GITLESS_REGISTRY_ACCEPTANCE_REPOSITORY and GITLESS_REGISTRY_CONFIG")
	}
	credentials, err := registryauth.Load(configPath)
	if err != nil {
		t.Fatalf("load real registry credentials: %v", err)
	}
	publisher, err := ocipublish.New(repository, nil, credentials, true)
	if err != nil {
		t.Fatalf("build private-registry publisher: %v", err)
	}
	manifest := []byte("apiVersion: source.toolkit.fluxcd.io/v1\n" +
		"kind: OCIRepository\nspec:\n  ref:\n    digest: " + acceptanceDigest + "\n")
	if err := publisher.Put(t.Context(), "private-acceptance", manifest); err != nil {
		t.Fatalf("publish through mounted credentials: %v", err)
	}
	current, err := publisher.CurrentDigest(t.Context(), "private-acceptance")
	if err != nil {
		t.Fatalf("read private publication: %v", err)
	}
	if current != acceptanceDigest {
		t.Fatalf("read source digest %q, want %q", current, acceptanceDigest)
	}
}

type rewrite struct{ host string }

func (transport rewrite) RoundTrip(request *http.Request) (*http.Response, error) {
	request.URL.Scheme, request.URL.Host = "http", transport.host
	return http.DefaultTransport.RoundTrip(request)
}

func config(t *testing.T, host, username, password string) *registryauth.DockerConfig {
	t.Helper()
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	body := `{"auths":{"` + host + `":{"auth":"` + auth + `"}}}`
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	credentials, err := registryauth.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return credentials
}

func TestAcceptanceDigestIsExact(t *testing.T) {
	if !strings.HasPrefix(acceptanceDigest, "sha256:") || len(acceptanceDigest) != 71 {
		t.Fatalf("acceptance digest is not an exact SHA-256 coordinate: %q", acceptanceDigest)
	}
}
