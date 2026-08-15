// Package ports declares what Gitless GitOps needs in order to admit and
// converge on a source, and nothing more.
//
// The split is the interesting part. Gitless GitOps is four questions, and
// Conduit's two realisations -- delegate to Flux, or converge itself -- answer
// the first three identically and only differ on the fourth:
//
//	which exact bytes does this selector name?   SourceResolver
//	who signed those bytes?                      PublisherVerifier
//	may they be deployed here?                   the domain, not a port
//	who makes the environment match them?        Converger
//
// Putting the admission decision in the domain rather than behind a port is
// what makes two adapters safe. If "may they be deployed" were a port, the Flux
// adapter and the Conduit adapter could answer it differently, and the
// difference would show up as a security property that depends on a deployment
// choice. As it stands they cannot: both are handed an Admission that only
// domain.Admit can construct.
package ports

import (
	"context"

	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

// SourceResolver answers which exact bytes a selector names right now.
//
// It is the only place mutability is allowed to exist, and it is deliberately
// separate from retrieval: resolving is a question about the registry's current
// opinion, and everything after it is about bytes that cannot change.
type SourceResolver interface {
	Resolve(ctx context.Context, selector domain.Selector) (domain.Pin, error)
}

// PublisherVerifier reports what it observed about a signature over exact bytes.
//
// It returns an Attestation rather than a boolean on purpose. A verifier that
// answered "yes" would be deciding Conduit's policy inside an adapter, where it
// could be lenient without anything failing; reporting what it saw leaves the
// judgement in the domain, where it is tested.
type PublisherVerifier interface {
	Verify(ctx context.Context, pin domain.Pin) (domain.Attestation, error)
}

// Converger makes an environment match admitted bytes.
//
// This is the port with two real adapters. One writes Flux manifests pinned to
// the admitted digest and lets Flux converge in-cluster; the other hands the
// admitted coordinate to Conduit's own reconciler, which compiles it into a
// governed Plan. Both receive an Admission, so neither can widen what the other
// would refuse.
type Converger interface {
	// Converge is expected to be idempotent for a given Admission: Gitless
	// GitOps re-states desired state continuously, so being called again with
	// the same admission must not produce a second deployment.
	Converge(ctx context.Context, environment string, admission domain.Admission) error

	// Deployed reports the digest this environment is currently converging on,
	// or empty if the environment has none yet. It is what lets a resolution
	// record say what it superseded.
	Deployed(ctx context.Context, environment string) (string, error)
}

// ResolutionLog records that a selector meant particular bytes.
//
// A channel is the only mutable thing this design tolerates, and an unrecorded
// move is indistinguishable from a compromise after the fact. Recording fails
// the admission rather than being best-effort: deploying without being able to
// say afterwards what was deployed is the outcome the log exists to prevent.
type ResolutionLog interface {
	Record(ctx context.Context, resolution domain.Resolution) error
}

// PolicySource answers which publisher an environment trusts.
//
// It is a port rather than configuration passed in because the answer is
// per-environment and may tighten over time -- production trusting only tagged
// releases while a development environment accepts branch builds is the normal
// case, and it must not require a different code path.
type PolicySource interface {
	PolicyFor(ctx context.Context, environment string) (domain.PublisherPolicy, error)
}
