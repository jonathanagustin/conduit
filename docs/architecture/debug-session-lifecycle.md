# Native debug-request lifecycle

Conduit implements #303's metadata phase through `terminal request` and
`/v1/debug-session-requests`. Requests are non-executing records. They grant no
credential, Pod admission, terminal attachment or delivery authority. Profile is
requested metadata; an image, registry or broker preflight has not been verified.
ADR 0015 records the H30 reversal and the pinned public-source review is in
`reference-platform/reference/debug-access.md`. Interactive broker execution,
reattachment, command recording, NODE/JIT access and console terminals remain
outstanding. The public 116-page CLI implementation profile is unchanged.

## Identity and current authority

Original actor and effective owner come from the Hub's existing authenticated
operator carrier, including its delegation checks. Opaque UTF-8 subjects up to
256 bytes are preserved exactly, including plus-addressed email and issuer IDs
containing a pipe. Controls and surrounding whitespace are rejected. Identity
subjects never become filenames or resource paths; resource-coordinate grammar
is separate.

The native `environment:debug-requester` role conveys
`debug-session-request:create` and `debug-session-request:read`.
`environment:debug-request-manager` also conveys
`debug-session-request:manage`. No Apollo terminal-role alias is introduced by
this metadata phase. Ordinary editor/operator roles convey no new permission.

Authority must cover **both** `environment/ENV` and
`environment/ENV/namespace/NAMESPACE`. A grant on the Environment alone does not
reach the namespace coordinate; an intentional `global/environment` grant can
cover both. Creation additionally requires that namespace to be declared in the
Environment's desired catalog. This establishes declared intent, not actual
cluster existence or a completed deployment. Historical reads/cleanup retain
current permission checks when a declared namespace is later removed.

Owner-visible reads and cleanup require current native read permission.
Management of another owner's records requires native manage permission at both
scopes. Knowing a request ID grants no access. Draft/HTTP inputs cannot provide
an owner, actor or management flag. Scope decisions repeat immediately before
journal appends; an identity change during an operation fails closed.

Identity/grant decisions and the journal are separate boundaries. Checking
current authority before an append is not a transaction with concurrent grant
administration. A stored request is never lasting authority. Every subsequent
operation, especially future broker admission and attachment, must reauthorize.

## Immutable lifecycle and audit facts

A request binds its ID, owner, original actor, Environment, namespace, explicit
profile reference, creation and fixed expiry. The native event vocabulary is
REQUESTED, EXPIRED, REVOKED and DELETED; there is no execution-ready state or
permit. The four-hour maximum comes from the public source; event names,
identifier grammar, schemas and request authority are registered Conduit-native
choices, marked `CONDUIT_NATIVE`.

Creation uses a caller-retained ID. Retrying the same owner/target/lifetime
returns the existing record without refreshing expiry or original actor. A
conflicting target/lifetime fails. A foreign key cannot be adopted. CLI-generated
IDs use 128 bits of randomness; IDs are idempotency identifiers, not bearer
credentials. The CLI includes the original ID in mutation errors so an
ambiguous acknowledgement can be resolved without blindly minting a new key.

Expiry is exclusive. Reads/listing reconcile a past-deadline REQUESTED record
into a persisted EXPIRED event before reporting that state. Expiry audit is
lazy: an unqueried record can retain its last recorded state past the deadline;
the immutable expiry is still authoritative. Future broker enforcement must
terminate sessions independently of this metadata reconciliation. EXPIRED
identifies automatic clock expiry; its system actor label is not a human
identity assertion.

Revocation/deletion cannot extend lifetime or alter owner/target. Revoke/delete
retries are idempotent. Deleted requests stay available in authorized immutable
history and cannot be revived by replay. A complete history has at most three
events: create, expire/revoke, delete. Gaps, duplicates, reordering and immutable-
binding changes fail validation. One event's validation alone proves no valid
transition, identity or authority.

Each event is its transition and audit fact together. A successful response
follows the journal's persisted acknowledgement. A failed acknowledgement after
commit can be ambiguous; callers retain the original ID and inspect history.
Permission-decision audit and journal persistence are not one cross-context
transaction. Neither record claims a Pod or command executed.

## Persistence and recovery

Runtime startup follows `REPOSITORY_KIND`: a private file journal beneath
`DATA_DIRECTORY/debug-session-journal`, or the existing mounted PostgreSQL DSN.
The memory implementation is a test reference only. A direct Hub composition
with a nil journal mounts no metadata routes.

File storage writes a canonical 0600 event file beneath a required private
directory, fsyncs it, atomically hard-links the exact next revision, then fsyncs
the directory before acknowledging. Unique revision names reject competing
writers across independent Store instances. Interrupted temporary files are
ignored. Failure after the canonical link is an ambiguous commit, not proof of
rollback. This is not a transaction with other Hub stores or a power-loss test.

PostgreSQL owns the independent `debug-session-journal` migration component.
A per-session transaction-scoped advisory lock surrounds complete-history and
expected-revision validation and append. Acknowledgement follows commit with
local `synchronous_commit=on`. The database operator still owns PostgreSQL/WAL
and storage durability settings. The real dump/restore drill includes the
journal's migration ledger and exact immutable event/owner/actor/expiry history,
checked before a repository constructor could repair missing schema.

The private codec is bounded to 8192 bytes and rejects duplicate or case-aliased
keys, unknown fields, trailing documents, invalid native state and external
ID/revision mismatches. Times normalize to UTC. Storage administrators remain
trusted; this is not signed protection against coherent privileged rewrites.

## Native HTTP and CLI

Routes are explicitly registered in the Hub authorization matrix:

- `POST /v1/debug-session-requests`: create/idempotently retrieve a metadata
  request from `sessionId`, `target` and optional whole-second `lifetimeSeconds`.
- `GET /v1/debug-session-requests?environment=ENV&namespace=NS`: owner-visible
  page; optional `after` and `limit` (1..100, default 50).
- `GET /v1/debug-session-requests/ID`: current metadata, reconciling expiry.
- `GET /v1/debug-session-requests/ID/history`: immutable authorized audit history,
  including a deleted request.
- `POST /v1/debug-session-requests/ID/revoke` and
  `DELETE /v1/debug-session-requests/ID`: owner cleanup or explicit management.

Responses are non-cacheable. JSON creation/POST content types, exact bounded
body keys, query selectors and duplicate handling are checked. Internal failure
messages do not expose database credentials or paths. Pages filter exact scope,
owner/management permission and deletion state. A continuation cursor comes
only from a returned visible ID. A 10000-record scan ceiling fails unavailable
rather than fabricating an empty/complete page; large catalogs may need indexed
queries. Paging is not a cross-record snapshot transaction.

`terminal request create` requires Environment, namespace and profile reference;
`--lifetime` defaults to four hours and accepts positive whole seconds up to
that bound. `--request-id` retains an idempotency key. `list`, `get`, `history`,
`revoke` and `delete` expose metadata only. JSON/YAML and explicit scope checks
run again after profile defaults load, before mutation. Unsupported Space
selection fails explicitly. The client validates response IDs, target, lifetime,
state, native tier, history and page/cursor consistency.

## Behavioral evidence and remaining work

Tests cover exact expiry, immutable bindings and histories; current dual-scope
permissions, ownership, manager access, revocation and delegation; malformed and
ambiguous inputs; persistence failure before acknowledgement; concurrent expiry;
file reopen/process-writer exit and independent writers; two PostgreSQL pools;
startup, real dump/restore; real CLI/Hub/file restart; retained retries; and
invalid CLI/profile settings before mutation. Existing delivery Plans remain
unchanged. Mutation checks constrain the selected security and persistence
branches; they do not certify a live terminal.

The actual Spoke broker, admitted image profiles, scoped Kubernetes execution,
independent credential/Pod expiry and cleanup, attachment/reattachment, command
recording and console latency/UI still need implementation and their own tests.
NODE/JIT elevation remains #304's separate governed boundary. #303 stays open.
