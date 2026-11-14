# Reviewed installation override boundary

Issue #305 reverses the historical refusal of recall/secret-requirement
override flags while retaining explicit review. The existing evidence is the
scoped release-note/CLI record in
[`ROSETTA-RELEASE-NOTES.md`](../reference-platform/ROSETTA-RELEASE-NOTES.md):
`v0.518.0` names module install/update recall overrides, and `v0.519.0` names
secret-requirement overrides. Those records do not establish a private override
API, review protocol, receipt format or enforcement implementation.

## Module metadata integration

As of 2026-11-14, the maintainer chose to retain the existing metadata-only
Module installation flow and record reviewed secret intent. The Hub composes
native Module proposal/apply routes and the CLI install override flags. This
adds no Module-to-Plan execution and does not change Entity Plan or Agent gates.

The current systems have distinct seams:

- Entity Plans enforce typed Environment `SecretRequirement` assignments and
  keys while validating and expanding an installation.
- Module installations currently store opaque secret declarations as metadata;
  Module recall selection is enforced by `resolveReleaseForChannel`.
- A stored override assertion must not be described as bypassing a Module
  secret gate that is not present, nor translated into fabricated secret values.

The integration must make that distinction explicit rather than weakening both
paths through a global boolean. Modules still need exact locked release/state
binding; Entity execution still needs last-safe-point review revalidation.

## Native review binding

A proposal names one exact Entity or Module INSTALL/UPDATE operation. Its target
contains Environment, installation, resource, version, baseline revision,
release digest, configuration digest and intended-state digest. New installs
have baseline revision zero; updates require a positive revision. No `latest`
or channel-selection authority is carried by the binding: adapters must resolve
an immutable target before constructing it.

Server-owned snapshots contain relevant authority/policy/grant digest and up to
64 current blocker fingerprints. Only recalls and secret requirements can be
selected by the two flags, and a selected flag must have a currently waivable
blocker. Other constraints cannot become waivers. Blocker IDs and hashes contain
no secret material; adapters must supply sanitized, current facts rather than
accepting snapshots from a transport caller.

The SHA-256 proposal identity binds target, flags, blocker snapshot, original
actor/effective author, rationale and review window. Blocker order is
canonicalized and caller-owned slices are copied. Rationale is nonempty,
bounded and normalized at its outer whitespace. The bounded JSON codec rejects
unknown fields, trailing documents, invalid identities and altered bindings.
The complete encoded document is limited to 64 KiB, including JSON escaping.

Review lifetime is explicitly bounded to one hour. Approval must be current,
requirements satisfied, free of a do-not-merge hold and independently reviewed:
neither the original nor effective reviewer may coincide with either author
identity. The authority adapter owns actual reviewer eligibility, including any
service-account policy; matching strings are not authentication or grants.

Revalidation compares the exact target and authority digest and checks each
remaining blocker against its reviewed identity, fingerprint and waivability.
A new/changed blocker, changed source revision/configuration/artifact, changed
authority, expired review, pending/cancelled review or self-review produces no
permit. Resolved blockers lose their waivers. Unrelated constraints still block
admission even if they were visible when the proposal was reviewed.

The resulting permit has unexported fields and no transport authority. It
allows only the exact current target/blocker pair, lasts at most 30 seconds,
cannot outlive the review and is invalid before its issuance time. An empty
digest or unknown blocker never matches an absent map entry.

## Authority port and remaining integration obligations

The application takes only target selectors, flag intent, rationale and review
lifetime. `ResolveProposal` must authenticate/authorize the caller and obtain
current facts; `CreateReview` must durably impose independent review. A returned
auto-approved acknowledgment is rejected rather than treated as authorization.
Pending and explicitly declined/error review outcomes may be returned; none is
execution authority.

`WithApproved` must authorize the current caller and hold the repository's
serialization boundary from current review/target/grant reads through audit and
the guarded effect. The application revalidates those facts within its callback,
rejects a missing/replayed callback and propagates an effect failure even when
an adapter erroneously ignores the callback error. This does not make a broken
adapter transactional: adapters must prove their actual durability, rollback,
concurrency and audit behavior before composition.

## Review and apply protocol

`module install NAME:VERSION --environment ENV --ignore-recalls
--ignore-secret-requirements --rationale "CAB-305"` creates a pending Change
Request. It does not install metadata immediately. JSON and YAML output expose
`valueTier: CONDUIT_NATIVE` and `secretRequirementsEffect: INTENT_ONLY`.

An independent reviewer uses `change-request approve REQUEST --rationale ...`.
The reviewer needs the explicit `module-installation:override-approve` operation
(for example the native `environment:module-override-reviewer` role). Ordinary
auto-approve policy does not satisfy the mandatory requirement. Both original
and delegated effective identities are retained; self-review fails admission.
Old Hub review records lacking the native identity proof fail closed.
Configure policies, teams and grants before proposing: authority changes
invalidate the sealed proposal and require a fresh request.

An Environment operator then runs `module override apply REQUEST`. The Hub
rechecks authorization, approval, expiry, immutable release/configuration,
installation baseline and current blockers under the repository serialization
boundary. Recall exceptions cover only the exact observed recalled release.
Secret intent never authorizes a recall or creates a secret waiver. Declarations
are retained unchanged. Successful repeated apply reads the stored historical
receipt without changing metadata again.

`module installation update INSTALLATION_ID --environment ENV --version VERSION
--ignore-recalls --rationale ...` proposes a native installed-metadata update;
`--dry-run` previews without writing a request or installation. Omitted version
resolves the tracked channel before review. The existing `module update`
command remains definition publication. Update retains installation variables,
channel and configuration reconciliation behavior and requires a newer release.

Native HTTP: `POST /v1/module-override-proposals` accepts `{draft, dryRun}`;
`POST /v1/module-override-proposals/{requestID}/apply` requires an empty body.
Caller identity is bound by the existing operator authentication carrier.
Unknown fields, extra documents, query parameters and oversized payloads fail.

Memory and PostgreSQL transactions roll back audit/effect failures. The file
adapter serializes visibility on the repository instance, but is not a
multi-file crash transaction: an interrupted or failed write can leave partial
state without a successful acknowledgment. Inspect the request execution and
installation before recovery; never treat a missing acknowledgment as success.
PostgreSQL is the transactional production path. Durable admission audit is
written before the metadata effect; completion audit and execution receipt are
part of the same transaction on transactional repositories.

## Verification

Domain tests cover immutable identity and encoding, expiry boundaries,
delegated self-review, target/authority/blocker drift, non-waivable constraints,
per-flag scope, resolved blockers and opaque short-lived permits. Application
tests use a controlled authority port to prove no effect for missing/stale/
unauthorized facts, cancellation, malformed acknowledgments, callback omission,
callback replay and ignored failures. Runtime tests additionally exercise memory, file restart and PostgreSQL adapters,
mandatory review, stale authority/release/baseline rejection, audit rollback and
CLI-to-Hub admission. PostgreSQL cases require the local CI database fixture.
