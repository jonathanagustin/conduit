---
status: accepted
date: 2026-08-14
decision-makers:
  - repository owner
consulted: []
informed: []
---

# Use PostgreSQL for transactional multi-replica Hub persistence

## Context and problem statement

The filesystem repository serializes only inside one process and the retained
PVC admits one writer. Conduit needs independent Hub replicas to agree on
approval, lease, idempotency, audit, and crash visibility without making domain
packages depend on a storage engine.

## Decision drivers

- Exactly one replica may win a Plan lease or reviewer transition.
- An authorization event must commit with the approval it permits.
- A process loss before commit must expose none of the attempted mutation.
- Credentials remain deployment-owned and are read from a mounted file.
- The existing Repository contract must remain executable during DDD extraction.
- Large immutable artifact bytes must not inflate every metadata transaction.

## Considered options

- PostgreSQL with a transitional versioned metadata snapshot and separate blob table.
- SQLite or an embedded key/value database on a shared volume.
- Cross-process locks around the existing filesystem repository.
- A fully normalized context-by-context PostgreSQL rewrite before cutover.

## Decision outcome

Chosen option: **PostgreSQL with a transitional versioned metadata snapshot and
separate blob table**, because the database supplies a portable transaction and
row-lock boundary across replicas while generated adapters preserve the current
domain contract. The production chart consumes PostgreSQL immediately; file
mode remains explicitly single-replica.

### Consequences

- Good: two independent repository instances produce one approval and one lease.
- Good: approval state, synchronized Plan, application evidence, and audit can
  commit together or roll back together.
- Good: artifact bytes remain content-addressed and outside the metadata row.
- Bad: all metadata writes serialize through one row and each operation decodes
  the snapshot, limiting throughput as state grows.
- Bad: PostgreSQL becomes a production dependency that the Environment owner
  must operate and back up.
- Unknown: node and database failover behavior remains unproved; the chart must
  not make a Rubix availability claim until that acceptance test exists.

### Confirmation

`scripts/test-postgres-repository.sh` runs the race detector against two
independent adapters and a digest-pinned disposable PostgreSQL service. It
checks concurrent approval and leasing, idempotency, abort rollback, and audit
failure rollback. `scripts/verify-helm-runtime.rb` checks two replicas,
zero-unavailable rolling updates, the disruption budget, and the read-only DSN
Secret mount. B4, H14, A7, and P10 retain the current status and method evidence.

## Pros and cons of the options

### PostgreSQL snapshot adapter

- Good, because it supplies database transactions now without leaking storage
  types through the legacy Repository interface.
- Bad, because one metadata row is a correctness-first bottleneck.

### Embedded database on shared storage

- Good, because it reduces external services.
- Bad, because cross-node locking and write-ahead-log behavior depend on the
  storage driver and still require shared persistent storage.

### Filesystem locks

- Good, because the change appears small.
- Bad, because lock expiry, process death, and multi-file decision/audit writes
  cannot provide the required database transaction semantics portably.

### Fully normalized rewrite

- Good, because context tables can scale independently.
- Bad, because rewriting all repository semantics before cutover creates a much
  larger simultaneous behavior migration with weaker like-for-like evidence.

## More information

- Supersedes: none
- Superseded by: none
- Evidence snapshot/revision: branch commit `2ce8b82` and its local gate output
- Pareto/value assessment: PostgreSQL snapshot is the smallest option that
  satisfies multi-replica correctness; normalization remains the throughput
  evolution path rather than a cutover prerequisite.
