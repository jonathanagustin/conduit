# Apollo GraphQL API recovery boundary

Reviewed 2026-08-03 against the official
[Apollo API Explorer documentation](https://www.palantir.com/docs/apollo/apollo-references/apollo-api-documentation/)
and an authorized, read-only capture of the Foundry **Connected Hubs** route.

## Public contract

Palantir documents that certain Apollo APIs are GraphQL and that the API
Explorer (GraphiQL) is exposed directly by an Apollo Hub. Its documentation
panel enumerates the queries and mutations accessible in that particular Hub.
The published examples establish at least these selections:

```graphql
query GetCurrentUser {
  me { id fullName }
}

query GetEnvironments {
  apollo {
    environments(pageSize: 100) {
      environments { id }
    }
  }
}
```

The documentation does not publish a universal Explorer pathname, endpoint,
downloadable SDL, authentication fixture, or complete schema. Consequently the
examples prove only their named fields and cannot support a broader
compatibility claim.

## Authorized discovery result

An authenticated capture of the current Foundry enrollment's
`/workspace/control-panel/connected-hubs` route completed without missing
artifacts at `2026-08-03T04:43:08.453Z`. Raw HTML, bundles, responses, and the
tenant origin remain outside this repository. The integrity receipt is:

| Artifact | Observation |
| --- | --- |
| Capture manifest | SHA-256 `8bef34e33677617ff6a922594a91a1b0b7249be892f6b993faff8d3e37ac99ee`; 231 requests and responses, 191/191 assets captured |
| Accessibility tree | SHA-256 `1e3c7a8f9df6bdcb916880c4360ee6c760116b5d5f502040e9ee6e31dc0ad8b6`; the Connected Hubs extension reports **Excluded from your plan** |
| GraphQL request shapes | Five Control Panel shell/enrollment operations; none is an Apollo Hub operation |
| Network result | No Apollo-, Hub-, connection-, or Marketplace-named service request beyond the Connected Hubs document route |

The plan gate prevents the extension from loading or listing connections. The
capture therefore exposes neither an Apollo Hub origin nor its API Explorer.
It is evidence of a blocked discovery path, not evidence that the enrollment
has no Apollo Hub connection.

## Recovery method once a Hub is authorized

The Factory/Palantir GraphQL recovery workflow is applicable with a strict
scope boundary:

1. Capture the authenticated Hub's Explorer and representative Apollo
   application routes, retaining downloaded bundles only in temporary storage.
2. Statically recover each shipped GraphQL operation together with its own
   transitive fragment dependency closure. Do not globally merge fragments by
   name because separate bundles may ship different same-named definitions.
3. Prefer standard introspection when the Hub permits it. If introspection is
   intentionally thin, use validation-only requests with a guaranteed-invalid
   guard to learn only exact field-output and argument-input types without
   executing the query or mutation.
4. Emit a deterministic **observed** SDL and fail unless it passes GraphQL
   schema validation and every captured operation plus exact closure validates
   against it.
5. Commit only scrubbed operation/type metadata, source hashes, completeness
   counts, and diagnostics. Never commit tenant origins, tokens, cookies,
   authenticated documents, response bodies, or private bundles.

This can close H38 only for the captured GraphQL contract. It cannot prove
unused private fields and does not establish the separate Hub-to-Spoke control
wire in H18. Generated Go clients, adapters, or resolvers must therefore be
labelled against the observed schema version/hash rather than as general
Palantir Apollo compatibility.

## Recovery result (2026-08-15)

An authorized Hub was reached and the method above was run. The result is
committed as `api/graphql/apollo.observed.graphql` with its recovery record and
completeness report under `docs/apollo/observed/`.

What the Hub established:

| Observation | Result |
| --- | --- |
| `query { me { id } }` | resolves |
| `query { apollo { environments(pageSize: 100) { environments { id } } } }` | resolves; the published shape is real |
| `Query.apollo` type name | `ApolloQuery` |
| Introspection | **thin by design** -- `__schema` returns 20 types and its `Query` advertises only `me`. It names no part of the Apollo surface. |
| Recovered | 13 types, 43 fields, 10 argument types, in 119 requests |
| Emitted to SDL | 41 fields. The other 3 are omitted because their return types were not established, and are listed in the completeness report. |

Because introspection is thin, the surface was recovered by validation probing:
a document carrying a guaranteed-invalid field fails validation as a whole, so
the Hub executes none of it while still reporting `FieldUndefined`,
`MissingFieldArgument`, `WrongType` and `SubSelectionRequired` for everything
else in the document. Only the return-type probe executes, selecting
`__typename` alone.

The guard is also a canary, and that is what makes the method fail closed.
"No error for this candidate" is evidence the candidate exists only if the Hub
actually validated the document; a malformed document returns a parse error and
no field errors, which reads exactly like "every candidate exists". The prober
therefore requires the guard's own error in every discovery response and aborts
otherwise. An earlier revision lacked that check, emitted an unbalanced
document, and reported fifteen invented fields on two types before the guard
was added.

## Bounds on this result

- It is one Hub on one date, answering one candidate set. A field nobody asked
  about is absent from the record, not from the Hub.
- Three fields could not be emitted and are recorded rather than guessed:
  `ApolloEnvironment.config` (return type not recovered),
  `ApolloProductDependency.versionRange` (its type was named but never
  described, which the depth bound and a skipped batch both produce), and
  `ApolloProductRelease.recallStatus` (its type has no recovered fields).
- **Cardinality was not established anywhere.** Validation probing cannot see
  through a list type: `[T]` and `T` accept the same sub-selection, so nothing
  in the responses distinguishes them. No recovered field therefore declares a
  list, which means the recovered object graph is only navigable through its
  singular fields -- `environment(rid:)` and `entity(rid:)` reach objects, while
  the page types' collection fields do not. Establishing cardinality needs a
  capture the current method cannot produce.
- No mutation type is reachable: `__schema.mutationType` is null.
- This establishes nothing about the private Hub-to-Agent control wire, which
  remains H9 and BLOCKED.
- Generated clients or adapters must be labelled against `schemaSha256` in the
  completeness report, not as general Palantir Apollo compatibility.

## Relationship to the captured Foundry gateway schema

A prior capture in the migration corpus records a Foundry GraphQL gateway
schema at `/graphql-gateway/api/graphql`: 26,140 lines, 1,884 types, 2,281
interfaces, fully introspectable. That capture contains **no Apollo deployment
domain**. Apollo appears in it four times and never as deployment:

| Where Apollo appears in the captured gateway | What it is |
| --- | --- |
| `ApolloEventReceiverConfiguration`, `apolloEventReceiverConfigurations` | monitoring event receivers |
| `MonitorAction_ApolloEventReceiverNotification` | a monitoring action |
| `MarketplaceApolloPublishingStatus`, `apolloSpaceId` | Marketplace publishing status |
| `ApolloEventReceiverMonitoringViewApplicationConfiguration` | a monitoring-view scalar |

There is no `apollo` root field anywhere in it. This recovery found
`Query.apollo: ApolloQuery` with 13 types and 43 fields of deployment domain.
The two results are in direct conflict and the conflict is recorded here rather
than resolved, because the evidence does not resolve it.

**What the conflict is not.** It is not one schema seen through two levels of
visibility. A filtered introspection can hide fields; it cannot invent them.
Both surfaces expose `Query.me`, and only one exposes `Query.apollo`, so the
surfaces genuinely differ rather than one being a redaction of the other. That
the captured gateway is fully introspectable while this Hub's `__schema`
returns 20 types is a second categorical difference in the same direction.

**Three readings remain, and this evidence cannot separate them.**

1. *Different endpoints.* The captured schema is explicitly from
   `/graphql-gateway/api/graphql`. This recovery record does not state which
   path was probed — see the bound below — so two different GraphQL surfaces on
   one host is consistent with everything recorded.
2. *Different enrollments.* The two may be separate Foundry deployments with
   different federated subgraphs.
3. *The schema changed* between the capture and 2026-08-15.

**A bound this comparison exposed in the recovery record itself.**
`hubFingerprint` is a salted digest of the endpoint's **origin only**. Two
distinct GraphQL surfaces on the same host therefore produce the same
fingerprint, so the committed record cannot establish which surface it
describes. `probe-apollo-schema.mjs` now also records an `endpointFingerprint`
over origin *and* path, but the committed record predates it and carries no
answer. Resolving this needs a fresh capture, not more reasoning about the
existing one.

Nothing here changes what the recovery established about the Hub it reached.
It changes what may be inferred about Apollo's GraphQL surface in general,
which is less than one capture suggests.

### A third observation (2026-08-19), by a different method

The captured `apollo-control-panel` 6.533.15 frontend closure — receipt in
[`../FRONTEND-CAS-CAPTURE.md`](../FRONTEND-CAS-CAPTURE.md) — yields the client's
**own** GraphQL operations, mined statically from the shipped bundles with no
live call: 24 complete operations and 30 fragments, committed as
[`../apollo-control-panel-graphql-operations-6.533.15.json`](../apollo-control-panel-graphql-operations-6.533.15.json).

It is a genuinely independent observation. Different date, different tenant
surface, and a method with no probe in it at all: these are the documents the
tenant *served to a browser*, so nothing here depends on validation probing,
on the guard/canary discipline, or on what a schema chose to introspect.

What it shows:

| Observation | Result |
| --- | --- |
| Root fields used | `me`, `enrollment`, `principal`, `resourceMetadata`, `sessionClassification`, `authenticationProvider`, and the Stargate user-directory reads and mutations |
| `apollo` root field | **absent** |
| Deployment vocabulary anywhere in the extract | **zero** matches |
| Client | `forge-graphql-client/0.0.0`, sending `Palantir-Asset-Track` and a bearer token |

**What it settles.** The 2026-08-03 capture recorded "five Control Panel
shell/enrollment operations; none is an Apollo Hub operation" from one route.
This is the same finding at 24 operations across the whole app closure, so that
result was not an artifact of which route was loaded. A Foundry frontend that
exposes `Query.me` and no `Query.apollo` is now observed twice, by two methods —
matching the captured gateway schema and differing from the Hub reached on
2026-08-15.

**What it does not settle, and this is the point.** It cannot separate the three
readings above, because *it does not establish which endpoint it posts to
either*. The only `/graphql` string in the closure is Apollo Client's library
default in `HttpLink`'s constructor; there is no `graphql-gateway` string in any
of the 30 blobs and no `uri:` literal in the app chunk, so the path is assembled
at runtime from configuration the capture does not contain. It therefore adds a
third surface-shaped observation with the same missing datum that made
`hubFingerprint` insufficient, rather than the endpoint identity that would
resolve the conflict.

So the conflict stands, with one reading now better supported and none
eliminated: whatever serves `Query.apollo` with 13 types of deployment domain,
it is not the surface this tenant's Control Panel talks to. Resolving it still
needs a fresh capture that records origin *and* path.

## The interface Conduit serves (2026-08-15)

The recovered surface is evidence. `api/graphql/apollo.interface.graphql` is the
interface Conduit actually serves, and it is deliberately larger: 41 recovered
coordinates plus 22 that no capture established, because an operator interface
that cannot reach plans, change requests or promotions is not usable for
delivery work.

Mixing the two is the risk the design exists to manage. An interface that blends
recovered truth with invention and does not say which is which cannot be
audited, and the invention silently becomes the claim.

**Every field carries its provenance,** in one of three tiers:

| Tier | What established it |
| --- | --- |
| `OBSERVED` | An authorized Hub answered for this coordinate. 41 fields. |
| `DOCUMENTED` | Apollo names the field or documents the concept — in the pinned OpenAPI, in an `apollo-cli` flag, in published documentation, or on a sibling type in the recovery record. No Hub answered for *this* coordinate. 22 fields. |
| `CONDUIT_NATIVE` | Conduit's own idea, no Apollo claim of any kind. **0 fields.** |

`GET /v1/apollo/graphql/schema` publishes the annotated SDL, and every response
carries an `extensions.provenance` block naming the `DOCUMENTED` and
`CONDUIT_NATIVE` coordinates it answered from. A client never has to take
Conduit's Apollo shape as an Apollo compatibility claim, and never has to read
this repository to find out.

**The bottom tier being empty is a finding, not a design.** The interface was
built assuming 22 fields were Conduit's invention. Reading the documentation
corpus already committed to this repository showed every one of them traces to
an Apollo source — including three whose names were wrong and have been
displaced (below). Nothing about what Conduit serves changed; what changed is
that the repository stopped overstating its own invention by 35 percentage
points. The tier stays in the enum because the next genuine invention needs
somewhere honest to go, and `TestNoServedFieldIsWithoutAnApolloSource` pins the
count so that adding an uncited field fails deliberately rather than drifting.

**Two DOCUMENTED coordinates rest on Beta documentation.**
`ApolloEnvironment.health` and `ApolloEntity.health` cite the Apollo Product
Specification Health page, which upstream marks `[Beta]` and whose callout says
the extension "may not be available on your Apollo Hub". The citation is real
and verified; it is simply weaker than a GA one, and the tier alone does not say
so. The gate reads maturity from the documentation manifest and names those
coordinates on every run, and the harness asserts that it does -- a disclosure
that vanishes looks exactly like one that had nothing to say.

**`DOCUMENTED` is not a weaker `OBSERVED`.** It says Apollo named the thing, and
nothing whatever about whether Conduit holds data behind it or whether a Hub
would answer for it. `Tier.Established()` is the single place that distinction
is made, and only `OBSERVED` returns true from it.

### Values Conduit builds behind coordinates Apollo declares

`tier` says who established that a coordinate **exists**. It has never said where
the **value** came from, and while the two were always the same nobody needed it
to. Three coordinates make them differ, and until
[ADR 0007](../../decisions/0007-registered-invention-over-refusal.md) the
interface had no way to say so — they served under a bare `OBSERVED` annotation
whose disclosure told clients an Apollo Hub had answered with them. It had not.

| Coordinate | Grammar, published by Apollo | What Conduit supplies |
| --- | --- | --- |
| `ApolloEnvironment.rid` | `apollo-openapi ri.apollo.main.environment.123` | the locator |
| `ApolloEntity.packageRid` | `apollo-openapi ri.apollo.main.package.123` | the locator |
| `ApolloEntity.id` | `apollo-openapi aeid:apollo-environment-demo:helm-chart:wordpress` | the value only; the shape is Apollo's |

These now serve as `@provenance(tier: OBSERVED, valueTier: CONDUIT_NATIVE)`, and
the response's provenance extension lists them under `conduitMintedValues` — a
**subset of `observedServed`**, not a fourth bucket, because the coordinate
really was recovered even though the string was not. The practical warning the
disclosure carries is that one of these values will not match anything if a
client round-trips it into a real Apollo API.

`ApolloEntity.id` is the one that changed its story. It was briefly registered as
an invention, on the reading that Apollo's example carried a Space in the second
segment and Conduit substituted an Environment. Apollo's example carries no
Space: the pinned OpenAPI publishes
`aeid:apollo-environment-demo:helm-chart:wordpress` twice, beside `environmentRid`
in the same object. Conduit's construction matches it in segment count, in the
`helm-chart` product-type token, and in the meaning of the second segment. The
disclosure stays — Conduit still composes the string from its own names rather
than receiving it — but there is nothing invented left to register, which is why
an entry in that file need not name an invention.

Note what is *not* invented: the RID grammar. The pinned OpenAPI publishes three
Apollo instantiations of `ri.<service>.main.<type>.<locator>`, and
`ri.autopilot.main.team.123` alongside them shows the service segment naming the
owning service. Only the locator is open — Apollo's examples use the placeholder
`123` — and the one Apollo locator whose format is documented, a Plan's, is a
UUID where Conduit uses a truncated digest for stability without storage.

Both are registered in
[`conduit-inventions.json`](../../evidence/conduit-inventions.json) with the
evidence that would displace them, and
`scripts/verify-minted-values.mjs` fails the build if the schema, the generator
input and the register stop agreeing.

### Displacement by documentation

Where Apollo publishes a name for something Conduit had named itself, the
invented name is **deleted**, not deprecated beside it. Three did not survive
contact with the pinned OpenAPI:

| Conduit invented | Apollo publishes | Citation |
| --- | --- | --- |
| `ApolloEntity.installedVersion` | `version` | `apollo-openapi Installation.version` |
| `ApolloPlan.environment` | `environmentRid` | `apollo-openapi Installation.environmentRid` |
| `ApolloPromotion.product` | `productId` | `apollo-openapi Product.id`, `Installation.productId` |

`TestTheDisplacedNamesAreNoLongerSelectable` proves both halves: the invented
names are unselectable, and the published ones answer. Proving only the refusal
would pass on an interface that had dropped the fields entirely.

The strongest citations point at the recovery record itself. `nextPageToken` is
`DOCUMENTED` rather than invented because the record carries that exact name on
four sibling page types — Apollo's own GraphQL name, established by capture. The
three collection fields (`plans`, `changeRequests`, `promotions`) cite the
record for their *shape* (`patternedOn`) and documentation for their *noun*
(`documentedBy`), because every recovered `XPage` names its collection after the
plural entity it pages. A `patternedOn` citation carrying the field's own name
is rejected: that would be a published name filed under the weaker word.

**Displacement is mechanical, not remembered.** `node
scripts/apollo-graphql/verify-apollo-interface.mjs` fails the build the moment a
capture recovers a coordinate still declared in
`scripts/apollo-graphql/conduit-native.json`, and names it. Finding ground truth
breaks the build until the invented definition is deleted so the recovered one
takes its place. That is the intended cost; the alternative is a repository that
quietly serves guesses it could have replaced. Documentation displaces the same
way a capture does: a field citing a published Apollo name while serving a
different one fails the gate.

Every citation is verified rather than trusted, because a citation nobody checks
is decoration — it reads as evidence and constrains nothing, which is worse than
an honest `CONDUIT_NATIVE`. `documentedBy` must resolve in
`docs/apollo/public-documentation-manifest.tsv`; `apollo-openapi X.y` must name a
property the pinned snapshot declares; `apollo-graphql Type.field` must appear in
the recovery record. `scripts/verify-apollo-interface.test.sh` plants fifteen
violations — including declaring an already-recovered coordinate as native on
both a leaf type and the root, a URL the manifest does not carry, an OpenAPI
property that does not exist, and a field citing Apollo's name while serving its
own — and requires the gate to reject each by name.

That harness stages the manifest and the OpenAPI snapshot into its temporary
tree deliberately. Omitting them made every citation unresolvable, so every case
rejected for that reason instead of its own and the harness passed while testing
nothing.

**Declared is not the same as answerable.** 9 recovered coordinates are
declared and unanswerable here. Those refuse rather than resolving to null: a
null is a value, and answering `ApolloBundle.rid: null` would assert an absence
Conduit has never observed. The refusal says so and sends the reader to a Hub.

The reason is not that Conduit holds none of these resources — it builds, signs
and imports Bundles, and serves Products and Product Releases today. It is that
Conduit holds no *Apollo* identifiers for them and has no Space. A Conduit
bundle is `bnd_<hex>`, which is a different identifier for a different object,
not an Apollo RID under another name.
[`docs/decisions/0005-apollo-coordinates-that-stay-refused.md`](../../decisions/0005-apollo-coordinates-that-stay-refused.md)
records all twelve, splits them by what would actually close each one, and is
pinned against the resolver by
`TestRefusedCoordinatesMatchTheDecisionRecord` — so serving one, or declaring a
thirteenth, fails the build until the record is updated.
`internal/apollographql/domain.Reconcile` publishes that count and fails the
build on the two cases that are defects -- a field Conduit added that nothing
backs, and a resolver claiming a coordinate the schema does not declare.

A cited name does not excuse a missing source. `DOCUMENTED` and
`CONDUIT_NATIVE` reconcile identically: Apollo publishing the name is evidence
about the *name*, never about whether Conduit holds data behind it, and Conduit
chose to declare the field either way.

| Property | Value |
| --- | --- |
| Recovered coordinates declared | 41 |
| — of which served | 32 |
| — of which declared but unanswerable | 9 |
| Coordinates served that no capture established | 22 (35% of the 63 declared; 41% of the 54 served) |
| — of which Apollo names or documents | 22 |
| — of which are Conduit's own invention | **0 (0%)** |
| Coordinates served, total | 54 (32 `OBSERVED` + 22 `DOCUMENTED`) |
| Mutation surface | none. The served SDL declares no mutation root, so validation refuses one; nothing hand-written enforces it. |
| Routes | `POST /v1/apollo/graphql`, `GET /v1/apollo/graphql/schema` |

Two numbers, and they answer different questions. `unrecoveredShare` (35%) is
the fraction no Hub has confirmed; it should fall as captures widen. Its
denominator is what the SDL **declares** — 63 — not what the server answers:
`build-interface-schema.mjs:260` divides by `observedFields + documentedFields +
conduitNativeFields`. Measured against the 54 coordinates actually served it is
41%. The declared figure is the published one because it is the property of the
contract; the served figure is stated here so the two are never silently swapped.
`conduitNativeShare` (0%) is the fraction no Apollo source of any kind supports;
it should stay honest rather than stay zero. Conflating them is how "documented"
quietly becomes "verified", which is the failure this whole interface exists to
prevent.
