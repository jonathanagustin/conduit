package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/gitless/application"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

var (
	repository = "ghcr.io/example/platform-runtime"
	digestA    = "sha256:aa11" + strings.Repeat("0", 60)
	digestB    = "sha256:bb22" + strings.Repeat("0", 60)
	issuer     = "https://token.actions.githubusercontent.com"
	identity   = "https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/v1.2.3"
)

func policy() domain.PublisherPolicy {
	return domain.PublisherPolicy{
		CertificateIssuer:   `^https://token\.actions\.githubusercontent\.com$`,
		CertificateIdentity: `^https://github\.com/jonathanagustin/conduit/\.github/workflows/platform-images\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$`,
	}
}

// --- doubles ----------------------------------------------------------------

type fakeResolver struct {
	digest string
	err    error
	calls  int
}

func (fake *fakeResolver) Resolve(context.Context, domain.Selector) (domain.Pin, error) {
	fake.calls++
	if fake.err != nil {
		return domain.Pin{}, fake.err
	}
	return domain.Pin{Repository: repository, Digest: fake.digest}, nil
}

type fakeVerifier struct {
	attestation domain.Attestation
	err         error
	useDigest   bool
}

func (fake *fakeVerifier) Verify(_ context.Context, pin domain.Pin) (domain.Attestation, error) {
	if fake.err != nil {
		return domain.Attestation{}, fake.err
	}
	attestation := fake.attestation
	if fake.useDigest {
		attestation.Digest = pin.Digest
	}
	return attestation, nil
}

type fakePolicies struct {
	policy domain.PublisherPolicy
	err    error
}

func (fake *fakePolicies) PolicyFor(context.Context, string) (domain.PublisherPolicy, error) {
	return fake.policy, fake.err
}

type fakeLog struct {
	recorded []domain.Resolution
	err      error
}

func (fake *fakeLog) Record(_ context.Context, resolution domain.Resolution) error {
	if fake.err != nil {
		return fake.err
	}
	fake.recorded = append(fake.recorded, resolution)
	return nil
}

// fakeConverger tracks what each environment converged on, per environment.
//
// An earlier version kept one global digest, which made a multi-environment
// test report a fresh environment as "unchanged" because a previous one had
// deployed the same bytes. Real convergers are per environment; a double that
// is not invents behaviour the product does not have.
type fakeConverger struct {
	deployed    map[string]string
	deployedErr error
	// failDeployedFor makes exactly one environment unreadable, so a test can
	// show a partial failure rather than a total one.
	failDeployedFor string
	converged       []string
	err             error
}

func (fake *fakeConverger) Converge(_ context.Context, environment string, admission domain.Admission) error {
	if fake.err != nil {
		return fake.err
	}
	if fake.deployed == nil {
		fake.deployed = map[string]string{}
	}
	fake.converged = append(fake.converged, admission.Pin().Digest)
	fake.deployed[environment] = admission.Pin().Digest
	return nil
}

func (fake *fakeConverger) Deployed(_ context.Context, environment string) (string, error) {
	if fake.failDeployedFor != "" && fake.failDeployedFor == environment {
		return "", errors.New("registry unreachable for " + environment)
	}
	return fake.deployed[environment], fake.deployedErr
}

type fixture struct {
	source    *application.Source
	resolver  *fakeResolver
	verifier  *fakeVerifier
	policies  *fakePolicies
	log       *fakeLog
	converger *fakeConverger
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	parts := &fixture{
		resolver: &fakeResolver{digest: digestA},
		verifier: &fakeVerifier{
			attestation: domain.Attestation{Signed: true, Issuer: issuer, Identity: identity},
			useDigest:   true,
		},
		policies:  &fakePolicies{policy: policy()},
		log:       &fakeLog{},
		converger: &fakeConverger{},
	}
	source, err := application.New(parts.resolver, parts.verifier, parts.policies, parts.log, parts.converger)
	if err != nil {
		t.Fatalf("build source: %v", err)
	}
	parts.source = source
	return parts
}

func channel(name string) domain.Selector {
	return domain.Selector{Kind: domain.ByChannel, Repository: repository, Channel: name}
}

// --- construction -----------------------------------------------------------

// A Source without a verifier would resolve and converge perfectly well, having
// silently dropped the only step that makes this Gitless GitOps rather than
// downloading something off the internet.
func TestASourceWithoutAVerifierIsRefused(t *testing.T) {
	parts := newFixture(t)
	_, err := application.New(parts.resolver, nil, parts.policies, parts.log, parts.converger)
	if err == nil {
		t.Fatal("a Source was built with no publisher verifier")
	}
	if !strings.Contains(err.Error(), "publisher verifier") {
		t.Errorf("the refusal does not name what is missing: %v", err)
	}
}

func TestEveryCollaboratorIsRequired(t *testing.T) {
	if _, err := application.New(nil, nil, nil, nil, nil); err == nil {
		t.Fatal("a Source was built with nothing")
	}
}

// --- the ordering that is the design ----------------------------------------

func TestAdmittedBytesAreRecordedThenConverged(t *testing.T) {
	parts := newFixture(t)
	outcome, err := parts.source.Reconcile(t.Context(), "prod", channel("latest-stable"))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if outcome.Admission.Pin().Digest != digestA {
		t.Errorf("converged on the wrong bytes: %s", outcome.Admission.Pin())
	}
	if len(parts.log.recorded) != 1 {
		t.Fatalf("expected one resolution record, got %d", len(parts.log.recorded))
	}
	record := parts.log.recorded[0]
	if record.Environment != "prod" || record.Pin.Digest != digestA || record.Publisher != identity {
		t.Errorf("the record does not describe what happened: %+v", record)
	}
	// The channel must survive in the record, because "we deployed what the
	// channel said" is only checkable if the channel is written down.
	if record.Selector.Channel != "latest-stable" {
		t.Errorf("the record lost the selector: %+v", record.Selector)
	}
	if len(parts.converger.converged) != 1 {
		t.Errorf("expected one convergence, got %d", len(parts.converger.converged))
	}
}

// A deployment nobody can attribute to a resolution is the state this design
// exists to prevent, so a log that cannot accept the record stops the deploy.
func TestABrokenResolutionLogStopsTheDeployment(t *testing.T) {
	parts := newFixture(t)
	parts.log.err = errors.New("resolution log unavailable")
	_, err := parts.source.Reconcile(t.Context(), "prod", channel("latest-stable"))
	if err == nil {
		t.Fatal("a deployment proceeded without being recorded")
	}
	if len(parts.converger.converged) != 0 {
		t.Fatalf("converged despite an unrecorded resolution: %v", parts.converger.converged)
	}
	if !strings.Contains(err.Error(), "before deploying it") {
		t.Errorf("the failure does not say why it mattered: %v", err)
	}
}

// --- refusals never fall through --------------------------------------------

func TestARefusedSourceIsNeverConverged(t *testing.T) {
	parts := newFixture(t)
	parts.verifier.attestation.Identity = "https://github.com/attacker/conduit/.github/workflows/x.yml@refs/tags/v1.0.0"
	_, err := parts.source.Reconcile(t.Context(), "prod", channel("latest-stable"))

	var refused *application.RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("expected a refusal, got %v", err)
	}
	if len(parts.converger.converged) != 0 {
		t.Fatal("a refused source was converged on anyway")
	}
	if len(parts.log.recorded) != 0 {
		t.Fatal("a refused source was recorded as a resolution")
	}
	found := false
	for _, reason := range refused.Reasons() {
		if reason == domain.ReasonUntrustedIdent {
			found = true
		}
	}
	if !found {
		t.Errorf("the refusal does not name the untrusted identity: %v", refused.Reasons())
	}
}

// "Could not check" and "checked, and it is unsigned" are different facts. Both
// refuse, and neither is allowed to look like the other.
func TestAVerifierThatCannotAnswerIsNotAVerifierThatSaidYes(t *testing.T) {
	parts := newFixture(t)
	parts.verifier.err = errors.New("transparency log unreachable")
	_, err := parts.source.Reconcile(t.Context(), "prod", channel("latest-stable"))
	if err == nil {
		t.Fatal("an unverifiable source was admitted")
	}
	var refused *application.RefusedError
	if errors.As(err, &refused) {
		t.Error("an unavailable verifier was reported as a policy refusal")
	}
	if len(parts.converger.converged) != 0 {
		t.Fatal("converged despite an unavailable verifier")
	}
}

func TestAnUnavailablePolicyStopsTheReconcile(t *testing.T) {
	parts := newFixture(t)
	parts.policies.err = errors.New("policy store unavailable")
	if _, err := parts.source.Reconcile(t.Context(), "prod", channel("latest-stable")); err == nil {
		t.Fatal("a source was admitted without knowing who the environment trusts")
	}
	if parts.resolver.calls != 0 {
		t.Error("the registry was consulted before the policy was known")
	}
}

// --- continuous reconciliation ----------------------------------------------

// Restating desired state on a schedule is what makes this GitOps. An unchanged
// channel must cost nothing but the check.
func TestAnUnchangedChannelDoesNotRedeployOrRelog(t *testing.T) {
	parts := newFixture(t)
	if _, err := parts.source.Reconcile(t.Context(), "prod", channel("latest-stable")); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	outcome, err := parts.source.Reconcile(t.Context(), "prod", channel("latest-stable"))
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if outcome.Moved {
		t.Error("an unchanged channel reported a move")
	}
	if len(parts.converger.converged) != 1 {
		t.Errorf("an unchanged channel deployed again: %d convergences", len(parts.converger.converged))
	}
	if len(parts.log.recorded) != 1 {
		t.Errorf("an unchanged channel wrote another resolution: %d records", len(parts.log.recorded))
	}
}

func TestAMovedChannelSupersedesWhatWasDeployed(t *testing.T) {
	parts := newFixture(t)
	if _, err := parts.source.Reconcile(t.Context(), "prod", channel("latest-stable")); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	parts.resolver.digest = digestB
	outcome, err := parts.source.Reconcile(t.Context(), "prod", channel("latest-stable"))
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if !outcome.Moved || outcome.Superseded != digestA {
		t.Fatalf("the move was not reported: moved=%v superseded=%s", outcome.Moved, outcome.Superseded)
	}
	record := parts.log.recorded[len(parts.log.recorded)-1]
	if !record.Moved() || record.Superseded != digestA {
		t.Errorf("the record does not show what was replaced: %+v", record)
	}
}

// The first deployment is not a move: there was nothing to move from, and
// reporting one would make every environment's first reconcile look like a
// change of source.
func TestTheFirstDeploymentIsNotAMove(t *testing.T) {
	parts := newFixture(t)
	outcome, err := parts.source.Reconcile(t.Context(), "prod", channel("latest-stable"))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if outcome.Moved {
		t.Error("a first deployment reported a move")
	}
}

func TestAnEnvironmentIsRequired(t *testing.T) {
	parts := newFixture(t)
	if _, err := parts.source.Reconcile(t.Context(), "  ", channel("latest")); err == nil {
		t.Fatal("reconciled an unnamed environment")
	}
}

func TestAMalformedSelectorIsRefusedBeforeAnythingIsAsked(t *testing.T) {
	parts := newFixture(t)
	_, err := parts.source.Reconcile(t.Context(), "prod", domain.Selector{Kind: domain.ByChannel, Repository: repository})
	if err == nil {
		t.Fatal("a malformed selector was reconciled")
	}
	if parts.resolver.calls != 0 {
		t.Error("the registry was consulted for a malformed selector")
	}
}
