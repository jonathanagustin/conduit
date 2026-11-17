package udsconverge_test

import (
	"context"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v4"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/udsconverge"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
	"github.com/jonathanagustin/conduit/internal/udsschema"
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
		PackageTag:         "1.2.3",
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

	manifest := store.manifests["prod"]
	var docs []map[string]any
	dec := yaml.NewDecoder(strings.NewReader(string(manifest)))
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
	// One bundle shape: the one uds-cli v0.41.0 reads. No apiVersion, and none
	// of the keys the retired shape used.
	bundle := docs[0]
	if bundle["kind"] != "UDSBundle" {
		t.Fatalf("doc[0] = %#v", bundle)
	}
	for _, retired := range []string{"apiVersion", "zarf-packages"} {
		if _, present := bundle[retired]; present {
			t.Fatalf("the bundle still carries %q: %#v", retired, bundle)
		}
	}
	firstPkg := bundle["packages"].([]any)[0].(map[string]any)
	if firstPkg["repository"] != repository || firstPkg["ref"] != "1.2.3@"+digestA {
		t.Fatalf("expected the package pinned as <tag>@<digest> in its repository, got %#v", firstPkg)
	}
	verification := firstPkg["keylessVerification"].(map[string]any)
	if verification["certificateIdentityRegexp"] != identPtn || verification["certificateOIDCIssuerRegexp"] != issuerPtn {
		t.Fatalf("the admitted publisher policy is not what the bundle verifies against: %#v", verification)
	}
	if docs[1]["kind"] != "Package" || docs[1]["apiVersion"] != "uds.dev/v1alpha1" {
		t.Fatalf("doc[1] = %#v", docs[1])
	}

	// Each rendered document, as bytes, validates against its pinned schema.
	parts := strings.SplitN(string(manifest), "---\n", 2)
	if len(parts) != 2 {
		t.Fatalf("manifest is not two documents:\n%s", manifest)
	}
	if violations := udsschema.ValidateBundle([]byte(parts[0])); len(violations) != 0 {
		t.Fatalf("rendered bundle: %v\n%s", violations, parts[0])
	}
	if violations := udsschema.ValidatePackage([]byte(parts[1])); len(violations) != 0 {
		t.Fatalf("rendered Package: %v\n%s", violations, parts[1])
	}
}

func renderOptions() udsconverge.Options {
	return udsconverge.Options{ServiceAccountName: "uds-runtime-reconciler", PullSecretName: "conduit-uds-pull", PackageTag: "1.2.3"}
}

// A bundle addresses a package by tag and pins it with the digest. Without the
// tag the only thing left to write is a bare digest, which uds-cli would read
// as a tag, so nothing is rendered.
func TestRenderRefusesABundleItCannotPin(t *testing.T) {
	t.Parallel()
	for _, tag := range []string{"", "1.2.3@sha256:abc", "has space", "a:b"} {
		options := renderOptions()
		options.PackageTag = tag
		if _, err := udsconverge.Render("prod", admission(t), options); err == nil {
			t.Errorf("rendered a bundle with package tag %q", tag)
		}
		if _, err := udsconverge.New(&memoryManifestStore{}, options); err == nil {
			t.Errorf("constructed a converger with package tag %q", tag)
		}
	}
}

// Render validates what it is about to return. An environment name the bundle
// schema does not allow is refused rather than emitted for uds to reject.
func TestRenderRefusesWhatThePinnedSchemaRejects(t *testing.T) {
	t.Parallel()
	_, err := udsconverge.Render("Prod_East", admission(t), renderOptions())
	if err == nil || !strings.Contains(err.Error(), "pinned schema rejects") || !strings.Contains(err.Error(), "metadata.name") {
		t.Fatalf("Render error = %v", err)
	}
	if _, err := udsconverge.Render("prod-east", admission(t), renderOptions()); err != nil {
		t.Fatalf("Render of a valid name: %v", err)
	}
}

func TestRenderBundleFromPackagesPinsByTagAndDigest(t *testing.T) {
	t.Parallel()
	pinned := udsconverge.BundlePackageRef{
		Name: "excalidraw", Repository: "oci://" + repository, Tag: "0.1.0", Digest: digestA,
		OptionalComponents: []string{"uds"},
	}
	raw, err := udsconverge.RenderBundleFromPackages("excalidraw-bundle", "0.1.0", "amd64", []udsconverge.BundlePackageRef{pinned})
	if err != nil {
		t.Fatalf("RenderBundleFromPackages: %v", err)
	}
	if violations := udsschema.ValidateBundle(raw); len(violations) != 0 {
		t.Fatalf("rendered bundle: %v\n%s", violations, raw)
	}
	var doc struct {
		Kind     string `yaml:"kind"`
		Packages []struct {
			Repository         string   `yaml:"repository"`
			Ref                string   `yaml:"ref"`
			OptionalComponents []string `yaml:"optionalComponents"`
		} `yaml:"packages"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Kind != "UDSBundle" || len(doc.Packages) != 1 || doc.Packages[0].Repository != repository ||
		doc.Packages[0].Ref != "0.1.0@"+digestA || len(doc.Packages[0].OptionalComponents) != 1 {
		t.Fatalf("bundle = %#v\n%s", doc, raw)
	}
	if strings.Contains(string(raw), "zarf-packages") || strings.Contains(string(raw), "optional-components") || strings.Contains(string(raw), "apiVersion") {
		t.Fatalf("the retired bundle shape was rendered:\n%s", raw)
	}

	refused := map[string]udsconverge.BundlePackageRef{
		"no digest":                {Name: "excalidraw", Repository: repository, Tag: "0.1.0"},
		"no tag":                   {Name: "excalidraw", Repository: repository, Digest: digestA},
		"digest in the repository": {Name: "excalidraw", Repository: repository + "@" + digestA, Tag: "0.1.0", Digest: digestA},
		// The bundle schema has no field for an archive checksum.
		"local path": {Name: "excalidraw", Path: "zarf-package-excalidraw-amd64-0.1.0.tar.zst", SHA256: strings.Repeat("b", 64)},
		"nothing":    {Name: "excalidraw"},
	}
	for name, pkg := range refused {
		if _, err := udsconverge.RenderBundleFromPackages("excalidraw-bundle", "0.1.0", "amd64", []udsconverge.BundlePackageRef{pkg}); err == nil {
			t.Errorf("%s: an unpinned package was rendered", name)
		}
	}
	if _, err := udsconverge.RenderBundleFromPackages("Excalidraw Bundle", "0.1.0", "amd64", []udsconverge.BundlePackageRef{pinned}); err == nil {
		t.Error("a bundle name the pinned schema rejects was rendered")
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
