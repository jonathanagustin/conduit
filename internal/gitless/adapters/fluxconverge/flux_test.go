package fluxconverge_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/fluxconverge"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

var (
	repository = "ghcr.io/example/platform-runtime"
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
			Signed: true, Digest: digestA,
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

func options() fluxconverge.Options {
	return fluxconverge.Options{
		ServiceAccountName: "platform-runtime-reconciler",
		PullSecretName:     "conduit-runtime-pull",
	}
}

func render(t *testing.T) []map[string]any {
	t.Helper()
	manifest, err := fluxconverge.Render("prod", admission(t), options())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var documents []map[string]any
	decoder := yaml.NewDecoder(strings.NewReader(string(manifest)))
	for {
		var document map[string]any
		if err := decoder.Decode(&document); err != nil {
			break
		}
		documents = append(documents, document)
	}
	if len(documents) != 2 {
		t.Fatalf("expected an OCIRepository and a Kustomization, got %d documents:\n%s", len(documents), manifest)
	}
	return documents
}

// The rule the whole architecture rests on, checked on the generated output
// rather than only on the committed example: a cluster selects by digest, so
// moving a channel cannot change what it is already running.
func TestTheRenderedSourceIsPinnedByDigestAndCarriesNoTag(t *testing.T) {
	source := render(t)[0]
	spec := source["spec"].(map[string]any)
	ref := spec["ref"].(map[string]any)
	if ref["digest"] != digestA {
		t.Fatalf("the source is not pinned to the admitted digest: %v", ref)
	}
	for _, forbidden := range []string{"tag", "semver"} {
		if _, present := ref[forbidden]; present {
			t.Errorf("the source carries a mutable %s selector: %v", forbidden, ref)
		}
	}
	if source["kind"] != "OCIRepository" {
		t.Errorf("the source is not an OCIRepository: %v", source["kind"])
	}
}

// Flux verifies again in-cluster. If the manifest named a laxer identity than
// the admission used, the cluster would accept bytes Conduit refused -- which
// is exactly the divergence that putting admission in the domain prevents.
func TestTheRenderedSourceVerifiesTheSamePublisherConduitAdmitted(t *testing.T) {
	spec := render(t)[0]["spec"].(map[string]any)
	verify := spec["verify"].(map[string]any)
	if verify["provider"] != "cosign" {
		t.Fatalf("the source does not require Cosign verification: %v", verify)
	}
	identities := verify["matchOIDCIdentity"].([]any)
	if len(identities) != 1 {
		t.Fatalf("expected exactly one trusted identity, got %d", len(identities))
	}
	identity := identities[0].(map[string]any)
	if identity["issuer"] != issuerPtn || identity["subject"] != identPtn {
		t.Fatalf("the manifest trusts a different publisher than the admission did: %v", identity)
	}
}

// Without impersonation the Kustomization reconciles with the controller's own
// authority, which is cluster-wide.
func TestTheRenderedKustomizationImpersonatesPrunesAndWaits(t *testing.T) {
	spec := render(t)[1]["spec"].(map[string]any)
	if spec["serviceAccountName"] != "platform-runtime-reconciler" {
		t.Errorf("the Kustomization does not impersonate a bounded identity: %v", spec["serviceAccountName"])
	}
	if spec["prune"] != true {
		t.Errorf("the Kustomization does not prune: %v", spec["prune"])
	}
	if spec["wait"] != true {
		t.Errorf("the Kustomization does not wait for health: %v", spec["wait"])
	}
	source := spec["sourceRef"].(map[string]any)
	if source["kind"] != "OCIRepository" {
		t.Errorf("the Kustomization does not read the OCI source: %v", source)
	}
}

// The two facts with no safe default. Guessing either is how this adapter would
// quietly widen authority.
func TestRenderingRefusesToGuessIdentityOrCredential(t *testing.T) {
	for name, broken := range map[string]fluxconverge.Options{
		"no ServiceAccount": {PullSecretName: "conduit-runtime-pull"},
		"no pull Secret":    {ServiceAccountName: "platform-runtime-reconciler"},
	} {
		if _, err := fluxconverge.Render("prod", admission(t), broken); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestRenderingRefusesAnUnnamedEnvironment(t *testing.T) {
	if _, err := fluxconverge.Render("  ", admission(t), options()); err == nil {
		t.Fatal("manifests were rendered for an unnamed environment")
	}
}

// A zero Admission cannot come from Admit, but it can come from a caller that
// forgot to check the refusals. Rendering it would emit a manifest with an
// empty digest and no verification.
func TestRenderingRefusesAnAdmissionThatNeverHappened(t *testing.T) {
	if _, err := fluxconverge.Render("prod", domain.Admission{}, options()); err == nil {
		t.Fatal("manifests were rendered for a source that was never admitted")
	}
}

// --- the Converger ----------------------------------------------------------

type fakeStore struct {
	put     []byte
	current string
	err     error
}

func (fake *fakeStore) Put(_ context.Context, _ string, manifest []byte) error {
	if fake.err != nil {
		return fake.err
	}
	fake.put = manifest
	return nil
}

func (fake *fakeStore) CurrentDigest(context.Context, string) (string, error) {
	return fake.current, nil
}

func TestConvergingPublishesTheManifest(t *testing.T) {
	store := &fakeStore{}
	converger, err := fluxconverge.New(store, options())
	if err != nil {
		t.Fatalf("build converger: %v", err)
	}
	if err := converger.Converge(t.Context(), "prod", admission(t)); err != nil {
		t.Fatalf("converge: %v", err)
	}
	if !strings.Contains(string(store.put), digestA) {
		t.Fatalf("the published manifest does not name the admitted digest:\n%s", store.put)
	}
}

func TestAFailedPublicationIsReported(t *testing.T) {
	converger, err := fluxconverge.New(&fakeStore{err: errors.New("registry unavailable")}, options())
	if err != nil {
		t.Fatalf("build converger: %v", err)
	}
	if err := converger.Converge(t.Context(), "prod", admission(t)); err == nil {
		t.Fatal("a failed publication was reported as a successful convergence")
	}
}

func TestBuildingRefusesWithoutAManifestStore(t *testing.T) {
	if _, err := fluxconverge.New(nil, options()); err == nil {
		t.Fatal("a Flux converger was built with nowhere to publish")
	}
}
