---
title: Operate a Hub
description: Identity, health, approvals, audit verification, backup and recovery procedures for a Conduit deployment.
---

Keep a deployment record outside the source repository: immutable version and
artifact digests, database and backup ownership, signing-key identifiers,
trusted identities, enabled components and the operators allowed to approve
changes. Use the [readiness record](/conduit/readiness/) to separate implemented
controls from unexercised capabilities and pending work.

## Identity and access

Production operators authenticate using the Hub's configured OIDC bearer or
certificate identity mechanism. Agents use mTLS or workload identity scoped to
an Environment. An operator credential does not become Agent authority.
Insecure identity headers are explicit development-only opt-ins and are refused
when combined with secure production identity.

Interactive human sign-in is still tracked in
[#818](https://github.com/jonathanagustin/conduit/issues/818). Do not assume an
identity-provider redirect establishes a Hub session, and do not substitute
development headers in a production deployment. Personal token issuance and
credential lifecycle must be evaluated against the actual merged version.

Resource grants determine what a caller may read or change. Index routes filter
rows according to their registered decision; a list's existence is not a grant
to every object it can name. The generated
[route register](/conduit/reference/route-coverage/) states each route's access
class. A proposal, an approval, a leased execution and an observed result are
separate records. A model cannot approve itself.

## Health and monitoring

Use `/health/live` for process liveness and `/health/ready` for readiness.
Neither proves that a tenant installation converged. Inspect Plan status and
reported installation coordinates for that conclusion.

The metrics listener is opt-in and depends on the configured durable services.
Opening a Service or declaring a monitor does not prove that it can scrape Hub
series. Record a scrape and evaluate the deployment's alert expressions. Request
metrics, tracing and notification exhaustion coverage are bounded by
[#839](https://github.com/jonathanagustin/conduit/issues/839).

Notifications need both preferences and a configured transport. A stored inbox
entry is distinct from transport acceptance, external receipt and a human
acknowledgement. Audit the enabled event types and emitters rather than treating
an event vocabulary as proof that every lifecycle transition emits it.

## Audit and recovery

The PostgreSQL audit ledger is append-only and digest chained. Independently
retain the checkpoint; a checkpoint kept only beside the database cannot detect
a coherent rewrite by the same database administrator. Use the procedures in
[audit integrity](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/authorization-audit-integrity.md)
and [backup and restore](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/backup-and-restore.md).

Before an upgrade, preserve database backups and the separately protected
signing/trust material, review pending migrations, and exercise recovery against
the actual deployment. `platform-delivery verify-schema` reports bounded schema
differences; it is not an automatic migration rollback. The migration ledger is
forward-only. Restoring a database does not restore a missing signing Secret.

The merged baseline includes verified JSONL export, offline archive checking and
an opt-in scheduled checkpoint verifier. With a read-only database identity and
an independently retained checkpoint, for example:

```bash
platform-delivery verify-authorization-audit \
  --checkpoint-file=/trusted/audit-checkpoint.json \
  --export-file=/archives/audit-new.jsonl > candidate-checkpoint.json
platform-delivery verify-authorization-audit \
  --archive-file=/archives/audit-new.jsonl \
  --checkpoint-file=/trusted/audit-checkpoint.json > verified-checkpoint.json
cmp candidate-checkpoint.json verified-checkpoint.json
```

The export destination must be new. Preserve successful matching receipts under
independent custody before extending the trusted prefix. The chart's optional
checkpoint job does not archive data or retain a new checkpoint for the
operator. Live-ledger pruning requires an accepted archive and expiry-receipt
design; deleting a prefix or resetting digests is not a retention procedure.

## Incident response

Preserve the failing Plan, its approval state, execution identifiers, exact
reported coordinates and relevant audit records before retrying. A timeout or
lost write response can have an unknown outcome: read the authoritative state
before submitting another mutation. Distinguish a configured worker from a live
one, and a queued notification from an exhausted delivery.

Repository CI status is separate from deployment health. Current hosted runner
allocation is blocked by
[#858](https://github.com/jonathanagustin/conduit/issues/858); the authorized
[local act process](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/local-act.md)
retains exact-head evidence without fabricating a hosted PASS or authorizing
publication.
