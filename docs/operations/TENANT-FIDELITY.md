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

### Four Apollo properties are omitted rather than answered with a constant, and the fifth cannot be

**Source class:** Public contract (the pinned OpenAPI's `required` sets).
**Divergence:** Conduit omits four optional properties it has no data for, and
serves one required property as a constant.

Issues #57 and #112 reported that `MAPPED-or-REFUSED` governs *which* Apollo
coordinates answer and nothing governed *what they answer with*. On the REST
surface four properties carried values no Conduit data and no publisher produced:
`Environment.environmentType` was the constant `"production"` for every
Environment — including one this repository's own test names `dev-use2` — and a
Namespace's `owners`, `editors` and `viewers` were three empty arrays, which
assert that no group holds those permissions rather than that nobody knows.
The response-shape gate passed all four, because `"production"` is a legal enum
member and `[]` is a legal array; shape is all a schema can check.

**What Conduit does now.** All four are absent from the response. The pinned
snapshot lists `rid`, `name` and `status` as `Environment`'s required properties
and `id` as `Namespace`'s only one, so every omitted property is optional and an
absent key is contract-legal. Under [ADR 0007](../decisions/0007-registered-invention-over-refusal.md)
this is not a refusal: the record forbids declining to answer a coordinate for
want of data, and says nothing requiring an invented value for an optional field
the contract already permits to be absent. Omission is the third option, and it
is the only one available here — REST carries no provenance tier, so there is no
way to serve a value and disclose it as Conduit's.

**Scope, stated because the fifth is the interesting one.** `Environment.status`
is **not** omitted, and could not be: it is in the snapshot's required set, so
omitting it to be honest about provenance would break the contract instead. It
stays the constant `"managed"`, and is declared in
[`../apollo/public-api.md`](../apollo/public-api.md) as a Conduit-supplied
constant that discriminates nothing. Being unable to fix it honestly is not a
licence to stop saying so.

**What would close it.** For `environmentType`, an environment-type concept in
Conduit's Environments domain, or a capture establishing what Apollo means by the
value for a Hub that is not Apollo. For the Namespace groups, a Namespace
permission model in Governance. For `status`, a managed/unmanaged distinction
Conduit could actually report.

**Detected by:** `TestApolloOmitsCoordinatesConduitHoldsNoAnswerFor` in
`internal/controlplane`, which reads the `required` sets out of the pinned
snapshot rather than restating them, decodes into `map[string]any` so an absent
key is distinguishable from an empty one, and fails if a future snapshot makes an
omitted property required or relaxes `status`.

### `Installation.version` is the version Conduit intends, not the version an Agent reported

**Source class:** Public contract (the snapshot's own description of the
property). **Divergence:** the data behind it.

The pinned snapshot describes `Installation.version` as *"The reported version
from the Apollo Agent responsible for managing this Installation."* Conduit
populates it from the reviewed **desired** state. The data is real Conduit data;
the contract sentence describes different data, so a client watching an upgrade
reads desired as observed, and the two disagree for exactly as long as an
in-flight or failed upgrade lasts (#57; #112 finding 4).

**Why Conduit does not simply serve the reported one.** Reported versions exist —
a Spoke agent finishing a Plan submits them and they land in the Environments
Report — but the join is unavailable, not merely unwired.
`environments.CompatibilityProjection` carries a `ProjectedEntity` with an Entity
*name*; a `ReportedEntity` is keyed on an installation *id*, and a Plan step
carries both because they are not the same thing. Joining on Product instead
would collapse two installations of one Product in one Environment. Serving a
join that looks right and is not is the failure
`TestHealthJoinsOnReportingVocabularyNotMintedIdentifiers` already exists to
prevent, so the honest move is to say which version this is rather than to
approximate the other.

**Scope, stated because it is narrow.** The value itself is unchanged and
correct as desired state. What changed is that the GraphQL coverage table stopped
claiming otherwise: `ApolloEntity.version` cited *"as reported by the Apollo
Agent"* verbatim from the contract, and now names the desired version explicitly.

**What would close it.** The compatibility projection carrying reporting's
installation id, at which point the reported version can be served under the
property whose description already promises it — and this row deleted rather than
softened.

**Detected by:** `TestInstallationVersionIsTheDesiredVersionNotAnAgentReport` in
`internal/apollo/application`, which sets a desired version of `2.0.0` against a
Report saying `1.0.0` and fails if the reported one ever reaches the projection
while this row still stands.

### `pageSize` is accepted and not honoured, and `nextPageToken` is always null

**Source class:** Public contract (the recovered GraphQL interface declares
`pageSize` on every collection field and `nextPageToken` on every page type).
**Divergence:** neither argument nor token does anything.

`pageSize` is a **required** argument on every collection field in
`api/graphql/apollo.interface.graphql` and no resolver in `internal/apollographql`
reads it: `environments(pageSize: 1)` against three returns three (#112 finding
5). `nextPageToken` answers null unconditionally, which is consistent — a page
that is the whole collection has no next one — but the pair together let a client
believe it is paging when it is not.

**Why Conduit declares this rather than implementing it.** Slicing the records is
trivial. The token is not. Emitting one means minting a cursor grammar, and no
capture establishes what an Apollo page token looks like, so the client would
receive a Conduit-invented string behind a coordinate recovered from a Hub —
precisely the defect #57 and #112 exist to report, reintroduced in the act of
closing them. `valueTier` cannot rescue it either: a disclosed invented cursor is
still one a client cannot use, and it would then have to survive round-tripping
back into a `pageSize`-and-token contract nothing establishes the semantics of.
The remaining option, truncating without a token, is the worst: a short page with
a null token tells the client the collection ended.

**Scope, stated because it is narrow.** GraphQL only. The public REST surface
publishes no pagination at all — the pinned snapshot contains neither `pageSize`
nor `nextPageToken` — so there is nothing to diverge from there.

**What would close it.** A capture or published statement establishing Apollo's
page-token grammar, which would make both the token and the argument
implementable against evidence rather than against a guess.

**Detected by:** `TestPageSizeIsNotHonouredAndTheDeclarationSaysSo` in
`internal/apollographql/application`, which asks for one of three Environments,
requires three, and fails if paging is implemented without this row being
revisited. The coverage table states the same thing where a reader of the served
interface will find it.

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

### Any blocked promotion creates promotion suppressions, not only a failed canary

**Source class:** Public contract (`reference/promotion.md`: *"A failed canary
creates an Entity-scoped promotion suppression"*). **Divergence:** the trigger is
wider than the published sentence.

`reconcilePromotionSuppressions` (`internal/controlplane/suppression.go`) creates
an Entity-scoped promotion suppression for every UNHEALTHY piece of evidence in
**any** BLOCKED promotion. A timed stage counts. So does a stage blocked by a
label requirement or by the overall evaluation timeout, as long as some Entity
also happened to report unhealthy -- the published rule names one trigger, and
Conduit acts on a class of them.

**Why Conduit keeps the wider trigger.** The suppression is not the promotion's
verdict, it is a hold on the Entity the evidence says is unhealthy, and that
Entity is unhealthy whichever clause stopped the promotion. Narrowing to a failed
canary would mean a timed stage watching an Entity fall over, blocking on it, and
then leasing the next Plan straight at it. The hold is bounded and self-clearing:
it is Entity-scoped, it carries the Release that produced it, and a later passing
evaluation for that Release removes it.

**Scope, stated because it is narrow.** Two limits are enforced rather than
declared. A canary stage suppresses only the Entities its `EntityFilters`
nominated -- an Entity excluded from the decision cannot be suppressed by it
(issue #66) -- and nothing at all is created from a PASSED promotion. Removal
stays deliberately wider than creation: it is keyed on Environment plus Release
and reads its Environments from the whole submitted evidence, so a record written
before that filter existed is still cleared by a later passing run. Narrowing
creation without leaving removal wide would have made those records permanent.

**What would close it.** A published statement of which blocked stages Apollo
suppresses on, or a decision that a timed stage should not hold an unhealthy
Entity.

**Detected by:** `TestPromotionFailureSuppressesCanaryAndRecoveryRemovesSuppression`,
`TestBlockedCanaryDoesNotSuppressAnEntityItsFiltersExcluded` and
`TestPassingCanaryStillClearsASuppressionItsFiltersWouldNotHaveCreated` in
`internal/controlplane`, which pin the timed-stage suppression, the excluded
canary Entity, and the removal reach respectively.

### A canary tier may be selected by Environment label, which no source establishes

**Source class:** Conduit extension. **Extension:** `PromotionEntityFilter` gains
`environmentLabels`, a label requirement alongside the two name lists.

Nothing in the pinned sources establishes that Apollo's canary Entity filters
select on anything but names, so this is a reviewed local decision and not a
reproduction. It exists because a tier written as a list of Environment names
cannot follow the fleet: the Environment stood up this morning is in the fleet
and not in the tier, nothing says so, and the stage goes on deciding a fleet-wide
promotion on the Environments somebody remembered to list (issue #169). A label
requirement makes tier membership a property of the Environment, so labelling a
new Environment extends the canary set with no policy edit.

**Scope, and the two limits that make it safe.** The Hub resolves the labels from
the Environment's most recently reviewed generation and stamps them onto the
evidence, *overwriting* whatever the submitter sent -- health evidence carries no
attestation, so a caller free to declare its own Environment labels could write
itself into any canary tier. And the stamped labels are recorded on the
`PromotionRecord`, not resolved again at replay: unlike an Environment-dependent
blocker this selection is **not** additive -- narrowing a canary set drops
unhealthy evidence and can turn BLOCKED into PASSED -- so it cannot be taken on
trust the way `ENVIRONMENT_CANNOT_VOUCH` is. Freezing the labels the decision
used keeps a record re-derivable from itself, and keeps a later relabelling from
rewriting a decision already made. Filtering by *Entity* label remains
unsupported for the unchanged reason: evidence carries no Entity labels.

**What would close it.** A published statement of what Apollo's canary Entity
filters may select on, which would either establish this shape or displace it.

**Detected by:** `TestCanarySetSelectedByEnvironmentLabelFollowsTheFleet` in
`internal/catalog/domain` and `TestCanaryTierFollowsTheFleetByEnvironmentLabel`
in `internal/controlplane`, which pin the selection rules and, respectively, the
overwrite, the replay, and the tier extending without a policy edit.

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
review-context field on the Plan proposal, which lives in `internal/controlplane`.

> **Correction, 2026-08-19.** This paragraph used to give the `internal/controlplane`
> line ceiling as the reason, and that reason is gone: the ceiling and its gate
> were removed, so `docs/operations/controlplane-ceiling.json` no longer exists.
> The divergence stands on the half below, which was always the stronger half
> and never depended on the ceiling. Recorded rather than silently reworded,
> because a reader who remembers the old reasoning should be able to see that it
> was retired rather than quietly dropped.

The repository's own doctrine already settled the other half
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

### Multipass federation refuses a username this deployment already means something by

Multipass is Foundry's IAM service, not Apollo's, and the claim class for
everything below is stated per row rather than inherited from this document's
Apollo scope.

**Reproduced faithfully.** The recovered `principals/User` carries `id`,
`username` and an `attributes` map and nothing else; given name, family name,
email, realm, organization and active are `@JsonIgnore` accessors over that map.
`internal/identity/domain/multipass.go` models exactly that and derives the rest,
including the recovered default that an ABSENT `multipass:active` means active.
Class: **Public contract**, source `com.palantir.multipass:multipass-api:3.2636.0`
by way of the deterministic TypeScript projection generated from the recovered
`multipass-legacy-api.yml`. The contract file itself is not present in the
read-only Foundry clone consulted here; the generated projection and its
`PROVENANCE.json` are.

**Where Conduit diverges.** `FederatedClaims` REFUSES to mint claims when the
federated username lands on a name this deployment reserves -- its
`BootstrapAdministrators`, plus the `local-developer` development fallback --
and refuses a user Multipass reports as deactivated. Multipass has no such rule;
this is a **Conduit extension** and exists because
`governance/application.ResourceDecision.Administrator` matches the bootstrap
list against `Principal.Username` with plain string equality and grants
administrator authority before any grant exists. Without the refusal, creating
an account in somebody else's directory would be enough to administer Conduit.

**Unknown, and recorded as Unknown.** `multipass:active` is compared to the
exact string `"true"`, matching the reference client. Whether the server's own
accessor parses the string case-insensitively is not established by any source
this repository holds. The comparison here is the stricter of the two, which is
the safe direction for a value whose only use is to refuse federation.

**Inert.** Nothing composes any of it. No route, handler or authorization path
reaches a `MultipassDirectory`, and Governance continues to make every
authorization decision from grants.

**Detected by:** `TestMultipassUserAttributeDerivation` and
`TestFederatedClaims` in `internal/identity/domain`, and the `httptest`-backed
route assertions in `internal/identity/adapters/multipasshttp`. No test here has
ever run against a live Multipass.

## Tenant runtime data

The immutable frontend bundle receives tenant-specific branding only through
the versioned, typed, server-resolved bootstrap described in
[`../ui/contracts/runtime-bootstrap.md`](../ui/contracts/runtime-bootstrap.md).
Both server and browser reject unknown keys, credentials, executable content,
external API origins, capability fields, and security-policy weakening. An
invalid configured document fails closed rather than inventing tenant state.
