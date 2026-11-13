# Conduit documentation

This directory documents Conduit as it exists and the contracts it is moving
toward. It is not a copy of the former Platform documentation set.

Living documentation is organized by responsibility:

- `architecture/` records Conduit's current boundaries, intended dependency
  direction, and explicitly named gaps.
- `decisions/` records accepted architectural choices and their confirmation
  gates.
- `apollo/` describes the bounded Apollo integration context and its pinned
  public compatibility profile.
- `operations/` contains the orchestration doctrine, assurance and harness
  requirements, tenant-fidelity, verification, and release guidance.
- `security/` states the trusted-technology and authority boundaries.
- `ui/` contains current interface contracts.

Evidence is different from documentation. Pinned upstream contracts, fixtures,
receipts, sanitized observations, security reports, and generated admission
records are retained under the relevant `evidence/` trees or beside the
validator that consumes them. Their presence proves only the claim named by the
record. It does not make an old design narrative authoritative and does not
establish private Apollo behavior.

When a living document and an immutable evidence record disagree, record the
conflict as unknown or a bounded gap. Do not rewrite evidence to match an
implementation claim.
