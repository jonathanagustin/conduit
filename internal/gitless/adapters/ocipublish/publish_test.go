package ocipublish_test

import (
	"os"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/ocipublish"
)

var digest = "sha256:aa11" + strings.Repeat("0", 60)

func manifests(sourceDigest string) []byte {
	return []byte(`apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: conduit-prod
  namespace: flux-system
spec:
  ref:
    digest: ` + sourceDigest + `
  url: oci://ghcr.io/example/platform-runtime
---
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: conduit-prod
  namespace: flux-system
spec:
  prune: true
`)
}

// requireRegistry needs a real one. The push and pull flows are a sequence of
// status codes, headers and redirects that a hand-written fake would model
// exactly as I imagined them, which is the same reason the cosign adapter is
// tested against the real binary.
func requireRegistry(t *testing.T) string {
	t.Helper()
	registry := os.Getenv("OCI_TEST_REGISTRY")
	if registry == "" {
		t.Skip("set OCI_TEST_REGISTRY to a host:port running an OCI registry")
	}
	return registry
}

func newPublisher(t *testing.T, repository string) *ocipublish.Publisher {
	t.Helper()
	publisher, err := ocipublish.New(repository, nil, ocipublish.Anonymous{}, true)
	if err != nil {
		t.Fatalf("build publisher: %v", err)
	}
	return publisher
}

func TestPublishedManifestsAreReadBackAsTheirSourceDigest(t *testing.T) {
	registry := requireRegistry(t)
	publisher := newPublisher(t, registry+"/conduit/roundtrip")

	if current, err := publisher.CurrentDigest(t.Context(), "prod"); err != nil || current != "" {
		t.Fatalf("an unpublished environment reported %q (%v)", current, err)
	}
	if err := publisher.Put(t.Context(), "prod", manifests(digest)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	current, err := publisher.CurrentDigest(t.Context(), "prod")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	// The answer is the SOURCE digest inside the manifests, not the digest of
	// the artifact carrying them. Confusing the two would make every republish
	// look like a source change.
	if current != digest {
		t.Fatalf("read back %q, want the source digest %q", current, digest)
	}
}

func TestRepublishingADifferentSourceChangesWhatIsReadBack(t *testing.T) {
	registry := requireRegistry(t)
	publisher := newPublisher(t, registry+"/conduit/republish")
	second := "sha256:bb22" + strings.Repeat("0", 60)

	if err := publisher.Put(t.Context(), "prod", manifests(digest)); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if err := publisher.Put(t.Context(), "prod", manifests(second)); err != nil {
		t.Fatalf("second publish: %v", err)
	}
	current, err := publisher.CurrentDigest(t.Context(), "prod")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if current != second {
		t.Fatalf("read back %q after republishing, want %q", current, second)
	}
}

// Each environment is its own tag. One environment's deployment must not be
// readable as another's, or a fleet would converge on whichever published last.
func TestEnvironmentsDoNotShareATag(t *testing.T) {
	registry := requireRegistry(t)
	publisher := newPublisher(t, registry+"/conduit/environments")
	staging := "sha256:cc33" + strings.Repeat("0", 60)

	if err := publisher.Put(t.Context(), "prod", manifests(digest)); err != nil {
		t.Fatalf("publish prod: %v", err)
	}
	if err := publisher.Put(t.Context(), "staging", manifests(staging)); err != nil {
		t.Fatalf("publish staging: %v", err)
	}
	for environment, want := range map[string]string{"prod": digest, "staging": staging} {
		got, err := publisher.CurrentDigest(t.Context(), environment)
		if err != nil {
			t.Fatalf("%s: %v", environment, err)
		}
		if got != want {
			t.Errorf("%s read back %q, want %q", environment, got, want)
		}
	}
}

// Republishing identical bytes must not re-upload them. A five-minute reconcile
// loop would otherwise be a five-minute upload loop.
func TestRepublishingIdenticalBytesIsAccepted(t *testing.T) {
	registry := requireRegistry(t)
	publisher := newPublisher(t, registry+"/conduit/idempotent")
	for range 3 {
		if err := publisher.Put(t.Context(), "prod", manifests(digest)); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	current, err := publisher.CurrentDigest(t.Context(), "prod")
	if err != nil || current != digest {
		t.Fatalf("read back %q (%v)", current, err)
	}
}

func TestManifestsWithNoOCIRepositoryAreRefusedOnReadBack(t *testing.T) {
	registry := requireRegistry(t)
	publisher := newPublisher(t, registry+"/conduit/notasource")
	if err := publisher.Put(t.Context(), "prod", []byte("apiVersion: v1\nkind: ConfigMap\n")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := publisher.CurrentDigest(t.Context(), "prod"); err == nil {
		t.Fatal("manifests declaring no source digest were read back as a digest")
	}
}

// --- construction -----------------------------------------------------------

func TestARepositoryWithNoRegistryHostIsRefused(t *testing.T) {
	if _, err := ocipublish.New("example/runtime", nil, nil, false); err == nil {
		t.Fatal("a repository with no registry host was accepted")
	}
}

func TestAnUnnamedEnvironmentIsRefused(t *testing.T) {
	publisher := newPublisher(t, "registry.example/team/manifests")
	if err := publisher.Put(t.Context(), "  ", manifests(digest)); err == nil {
		t.Fatal("manifests were published for an unnamed environment")
	}
}
