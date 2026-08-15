---
status: accepted
date: 2026-08-15
decision-makers:
  - repository owner
consulted: []
informed: []
---

# Own Gitless GitOps as a bounded context with two convergence adapters

## Context and problem statement

Conduit already does the central thing the Flux D2 reference architecture calls
Gitless GitOps: desired state is exact, signed bytes in a registry, Git is the
authoring and review system, and no production cluster holds a Git credential.
ADR 0001 decided it and `scripts/verify-flux-runtime.rb` enforces it.

What Conduit did not have was anywhere for the idea to live. The rules that
decide which bytes may be deployed and on whose authority were spread across a
Ruby gate reading YAML, a release workflow, a runtime source descriptor, and a
manifest example. Nothing executed them together, and nothing could answer the
question in one place.

That matters more than it looks, because Conduit converges two different ways.
ADR 0001 delegates the runtime to Flux. ADR 0003 keeps tenant delivery on
Conduit's own reconciler so that Flux does not acquire tenant intent, approval
or Plan semantics. Two convergence paths with the admission rules written down
separately is two admission policies waiting to disagree.

## Decision drivers

- Which bytes may be deployed, and on whose authority, must have exactly one
  answer that is executed rather than reviewed.
- The choice between converging through Flux and converging through Conduit
  must be a deployment decision, never a security one.
- Continuous reconciliation must be safe to run on a schedule: an unchanged
  source must cost a check and nothing else.
- A deployment must be attributable afterwards, including when the selector
  that produced it was mutable.
- Domain code must stay free of registries, signatures, clusters and Flux types,
  per ADR 0001's ports-and-adapters commitment.

## Considered options

- Leave admission in the Ruby gate and the release workflow.
- Put admission behind a port so each convergence path supplies its own.
- Put admission in the domain and give both convergence paths one port.
- Adopt the D2 reference architecture wholesale, including its mutable channel
  tags and Flux Operator/ResourceSet APIs.

## Decision outcome

Chosen option: **put admission in the domain and give both convergence paths one
port**.

`internal/gitless` holds the context. `domain` owns what may be deployed;
`ports` declares resolve, verify, converge and record; `application` fixes the
order they happen in; and two adapters realise convergence —
`adapters/fluxconverge` renders the manifests Flux reads, and
`adapters/conduitconverge` fetches the artifact and hands it to Conduit's own
desired state.

Admission is deliberately **not** a port. If it were, the two adapters could
answer "may these bytes be deployed" differently, and a security property of
the system would depend on which topology was in use. As built, neither can:
both receive an `Admission` value that only `domain.Admit` constructs.

Four rules move into the domain:

- **A selector is not a source.** A channel is a name somebody can move; a
  digest is bytes nobody can change. What converges is always the digest, and
  the resolution is recorded *before* the deployment, so a broken log stops
  delivery rather than producing a deployment nobody can attribute.
- **A signer policy must be anchored.** An unanchored pattern lets
  `github.com/example/conduit` match
  `github.com/attacker/github.com/example/conduit`. `Validate` refuses it.
- **A verifier reports; it does not decide.** Adapters return what they observed
  on the certificate and the domain judges it, so no adapter can be lenient on
  Conduit's behalf. "Could not check" stays distinct from "checked, and it is
  unsigned", and both refuse.
- **Every reason is returned, not the first.** An artifact signed by the wrong
  identity at the wrong digest has two things wrong with it, and reporting one
  invites fixing one.

D2's mutable channel tags are **not** adopted. D2 has staging track `latest` and
production track `latest-stable`; ADR 0001 records as a deliberate consequence
that "moving a convenient tag cannot silently deploy different bytes", and the
gate enforces it. A channel may be *selected*, but it is resolved to a digest,
recorded, and only the digest converges — so continuous delivery works without
a moved name changing what a cluster is already running. The Flux adapter emits
`spec.ref.digest` and never a tag or semver range.

Flux Operator and the `ResourceSet` APIs are not adopted here either. ADR 0001
permits evaluating ResourceSets as a bootstrap adapter; if that happens it is a
third adapter behind the same `Converger` port, and this decision is what makes
that cheap.

### Consequences

- Good: the two convergence paths cannot diverge on what they admit, and a test
  drives both through the same six cases to prove it.
- Good: the rules are executable and mutation-tested rather than reviewed;
  eleven planted mutations were all killed, including an unsigned artifact
  admitted, an attestation over other bytes accepted, an unanchored policy
  tolerated, a Flux manifest carrying a mutable tag, and a manifest dropping
  impersonation.
- Good: a compromised or broken mirror is caught. The Conduit adapter re-hashes
  what the registry served, because admission proves a publisher signed a
  coordinate and not that the registry then delivered it.
- Bad: a resolution log outage blocks delivery. This is chosen: delivery nobody
  can audit is the more expensive failure.
- Bad: the Ruby gate and the Go domain now both express digest pinning and
  signer policy for the committed example. The duplication is bounded and the
  Go adapter's tests assert the same properties, but it is duplication.
- Unknown: nothing is wired into the Hub yet. The resolver, verifier and
  resolution log are ports with no production adapters, so no runtime behaviour
  changes from this decision.

## Confirmation

`go test ./internal/gitless/...` covers the domain rules, the ordering the
application fixes, and both adapters. `equivalence_test.go` is the confirmation
that matters: it builds both realisations against the same doubles and requires
identical admission for a good release, unsigned bytes, a foreign publisher, a
foreign issuer, a branch build against a release policy, and an attestation over
different bytes.

`scripts/verify-domain-boundaries.mjs` continues to reject infrastructure
imports in domain packages.

Not confirmed on a cluster: `kind`, `kubectl`, `flux` and `cosign` are absent
from the development sandbox, so the rendered manifests are checked structurally
rather than applied. `scripts/test-gitless-clean-cluster.sh` remains the
executable cluster proof for ADR 0001 and is unchanged by this decision.

## More information

- Supersedes: nothing. It gives ADR 0001's decision a home and ADR 0003's
  boundary an executable statement.
- Superseded by: none
- Relationship to Flux D2: Conduit adopts D2's core — Gitless GitOps, OCI
  artifacts, Cosign keyless identity, bounded reconciler impersonation — and
  exceeds it on artifact selection by refusing mutable tags. Conduit does not
  implement D2's `d2-apps` layer; Conduit *is* that layer, with Plans and
  approvals where D2 has HelmReleases.
- Evidence snapshot/revision: branch commit `a1c0d10`
- Pareto/value assessment: about 1,000 lines, no new dependency, no runtime
  behaviour change, in exchange for the admission rules having one executable
  home and the two convergence paths being provably equivalent where it counts.
