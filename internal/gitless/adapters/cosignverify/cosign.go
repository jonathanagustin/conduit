// Package cosignverify reports who signed an artifact, using the cosign CLI.
//
// It shells out rather than linking sigstore's Go libraries. That is a real
// trade -- a subprocess and an external binary against a large dependency tree
// reaching into TUF, Rekor and Fulcio clients -- and it was made this way
// because the release workflow already signs with this same binary, so the
// verifying side and the signing side stay one tool with one version to pin.
//
// The important detail is how it is invoked. cosign normally decides: you give
// it an identity and it says yes or no. That would put Conduit's publisher
// policy inside an adapter, where being lenient costs nothing and nothing
// fails. So it is invoked with permissive patterns and used only to EXTRACT --
// the certificate's subject and issuer come back as observations, and
// domain.Admit does the judging.
//
// Reading that and concluding the policy is permissive would be the natural
// mistake. It is not: nothing here returns a decision, and the only way to
// obtain an Admission is to satisfy domain.Admit.
package cosignverify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

// anyIdentity makes cosign extract instead of decide. See the package comment.
const anyIdentity = ".*"

// dockerConfigVariable is how cosign is told about a private registry.
//
// It is an environment variable and not a flag, and that distinction is the
// whole of issue #220's first defect. `cosign verify` has no --registry-config;
// it has --registry-username/-password/-token, which would put credential bytes
// in argv, and it reads a mounted Docker configuration only through this
// variable. Passing a flag cosign does not have is not a lenient no-op: cosign
// exits 1 with "unknown flag", which is a hard verifier error, so the credential
// feature could not be switched on at all without failing every reconcile.
//
// Verified empirically against the checksum-pinned v3.1.3 release this
// repository ships (sha256 4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049
// 520bc7d9df9efc7f71): with the variable set to a directory holding a
// config.json, cosign sends `Authorization: Basic ...` to the registry; without
// it, the same run sends none and the registry answers 401.
const dockerConfigVariable = "DOCKER_CONFIG"

// Verifier reports what cosign observed about an artifact's signature.
type Verifier struct {
	binary string
	// extra is appended to every invocation, for a caller that needs a
	// non-default trust root or registry TLS material.
	extra []string
	// dockerConfigDirectory is exported to the child as DOCKER_CONFIG. It is a
	// DIRECTORY holding a config.json, which is what cosign reads; naming the
	// credential file itself would leave verification silently anonymous.
	dockerConfigDirectory string
}

// Option configures a Verifier. Options exist rather than more positional
// parameters because the two things a private registry needs -- a credential
// directory and possibly a CA bundle -- travel by different mechanisms, and
// collapsing them into one string slice is how a flag cosign does not have got
// shipped in the first place.
type Option func(*Verifier)

// WithRegistryConfigDirectory names the directory holding the Docker
// config.json cosign should authenticate registries with.
//
// A directory, not a file. cosign joins "config.json" onto it, and a directory
// with no config.json is not an error to cosign -- it simply pulls anonymously.
// Callers should therefore validate the path before getting here; see
// registryauth.DockerConfigDirectory.
func WithRegistryConfigDirectory(directory string) Option {
	return func(verifier *Verifier) { verifier.dockerConfigDirectory = strings.TrimSpace(directory) }
}

// WithArguments appends arguments before the immutable artifact reference.
// Credential CONTENTS must never travel this way: argv is world-readable on a
// shared host. It is for things like --registry-cacert, which names a path.
func WithArguments(arguments ...string) Option {
	return func(verifier *Verifier) { verifier.extra = append(verifier.extra, arguments...) }
}

// New takes the cosign binary to run. An empty name means "cosign", resolved on
// PATH; a missing binary is reported when a verification is attempted rather
// than at construction, so a Hub that never verifies anything still starts.
func New(binary string, options ...Option) *Verifier {
	if strings.TrimSpace(binary) == "" {
		binary = "cosign"
	}
	verifier := &Verifier{binary: binary}
	for _, option := range options {
		option(verifier)
	}
	return verifier
}

// entry is the part of cosign's JSON this adapter reads. The rest of the
// document -- the bundle, the Fulcio extension OIDs, the workflow annotations --
// is deliberately ignored: reading more would be inventing meaning for fields
// nothing here checks.
type entry struct {
	Critical struct {
		Image struct {
			Digest string `json:"docker-manifest-digest"`
		} `json:"image"`
	} `json:"critical"`
	Optional struct {
		Subject string `json:"Subject"`
		Issuer  string `json:"Issuer"`
	} `json:"optional"`
}

// Verify runs cosign against exact bytes and reports what it saw.
//
// An unsigned artifact is not an error: it is an Attestation with Signed false,
// which the domain refuses. An error means cosign could not answer at all --
// binary missing, registry unreachable, transparency log down -- and the
// application keeps that distinct from "checked, and it is unsigned" while
// refusing both.
func (verifier *Verifier) Verify(ctx context.Context, pin domain.Pin) (domain.Attestation, error) {
	if err := pin.Validate(); err != nil {
		return domain.Attestation{}, fmt.Errorf("refusing to verify an invalid pin: %w", err)
	}
	arguments := append([]string{
		"verify",
		"--certificate-identity-regexp", anyIdentity,
		"--certificate-oidc-issuer-regexp", anyIdentity,
		"--output", "json",
	}, verifier.extra...)
	arguments = append(arguments, pin.String())

	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(ctx, verifier.binary, arguments...)
	command.Stdout, command.Stderr = &stdout, &stderr
	command.Env = verifier.environment(os.Environ())

	if err := command.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && looksUnsigned(stderr.String()) {
			// cosign exits non-zero for "no signatures" as well as for "could
			// not look". Only the first is an observation about the artifact.
			return domain.Attestation{Signed: false, Digest: pin.Digest}, nil
		}
		return domain.Attestation{}, fmt.Errorf("cosign could not answer for %s: %w: %s",
			pin, err, firstLine(stderr.String()))
	}

	var entries []entry
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		return domain.Attestation{}, fmt.Errorf("cosign output for %s was not the expected JSON: %w", pin, err)
	}
	if len(entries) == 0 {
		return domain.Attestation{Signed: false, Digest: pin.Digest}, nil
	}

	// More than one signature is normal and not this adapter's decision to
	// resolve. The first entry that names a certificate is reported; if none
	// does, the artifact is signed by something with no keyless identity, which
	// the domain refuses because it cannot satisfy any publisher policy.
	for _, candidate := range entries {
		if candidate.Optional.Subject == "" && candidate.Optional.Issuer == "" {
			continue
		}
		return domain.Attestation{
			Signed:   true,
			Issuer:   candidate.Optional.Issuer,
			Identity: candidate.Optional.Subject,
			// The digest cosign says it checked, not the one that was asked
			// for. The domain compares them, so a verifier that reported on
			// something else is caught rather than believed.
			Digest: candidate.Critical.Image.Digest,
		}, nil
	}
	return domain.Attestation{Signed: false, Digest: pin.Digest}, nil
}

// environment is the child's environment: the parent's, with DOCKER_CONFIG
// replaced rather than appended.
//
// Replaced, because an ambient DOCKER_CONFIG in the Hub's own environment must
// not decide which credentials verification uses. os/exec would in fact honour
// a trailing duplicate, but a reader cannot see that from here, and "which of
// two DOCKER_CONFIG entries wins" is not a thing this should depend on.
//
// Returning the parent's environment unchanged when nothing is configured is
// deliberate: cosign still needs PATH, HOME and the proxy variables, and a Hub
// pulling public artifacts never had a Docker configuration to begin with.
func (verifier *Verifier) environment(parent []string) []string {
	if verifier.dockerConfigDirectory == "" {
		return parent
	}
	child := make([]string, 0, len(parent)+1)
	for _, entry := range parent {
		if name, _, found := strings.Cut(entry, "="); found && name == dockerConfigVariable {
			continue
		}
		child = append(child, entry)
	}
	return append(child, dockerConfigVariable+"="+verifier.dockerConfigDirectory)
}

// looksUnsigned distinguishes "cosign looked and found nothing" from "cosign
// could not look". The strings are cosign's own; a future release that changes
// them turns an unsigned artifact into an error, which refuses either way and
// is the safe direction for this to be wrong in.
func looksUnsigned(stderr string) bool {
	lowered := strings.ToLower(stderr)
	for _, phrase := range []string{
		"no signatures found",
		"no matching signatures",
		"manifest unknown",
	} {
		if strings.Contains(lowered, phrase) {
			return true
		}
	}
	return false
}

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}
