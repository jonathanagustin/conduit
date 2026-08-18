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

// --- how a private registry is reached --------------------------------------
//
// The version of these tests that shipped with #220 asserted only that
// "--registry-config <path>" appeared in argv, against a stub that echoed its
// arguments and accepted anything. cosign v3.1.3 -- the release this repository
// pins by checksum and ships in the Hub image -- has no such flag, so the exact
// argv the adapter produced exited 1 with "unknown flag: --registry-config",
// which matches none of looksUnsigned's phrases and is therefore a hard verifier
// error on every reconcile. The gate could not see it because the stub had no
// opinion about flags.
//
// So the stub below has one. It refuses any flag outside the set cosign v3.1.3's
// `verify` command actually accepts, and TestAFlagCosignDoesNotHaveIsAHardError
// exercises that refusal, so a future change that reintroduces an invented flag
// fails here rather than in a cluster.

// verifyFlags is `cosign verify --help` at v3.1.3, restricted to the registry
// and output flags an adapter has any reason to send. Read off the checksum-
// pinned binary (sha256 4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9
// df9efc7f71), not off documentation.
const verifyFlags = "--certificate-identity --certificate-identity-regexp " +
	"--certificate-oidc-issuer --certificate-oidc-issuer-regexp --output " +
	"--registry-username --registry-password --registry-token --registry-cacert " +
	"--registry-client-cert --registry-client-key --registry-server-name " +
	"--allow-http-registry --allow-insecure-registry --k8s-keychain"

// strictCosign stands in for the binary the way the previous stub did, plus the
// one behaviour that matters: it exits 1 on a flag cosign does not have, with
// the message cosign itself prints. It records argv and DOCKER_CONFIG so a test
// can assert what the child was actually given.
func strictCosign(t *testing.T) (binary, argv, dockerConfig string) {
	t.Helper()
	directory := t.TempDir()
	binary = filepath.Join(directory, "cosign")
	argv = filepath.Join(directory, "argv")
	dockerConfig = filepath.Join(directory, "docker-config")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > '" + argv + "'\n" +
		"printf '%s' \"${DOCKER_CONFIG-<unset>}\" > '" + dockerConfig + "'\n" +
		"known='" + verifyFlags + "'\n" +
		"for argument in \"$@\"; do\n" +
		"  case \"${argument}\" in\n" +
		"    --*)\n" +
		"      name=\"${argument%%=*}\"\n" +
		"      case \" ${known} \" in\n" +
		"        *\" ${name} \"*) ;;\n" +
		"        *) echo \"Error: unknown flag: ${name}\" >&2; exit 1;;\n" +
		"      esac\n" +
		"      ;;\n" +
		"  esac\n" +
		"done\n" +
		"cat <<'JSON'\n" + realShape + "\nJSON\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary, argv, dockerConfig
}

func read(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// The fix for #220's first defect, asserted on both sides: the credential
// directory reaches cosign as DOCKER_CONFIG, and nothing about it reaches argv.
func TestTheRegistryConfigurationReachesCosignAsDockerConfigRatherThanAFlag(t *testing.T) {
	binary, argv, dockerConfig := strictCosign(t)
	directory := t.TempDir()

	verifier := cosignverify.New(binary, cosignverify.WithRegistryConfigDirectory(directory))
	if _, err := verifier.Verify(t.Context(), pin()); err != nil {
		t.Fatalf("verify against a stub that refuses flags cosign does not have: %v", err)
	}

	if got := read(t, dockerConfig); got != directory {
		t.Errorf("cosign was run with DOCKER_CONFIG=%q, want %q", got, directory)
	}
	passed := read(t, argv)
	if strings.Contains(passed, "--registry-config") {
		t.Errorf("the adapter still passes a flag cosign has no such thing as:\n%s", passed)
	}
	// argv is world-readable on a shared host, so neither the credential
	// directory nor anything under it belongs in it.
	if strings.Contains(passed, directory) {
		t.Errorf("the credential directory reached argv:\n%s", passed)
	}
	if want := pin().String() + "\n"; !strings.HasSuffix(passed, want) {
		t.Errorf("cosign arguments do not end with the immutable pin:\n%s", passed)
	}
}

// This is the guard on the guard. If the stub ever stops rejecting a flag
// cosign does not have, this test stops failing, and the test above goes back to
// being satisfiable by an adapter that cannot work against the real binary.
func TestAFlagCosignDoesNotHaveIsAHardError(t *testing.T) {
	binary, _, _ := strictCosign(t)
	verifier := cosignverify.New(binary, cosignverify.WithArguments("--registry-config", t.TempDir()))
	attestation, err := verifier.Verify(t.Context(), pin())
	if err == nil {
		t.Fatalf("a flag cosign does not have produced an attestation: %+v", attestation)
	}
	if attestation.Signed {
		t.Fatal("a failed invocation reported a signature")
	}
}

// An ambient DOCKER_CONFIG in the Hub's own environment must not decide which
// credentials verification uses. The configured one wins, unambiguously.
func TestTheConfiguredCredentialDirectoryOverridesAnAmbientOne(t *testing.T) {
	binary, _, dockerConfig := strictCosign(t)
	ambient := t.TempDir()
	configured := t.TempDir()
	t.Setenv("DOCKER_CONFIG", ambient)

	verifier := cosignverify.New(binary, cosignverify.WithRegistryConfigDirectory(configured))
	if _, err := verifier.Verify(t.Context(), pin()); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got := read(t, dockerConfig); got != configured {
		t.Fatalf("cosign was run with DOCKER_CONFIG=%q, want the configured %q", got, configured)
	}
}

// And with nothing configured the child keeps the parent's environment. cosign
// still needs PATH, HOME and the proxy variables, and a Hub pulling public
// artifacts never had a Docker configuration to begin with.
func TestNothingConfiguredLeavesTheEnvironmentAlone(t *testing.T) {
	binary, _, dockerConfig := strictCosign(t)
	ambient := t.TempDir()
	t.Setenv("DOCKER_CONFIG", ambient)

	if _, err := cosignverify.New(binary).Verify(t.Context(), pin()); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got := read(t, dockerConfig); got != ambient {
		t.Fatalf("cosign was run with DOCKER_CONFIG=%q, want the inherited %q", got, ambient)
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
