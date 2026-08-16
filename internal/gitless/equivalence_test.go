package gitless_test

// The test that justifies the architecture.
//
// Conduit realises Gitless GitOps two ways: delegate convergence to Flux, or
// converge with its own reconciler. The claim made by putting admission in the
// domain rather than behind a port is that the choice cannot change what is
// deployed -- not "should not", cannot. This drives both adapters through the
// same application service with the same inputs and requires them to agree,
// case for case.
//
// If the two ever disagreed, a security property of this system would depend on
// a deployment topology, which is the failure mode nobody notices until it
// matters.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/conduitconverge"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/fluxconverge"
	"github.com/jonathanagustin/conduit/internal/gitless/application"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

const (
	repository   = "ghcr.io/example/platform-runtime"
	trustedIssue = "https://token.actions.githubusercontent.com"
	trustedIdent = "https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/v1.2.3"
)

var payload = []byte(`{"environment":"prod","installations":[]}`)

func digestOf(bytes []byte) string {
	sum := sha256.Sum256(bytes)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func policy() domain.PublisherPolicy {
	return domain.PublisherPolicy{
		CertificateIssuer:   `^https://token\.actions\.githubusercontent\.com$`,
		CertificateIdentity: `^https://github\.com/jonathanagustin/conduit/\.github/workflows/platform-images\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$`,
	}
}

// --- shared doubles ---------------------------------------------------------

type resolver struct{ digest string }

func (fake resolver) Resolve(context.Context, domain.Selector) (domain.Pin, error) {
	return domain.Pin{Repository: repository, Digest: fake.digest}, nil
}

type verifier struct{ attestation domain.Attestation }

func (fake verifier) Verify(context.Context, domain.Pin) (domain.Attestation, error) {
	return fake.attestation, nil
}

type policies struct{}

func (policies) PolicyFor(context.Context, string) (domain.PublisherPolicy, error) {
	return policy(), nil
}

type recordingLog struct{ records []domain.Resolution }

func (fake *recordingLog) Record(_ context.Context, resolution domain.Resolution) error {
	fake.records = append(fake.records, resolution)
	return nil
}

// publishedDigest finds the digest a rendered manifest pins. A real Flux store
// answers CurrentDigest from what was published to it, so a double whose
// CurrentDigest cannot see its own Put is not a store -- it is a store that has
// never deployed anything, which is a different fixture and a much weaker one.
var publishedDigest = regexp.MustCompile(`sha256:[0-9a-f]{64}`)

type manifestStore struct {
	published []byte
	current   string
	calls     int
}

func (fake *manifestStore) Put(_ context.Context, _ string, manifest []byte) error {
	fake.calls++
	fake.published = manifest
	// Wired to CurrentDigest, which it was not. Put wrote `published` and
	// CurrentDigest returned `current`, and nothing ever set `current` -- so
	// Deployed() always answered "", the already-there short circuit never
	// fired, and the Flux path re-recorded and re-converged on every tick
	// inside the test asserting that it did not. That test passed because it
	// asserted only Moved, which is false either way, and never counted
	// convergences. It counts them now.
	if found := publishedDigest.Find(manifest); found != nil {
		fake.current = string(found)
	}
	return nil
}

func (fake *manifestStore) CurrentDigest(context.Context, string) (string, error) {
	return fake.current, nil
}

type artifactFetcher struct{ payload []byte }

func (fake artifactFetcher) Fetch(context.Context, domain.Pin) ([]byte, error) {
	return fake.payload, nil
}

type desiredStateStore struct {
	generation string
	calls      int
}

func (fake *desiredStateStore) PutDesiredState(_ context.Context, _, generation string, _ []byte) error {
	fake.calls++
	fake.generation = generation
	return nil
}

func (fake *desiredStateStore) CurrentGeneration(context.Context, string) (string, error) {
	return fake.generation, nil
}

// --- the harness ------------------------------------------------------------

// approach is one realisation of Gitless GitOps, plus a way to ask whether it
// actually deployed anything.
type approach struct {
	name string
	// converged counts convergences rather than answering whether any
	// happened. "Did it deploy" cannot express "and it did not deploy again",
	// which is the property the unchanged-channel case exists to assert.
	build func(t *testing.T, attestation domain.Attestation, digest string) (*application.Source, func() int)
}

func approaches() []approach {
	return []approach{
		{
			name: "convergence delegated to Flux",
			build: func(t *testing.T, attestation domain.Attestation, digest string) (*application.Source, func() int) {
				t.Helper()
				store := &manifestStore{}
				converger, err := fluxconverge.New(store, fluxconverge.Options{
					ServiceAccountName: "platform-runtime-reconciler",
					PullSecretName:     "conduit-runtime-pull",
				})
				if err != nil {
					t.Fatalf("build Flux converger: %v", err)
				}
				source, err := application.New(
					resolver{digest: digest}, verifier{attestation: attestation}, policies{}, &recordingLog{}, converger)
				if err != nil {
					t.Fatalf("build source: %v", err)
				}
				return source, func() int { return store.calls }
			},
		},
		{
			name: "convergence owned by Conduit",
			build: func(t *testing.T, attestation domain.Attestation, digest string) (*application.Source, func() int) {
				t.Helper()
				store := &desiredStateStore{}
				converger, err := conduitconverge.New(artifactFetcher{payload: payload}, store, 0)
				if err != nil {
					t.Fatalf("build Conduit converger: %v", err)
				}
				source, err := application.New(
					resolver{digest: digest}, verifier{attestation: attestation}, policies{}, &recordingLog{}, converger)
				if err != nil {
					t.Fatalf("build source: %v", err)
				}
				return source, func() int { return store.calls }
			},
		},
	}
}

func channel() domain.Selector {
	return domain.Selector{Kind: domain.ByChannel, Repository: repository, Channel: "latest-stable"}
}

// --- the cases both approaches must agree on --------------------------------

func TestBothApproachesAdmitAndRefuseTheSameSources(t *testing.T) {
	good := digestOf(payload)
	cases := []struct {
		name        string
		attestation domain.Attestation
		digest      string
		admitted    bool
		reason      domain.RefusalReason
	}{
		{
			name:        "a release signed by the trusted publisher",
			attestation: domain.Attestation{Signed: true, Issuer: trustedIssue, Identity: trustedIdent, Digest: good},
			digest:      good, admitted: true,
		},
		{
			name:        "unsigned bytes",
			attestation: domain.Attestation{Signed: false, Digest: good},
			digest:      good, reason: domain.ReasonUnsigned,
		},
		{
			name: "a foreign publisher",
			attestation: domain.Attestation{
				Signed: true, Issuer: trustedIssue, Digest: good,
				Identity: "https://github.com/attacker/conduit/.github/workflows/platform-images.yml@refs/tags/v9.9.9",
			},
			digest: good, reason: domain.ReasonUntrustedIdent,
		},
		{
			name: "a foreign issuer",
			attestation: domain.Attestation{
				Signed: true, Issuer: "https://token.actions.example.com", Identity: trustedIdent, Digest: good,
			},
			digest: good, reason: domain.ReasonUntrustedIssue,
		},
		{
			name: "a branch build where the policy requires a tag",
			attestation: domain.Attestation{
				Signed: true, Issuer: trustedIssue, Digest: good,
				Identity: "https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/heads/main",
			},
			digest: good, reason: domain.ReasonUntrustedIdent,
		},
		{
			name: "an attestation over different bytes",
			attestation: domain.Attestation{
				Signed: true, Issuer: trustedIssue, Identity: trustedIdent,
				Digest: "sha256:" + strings.Repeat("c", 64),
			},
			digest: good, reason: domain.ReasonDigestMismatch,
		},
	}

	for _, testCase := range cases {
		for _, realisation := range approaches() {
			t.Run(testCase.name+", "+realisation.name, func(t *testing.T) {
				source, converged := realisation.build(t, testCase.attestation, testCase.digest)
				_, err := source.Reconcile(t.Context(), "prod", channel())

				if testCase.admitted {
					if err != nil {
						t.Fatalf("refused a source both approaches must admit: %v", err)
					}
					if converged() == 0 {
						t.Fatal("admitted the source but deployed nothing")
					}
					return
				}

				var refused *application.RefusedError
				if !errors.As(err, &refused) {
					t.Fatalf("expected a refusal, got %v", err)
				}
				found := false
				for _, reason := range refused.Reasons() {
					if reason == testCase.reason {
						found = true
					}
				}
				if !found {
					t.Errorf("expected reason %s, got %v", testCase.reason, refused.Reasons())
				}
				if converged() > 0 {
					t.Fatal("a refused source was deployed anyway")
				}
			})
		}
	}
}

// Continuous reconciliation must be equally cheap either way. An approach that
// redeployed on every interval would turn a schedule into a deployment storm.
func TestNeitherApproachRedeploysAnUnchangedChannel(t *testing.T) {
	good := digestOf(payload)
	attestation := domain.Attestation{Signed: true, Issuer: trustedIssue, Identity: trustedIdent, Digest: good}
	for _, realisation := range approaches() {
		t.Run(realisation.name, func(t *testing.T) {
			source, converged := realisation.build(t, attestation, good)
			for attempt := range 3 {
				outcome, err := source.Reconcile(t.Context(), "prod", channel())
				if err != nil {
					t.Fatalf("reconcile %d: %v", attempt, err)
				}
				if attempt > 0 && outcome.Moved {
					t.Fatalf("reconcile %d reported a move for an unchanged channel", attempt)
				}
			}
			// The assertion this case was named for. Moved is false whether the
			// approach short-circuits or re-converges every tick, so asserting
			// it alone let the Flux path re-deploy three times inside the test
			// that says neither approach redeploys.
			if got := converged(); got != 1 {
				t.Fatalf("three reconciles of an unchanged channel converged %d times, want 1", got)
			}
		})
	}
}

// TestTheApproachesDivergeOnAMirrorServingDifferentBytes is the case the six
// above cannot reach, and it is the one where the two approaches genuinely
// disagree.
//
// Every other case varies an admission input, and admission is computed once
// before either converger is chosen -- so no admission input can make them
// differ. This one lives past admission: a mirror serves bytes whose digest is
// not the digest that was admitted.
//
// Conduit re-hashes what arrived and refuses, because admission established
// that a trusted publisher signed a digest and only this establishes that the
// registry then served it. Flux never fetches; it renders a manifest naming the
// digest and reports success, deferring the integrity check to the cluster.
//
// So at the Hub the two report opposite outcomes for identical tampering. That
// is deliberate and documented in ADR 0004, and it was asserted by nothing --
// which meant the equivalence suite read as "these are interchangeable" without
// stating the boundary of the claim. Pinned here so a change to either side has
// to come past this test and restate the divergence on purpose.
func TestTheApproachesDivergeOnAMirrorServingDifferentBytes(t *testing.T) {
	admitted := digestOf(payload)
	attestation := domain.Attestation{Signed: true, Issuer: trustedIssue, Identity: trustedIdent, Digest: admitted}
	tampered := []byte("bytes the publisher never signed")

	t.Run("Conduit refuses what the mirror substituted", func(t *testing.T) {
		store := &desiredStateStore{}
		converger, err := conduitconverge.New(artifactFetcher{payload: tampered}, store, 0)
		if err != nil {
			t.Fatalf("build Conduit converger: %v", err)
		}
		source, err := application.New(
			resolver{digest: admitted}, verifier{attestation: attestation}, policies{}, &recordingLog{}, converger)
		if err != nil {
			t.Fatalf("build source: %v", err)
		}
		if _, err := source.Reconcile(t.Context(), "prod", channel()); err == nil {
			t.Fatal("substituted bytes were converged on")
		}
		if store.calls != 0 {
			t.Errorf("desired state was written for bytes that were never admitted: %d call(s)", store.calls)
		}
	})

	t.Run("Flux publishes the manifest and defers the check to the cluster", func(t *testing.T) {
		store := &manifestStore{}
		converger, err := fluxconverge.New(store, fluxconverge.Options{
			ServiceAccountName: "platform-runtime-reconciler",
			PullSecretName:     "conduit-runtime-pull",
		})
		if err != nil {
			t.Fatalf("build Flux converger: %v", err)
		}
		source, err := application.New(
			resolver{digest: admitted}, verifier{attestation: attestation}, policies{}, &recordingLog{}, converger)
		if err != nil {
			t.Fatalf("build source: %v", err)
		}
		// It succeeds because it never looked. The manifest pins the admitted
		// digest, so what the mirror served is the cluster's problem to catch.
		if _, err := source.Reconcile(t.Context(), "prod", channel()); err != nil {
			t.Fatalf("the Flux path fetched bytes it is documented not to fetch: %v", err)
		}
		if store.calls != 1 {
			t.Errorf("want one manifest published, got %d", store.calls)
		}
		if !bytes.Contains(store.published, []byte(admitted)) {
			t.Error("the published manifest does not pin the admitted digest")
		}
	})
}
