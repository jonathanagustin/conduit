// Package domain owns what Gitless GitOps means in Conduit, independently of
// who carries it out.
//
// Gitless GitOps is one idea: the desired state of a system is a set of exact,
// signed bytes in a registry, built from manifests reviewed in Git, and a
// cluster converges on them without ever holding a Git credential. Conduit can
// realise that in two ways -- delegate convergence to Flux, or converge with
// its own reconciler and Agent -- and the whole point of putting the idea here
// is that the choice must not change what is admitted.
//
// So this package owns the part that must never differ: which bytes may be
// deployed, and on whose authority. Two rules carry almost all of it.
//
//	A selector is not a source. A channel is a name somebody can move; a digest
//	is bytes nobody can change. Resolution turns the first into the second, and
//	what converges is always the digest -- so "we deployed what the channel said"
//	is a statement with a recorded answer rather than a moment in time.
//
//	Verification is admission, not decoration. An artifact nobody can attribute
//	to a trusted publisher is refused, and refused loudly. It is never admitted
//	with a warning, because a warning is a thing a pipeline learns to ignore.
//
// Nothing here reads a registry, a clock, a store or a cluster. It is given
// what was observed and returns findings.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// digestPattern is the only shape an immutable coordinate may take. Uppercase
// hex and short digests are rejected rather than normalised: a coordinate that
// had to be repaired before use is a coordinate somebody could have repaired
// differently.
var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// SelectorKind separates the two ways an environment can name what it wants.
type SelectorKind string

const (
	// ByDigest names exact bytes. It cannot be moved, and it cannot pick up a
	// new release on its own.
	ByDigest SelectorKind = "DIGEST"
	// ByChannel names a mutable pointer such as a release channel. It is how
	// continuous delivery happens at all, and it is why resolution has to be
	// recorded: the same channel means different bytes on different days.
	ByChannel SelectorKind = "CHANNEL"
)

// Selector is how an environment says what it wants.
type Selector struct {
	Kind SelectorKind
	// Repository is the registry coordinate without a tag or digest, e.g.
	// "ghcr.io/example/platform-runtime".
	Repository string
	// Digest is set when Kind is ByDigest.
	Digest string
	// Channel is set when Kind is ByChannel, e.g. "latest-stable".
	Channel string
}

func (selector Selector) Validate() error {
	if strings.TrimSpace(selector.Repository) == "" || selector.Repository != strings.TrimSpace(selector.Repository) {
		return errors.New("selector requires a canonical registry repository")
	}
	switch selector.Kind {
	case ByDigest:
		if !digestPattern.MatchString(selector.Digest) {
			return errors.New("a digest selector requires an exact lowercase sha256 coordinate")
		}
		if selector.Channel != "" {
			return errors.New("a digest selector must not also name a channel")
		}
	case ByChannel:
		if strings.TrimSpace(selector.Channel) == "" || selector.Channel != strings.TrimSpace(selector.Channel) {
			return errors.New("a channel selector requires a canonical channel name")
		}
		if selector.Digest != "" {
			return errors.New("a channel selector must not also name a digest")
		}
	default:
		return fmt.Errorf("unknown selector kind %q", selector.Kind)
	}
	return nil
}

// String renders the selector the way an operator would write it.
func (selector Selector) String() string {
	if selector.Kind == ByDigest {
		return selector.Repository + "@" + selector.Digest
	}
	return selector.Repository + ":" + selector.Channel
}

// Pin is exact bytes: the only thing this package will let anything converge on.
type Pin struct {
	Repository string
	Digest     string
}

func (pin Pin) Validate() error {
	if strings.TrimSpace(pin.Repository) == "" || pin.Repository != strings.TrimSpace(pin.Repository) {
		return errors.New("pin requires a canonical registry repository")
	}
	if !digestPattern.MatchString(pin.Digest) {
		return errors.New("pin requires an exact lowercase sha256 coordinate")
	}
	return nil
}

func (pin Pin) String() string { return pin.Repository + "@" + pin.Digest }

// PublisherPolicy is who an environment is willing to accept bytes from.
//
// The two fields are anchored regular expressions over a keyless signing
// certificate, which is the vocabulary the release workflow, the Flux manifests
// and the runtime source descriptor already share. Restating it here rather
// than inventing a new one is deliberate: a second way to say "who may publish"
// is a second thing that can disagree.
type PublisherPolicy struct {
	CertificateIssuer   string
	CertificateIdentity string
}

func (policy PublisherPolicy) Validate() error {
	for name, pattern := range map[string]string{
		"certificate issuer":   policy.CertificateIssuer,
		"certificate identity": policy.CertificateIdentity,
	} {
		if strings.TrimSpace(pattern) == "" {
			return fmt.Errorf("publisher policy requires a %s pattern", name)
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("publisher policy %s pattern does not compile: %w", name, err)
		}
		// An unanchored pattern is the classic way a signer policy becomes
		// decoration: `github.com/example/conduit` matches
		// `github.com/attacker/github.com/example/conduit`. Requiring anchors
		// makes the mistake unrepresentable rather than reviewable.
		if !strings.HasPrefix(pattern, "^") || !strings.HasSuffix(compiled.String(), "$") {
			return fmt.Errorf("publisher policy %s pattern must be anchored with ^ and $", name)
		}
	}
	return nil
}

// Attestation is what a verifier actually observed on the signature. It is the
// verifier's report, not its verdict: deciding whether it satisfies the policy
// is this package's job, so no adapter can be lenient on Conduit's behalf.
type Attestation struct {
	// Signed says whether any trusted-root signature was found at all.
	Signed bool
	// Issuer and Identity are the values on the certificate, verbatim.
	Issuer   string
	Identity string
	// Digest is the coordinate the verifier says it checked. It is compared
	// against the pin, so a verifier that checked something else is caught
	// rather than believed.
	Digest string
}

// RefusalReason is why an artifact was not admitted. They are named so a log
// line can be counted and alerted on rather than parsed.
type RefusalReason string

const (
	ReasonUnsigned       RefusalReason = "UNSIGNED"
	ReasonUntrustedIssue RefusalReason = "UNTRUSTED_ISSUER"
	ReasonUntrustedIdent RefusalReason = "UNTRUSTED_IDENTITY"
	ReasonDigestMismatch RefusalReason = "VERIFIER_CHECKED_A_DIFFERENT_DIGEST"
	ReasonMutableSource  RefusalReason = "MUTABLE_SOURCE_NOT_PERMITTED"
)

// Refusal is a finding, not an exception. The caller decides what to do with
// it; this package decides what is true about it.
type Refusal struct {
	Reason RefusalReason
	Detail string
}

func (refusal Refusal) Error() string { return string(refusal.Reason) + ": " + refusal.Detail }

// Admission is bytes that may be converged on, together with the evidence that
// let them through. It is deliberately impossible to construct outside Admit.
type Admission struct {
	pin       Pin
	policy    PublisherPolicy
	identity  string
	issuer    string
	fromKind  SelectorKind
	fromLabel string
}

// Pin is the exact coordinate that was admitted. Everything downstream converges
// on this and never on the selector that produced it.
func (admission Admission) Pin() Pin { return admission.pin }

// Policy is the publisher policy this admission satisfied.
func (admission Admission) Policy() PublisherPolicy { return admission.policy }

// Publisher is the certificate identity that actually signed the bytes, which
// is what an audit wants rather than the pattern it matched.
func (admission Admission) Publisher() (issuer, identity string) {
	return admission.issuer, admission.identity
}

// Selector describes what was asked for, so a record of this admission can say
// "channel X meant this digest" rather than only "this digest".
func (admission Admission) Selector() (SelectorKind, string) {
	return admission.fromKind, admission.fromLabel
}

// Admit is the whole decision.
//
// It takes what was asked for, the exact bytes a resolver said that means, and
// what a verifier observed about them, and either returns something that may be
// converged on or says precisely why not. Every refusal is returned, not just
// the first: an artifact signed by the wrong identity at the wrong digest has
// two things wrong with it, and reporting one would invite fixing one.
func Admit(selector Selector, pin Pin, attestation Attestation, policy PublisherPolicy) (Admission, []Refusal) {
	if err := selector.Validate(); err != nil {
		return Admission{}, []Refusal{{Reason: ReasonMutableSource, Detail: err.Error()}}
	}
	if err := pin.Validate(); err != nil {
		return Admission{}, []Refusal{{Reason: ReasonDigestMismatch, Detail: err.Error()}}
	}
	if err := policy.Validate(); err != nil {
		return Admission{}, []Refusal{{Reason: ReasonUnsigned, Detail: "unusable publisher policy: " + err.Error()}}
	}

	var refusals []Refusal
	refuse := func(reason RefusalReason, detail string) {
		refusals = append(refusals, Refusal{Reason: reason, Detail: detail})
	}

	// A digest selector must resolve to itself. A resolver that returned
	// different bytes for an exact coordinate is the one failure that would
	// make every other guarantee here meaningless.
	if selector.Kind == ByDigest && selector.Digest != pin.Digest {
		refuse(ReasonDigestMismatch, fmt.Sprintf(
			"%s was selected by exact digest but resolved to %s", selector, pin.Digest))
	}
	if selector.Repository != pin.Repository {
		refuse(ReasonDigestMismatch, fmt.Sprintf(
			"%s resolved to a different repository, %s", selector, pin.Repository))
	}

	if !attestation.Signed {
		refuse(ReasonUnsigned, fmt.Sprintf("%s carries no signature from a trusted root", pin))
	}
	// Checked before the pattern comparisons: an attestation about other bytes
	// tells us nothing about these, so matching its issuer would be worse than
	// not looking.
	if attestation.Digest != pin.Digest {
		refuse(ReasonDigestMismatch, fmt.Sprintf(
			"the verifier reported on %s while %s was being admitted", attestation.Digest, pin))
	}
	if attestation.Signed && attestation.Digest == pin.Digest {
		if !regexp.MustCompile(policy.CertificateIssuer).MatchString(attestation.Issuer) {
			refuse(ReasonUntrustedIssue, fmt.Sprintf(
				"issuer %q does not satisfy %s", attestation.Issuer, policy.CertificateIssuer))
		}
		if !regexp.MustCompile(policy.CertificateIdentity).MatchString(attestation.Identity) {
			refuse(ReasonUntrustedIdent, fmt.Sprintf(
				"identity %q does not satisfy %s", attestation.Identity, policy.CertificateIdentity))
		}
	}

	if len(refusals) > 0 {
		sort.Slice(refusals, func(left, right int) bool { return refusals[left].Reason < refusals[right].Reason })
		return Admission{}, refusals
	}
	label := selector.Digest
	if selector.Kind == ByChannel {
		label = selector.Channel
	}
	return Admission{
		pin: pin, policy: policy,
		issuer: attestation.Issuer, identity: attestation.Identity,
		fromKind: selector.Kind, fromLabel: label,
	}, nil
}

// Resolution is the record that a channel meant particular bytes at a moment.
//
// It exists because a channel is the only mutable thing Gitless GitOps
// tolerates, and an unrecorded move is indistinguishable from a compromise
// afterwards. A digest selector produces one too, so the log answers "what did
// this environment deploy?" without the reader having to know which kind of
// selector was used.
type Resolution struct {
	Environment string
	Selector    Selector
	Pin         Pin
	Publisher   string
	// Superseded is the digest this selector previously meant, empty the first
	// time. It is what makes a channel move visible as a change rather than as
	// another line.
	Superseded string
}

func (resolution Resolution) Validate() error {
	if strings.TrimSpace(resolution.Environment) == "" {
		return errors.New("a resolution belongs to an environment")
	}
	if err := resolution.Selector.Validate(); err != nil {
		return err
	}
	if err := resolution.Pin.Validate(); err != nil {
		return err
	}
	if resolution.Superseded != "" && !digestPattern.MatchString(resolution.Superseded) {
		return errors.New("a superseded coordinate must be an exact lowercase sha256 digest")
	}
	if resolution.Superseded == resolution.Pin.Digest {
		return errors.New("a resolution that supersedes itself is not a change")
	}
	return nil
}

// Moved says whether this resolution changed what the environment deploys. It
// is the question a notification wants, and keeping it here stops two callers
// answering it differently.
func (resolution Resolution) Moved() bool {
	return resolution.Superseded != "" && resolution.Superseded != resolution.Pin.Digest
}
