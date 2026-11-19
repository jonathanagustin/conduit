# Durable package validation evidence

This is a Conduit extension of the registered
`packagevalidation.EvidenceAdmission` contract. It persists authenticated
validation evidence; it grants no approval, publication, application-trial or
deployment authority. The adapter is not exposed through a new HTTP endpoint
and is not yet composed into packaging handoff.

## Trusted transaction boundary

`internal/packagevalidation/adapters/postgres` implements the existing
`ports.Authority`. Its required authorizer receives the same SQL transaction
used for attempt lookup, verification, admission, audit and challenge consumption.
An integration must authenticate the original caller and lock current trusted
owner, target and policy facts through that transaction until commit. It must
not use an independent pool transaction for those decisions, re-enter the Store,
or accept attempt/policy setup from generated output. Run/revision locators must
be globally scoped within the authority database; a deployer must not reuse a
locator for another tenant or Hub.

Owner setup is an internal API, not a producer endpoint. A locator cannot be
replaced, a challenge is unique across attempts, cancellation cannot be undone,
and a consumed attempt cannot be reset or edited. Trusted updates can invalidate
an old signed result by changing its current target, configuration or policy
pins. Governance is mandatory even for historical reads and setup/control calls.

The adapter reuses the existing attestation verifier, exact manifest and policy
pins. A verification callback receives a copy of trusted facts and cannot replace
the stored policy by mutating that argument. Callback results are independently
checked against the signed document, producer, identity and expiry. Admission,
audit and consumed state commit in one transaction under a per-locator lock.
Any SQL failure rolls them all back. There is no automatic retry after a lost
commit acknowledgement: a committed challenge remains consumed after reopening.

The store requires PostgreSQL `fsync=on` and sets transaction-local
`synchronous_commit=on`. This concerns primary commit acknowledgement, not a
claim of replica failover, backup or disaster-recovery qualification. Expiry is
checked before and after commit. An expired acknowledgement is refused even if
the transaction committed; that challenge remains consumed and is not replayed.
Historical receipt retrieval does not make evidence fresh again. Final handoff
must independently revalidate current facts, revocation, freshness and governance.

## Qualification and remaining work

The existing PostgreSQL verification script explicitly runs the adapter tests
against its pinned disposable server. Tests use owned `pgtest.Fresh` databases,
not destructive changes to a shared database. Coverage includes two-pool
contention, close/reopen replay, trigger-injected failure at each write boundary,
changed trusted content, cancellation, unauthorized reads, forged callback output
and expiry before commit or after persistence but before acknowledgement.

A local run without `CONDUIT_TEST_POSTGRES_DSN` explicitly skips these database
tests; that is compilation and non-database validation only. It is not durable
receiver qualification. Real PostgreSQL tests must pass on the exact proposed
tree before merge. No live application check, producer-signing custody, governed
final-handoff integration or isolated application/restore trial is established by
this adapter. Those #639 acceptance items remain open.
