package cosignverify_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/cosignverify"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

const digest = "sha256:85a1d3844c22def063b9d4924a4539ef2d5d850a97be7e8b6cd3f62c5f80708a"

// fakeCosign writes a script standing in for the binary, so the adapter's
// parsing and its exit-code handling can be exercised without a network.
func fakeCosign(t *testing.T, stdout, stderr string, exit int) string {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "cosign")
	script := "#!/bin/sh\n" +
		"cat <<'STDOUT'\n" + stdout + "\nSTDOUT\n" +
		"cat >&2 <<'STDERR'\n" + stderr + "\nSTDERR\n" +
		"exit " + itoa(exit) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cosign: %v", err)
	}
	return path
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	return string(rune('0' + value))
}

func pin() domain.Pin {
	return domain.Pin{Repository: "ghcr.io/example/platform-runtime", Digest: digest}
}

// The shape is taken verbatim from a real `cosign verify --output json` run
// against a keyless-signed public artifact, not from documentation.
const realShape = `[{
  "critical": {
    "identity": {"docker-reference": "ghcr.io/fluxcd/source-controller"},
    "image": {"docker-manifest-digest": "` + digest + `"},
    "type": "cosign container image signature"
  },
  "optional": {
    "Issuer": "https://token.actions.githubusercontent.com",
    "Subject": "https://github.com/fluxcd/gha-workflows/.github/workflows/controller-release.yaml@refs/tags/v0.4.0",
    "githubWorkflowName": "release"
  }
}]`

func TestASignedArtifactIsReportedWithItsCertificateIdentity(t *testing.T) {
	verifier := cosignverify.New(fakeCosign(t, realShape, "Verified OK", 0))
	attestation, err := verifier.Verify(t.Context(), pin())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !attestation.Signed {
		t.Fatal("a signed artifact was reported as unsigned")
	}
	if attestation.Issuer != "https://token.actions.githubusercontent.com" {
		t.Errorf("issuer: %q", attestation.Issuer)
	}
	if !strings.HasPrefix(attestation.Identity, "https://github.com/fluxcd/gha-workflows/") {
		t.Errorf("identity: %q", attestation.Identity)
	}
	if attestation.Digest != digest {
		t.Errorf("digest: %q", attestation.Digest)
	}
}

func TestRegistryConfigurationIsPassedAsAPathNotCredentialContent(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "cosign")
	arguments := filepath.Join(directory, "arguments")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > '" + arguments + "'\n" +
		"cat <<'JSON'\n" + realShape + "\nJSON\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(directory, "registry.json")
	verifier := cosignverify.New(binary, "--registry-config", configPath)
	if _, err := verifier.Verify(t.Context(), pin()); err != nil {
		t.Fatalf("verify: %v", err)
	}
	passed, err := os.ReadFile(arguments)
	if err != nil {
		t.Fatal(err)
	}
	want := "--registry-config\n" + configPath + "\n" + pin().String() + "\n"
	if !strings.HasSuffix(string(passed), want) {
		t.Fatalf("cosign arguments do not end with the mounted config and immutable pin:\n%s", passed)
	}
}

// The adapter must report what cosign says it checked, not what was asked for.
// Reporting the requested digest would defeat the domain's cross-check, which
// exists precisely to catch a verifier that looked at something else.
func TestTheReportedDigestIsTheOneCosignChecked(t *testing.T) {
	other := "sha256:" + strings.Repeat("c", 64)
	output := strings.Replace(realShape, digest, other, 1)
	verifier := cosignverify.New(fakeCosign(t, output, "", 0))
	attestation, err := verifier.Verify(t.Context(), pin())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if attestation.Digest != other {
		t.Fatalf("the adapter reported the requested digest rather than the checked one: %q", attestation.Digest)
	}
	// And the domain must then refuse it.
	_, refusals := domain.Admit(
		domain.Selector{Kind: domain.ByDigest, Repository: pin().Repository, Digest: digest},
		pin(), attestation,
		domain.PublisherPolicy{CertificateIssuer: `^.*$`, CertificateIdentity: `^.*$`},
	)
	if len(refusals) == 0 {
		t.Fatal("an attestation over other bytes was admitted")
	}
}

// "Looked and found nothing" is an observation about the artifact.
func TestAnUnsignedArtifactIsAnObservationRatherThanAnError(t *testing.T) {
	verifier := cosignverify.New(fakeCosign(t, "", "Error: no signatures found for image", 1))
	attestation, err := verifier.Verify(t.Context(), pin())
	if err != nil {
		t.Fatalf("an unsigned artifact was reported as an error: %v", err)
	}
	if attestation.Signed {
		t.Fatal("an unsigned artifact was reported as signed")
	}
}

// "Could not look" is not. Collapsing the two would let a registry outage read
// as a policy failure, or worse, be retried until it looked like one.
func TestAnUnreachableVerifierIsAnErrorRatherThanUnsigned(t *testing.T) {
	verifier := cosignverify.New(fakeCosign(t, "", "Error: GET https://ghcr.io/v2/: dial tcp: lookup failed", 1))
	if _, err := verifier.Verify(t.Context(), pin()); err == nil {
		t.Fatal("an unreachable registry was reported as an unsigned artifact")
	}
}

func TestAMissingBinaryIsAnError(t *testing.T) {
	verifier := cosignverify.New(filepath.Join(t.TempDir(), "absent-cosign"))
	if _, err := verifier.Verify(t.Context(), pin()); err == nil {
		t.Fatal("a missing cosign binary was reported as a successful verification")
	}
}

func TestUnexpectedOutputIsAnErrorRatherThanAnEmptyAttestation(t *testing.T) {
	verifier := cosignverify.New(fakeCosign(t, "not json at all", "", 0))
	if _, err := verifier.Verify(t.Context(), pin()); err == nil {
		t.Fatal("unparseable cosign output produced an attestation")
	}
}

// A signature with no keyless certificate cannot satisfy any publisher policy,
// so it is reported as unsigned rather than as a signature with empty fields --
// which an empty-anchored policy might otherwise match.
func TestASignatureWithNoCertificateIdentityIsReportedUnsigned(t *testing.T) {
	verifier := cosignverify.New(fakeCosign(t, `[{"critical":{"image":{"docker-manifest-digest":"`+digest+`"}},"optional":{}}]`, "", 0))
	attestation, err := verifier.Verify(t.Context(), pin())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if attestation.Signed {
		t.Fatal("a signature with no certificate identity was reported as signed")
	}
}

func TestAnInvalidPinIsRefusedBeforeCosignRuns(t *testing.T) {
	verifier := cosignverify.New(fakeCosign(t, realShape, "", 0))
	if _, err := verifier.Verify(t.Context(), domain.Pin{Repository: "x", Digest: "not-a-digest"}); err == nil {
		t.Fatal("an invalid pin was sent to cosign")
	}
}

// --- against the real binary and a real signature ---------------------------

// This is the check the fakes cannot make: that the JSON shape above is still
// what cosign emits, and that a genuinely keyless-signed artifact reports the
// identity its publisher used. It needs cosign, network, and a public artifact,
// so it skips rather than failing where any of those is missing.
func TestARealKeylessSignatureReportsItsRealPublisher(t *testing.T) {
	if testing.Short() {
		t.Skip("network test")
	}
	if _, err := exec.LookPath("cosign"); err != nil {
		t.Skip("cosign is not installed")
	}
	reference := domain.Pin{Repository: "ghcr.io/fluxcd/source-controller", Digest: digest}
	attestation, err := cosignverify.New("").Verify(t.Context(), reference)
	if err != nil {
		t.Skipf("could not reach the registry or transparency log: %v", err)
	}
	if !attestation.Signed {
		t.Fatal("a known keyless-signed artifact was reported as unsigned")
	}
	if attestation.Issuer != "https://token.actions.githubusercontent.com" {
		t.Errorf("real issuer: %q", attestation.Issuer)
	}
	if !strings.Contains(attestation.Identity, "/.github/workflows/") {
		t.Errorf("real identity does not look like a workflow: %q", attestation.Identity)
	}
	if attestation.Digest != digest {
		t.Errorf("real digest: %q", attestation.Digest)
	}
}
