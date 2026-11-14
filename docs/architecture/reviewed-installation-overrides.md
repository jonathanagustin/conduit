# Reviewed installation override boundary

Issue #305 reverses the historical refusal of recall/secret-requirement
override flags while retaining explicit review. The existing evidence is the
scoped release-note/CLI record in
[`ROSETTA-RELEASE-NOTES.md`](../reference-platform/ROSETTA-RELEASE-NOTES.md):
`v0.518.0` names module install/update recall overrides, and `v0.519.0` names
secret-requirement overrides. Those records do not establish a private override
API, review protocol, receipt format or enforcement implementation.

## Foundation, not an enabled bypass

`internal/installoverride` defines native intent, immutable review binding and
an application authority port. It is not yet composed into a Hub route, CLI,
Module service, Plan builder or Agent. Existing recall and secret enforcement
remain in place. No new flag joins the reviewed compatibility profile in this
foundation change, and issue #305 remains open for runtime integration.

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

`WithApproved` must authorize the current caller and hold the repository's
serialization boundary from current review/target/grant reads through audit and
the guarded effect. The application revalidates those facts within its callback,
rejects a missing/replayed callback and propagates an effect failure even when
an adapter erroneously ignores the callback error. This does not make a broken
adapter transactional: adapters must prove their actual durability, rollback,
concurrency and audit behavior before composition.

Runtime integration must supply those adapters, mandatory approval/audit,
strict native transport, applicable CLI flags and last-safe-point checks.
Missing/foreign/expired approval must never fall through to ordinary execution.
New recall/secret constraints must not inherit an old blanket exception, and
unsupported Module secret semantics must remain explicit. These are required
integration work, not claims established by the pure kernel tests.

## Verification

Domain tests cover immutable identity and encoding, expiry boundaries,
delegated self-review, target/authority/blocker drift, non-waivable constraints,
per-flag scope, resolved blockers and opaque short-lived permits. Application
tests use a controlled authority port to prove no effect for missing/stale/
unauthorized facts, cancellation, malformed acknowledgments, callback omission,
callback replay and ignored failures. They establish the kernel and port
contract only; they are not production authorization or database evidence.
