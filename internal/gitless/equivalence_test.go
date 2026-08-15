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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

type manifestStore struct {
	published []byte
	current   string
}

func (fake *manifestStore) Put(_ context.Context, _ string, manifest []byte) error {
	fake.published = manifest
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
	name  string
	build func(t *testing.T, attestation domain.Attestation, digest string) (*application.Source, func() bool)
}

func approaches() []approach {
	return []approach{
		{
			name: "convergence delegated to Flux",
			build: func(t *testing.T, attestation domain.Attestation, digest string) (*application.Source, func() bool) {
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
				return source, func() bool { return len(store.published) > 0 }
			},
		},
		{
			name: "convergence owned by Conduit",
			build: func(t *testing.T, attestation domain.Attestation, digest string) (*application.Source, func() bool) {
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
				return source, func() bool { return store.calls > 0 }
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
				source, deployed := realisation.build(t, testCase.attestation, testCase.digest)
				_, err := source.Reconcile(t.Context(), "prod", channel())

				if testCase.admitted {
					if err != nil {
						t.Fatalf("refused a source both approaches must admit: %v", err)
					}
					if !deployed() {
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
				if deployed() {
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
			source, _ := realisation.build(t, attestation, good)
			for attempt := range 3 {
				outcome, err := source.Reconcile(t.Context(), "prod", channel())
				if err != nil {
					t.Fatalf("reconcile %d: %v", attempt, err)
				}
				if attempt > 0 && outcome.Moved {
					t.Fatalf("reconcile %d reported a move for an unchanged channel", attempt)
				}
			}
		})
	}
}
