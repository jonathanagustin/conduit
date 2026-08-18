# Tenant fidelity

Conduit may reproduce an Apollo behavior only when a named source establishes
that behavior. Similar vocabulary or a plausible interface is not evidence.

## Claim classes

| Class | Required basis | Allowed statement |
| --- | --- | --- |
| Public contract | Pinned Palantir OpenAPI, Conjure, public documentation, published source, or captured public CLI output | The shape and behavior established by that source |
| Tenant observation | Authorized, sanitized receipt with tenant, version, time, route, and state | The behavior observed within that scope |
| Corroboration | An independent source that agrees with a bounded claim | Support for the claim, never a new protocol fact |
| Conduit extension | A reviewed local decision | Conduit behavior identified as an extension or divergence |
| Unknown | Missing, ambiguous, inaccessible, or stale evidence | A backlog item or unavailable state |

Evidence keeps its class. A screen does not establish its backing API. A client
type establishes the source document's type surface, not an unpublished server
implementation. A tenant observation does not establish behavior for another
tenant or version.

## Change record

An Apollo-facing pull request must state:

- the changed behavior or data shape;
- the claim class and immutable source coordinate;
- the scope the source does not establish;
- the test, generated-output comparison, or sanitized fixture that detects drift;
- each deliberate Conduit extension or divergence.

Missing evidence is still classified `Unknown`, and `Unknown` no longer means
stop. Per [ADR 0007](../decisions/0007-registered-invention-over-refusal.md),
build against an educated guess, disclose it in band where the surface can carry
a provenance tier, and register it in
[`../evidence/conduit-inventions.json`](../evidence/conduit-inventions.json)
with the capture that would displace it.

Two obligations come with that. **Exhaust this repository's own sources before
inventing** -- the documentation manifest and the images referenced from it
included. **Displacement means reimplementing against the evidence**, not bending
the guess until it agrees; a shape chosen without constraint will always bend.

## Contract boundary

Pinned OpenAPI or Conjure is the transport authority where it exists. Generated
DTOs, client types, and strict server interfaces should meet a bounded Apollo
adapter. Conduit domain policy stays behind that adapter. Generation must be
deterministic from committed inputs and fail when committed output is stale.

The pinned public OpenAPI now generates the complete 13-operation route set,
transport DTOs, strict server interface, and router. That proves only the named
public transport shape; it is not proof of compatibility with a private
Hub-to-Agent protocol.

## Declared divergences

Behaviours where Conduit keeps Apollo's published vocabulary and does something
different with it. Each is a `Conduit extension or divergence` under the claim
classes above, recorded here because the Change record section requires it.

### A security recall blocks an Entity command that would deploy the Release

**Source class:** Public contract (commands bypass recall eligibility).
**Divergence:** Conduit refuses one narrow case of it.

`commands.go` refuses an INSTALL or ENFORCE_* command whose target Release is
recalled with source `SECURITY`. The published behaviour, and this repository's
own note in `reference/commands.md`, is that commands bypass recall eligibility.

**Why Conduit diverges.** A command is the break-glass path, and the argument
for bypassing recall is that an operator in an incident should not be blocked by
policy. That argument does not reach deploying a Release withdrawn *for a
security reason*: the recall says these bytes are the problem, and the command
would install them. The break-glass paths that matter in an incident stay open
-- uninstall and restart are not refused -- so the operator can always remove or
restart the workload, just not roll forward onto a Release recalled for
security.

**Scope, stated because it is narrow.** Only `RecallSourceSecurity`. A manual
recall does not block a command, which is the published behaviour.

**What would close it.** A decision that break-glass outranks a security recall,
or a published statement that Apollo blocks this too.

**Detected by:** the command tests in `internal/controlplane`, which exercise a
security-recalled target.

### `RECALL_WHEN_UNHEALTHY` blocks the promotion; it does not recall the Release

**Source class:** Public contract (the condition name and the canary stage
shape). **Divergence:** the effect.

A canary condition whose action is `RECALL_WHEN_UNHEALTHY` causes Conduit to
append a promotion blocker. It does not create a `Recall`. Nothing in Conduit
produces a `Recall` from a canary evaluation; the only automatic recall producer
is security-scan derivation (`internal/controlplane/catalog.go`).

The two are different in kind, which is why this is declared rather than treated
as naming. A recall withdraws a Release from selection **everywhere**, including
environments that already hold it and installs that have not happened yet. A
blocker stops **this** promotion.

**Why Conduit blocks.** `PromotionEvaluationRequest.Evidence` is caller-submitted
and carries no attestation. Deriving a fleet-wide withdrawal from it would let
any principal holding `resource:operate` remove any Release from every
environment by asserting that enough canaries are unhealthy. Blocking one
promotion on unverified evidence is recoverable; withdrawing a Release
fleet-wide on it is not. This is the same class of exposure tracked in issue #54
for scan evidence, and it is why the divergence is deliberate rather than
pending.

**What would close it.** Hub-witnessed or attested health evidence. Until then a
real `Recall` here would be a denial-of-service primitive rather than
conformance.

**Detected by:** `TestCanaryRecallConditionBlocksRatherThanRecalling` in
`internal/catalog/domain`, which asserts the blocker message describes blocking
and that the evaluation produces no recall.

### Six apollo-cli flags Conduit does not carry, and one it refuses out loud

**Source class:** Public contract (the flags appear in the pinned apollo-cli
command contract). **Divergence:** Conduit registers a published flag only
where it reaches a decision.

`docs/apollo/cli-command-contract.json` records the flags each published
command accepts, and Conduit registered six it never read: `release-channel
export --latest/-l`, `environment version compare --all`, `product promotion
set --override`, `cve check --rescan`, `product-release verify --dry-run`, and
`release-channel diff --all`. Each parsed, and each was either echoed back into
the output document or discarded, so `--flag` and `--flag=false` produced
identical results.

**What Conduit does now.** `release-channel diff --all` was implemented -- the
default narrows to the fields that differ, and `--all` restores the full
definitions the flag always described. `product promotion set --override` is
registered on `append` only, the action that has something to override.
`environment version compare --all`, `cve check --rescan` and `product-release
verify --dry-run` are gone, because each named the command's only mode: those
commands already return every reported Entity absent a Release Channel filter,
already scan freshly on every call, and already publish nothing. `release-channel
export --latest` stays registered and refuses with its reason, because Conduit
serves no Release Channel definition from the Hub and an operator carrying an
apollo-cli script across should be told that rather than told "unknown flag".

**Scope, stated because it is narrow.** Only the flags above. The pinned
inventory of command *paths* is unchanged, and `contract_test.go` -- which pins
paths, not flags -- still passes untouched.

**What would close it.** A Hub endpoint serving a Release Channel definition
would give `--latest` something to fetch; a cached scan result would give
`--rescan` something to bypass.

**Detected by:** `TestActionSpecificFlagsAreRegisteredOnTheRightSubcommandOnly`,
`TestReleaseChannelExportRefusesLatestRatherThanIgnoringIt` and
`TestReleaseChannelDiffNarrowsToDifferingFieldsUnlessAll` in
`internal/conduitcli`.
### The review-context flags are absent, not accepted and discarded

**Source class:** Public contract (`--compliance-ticket` on
`environment artifact-registry assign|unassign` and
`environment release-channel set`, `--archive-rationale` on
`release-channel update`, all recorded in
[`../apollo/cli-command-contract.json`](../apollo/cli-command-contract.json)).
**Divergence:** Conduit does not offer them at all.

Every one of those commands ends in a Plan proposal, and `POST /v1/plans`
carries a `DesiredState` and nothing else. Conduit accepted the flags anyway --
required them, in fact, and `environment delete` required a Conduit-invented
`--rationale` on top -- and printed that the value was "retained in review
context". Nothing transmitted it (#175). The requirement was worse than an
ignored optional flag: it forced an operator to produce audit metadata under a
printed promise, and manufactured confidence in a record that was never
written. An auditor asking which ticket authorized an Environment deletion got
nothing, with no trace that the answer had been collected and dropped.

**Why Conduit drops them rather than carrying them.** Carrying the value means a
review-context field on the Plan proposal, which lives in `internal/controlplane`
-- the monolith that is exactly on its recorded line ceiling
(`docs/operations/controlplane-ceiling.json`), a ratchet that may only move
down. Raising a migration ceiling to hold a CLI convenience field inverts what
the ratchet is for. The repository's own doctrine already settled the other half
of the choice, at `internal/conduitcli/apollo_parity_commands.go`: *"a rationale
would be accepted and dropped. A flag that silently does nothing is worse than
an absent one."* An honest absent flag beats a false promise.

**Scope, stated because it is narrow.** Only the flags whose value had no
transport. `plan approve --rationale` and `changerequest approve|reject
--rationale` keep theirs, because those calls do carry the rationale to the Hub.

**What would close it.** A review context on the Plan proposal, in a bounded
context of its own rather than in `internal/controlplane`, at which point these
flags can return and mean what they say.

**Detected by:** `TestReviewContextFlagsAreAbsentRatherThanDemandedAndDiscarded`
and `TestNoFlagPromisesRetentionInReviewContext` in `internal/conduitcli`, which
run each command without the flag, reject it as unknown when passed, and fail if
any flag in the command tree advertises retention in review context again.

## Tenant runtime data

The immutable frontend bundle receives tenant-specific branding only through
the versioned, typed, server-resolved bootstrap described in
[`../ui/contracts/runtime-bootstrap.md`](../ui/contracts/runtime-bootstrap.md).
Both server and browser reject unknown keys, credentials, executable content,
external API origins, capability fields, and security-policy weakening. An
invalid configured document fails closed rather than inventing tenant state.
