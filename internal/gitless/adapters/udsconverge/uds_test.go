package udsconverge_test

import (
	"context"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v4"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/udsconverge"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

var (
	repository = "ghcr.io/example/excalidraw-uds"
	digestA    = "sha256:aa11" + strings.Repeat("0", 60)
	issuerPtn  = `^https://token\.actions\.githubusercontent\.com$`
	identPtn   = `^https://github\.com/jonathanagustin/conduit/\.github/workflows/platform-images\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$`
)

func admission(t *testing.T) domain.Admission {
	t.Helper()
	policy := domain.PublisherPolicy{CertificateIssuer: issuerPtn, CertificateIdentity: identPtn}
	admitted, refusals := domain.Admit(
		domain.Selector{Kind: domain.ByChannel, Repository: repository, Channel: "latest-stable"},
		domain.Pin{Repository: repository, Digest: digestA},
		domain.Attestation{
			Signed:   true,
			Digest:   digestA,
			Issuer:   "https://token.actions.githubusercontent.com",
			Identity: "https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/v1.2.3",
		},
		policy,
	)
	if len(refusals) != 0 {
		t.Fatalf("fixture was refused: %v", refusals)
	}
	return admitted
}

type memoryManifestStore struct {
	manifests map[string][]byte
	digests   map[string]string
}

func (m *memoryManifestStore) Put(_ context.Context, environment string, manifest []byte) error {
	if m.manifests == nil {
		m.manifests = map[string][]byte{}
	}
	if m.digests == nil {
		m.digests = map[string]string{}
	}
	m.manifests[environment] = manifest
	m.digests[environment] = digestA
	return nil
}

func (m *memoryManifestStore) CurrentDigest(_ context.Context, environment string) (string, error) {
	return m.digests[environment], nil
}

func TestUDSConvergerRendersDigestPinnedBundleAndPackage(t *testing.T) {
	t.Parallel()
	store := &memoryManifestStore{}
	converger, err := udsconverge.New(store, udsconverge.Options{
		Namespace:          "excalidraw",
		ServiceAccountName: "uds-runtime-reconciler",
		PullSecretName:     "conduit-uds-pull",
		ExposePort:         8080,
		ExposeService:      "excalidraw",
		ExposeHost:         "excalidraw",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := converger.Converge(context.Background(), "prod", admission(t)); err != nil {
		t.Fatalf("Converge: %v", err)
	}
	deployed, err := converger.Deployed(context.Background(), "prod")
	if err != nil || deployed != digestA {
		t.Fatalf("Deployed = %q, %v; want %q", deployed, err, digestA)
	}

	var docs []map[string]any
	dec := yaml.NewDecoder(strings.NewReader(string(store.manifests["prod"])))
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			break
		}
		docs = append(docs, doc)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 YAML documents (UDSBundle + Package), got %d", len(docs))
	}
	if docs[0]["kind"] != "UDSBundle" || docs[0]["apiVersion"] != "uds.dev/v1alpha1" {
		t.Fatalf("doc[0] = %#v", docs[0])
	}
	pkgs := docs[0]["zarf-packages"].([]any)
	firstPkg := pkgs[0].(map[string]any)
	if firstPkg["ref"] != digestA || !strings.HasSuffix(firstPkg["repository"].(string), "@"+digestA) {
		t.Fatalf("expected digest-pinned zarf-packages entry, got %#v", firstPkg)
	}
	if docs[1]["kind"] != "Package" || docs[1]["apiVersion"] != "uds.dev/v1alpha1" {
		t.Fatalf("doc[1] = %#v", docs[1])
	}
}

func TestRenderBundleFromPackagesRequiresDigestOrSHA256(t *testing.T) {
	t.Parallel()
	raw, err := udsconverge.RenderBundleFromPackages("excalidraw-bundle", "0.1.0", "amd64", []udsconverge.BundlePackageRef{
		{
			Name:   "excalidraw",
			Path:   "zarf-package-excalidraw-amd64-0.1.0.tar.zst",
			SHA256: strings.Repeat("b", 64),
		},
	})
	if err != nil {
		t.Fatalf("RenderBundleFromPackages: %v", err)
	}
	if !strings.Contains(string(raw), "kind: UDSBundle") || !strings.Contains(string(raw), "sha256:"+strings.Repeat("b", 64)) {
		t.Fatalf("unexpected bundle YAML:\n%s", string(raw))
	}

	if _, err := udsconverge.RenderBundleFromPackages("excalidraw-bundle", "0.1.0", "amd64", []udsconverge.BundlePackageRef{
		{Name: "excalidraw", Path: "zarf-package-excalidraw-amd64-0.1.0.tar.zst"},
	}); err == nil {
		t.Fatal("expected unpinned local package to be rejected")
	}
}

func TestParsePackageStatusProjectsUDSCorePhaseAndGeneration(t *testing.T) {
	t.Parallel()
	sample := []byte(`apiVersion: uds.dev/v1alpha1
kind: Package
metadata:
  name: excalidraw
  namespace: excalidraw
  generation: 3
  annotations:
    conduit.delivery/source-digest: ` + digestA + `
status:
  phase: Ready
  observedGeneration: 3
  endpoints:
    - excalidraw.uds.dev
  ssoClients:
    - excalidraw-sso
`)
	obs, err := udsconverge.ParsePackageStatus(sample)
	if err != nil {
		t.Fatalf("ParsePackageStatus: %v", err)
	}
	if !obs.Healthy || !obs.Converged || obs.Phase != "Ready" || obs.SourceDigest != digestA {
		t.Fatalf("unexpected observation: %#v", obs)
	}
	if len(obs.Endpoints) != 1 || obs.Endpoints[0] != "excalidraw.uds.dev" {
		t.Fatalf("unexpected endpoints: %#v", obs.Endpoints)
	}
}
