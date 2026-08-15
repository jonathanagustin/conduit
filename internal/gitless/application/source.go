// Package application holds the Gitless GitOps ports together.
//
// The order of operations is the design, and it is short enough to state: ask
// the registry what the selector means, ask a verifier who signed those exact
// bytes, let the domain decide whether that is admissible here, record what the
// selector meant, and only then converge.
//
// Two of those steps are easy to get subtly wrong, so they are pinned here
// rather than left to a caller.
//
// Recording happens before converging. A deployment nobody can attribute to a
// resolution is exactly the state Gitless GitOps is supposed to make
// impossible, so a log that cannot accept the record stops the deployment. The
// cost is that a broken log blocks delivery; the alternative is delivery nobody
// can audit, and that is the more expensive failure.
//
// Refusals never fall through to convergence. There is no "warn and continue"
// path in this package, because a warning is a thing a pipeline learns to
// ignore.
package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jonathanagustin/conduit/internal/gitless/domain"
	"github.com/jonathanagustin/conduit/internal/gitless/ports"
)

// Source admits and converges Gitless GitOps sources for environments.
type Source struct {
	resolver  ports.SourceResolver
	verifier  ports.PublisherVerifier
	policies  ports.PolicySource
	log       ports.ResolutionLog
	converger ports.Converger
}

// New refuses to build a Source that is missing any of its collaborators.
//
// A nil verifier is the one worth naming: a Source without one would resolve
// and converge perfectly well, and would have quietly dropped the only step
// that distinguishes Gitless GitOps from downloading something off the internet.
func New(
	resolver ports.SourceResolver,
	verifier ports.PublisherVerifier,
	policies ports.PolicySource,
	log ports.ResolutionLog,
	converger ports.Converger,
) (*Source, error) {
	missing := []string{}
	if resolver == nil {
		missing = append(missing, "source resolver")
	}
	if verifier == nil {
		missing = append(missing, "publisher verifier")
	}
	if policies == nil {
		missing = append(missing, "policy source")
	}
	if log == nil {
		missing = append(missing, "resolution log")
	}
	if converger == nil {
		missing = append(missing, "converger")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("Gitless GitOps requires %s", strings.Join(missing, ", "))
	}
	return &Source{resolver: resolver, verifier: verifier, policies: policies, log: log, converger: converger}, nil
}

// RefusedError carries every reason a source was not admitted.
type RefusedError struct {
	Selector domain.Selector
	Refusals []domain.Refusal
}

func (refused *RefusedError) Error() string {
	reasons := make([]string, 0, len(refused.Refusals))
	for _, refusal := range refused.Refusals {
		reasons = append(reasons, refusal.Error())
	}
	return fmt.Sprintf("refused %s: %s", refused.Selector, strings.Join(reasons, "; "))
}

// Reasons lists the refusal reasons alone, for a caller that wants to count or
// alert on them rather than read them.
func (refused *RefusedError) Reasons() []domain.RefusalReason {
	reasons := make([]domain.RefusalReason, 0, len(refused.Refusals))
	for _, refusal := range refused.Refusals {
		reasons = append(reasons, refusal.Reason)
	}
	return reasons
}

// Outcome is what one reconcile did, so a caller can tell a no-op from a move
// without comparing digests itself.
type Outcome struct {
	Admission domain.Admission

	// Deployed is true when this reconcile actually converged. It is separate
	// from Moved because a first deployment moves nothing and is emphatically
	// not a no-op -- a caller with only Moved cannot tell "already there" from
	// "just deployed for the first time", which is the difference between
	// silence and a release notification.
	Deployed bool

	// Moved is true when this reconcile changed the digest the environment was
	// already converging on. A channel that has not moved produces false here
	// and no second deployment, which is what makes continuous reconciliation
	// cheap enough to run on a short interval.
	Moved bool

	// Superseded is the digest that was previously deployed, empty the first
	// time this environment converged on anything.
	Superseded string
}

// Reconcile brings one environment up to date with one selector.
//
// It is safe to call on a schedule and expected to be: restating desired state
// continuously is what makes this GitOps rather than a deployment script, and
// an unchanged channel costs a resolve, a verify and nothing else.
func (source *Source) Reconcile(ctx context.Context, environment string, selector domain.Selector) (Outcome, error) {
	if strings.TrimSpace(environment) == "" {
		return Outcome{}, errors.New("Gitless GitOps reconciles a named environment")
	}
	if err := selector.Validate(); err != nil {
		return Outcome{}, fmt.Errorf("selector for %s: %w", environment, err)
	}

	policy, err := source.policies.PolicyFor(ctx, environment)
	if err != nil {
		return Outcome{}, fmt.Errorf("publisher policy for %s: %w", environment, err)
	}

	pin, err := source.resolver.Resolve(ctx, selector)
	if err != nil {
		return Outcome{}, fmt.Errorf("resolve %s: %w", selector, err)
	}

	attestation, err := source.verifier.Verify(ctx, pin)
	if err != nil {
		// A verifier that could not answer is not a verifier that said yes.
		// Failing here rather than admitting an empty attestation keeps
		// "unavailable" and "unsigned" as different facts, while refusing both.
		return Outcome{}, fmt.Errorf("verify %s: %w", pin, err)
	}

	admission, refusals := domain.Admit(selector, pin, attestation, policy)
	if len(refusals) > 0 {
		return Outcome{}, &RefusedError{Selector: selector, Refusals: refusals}
	}

	deployed, err := source.converger.Deployed(ctx, environment)
	if err != nil {
		return Outcome{}, fmt.Errorf("read the digest %s currently converges on: %w", environment, err)
	}
	if deployed == pin.Digest {
		// Already there. No record and no convergence: a resolution log that
		// gained a line every reconcile interval would bury the moves that
		// matter under the ones that did not happen.
		return Outcome{Admission: admission, Deployed: false, Moved: false, Superseded: deployed}, nil
	}

	_, identity := admission.Publisher()
	resolution := domain.Resolution{
		Environment: environment, Selector: selector, Pin: pin,
		Publisher: identity, Superseded: deployed,
	}
	if err := source.log.Record(ctx, resolution); err != nil {
		return Outcome{}, fmt.Errorf("record what %s resolved to before deploying it: %w", selector, err)
	}

	if err := source.converger.Converge(ctx, environment, admission); err != nil {
		return Outcome{}, fmt.Errorf("converge %s on %s: %w", environment, pin, err)
	}
	return Outcome{Admission: admission, Deployed: true, Moved: deployed != "", Superseded: deployed}, nil
}
