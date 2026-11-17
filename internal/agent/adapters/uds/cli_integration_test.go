package uds

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestPinnedCLIRejectsUntrustedBundleAndIsolatesConfiguration(t *testing.T) {
	if os.Getenv("CONDUIT_TEST_UDS_EXECUTOR_CLI") != "1" {
		t.Skip("set CONDUIT_TEST_UDS_EXECUTOR_CLI=1 with pinned ZARF_BIN and UDS_BIN")
	}
	zarf, uds := os.Getenv("ZARF_BIN"), os.Getenv("UDS_BIN")
	if zarf == "" || uds == "" {
		t.Fatal("explicit pinned ZARF_BIN and UDS_BIN are required")
	}
	root := t.TempDir()
	// Both CLIs must ignore these real inherited configuration mechanisms.
	poison := filepath.Join(root, "poison.yaml")
	writeCLIFile(t, poison, []byte("invalid: [configuration\n"))
	t.Setenv("UDS_CONFIG", poison)
	t.Setenv("ZARF_CONFIG", poison)
	t.Setenv("UDS_UNREVIEWED", "ambient-unreviewed")
	t.Setenv("ZARF_VAR_UNREVIEWED", "ambient-unreviewed")
	t.Setenv("UDS_SKIP_SIGNATURE_VALIDATION", "true")
	t.Setenv("CLI_FEATURES", "NextMode=true")
	run := func(binary string, args ...string) []byte {
		t.Helper()
		out, err := (scopedRunner{}).Run(t.Context(), binary, args...)
		if err != nil {
			t.Fatalf("pinned CLI %s %q: %v\n%s", binary, args, err, out)
		}
		return out
	}
	if out := string(run(zarf, "version")); !strings.Contains(out, "v0.82.0") {
		t.Fatalf("expected Zarf v0.82.0, got %s", out)
	}
	if out := string(run(uds, "version")); !strings.Contains(out, "v0.41.0") {
		t.Fatalf("expected UDS v0.41.0, got %s", out)
	}
	packageDir := filepath.Join(root, "package")
	if err := os.Mkdir(packageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(packageDir, "zarf.yaml"), []byte(`kind: ZarfPackageConfig
metadata:
  name: conduit-signature-fixture
  version: 1.0.0
variables:
  - name: REVIEWED
    default: package-default
  - name: UNREVIEWED
    default: package-default
components:
  - name: fixture
    required: true
    manifests:
      - name: values
        namespace: fixture
        files:
          - config.yaml
`))
	writeCLIFile(t, filepath.Join(packageDir, "config.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: fixture\ndata:\n  reviewed: '###ZARF_VAR_REVIEWED###'\n  unreviewed: '###ZARF_VAR_UNREVIEWED###'\n"))
	run(zarf, "package", "create", packageDir, "--confirm", "--skip-sbom", "--architecture", runtime.GOARCH, "--output", root)
	packagePath := filepath.Join(root, "zarf-package-conduit-signature-fixture-"+runtime.GOARCH+"-1.0.0.tar.zst")
	pairs, err := setFlags(controlplane.PlanStep{Values: json.RawMessage(`{"REVIEWED":"approved,exact"}`)}, "--set-variables", zarfVariableName)
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(run(zarf, append([]string{"package", "inspect", "manifests", packagePath}, pairs...)...))
	if !strings.Contains(manifest, "approved,exact") || !strings.Contains(manifest, "package-default") || strings.Contains(manifest, "ambient-unreviewed") {
		t.Fatalf("reviewed values or CLI isolation lost: %s", manifest)
	}
	writeCLIFile(t, filepath.Join(root, "uds-bundle.yaml"), []byte("kind: UDSBundle\nmetadata:\n  name: conduit-signature-fixture\n  version: 1.0.0\npackages:\n  - name: conduit-signature-fixture\n    path: .\n    ref: 1.0.0\n"))
	run(uds, "create", root, "--confirm", "--architecture", runtime.GOARCH, "--output", root, "--no-log-file")
	unsigned := filepath.Join(root, "uds-bundle-conduit-signature-fixture-"+runtime.GOARCH+"-1.0.0.tar.zst")
	expanded := filepath.Join(root, "expanded")
	run(zarf, "tools", "archiver", "decompress", unsigned, expanded)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicPath := filepath.Join(root, "publisher.pub")
	writePublicKey(t, publicPath, &key.PublicKey)
	// Verify's trust flags are exercised without a cluster. Supplying a key
	// to the explicit Zarf gate must reject an unsigned package, rather than
	// succeeding with checksum-only verification.
	if out, err := (scopedRunner{}).Run(t.Context(), zarf, "package", "verify", packagePath, "--key", publicPath); err == nil || !strings.Contains(string(out), "not signed") {
		t.Fatalf("Zarf verify accepted unsigned package with an admitted key: %v\n%s", err, out)
	}
	wrong, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrongPath := filepath.Join(root, "other-publisher.pub")
	writePublicKey(t, wrongPath, &wrong.PublicKey)
	signBundleFixture(t, expanded, key)
	signed := filepath.Join(root, "uds-bundle-conduit-signed-"+runtime.GOARCH+"-1.0.0.tar.zst")
	run(zarf, "tools", "archiver", "compress", filepath.Join(expanded, "blobs"), filepath.Join(expanded, "index.json"), filepath.Join(expanded, "oci-layout"), signed)
	for _, trial := range []struct {
		name, archive, key, diagnostic string
		accepted                       bool
	}{
		{"matching signer", signed, publicPath, "", true},
		{"wrong signer", signed, wrongPath, "signature", false},
		{"unsigned", unsigned, publicPath, "not signed", false},
	} {
		t.Run(trial.name, func(t *testing.T) {
			out, err := (scopedRunner{}).Run(t.Context(), uds, "inspect", trial.archive, "--key", trial.key, "--no-log-file")
			if trial.accepted {
				if err != nil || !strings.Contains(string(out), "conduit-signature-fixture") {
					t.Fatalf("matching signature rejected: %v\n%s", err, out)
				}
			} else if err == nil || !strings.Contains(strings.ToLower(string(out)), trial.diagnostic) {
				t.Fatalf("expected cryptographic rejection (%s): %v\n%s", trial.diagnostic, err, out)
			}
			// Exercise Apply's real verification gate while intercepting every
			// mutation. Only the matching signer may reach the deploy command.
			runner := &bundleProofRunner{binary: uds, archive: trial.archive}
			e := Executor{Runner: runner, UDSBin: uds, VerificationKeyPath: trial.key, Architecture: runtime.GOARCH}
			step := controlplane.PlanStep{InstallationID: "fixture", Namespace: "fixture", Chart: controlplane.Artifact{Kind: controlplane.ArtifactKindUDSBundle, Repository: "example.invalid/fixture", Digest: "sha256:" + strings.Repeat("a", 64), Architectures: []string{runtime.GOARCH}, PackageName: "conduit-signature-fixture"}}
			_, applyErr := e.Apply(t.Context(), controlplane.Plan{}, step)
			if applyErr == nil || (runner.deploys == 1) != trial.accepted || runner.deploys > 1 {
				t.Fatalf("untrusted bundle passed the real executor verification gate: deploys=%d error=%v", runner.deploys, applyErr)
			}
		})
	}
}

type bundleProofRunner struct {
	binary, archive string
	deploys         int
}

func (r *bundleProofRunner) Run(ctx context.Context, binary string, args ...string) ([]byte, error) {
	if binary == r.binary && len(args) > 0 {
		switch args[0] {
		case "inspect":
			local := append([]string(nil), args...)
			local[1] = r.archive // Local archive conformance; no OCI transport claim.
			return (scopedRunner{}).Run(ctx, binary, local...)
		case "deploy":
			r.deploys++
			return nil, errors.New("fixture intercepted deploy; no cluster mutation")
		}
	}
	if len(args) > 2 && args[0] == "get" && (args[2] == "zarf-state" || args[2] == "packages.uds.dev") {
		return []byte("present"), nil
	}
	return nil, nil
}

func writeCLIFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writePublicKey(t *testing.T, path string, key *ecdsa.PublicKey) {
	t.Helper()
	encoded, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded}))
}

type fixtureDescriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int               `json:"size"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// UDS v0.41.0 legacy bundles sign uds-bundle.yaml with a base64-encoded
// ECDSA/SHA256 signature. The fixture changes only the OCI metadata/signature
// layers and their parent digest references; it has no external signing service
// or private-key file. The real CLI establishes compatibility with this format.
func signBundleFixture(t *testing.T, directory string, key *ecdsa.PrivateKey) {
	t.Helper()
	var index struct {
		SchemaVersion int                 `json:"schemaVersion"`
		MediaType     string              `json:"mediaType"`
		Manifests     []fixtureDescriptor `json:"manifests"`
	}
	readJSON := func(path string, target any) {
		raw, err := os.ReadFile(path)
		if err != nil || json.Unmarshal(raw, target) != nil {
			t.Fatalf("read fixture OCI document %s: %v", path, err)
		}
	}
	blob := func(digest string) string {
		return filepath.Join(directory, "blobs", "sha256", strings.TrimPrefix(digest, "sha256:"))
	}
	readJSON(filepath.Join(directory, "index.json"), &index)
	var manifest struct {
		SchemaVersion int                 `json:"schemaVersion"`
		MediaType     string              `json:"mediaType"`
		Config        fixtureDescriptor   `json:"config"`
		Layers        []fixtureDescriptor `json:"layers"`
		Annotations   map[string]string   `json:"annotations,omitempty"`
	}
	readJSON(blob(index.Manifests[0].Digest), &manifest)
	var metadata []byte
	for _, layer := range manifest.Layers {
		if layer.Annotations["org.opencontainers.image.title"] == "uds-bundle.yaml" {
			var err error
			metadata, err = os.ReadFile(blob(layer.Digest))
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(metadata) == 0 {
		t.Fatal("fixture has no bundle metadata")
	}
	hash := sha256.Sum256(metadata)
	signature, err := ecdsa.SignASN1(rand.Reader, key, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	put := func(raw []byte) fixtureDescriptor {
		sum := sha256.Sum256(raw)
		digest := "sha256:" + hex.EncodeToString(sum[:])
		writeCLIFile(t, blob(digest), raw)
		return fixtureDescriptor{MediaType: "application/vnd.zarf.layer.v1.blob", Digest: digest, Size: len(raw)}
	}
	sig := put([]byte(base64.StdEncoding.EncodeToString(signature)))
	sig.Annotations = map[string]string{"org.opencontainers.image.title": "uds-bundle.yaml.sig"}
	manifest.Layers = append(manifest.Layers, sig)
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	updated := put(raw)
	index.Manifests[0].Digest, index.Manifests[0].Size = updated.Digest, updated.Size
	raw, err = json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(directory, "index.json"), raw)
}
