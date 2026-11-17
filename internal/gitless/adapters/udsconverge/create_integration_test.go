package udsconverge_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/udsconverge"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
	"github.com/jonathanagustin/conduit/internal/udsschema"
)

// The Zarf gate opts in after installing both pinned executables. Ordinary Go
// tests remain offline. Nothing here deploys, signs, or publishes externally:
// Zarf builds a file-only package in a disposable loopback registry, and UDS
// creates and inspects archives from the actual, unchanged renderer output.
func TestRenderedBundlesCreateWithPinnedCLI(t *testing.T) {
	if os.Getenv("CONDUIT_TEST_UDS_CREATE") != "1" {
		t.Skip("the Zarf gate sets CONDUIT_TEST_UDS_CREATE=1 with ZARF_BIN and UDS_BIN")
	}
	zarf, uds := os.Getenv("ZARF_BIN"), os.Getenv("UDS_BIN")
	if zarf == "" || uds == "" {
		t.Fatal("the generated-bundle gate requires both ZARF_BIN and UDS_BIN")
	}
	architecture := runtime.GOARCH
	work := t.TempDir()
	registry := newBundleFixtureRegistry(t)
	for _, dir := range []string{"home", "tmp", "docker"} {
		if err := os.Mkdir(filepath.Join(work, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	invoke := func(t *testing.T, dir, binary string, args ...string) ([]byte, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, binary, args...)
		command.Dir = dir
		// Do not inherit registry credentials, cluster configuration, proxies,
		// or a user's UDS/Zarf configuration.
		command.Env = []string{
			"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Join(work, "home"),
			"TMPDIR=" + filepath.Join(work, "tmp"),
			"DOCKER_CONFIG=" + filepath.Join(work, "docker"), "ZARF_NO_PROGRESS=true",
		}
		return command.CombinedOutput()
	}
	run := func(t *testing.T, dir, binary string, args ...string) []byte {
		t.Helper()
		out, err := invoke(t, dir, binary, args...)
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", binary, args, err, out)
		}
		return out
	}
	if got := strings.TrimSpace(string(run(t, work, uds, "version"))); got != "v"+udsschema.CLIVersion {
		t.Fatalf("UDS CLI = %q; want v%s", got, udsschema.CLIVersion)
	}
	if got := strings.TrimSpace(string(run(t, work, zarf, "version"))); got != "v1.0.0" {
		t.Fatalf("Zarf = %q; want v1.0.0", got)
	}
	writeBundleFixture(t, filepath.Join(work, "zarf.yaml"), []byte(`kind: ZarfPackageConfig
metadata:
  name: conduit-fixture
  version: 0.1.0
components:
  - name: uds
    files:
      - source: fixture.txt
        target: /tmp/conduit-uds-create-fixture.txt
`))
	writeBundleFixture(t, filepath.Join(work, "fixture.txt"), []byte("generated bundle CLI fixture; never deployed\n"))
	run(t, work, zarf, "package", "create", work, "--architecture", architecture, "--confirm", "--skip-sbom",
		"--output", "oci://"+strings.TrimSuffix(registry.repository(), "/conduit-fixture"), "--no-color", "--plain-http")
	// Capture the tag and digest Zarf actually stored; no invented digest or
	// mutable tag is used by UDS.
	tag, digest := registry.packagePin(t)
	admitted, refusals := domain.Admit(
		domain.Selector{Kind: domain.ByChannel, Repository: registry.repository(), Channel: "stable"},
		domain.Pin{Repository: registry.repository(), Digest: digest},
		domain.Attestation{Signed: true, Digest: digest, Issuer: "https://token.actions.githubusercontent.com",
			Identity: "https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/v1.2.3"},
		domain.PublisherPolicy{CertificateIssuer: issuerPtn, CertificateIdentity: identPtn},
	)
	if len(refusals) != 0 {
		t.Fatalf("fixture admission: %v", refusals)
	}
	manifests, err := udsconverge.Render("fixture", admitted, udsconverge.Options{
		ServiceAccountName: "fixture", PullSecretName: "fixture", PackageTag: tag, Architecture: architecture,
	})
	if err != nil {
		t.Fatal(err)
	}
	bundle, _, ok := strings.Cut(string(manifests), "---\n")
	if !ok {
		t.Fatal("Render did not return the bundle and Package documents")
	}
	mapped, err := udsconverge.RenderBundleFromPackages("mapped-fixture", "0.1.0", architecture, []udsconverge.BundlePackageRef{{
		Name: "conduit-fixture", Repository: registry.repository(), Tag: tag, Digest: digest,
		OptionalComponents: []string{"uds"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	createArgs := func(dir string) []string {
		// These flags are confined to the unsigned loopback HTTP fixture.
		// Archive/schema conformance does not establish signature or transport
		// security. Keep the renderer's publisher policy in the YAML unchanged.
		return []string{"create", ".", "--architecture", architecture, "--confirm", "--no-color", "--no-progress",
			"--no-log-file", "--uds-cache", filepath.Join(dir, "cache"), "--insecure", "--skip-signature-validation"}
	}
	for _, fixture := range []struct{ name, yaml string }{
		{"conduit-fixture", bundle}, {"mapped-fixture", string(mapped)},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			dir := filepath.Join(work, fixture.name)
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			writeBundleFixture(t, filepath.Join(dir, "uds-bundle.yaml"), []byte(fixture.yaml))
			run(t, dir, uds, createArgs(dir)...)
			archive := filepath.Join(dir, "uds-bundle-"+fixture.name+"-"+architecture+"-0.1.0.tar.zst")
			if info, err := os.Stat(archive); err != nil || info.Size() == 0 {
				t.Fatalf("uds create did not write a nonempty bundle archive: %v", err)
			}
			inspected := string(run(t, dir, uds, "inspect", archive, "--architecture", architecture, "--no-color", "--no-progress",
				"--no-log-file", "--uds-cache", filepath.Join(dir, "cache"), "--insecure", "--skip-signature-validation"))
			if !strings.Contains(inspected, registry.repository()) || !strings.Contains(inspected, tag+"@"+digest) {
				t.Fatalf("created bundle lost its exact package coordinate:\n%s", inspected)
			}
		})
	}
	// A schema cannot know which digest or optional component a registry
	// contains. Require the real CLI to reject both mistakes, rather than
	// accepting a test that only happens to emit a buildable happy path.
	for _, invalid := range []struct{ name, digest, component, reason string }{
		{"wrong-digest", "sha256:" + strings.Repeat("0", 64), "uds", "not found"},
		{"missing-component", digest, "not-present", "does not exist in upstream"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			dir := filepath.Join(work, invalid.name)
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			raw, err := udsconverge.RenderBundleFromPackages(invalid.name, "0.1.0", architecture, []udsconverge.BundlePackageRef{{
				Name: "conduit-fixture", Repository: registry.repository(), Tag: tag, Digest: invalid.digest,
				OptionalComponents: []string{invalid.component},
			}})
			if err != nil {
				t.Fatal(err)
			}
			writeBundleFixture(t, filepath.Join(dir, "uds-bundle.yaml"), raw)
			out, err := invoke(t, dir, uds, createArgs(dir)...)
			if err == nil || !strings.Contains(string(out), invalid.reason) {
				t.Fatalf("uds create must refuse %s with %q; error=%v\n%s", invalid.name, invalid.reason, err, out)
			}
		})
	}
}

func writeBundleFixture(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Only the OCI Distribution operations used by Zarf create and UDS create are
// needed. The real CLIs produce/consume the manifests and blobs; this server
// stores opaque bytes and verifies upload digests. It binds only to loopback.
type bundleFixtureRegistry struct {
	server                    *httptest.Server
	mu                        sync.Mutex
	blobs, manifests, uploads map[string][]byte
	mediaTypes                map[string]string
	tag                       string
}

func newBundleFixtureRegistry(t *testing.T) *bundleFixtureRegistry {
	r := &bundleFixtureRegistry{blobs: map[string][]byte{}, manifests: map[string][]byte{}, uploads: map[string][]byte{}, mediaTypes: map[string]string{}}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
		const prefix = "/v2/fixture/conduit-fixture/"
		path := strings.TrimPrefix(request.URL.Path, prefix)
		switch {
		case request.URL.Path == "/v2/":
			w.WriteHeader(http.StatusOK)
		case request.Method == http.MethodPost && path == "blobs/uploads/":
			id := fmt.Sprint(len(r.uploads))
			r.uploads[id] = nil
			w.Header().Set("Location", prefix+"blobs/uploads/"+id)
			w.WriteHeader(http.StatusAccepted)
		case strings.HasPrefix(path, "blobs/uploads/"):
			id := strings.TrimPrefix(path, "blobs/uploads/")
			body, err := io.ReadAll(io.LimitReader(request.Body, 4<<20))
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			r.uploads[id] = append(r.uploads[id], body...)
			if request.Method == http.MethodPut {
				digest := request.URL.Query().Get("digest")
				if digest != fmt.Sprintf("sha256:%x", sha256.Sum256(r.uploads[id])) {
					t.Error("Zarf upload digest does not match its bytes")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				r.blobs[digest] = r.uploads[id]
				w.Header().Set("Location", prefix+"blobs/"+digest)
				w.WriteHeader(http.StatusCreated)
			} else {
				w.Header().Set("Location", request.URL.Path)
				w.WriteHeader(http.StatusAccepted)
			}
		case strings.HasPrefix(path, "manifests/") && request.Method == http.MethodPut:
			body, err := io.ReadAll(io.LimitReader(request.Body, 4<<20))
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
			ref := strings.TrimPrefix(path, "manifests/")
			r.manifests[ref], r.manifests[digest] = body, body
			r.mediaTypes[ref], r.mediaTypes[digest] = request.Header.Get("Content-Type"), request.Header.Get("Content-Type")
			if !strings.HasPrefix(ref, "sha256:") {
				r.tag = ref
			}
			w.Header().Set("Docker-Content-Digest", digest)
			w.WriteHeader(http.StatusCreated)
		case request.Method == http.MethodGet || request.Method == http.MethodHead:
			var body []byte
			if strings.HasPrefix(path, "blobs/") {
				body = r.blobs[strings.TrimPrefix(path, "blobs/")]
				w.Header().Set("Content-Type", "application/octet-stream")
			} else if strings.HasPrefix(path, "manifests/") {
				body = r.manifests[strings.TrimPrefix(path, "manifests/")]
				w.Header().Set("Content-Type", r.mediaTypes[strings.TrimPrefix(path, "manifests/")])
			}
			if body == nil {
				http.NotFound(w, request)
				return
			}
			w.Header().Set("Docker-Content-Digest", fmt.Sprintf("sha256:%x", sha256.Sum256(body)))
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			w.WriteHeader(http.StatusOK)
			if request.Method == http.MethodGet {
				_, _ = w.Write(body)
			}
		default:
			t.Errorf("unexpected registry operation: %s %s", request.Method, request.URL)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *bundleFixtureRegistry) repository() string {
	return strings.TrimPrefix(r.server.URL, "http://") + "/fixture/conduit-fixture"
}

func (r *bundleFixtureRegistry) packagePin(t *testing.T) (string, string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tag == "" {
		t.Fatal("Zarf did not publish its package manifest")
	}
	return r.tag, fmt.Sprintf("sha256:%x", sha256.Sum256(r.manifests[r.tag]))
}
