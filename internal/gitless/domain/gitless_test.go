package domain_test

import (
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

const (
	repository = "ghcr.io/example/platform-runtime"
	issuer     = "https://token.actions.githubusercontent.com"
	identity   = "https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/v1.2.3"
)

var (
	digestA = "sha256:aa11" + strings.Repeat("0", 60)
	digestB = "sha256:bb22" + strings.Repeat("0", 60)
)

func policy() domain.PublisherPolicy {
	return domain.PublisherPolicy{
		CertificateIssuer:   `^https://token\.actions\.githubusercontent\.com$`,
		CertificateIdentity: `^https://github\.com/jonathanagustin/conduit/\.github/workflows/platform-images\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$`,
	}
}

func channel(name string) domain.Selector {
	return domain.Selector{Kind: domain.ByChannel, Repository: repository, Channel: name}
}

func pinned(digest string) domain.Selector {
	return domain.Selector{Kind: domain.ByDigest, Repository: repository, Digest: digest}
}

func good(digest string) domain.Attestation {
	return domain.Attestation{Signed: true, Issuer: issuer, Identity: identity, Digest: digest}
}

func reasons(refusals []domain.Refusal) []domain.RefusalReason {
	out := make([]domain.RefusalReason, 0, len(refusals))
	for _, refusal := range refusals {
		out = append(out, refusal.Reason)
	}
	return out
}

func has(refusals []domain.Refusal, reason domain.RefusalReason) bool {
	for _, refusal := range refusals {
		if refusal.Reason == reason {
			return true
		}
	}
	return false
}

// --- the happy paths, which must stay narrow -------------------------------

func TestAChannelResolvingToSignedBytesIsAdmitted(t *testing.T) {
	admission, refusals := domain.Admit(
		channel("latest-stable"), domain.Pin{Repository: repository, Digest: digestA}, good(digestA), policy())
	if len(refusals) != 0 {
		t.Fatalf("refused a correctly signed artifact: %v", reasons(refusals))
	}
	if admission.Pin().Digest != digestA {
		t.Errorf("admitted the wrong bytes: %s", admission.Pin())
	}
	// What converges must be the digest; the channel survives only as a record
	// of what was asked for.
	kind, label := admission.Selector()
	if kind != domain.ByChannel || label != "latest-stable" {
		t.Errorf("the admission lost what was asked for: %s %q", kind, label)
	}
	if _, publisher := admission.Publisher(); publisher != identity {
		t.Errorf("the admission lost who signed: %q", publisher)
	}
}

func TestADigestSelectorResolvingToItselfIsAdmitted(t *testing.T) {
	_, refusals := domain.Admit(
		pinned(digestA), domain.Pin{Repository: repository, Digest: digestA}, good(digestA), policy())
	if len(refusals) != 0 {
		t.Fatalf("refused an exact coordinate: %v", reasons(refusals))
	}
}

// --- the refusals, which are the point --------------------------------------

// The single failure that would make every other guarantee meaningless: an
// exact coordinate that resolves to something else.
func TestADigestSelectorThatResolvesElsewhereIsRefused(t *testing.T) {
	_, refusals := domain.Admit(
		pinned(digestA), domain.Pin{Repository: repository, Digest: digestB}, good(digestB), policy())
	if !has(refusals, domain.ReasonDigestMismatch) {
		t.Fatalf("an exact coordinate resolved elsewhere and was admitted: %v", reasons(refusals))
	}
}

func TestUnsignedBytesAreRefused(t *testing.T) {
	_, refusals := domain.Admit(
		channel("latest"), domain.Pin{Repository: repository, Digest: digestA},
		domain.Attestation{Signed: false, Digest: digestA}, policy())
	if !has(refusals, domain.ReasonUnsigned) {
		t.Fatalf("unsigned bytes were admitted: %v", reasons(refusals))
	}
}

func TestAnUntrustedIdentityIsRefused(t *testing.T) {
	attestation := good(digestA)
	attestation.Identity = "https://github.com/attacker/conduit/.github/workflows/platform-images.yml@refs/tags/v1.2.3"
	_, refusals := domain.Admit(channel("latest"), domain.Pin{Repository: repository, Digest: digestA}, attestation, policy())
	if !has(refusals, domain.ReasonUntrustedIdent) {
		t.Fatalf("a foreign publisher was admitted: %v", reasons(refusals))
	}
}

func TestAnUntrustedIssuerIsRefused(t *testing.T) {
	attestation := good(digestA)
	attestation.Issuer = "https://token.actions.example.com"
	_, refusals := domain.Admit(channel("latest"), domain.Pin{Repository: repository, Digest: digestA}, attestation, policy())
	if !has(refusals, domain.ReasonUntrustedIssue) {
		t.Fatalf("a foreign issuer was admitted: %v", reasons(refusals))
	}
}

// A branch build is signed by the same workflow and the same issuer. Only the
// ref distinguishes it from a release, which is exactly what a production
// policy is for.
func TestAnUntaggedBuildDoesNotSatisfyAReleasePolicy(t *testing.T) {
	attestation := good(digestA)
	attestation.Identity = "https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/heads/main"
	_, refusals := domain.Admit(channel("latest"), domain.Pin{Repository: repository, Digest: digestA}, attestation, policy())
	if !has(refusals, domain.ReasonUntrustedIdent) {
		t.Fatalf("a branch build satisfied a release policy: %v", reasons(refusals))
	}
}

// An attestation about other bytes tells us nothing about these. Matching its
// issuer would be worse than not looking, so the mismatch is caught before the
// patterns are consulted at all.
func TestAnAttestationAboutOtherBytesIsRefused(t *testing.T) {
	_, refusals := domain.Admit(
		channel("latest"), domain.Pin{Repository: repository, Digest: digestA}, good(digestB), policy())
	if !has(refusals, domain.ReasonDigestMismatch) {
		t.Fatalf("an attestation over different bytes was accepted: %v", reasons(refusals))
	}
	if has(refusals, domain.ReasonUntrustedIssue) || has(refusals, domain.ReasonUntrustedIdent) {
		t.Errorf("the patterns were consulted against an irrelevant attestation: %v", reasons(refusals))
	}
}

func TestResolvingIntoADifferentRepositoryIsRefused(t *testing.T) {
	_, refusals := domain.Admit(
		channel("latest"), domain.Pin{Repository: "ghcr.io/attacker/platform-runtime", Digest: digestA},
		good(digestA), policy())
	if !has(refusals, domain.ReasonDigestMismatch) {
		t.Fatalf("a resolution into a foreign repository was admitted: %v", reasons(refusals))
	}
}

// Every reason is returned, because reporting one invites fixing one.
func TestEveryReasonIsReported(t *testing.T) {
	attestation := domain.Attestation{Signed: true, Issuer: "wrong", Identity: "wrong", Digest: digestA}
	_, refusals := domain.Admit(pinned(digestB), domain.Pin{Repository: repository, Digest: digestA}, attestation, policy())
	for _, want := range []domain.RefusalReason{
		domain.ReasonDigestMismatch, domain.ReasonUntrustedIssue, domain.ReasonUntrustedIdent,
	} {
		if !has(refusals, want) {
			t.Errorf("missing reason %s in %v", want, reasons(refusals))
		}
	}
}

// --- the policy itself ------------------------------------------------------

// The classic way a signer policy becomes decoration.
func TestAnUnanchoredPolicyIsRefused(t *testing.T) {
	loose := policy()
	loose.CertificateIdentity = `https://github\.com/jonathanagustin/conduit/.*`
	if err := loose.Validate(); err == nil {
		t.Fatal("an unanchored identity pattern was accepted")
	}
	// And it must not be usable through Admit either, or the check above would
	// only be advice.
	attacker := good(digestA)
	attacker.Identity = "https://github.com/attacker/https://github.com/jonathanagustin/conduit/x"
	_, refusals := domain.Admit(channel("latest"), domain.Pin{Repository: repository, Digest: digestA}, attacker, loose)
	if len(refusals) == 0 {
		t.Fatal("an unanchored policy admitted a prefix-matched foreign identity")
	}
}

func TestAnUncompilablePolicyIsRefused(t *testing.T) {
	broken := policy()
	broken.CertificateIssuer = "^([a-z$"
	if err := broken.Validate(); err == nil {
		t.Fatal("a policy that does not compile was accepted")
	}
}

func TestAnEmptyPolicyIsRefused(t *testing.T) {
	if err := (domain.PublisherPolicy{}).Validate(); err == nil {
		t.Fatal("an empty publisher policy was accepted")
	}
	_, refusals := domain.Admit(
		channel("latest"), domain.Pin{Repository: repository, Digest: digestA}, good(digestA), domain.PublisherPolicy{})
	if len(refusals) == 0 {
		t.Fatal("an empty publisher policy admitted an artifact")
	}
}

// --- selectors and pins -----------------------------------------------------

func TestSelectorValidation(t *testing.T) {
	for name, selector := range map[string]domain.Selector{
		"no repository":       {Kind: domain.ByChannel, Channel: "latest"},
		"unknown kind":        {Kind: "SOMETHING", Repository: repository, Channel: "latest"},
		"channel and digest":  {Kind: domain.ByChannel, Repository: repository, Channel: "latest", Digest: digestA},
		"digest and channel":  {Kind: domain.ByDigest, Repository: repository, Digest: digestA, Channel: "latest"},
		"uppercase digest":    {Kind: domain.ByDigest, Repository: repository, Digest: strings.ToUpper(digestA)},
		"short digest":        {Kind: domain.ByDigest, Repository: repository, Digest: "sha256:abcd"},
		"untrimmed channel":   {Kind: domain.ByChannel, Repository: repository, Channel: " latest "},
		"untrimmed reposito1": {Kind: domain.ByChannel, Repository: " " + repository, Channel: "latest"},
	} {
		if err := selector.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := channel("latest").Validate(); err != nil {
		t.Errorf("a well-formed channel selector was rejected: %v", err)
	}
	if err := pinned(digestA).Validate(); err != nil {
		t.Errorf("a well-formed digest selector was rejected: %v", err)
	}
}

// A digest that had to be repaired before use is one somebody could have
// repaired differently, so uppercase hex is refused rather than lowercased.
func TestAnUppercaseDigestIsRefusedRatherThanNormalised(t *testing.T) {
	pin := domain.Pin{Repository: repository, Digest: strings.ToUpper(digestA)}
	if err := pin.Validate(); err == nil {
		t.Fatal("an uppercase digest was accepted")
	}
}

// --- resolutions ------------------------------------------------------------

func TestAResolutionKnowsWhetherItMoved(t *testing.T) {
	first := domain.Resolution{
		Environment: "prod", Selector: channel("latest-stable"),
		Pin: domain.Pin{Repository: repository, Digest: digestA},
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("a first resolution was rejected: %v", err)
	}
	if first.Moved() {
		t.Error("a first resolution reported a move")
	}
	moved := first
	moved.Superseded = digestB
	if !moved.Moved() {
		t.Error("a changed digest did not report a move")
	}
}

func TestAResolutionThatSupersedesItselfIsRefused(t *testing.T) {
	resolution := domain.Resolution{
		Environment: "prod", Selector: channel("latest-stable"),
		Pin: domain.Pin{Repository: repository, Digest: digestA}, Superseded: digestA,
	}
	if err := resolution.Validate(); err == nil {
		t.Fatal("a resolution that supersedes itself was accepted as a change")
	}
}

func TestAResolutionNeedsAnEnvironmentAndAValidPin(t *testing.T) {
	for name, resolution := range map[string]domain.Resolution{
		"no environment": {Selector: channel("latest"), Pin: domain.Pin{Repository: repository, Digest: digestA}},
		"bad pin":        {Environment: "prod", Selector: channel("latest"), Pin: domain.Pin{Repository: repository}},
		"bad superseded": {
			Environment: "prod", Selector: channel("latest"),
			Pin: domain.Pin{Repository: repository, Digest: digestA}, Superseded: "nonsense",
		},
	} {
		if err := resolution.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
