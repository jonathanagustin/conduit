# Agent credential verification — 2026-11-17

Issue #847. This record replaces the runbook's old statement that PostgreSQL
deny-list tests had not run. It does not qualify a UDS installation.

## Source and execution

Merged source: `d7aac458de0ac50b951a555dab677831da265510`.
Tests ran at `e1b06dca73bab018f6a57e2bd3dca7aea94033f7`, whose only changes
from that source are the five console budget files in PR #979; no identity,
Hub, control-plane, Go module or test source differs. Linux, Go 1.27.4,
PostgreSQL 18.6, race instrumentation enabled.

The database was a disposable task-owned container, image
`postgres@sha256:a1d02e4bd40c94d3bf2bdd3678c137388e76d9efcd23c285e9429d336a834b44`,
with temporary storage and a loopback-only port. Tests create isolated databases
and remove them. Existing planning/Space containers were untouched.

## Results

All three PostgreSQL top-level tests passed, without skips:

- `TestPostgresStoreConformsToTheAgentCredentialPort`: agent/environment cutoff
  monotonicity and concurrent denial, exact scoped reads/removal, bounded lists,
  latest certificate and expiry visibility.
- `TestPostgresRevocationsConvergeBetweenReplicasImmediately`: independent
  pools observe revocation without a cache-expiry interval.
- `TestPostgresAgentCredentialRemovalAndAuditShareTheVisibilityBoundary`:
  committed removal and cancellation after delete; denial and audit visibility
  remain consistent.

Command (connection supplied only through the test environment):

```sh
go test -race -count=1 -v ./internal/identity/adapters/revocationpostgres ./internal/hub/composition -run '^TestPostgres(StoreConformsToTheAgentCredentialPort|RevocationsConvergeBetweenReplicasImmediately|AgentCredentialRemovalAndAuditShareTheVisibilityBoundary)$'
```

Four control-plane rotation tests also passed with race instrumentation:

- `TestAgentCARotationWithOverlapAdmitsBothRootsAndRetiringTheOldRootRefusesItsCertificates`
- `TestAnAgentCAFileThatHoldsNoCertificateIsRefusedAtStart`
- `TestBundleSigningKeyRotationWithOverlapAndRetirement`
- `TestATrustListEntryFiledUnderTheWrongIdentifierIsRefusedAtStart`

These drive actual TLS connections/trust checks through overlap and retirement,
and malformed-trust refusal. They are process-level rehearsals.

## Remaining qualification

No cert-manager manifests, UDS enrollment, deployed restart/rotation or
Prometheus alert was exercised. The runbooks retain these limitations. This
record does not close #847 or establish a production CA custody policy.
