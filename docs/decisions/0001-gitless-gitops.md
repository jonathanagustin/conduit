---
status: accepted
date: 2026-08-13
decision-makers:
  - repository owner
consulted: []
informed: []
---

# Use signed OCI artifacts as the production deployment source

## Context and problem statement

Production clusters need a reconstructible deployment source without direct
Git access. The design must preserve manual publication, private distribution,
exact artifact identity, least-privilege access, tenant governance, and recovery
under disk pressure.

## Decision drivers

- Production clusters must not hold Git credentials.
- A release must be selected and audited by digest.
- Publication must remain manual because hosted Actions time is controlled.
- Conduit must retain Product, Plan, approval, lease, and reporting authority.
- Private-registry storage must support reconstructing disposable local caches.

## Considered options

- Let Flux pull deployment state directly from Git.
- Publish signed OCI sources and let Flux verify and reconcile them.
- Let Flux ResourceSets become the tenant desired-state and approval system.

## Decision outcome

Chosen option: **publish signed OCI sources and let Flux verify and reconcile
them**. Git remains the authoring system. A manually dispatched workflow signs
the registry-reported digest, production Flux sources pin that digest, and
clusters receive only bounded registry-read authority.

Flux reconciles the Conduit runtime and cluster-owner prerequisites. Conduit's
native reconciler remains the semantic owner of tenant delivery. ResourceSets
may be evaluated as a bootstrap adapter, but they are not Conduit's desired
state, policy, or approval store.

This follows ports and adapters. Application services depend on narrow artifact
resolution, publisher verification, content restoration, release execution,
and observation ports. OCI/ORAS, Cosign, caches, Helm, and Flux manifests are
adapters. Flux and registry API types do not enter Conduit domain packages.

### Consequences

- Good: cluster compromise does not expose a Git credential or grant source
  modification rights.
- Good: the same reviewed bytes can be restored, mirrored, and audited by
  digest.
- Good: Kubernetes convergence remains delegated to a mature controller while
  Conduit keeps its domain authority.
- Bad: private clusters need a read-only registry credential or supported
  workload identity and a separate one-time Flux bootstrap path.
- Bad: promotion requires an explicit release operation; moving a convenient
  tag cannot silently deploy different bytes.
- Unknown: the complete clean-cluster bootstrap and wrong-signer rejection path
  has not yet passed an executable acceptance test.

### Confirmation

`scripts/verify-flux-runtime.rb` checks digest pinning, private pull-secret
references, signer policy, and bounded Flux identity. `scripts/verify-trusted-
supply-chain.rb` checks manual publication's signing and release descriptor.
The final confirmation is backlog item B7: a clean-cluster acceptance test with
private OCI pull, signer rejection, exact reconciliation, and no Git resources
or credentials.

## Pros and cons of the options

### Flux pulls Git

- Good, because the workflow is familiar and simple.
- Bad, because every production cluster depends on the Git service and carries
  a Git credential.
- Bad, because repository revision and deployable content identity are easier
  to conflate.

### Signed OCI deployment source

- Good, because registries provide content addressing, private read access,
  mirroring, and a natural signature boundary.
- Good, because cluster access can be pull-only and disconnected from source
  review permissions.
- Bad, because publication and promotion need explicit tooling and receipts.

### Flux owns tenant delivery semantics

- Good, because Flux can template and reconcile Kubernetes resources directly.
- Bad, because this duplicates or bypasses Conduit Plans, approvals, leases,
  rollback intent, and attributed reports.

## More information

- Supersedes: none
- Superseded by: none
- Evidence snapshot/revision: Conduit branch `codex/bootstrap-zenith-license`
- Pareto/value assessment: signed OCI adds registry operations but reduces
  credential blast radius, runtime Git dependency, and cache-recovery risk
