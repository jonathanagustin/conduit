package conduitconverge_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/conduitconverge"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

const repository = "ghcr.io/example/platform-runtime"

func digestOf(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func admissionFor(t *testing.T, digest string) domain.Admission {
	t.Helper()
	admitted, refusals := domain.Admit(
		domain.Selector{Kind: domain.ByChannel, Repository: repository, Channel: "latest-stable"},
		domain.Pin{Repository: repository, Digest: digest},
		domain.Attestation{
			Signed: true, Digest: digest,
			Issuer:   "https://token.actions.githubusercontent.com",
			Identity: "https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/v1.2.3",
		},
		domain.PublisherPolicy{
			CertificateIssuer:   `^https://token\.actions\.githubusercontent\.com$`,
			CertificateIdentity: `^https://github\.com/jonathanagustin/conduit/\.github/workflows/platform-images\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$`,
		},
	)
	if len(refusals) != 0 {
		t.Fatalf("fixture was refused: %v", refusals)
	}
	return admitted
}

type fakeFetcher struct {
	payload []byte
	err     error
}

func (fake *fakeFetcher) Fetch(context.Context, domain.Pin) ([]byte, error) {
	return fake.payload, fake.err
}

type fakeStore struct {
	environment string
	generation  string
	payload     []byte
	current     string
	err         error
	calls       int
}

func (fake *fakeStore) PutDesiredState(_ context.Context, environment, generation string, payload []byte) error {
	fake.calls++
	if fake.err != nil {
		return fake.err
	}
	fake.environment, fake.generation, fake.payload = environment, generation, payload
	fake.current = generation
	return nil
}

func (fake *fakeStore) CurrentGeneration(context.Context, string) (string, error) {
	return fake.current, nil
}

func TestAdmittedBytesBecomeDesiredStateGeneratedByTheirDigest(t *testing.T) {
	payload := []byte(`{"environment":"prod","installations":[]}`)
	digest := digestOf(payload)
	store := &fakeStore{}
	converger, err := conduitconverge.New(&fakeFetcher{payload: payload}, store, 0)
	if err != nil {
		t.Fatalf("build converger: %v", err)
	}
	if err := converger.Converge(t.Context(), "prod", admissionFor(t, digest)); err != nil {
		t.Fatalf("converge: %v", err)
	}
	// The generation must be the digest. A counter could repeat across
	// environments and a timestamp could go backwards; either would let two
	// different desired states look like one.
	if store.generation != digest {
		t.Errorf("desired state was recorded under %q rather than its digest", store.generation)
	}
	if store.environment != "prod" || string(store.payload) != string(payload) {
		t.Errorf("desired state was recorded wrong: %s %s", store.environment, store.payload)
	}
}

// The check that admission cannot make. Admission proved a publisher signed a
// coordinate; only this proves the registry then served those bytes.
func TestBytesThatAreNotTheAdmittedBytesAreRefused(t *testing.T) {
	admitted := []byte(`{"environment":"prod"}`)
	tampered := []byte(`{"environment":"prod","extra":"payload"}`)
	store := &fakeStore{}
	converger, err := conduitconverge.New(&fakeFetcher{payload: tampered}, store, 0)
	if err != nil {
		t.Fatalf("build converger: %v", err)
	}
	err = converger.Converge(t.Context(), "prod", admissionFor(t, digestOf(admitted)))
	if err == nil {
		t.Fatal("a mirror served different bytes and they became desired state")
	}
	if !strings.Contains(err.Error(), "not the bytes admitted") {
		t.Errorf("the refusal does not say what went wrong: %v", err)
	}
	if store.calls != 0 {
		t.Fatal("tampered bytes reached the desired-state store")
	}
}

func TestAnEmptyArtifactIsRefused(t *testing.T) {
	store := &fakeStore{}
	converger, _ := conduitconverge.New(&fakeFetcher{payload: nil}, store, 0)
	if err := converger.Converge(t.Context(), "prod", admissionFor(t, digestOf(nil))); err == nil {
		t.Fatal("an empty artifact became desired state")
	}
	if store.calls != 0 {
		t.Fatal("an empty artifact reached the store")
	}
}

// An unbounded read from a registry is a way for a compromised mirror to
// exhaust the Hub rather than to lie to it.
func TestAnOversizedArtifactIsRefused(t *testing.T) {
	payload := []byte(strings.Repeat("a", 1024))
	store := &fakeStore{}
	converger, _ := conduitconverge.New(&fakeFetcher{payload: payload}, store, 512)
	err := converger.Converge(t.Context(), "prod", admissionFor(t, digestOf(payload)))
	if err == nil {
		t.Fatal("an oversized artifact was accepted")
	}
	if !strings.Contains(err.Error(), "bound") {
		t.Errorf("the refusal does not name the bound: %v", err)
	}
	if store.calls != 0 {
		t.Fatal("an oversized artifact reached the store")
	}
}

func TestAFailedFetchIsReported(t *testing.T) {
	converger, _ := conduitconverge.New(&fakeFetcher{err: errors.New("registry unavailable")}, &fakeStore{}, 0)
	if err := converger.Converge(t.Context(), "prod", admissionFor(t, digestOf([]byte("x")))); err == nil {
		t.Fatal("a failed fetch was reported as a successful convergence")
	}
}

func TestDeployedReportsTheCurrentGeneration(t *testing.T) {
	payload := []byte(`{"environment":"prod"}`)
	digest := digestOf(payload)
	store := &fakeStore{}
	converger, _ := conduitconverge.New(&fakeFetcher{payload: payload}, store, 0)
	if deployed, _ := converger.Deployed(t.Context(), "prod"); deployed != "" {
		t.Errorf("an environment with no desired state reported %q", deployed)
	}
	if err := converger.Converge(t.Context(), "prod", admissionFor(t, digest)); err != nil {
		t.Fatalf("converge: %v", err)
	}
	deployed, err := converger.Deployed(t.Context(), "prod")
	if err != nil || deployed != digest {
		t.Errorf("Deployed reported %q (%v)", deployed, err)
	}
}

func TestBuildingRequiresBothCollaborators(t *testing.T) {
	if _, err := conduitconverge.New(nil, &fakeStore{}, 0); err == nil {
		t.Error("built with no fetcher")
	}
	if _, err := conduitconverge.New(&fakeFetcher{}, nil, 0); err == nil {
		t.Error("built with no store")
	}
}

func TestAnUnnamedEnvironmentIsRefused(t *testing.T) {
	converger, _ := conduitconverge.New(&fakeFetcher{payload: []byte("x")}, &fakeStore{}, 0)
	if err := converger.Converge(t.Context(), " ", admissionFor(t, digestOf([]byte("x")))); err == nil {
		t.Fatal("converged an unnamed environment")
	}
}

func TestAnAdmissionThatNeverHappenedIsRefused(t *testing.T) {
	converger, _ := conduitconverge.New(&fakeFetcher{payload: []byte("x")}, &fakeStore{}, 0)
	if err := converger.Converge(t.Context(), "prod", domain.Admission{}); err == nil {
		t.Fatal("converged on a source that was never admitted")
	}
}
