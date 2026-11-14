---
status: superseded
superseded-by: 0007-registered-invention-over-refusal
date: 2026-08-15
decision-makers:
  - repository owner
consulted: []
informed: []
---

# Refuse the twelve Apollo coordinates Conduit cannot answer truthfully

> **Superseded on 2026-08-17 by [ADR 0007](0007-registered-invention-over-refusal.md).**
>
> The owner overruled the preference for refusal: Conduit now implements against
> an educated guess where there is no ground truth, marks it, and reimplements
> when evidence arrives.
>
> **This record's close conditions were never met, and were not met at the time
> of superseding.** Two captures made since say so explicitly, and ADR 0007
> quotes them rather than paraphrasing. Nothing about the supersession should be
> read as this record having been answered — it was overruled.
>
> What holds, and is carried forward: a null is a claim, and serving an invented
> value while presenting it as recovered is not allowed. ADR 0007 addresses the
> second by adding `valueTier` to the provenance directive, so an invented value
> discloses itself in band rather than being refused for want of a way to say so.
>
> The figures below are frozen at 2026-08-15. The live counts are in ADR 0007.

## Context and problem statement

Conduit's Apollo-shaped GraphQL interface declares every coordinate a capture
recovered from an Apollo Hub, so the interface keeps Apollo's shape whether or
not Conduit has data behind each field. Twelve of those coordinates are declared
and unanswerable. They refuse at request time rather than resolving to null,
because a GraphQL null is a value: `ApolloBundle.rid: null` asserts that this
bundle has no resource identifier, which Conduit has never observed.

Several of the twelve look closable. Two have been proposed as work — issue #12
argued the bundle coordinates were servable because the bundle documents
disclaim the wire format rather than the concept. That reading of the documents
is correct and the conclusion still does not follow, which is the kind of thing
worth writing down once rather than rediscovering.

This record exists so that a later reader does not re-open them one at a time,
and so the reasons are separable: two of the three groups below close on a
decision that has not been made, and one closes only on evidence nobody has.

## Decision drivers

- A refusal states something true; a null states something false.
- Declaring a coordinate is Apollo's shape. Answering it is Conduit's claim.
- Displacement runs one way: ground truth displaces invention, never the
  reverse. Serving Conduit's own identifier under an Apollo field name would
  invent a correspondence and then present it as recovered.
- The count must stay honest. It fell 18 → 17 → 12 as real coordinates were
  served, and the remaining twelve should fall the same way or not at all.

## Considered options

- Resolve the unanswerable coordinates to null.
- Serve Conduit's nearest equivalent under the Apollo field name.
- Refuse, and record why each group is refused.

## Decision outcome

Chosen option: **refuse, and record why**. The twelve divide into three groups
with genuinely different fates.

### Group 1 — no Apollo resource identifiers (6)

`ApolloBundle.rid`, `ApolloProduct.rid`, `ApolloProductRelease.rid`,
`ApolloQuery.bundle`, `ApolloQuery.bundles`, `ApolloQuery.productRelease`.

Conduit holds all three of these resources. It builds, signs, imports and
transfers Bundles; it holds Products and Product Releases and serves
`ApolloProduct.productId` and `ApolloProductRelease.mavenCoordinate` today. What
it does not hold is an Apollo RID for any of them. A Conduit bundle is
identified as `bnd_<hex>`, minted by `internal/artifacts/domain`, and that is
not an Apollo RID under a different name — it is a different identifier for a
different object in a different system.

Issue #12's premise was right and its conclusion was not. `SourceFormatNote`
does disclaim the wire format rather than the concept: "Platform-native format
derived from public Apollo semantics; not a Palantir wire-format claim." Conduit
therefore implements the Bundle *concept* freely. But `rid` is not the concept,
it is the identity, and Conduit has never been told what an Apollo Bundle RID
for one of its bundles would be. The two query entry points take an Apollo RID
as their argument and have nothing to resolve it against for the same reason.

The tempting move is the one `ApolloEnvironment.id` looked like. That coordinate
was served from `Environment.Name` — but only because Apollo's own status routes
accept the name as `{environmentId}`, which is documented evidence of a
correspondence. No comparable statement exists for any RID here. Note also that
`ri.apollo.main.bundle` appears in this repository only in Conduit's own GraphQL
fixtures; it is our test data, not evidence, and citing it would be circular.

**Closes on:** a capture, or a published statement, establishing how an Apollo
RID is formed for a resource Conduit holds. Not on effort.

### Group 2 — no Space tenancy boundary (5)

`ApolloBundle.spaceId`, `ApolloEntity.spaceId`, `ApolloEnvironment.spaceId`,
`ApolloProduct.spaceId`, `ApolloProductRelease.spaceId`.

Conduit has no Space, and as of 2026-08-16 it will not have one: issue #17
closed as won't-do, choosing the honest refusal over a single default Space.
There is therefore no value to serve and no honest default -- every resource
belonging to one implicit Space would itself be a tenancy claim.

**Closes on:** nothing currently foreseen. These five are the accepted,
permanent cost of that decision rather than the cost of leaving it open. The
cost was weighed rather than waved past: #17 recorded that Apollo's own CLI
carries space-id arguments (v0.500.0, v0.531.0), which is real evidence that
Apollo treats the Space as load-bearing, and the decision went the other way
anyway. The reason is that the argument for adopting a Space was about
*naming*, not about data -- Conduit would still have had nothing to put in the
field but a synthesized constant, and serving one under an Apollo field name is
the invented correspondence this record's third decision driver forbids. A
single default Space is also a discriminator that discriminates nothing until a
second exists.

**Reopens on:** a real multi-tenant deployment, or a capture establishing what
an Apollo Space id is for a resource Conduit holds. Either would displace this
record rather than amend it.

**Corroborated 2026-08-17; the condition above is NOT met.** A bytecode-exact
projection of `com.palantir.controlpanel:control-panel-api-objects:1.1588.0`
yields Foundry's three Apollo control-panel operations in full, recorded in
[`../evidence/apollo-control-panel-seam.json`](../evidence/apollo-control-panel-seam.json).
`CreateApolloSpaceRequest` carries only an admin principal set, and the platform
mints and returns the `spaceRid`; a caller never states an identifier.

That cuts with this decision rather than against it, and it is worth saying why
it does not reopen anything. The condition is a Space id **for a resource
Conduit holds**. No Conduit resource gains one here. `spaceRid` is a
Foundry-side identifier; `ApolloSpaceId` is an opaque string in the pinned
interop IR, linked to Foundry by Maven group, and nothing establishes the two
are the same. The capture does not even fix a format. Reading this as a met
condition requires softening "what an Apollo Space id is for a resource Conduit
holds" into "how a Space id is formed" -- a weaker condition the evidence does
meet, which is exactly the substitution this note exists to refuse.

The first driver is also unaffected: a seam artifact is not a Hub answering, so
none of these facts could be served under an `OBSERVED` coordinate whatever they
said. What changes is the footing. The refusal was held on the absence of
evidence and is now held on evidence.

Note for readers of older revisions: #16 (scan-before-install) was recorded here
as gated on the same decision. It was not. #16 shipped without a Space, and is
closed.

### Group 3 — no namespace (1)

`ApolloEnvironment.namespaceId`.

The same shape as group 2, listed separately because it is a distinct Apollo
concept that #17 did not decide. The namespace half was split out to #26, which
recommends deferring; until that is taken up this coordinate is refused for the
same reason as group 2 -- there is no value to serve -- but by an open question
rather than by a settled one.

## Consequences

- The interface's published counts stay meaningful. As of 2026-08-15 the
  interface declared 41 recovered coordinates, serving 29 and refusing 12, none
  of Conduit's own invention. Declared and served are counted separately on
  purpose. This line previously read "41 recovered coordinates served, 12
  declared and unanswerable", which counts the twelve twice and exceeds the 41
  that exist — the conflation this record was written to refuse, restated in its
  own consequences. *(Frozen figures; the live claim moved to ADR 0007 when this
  record was superseded, so that a superseded document cannot drift into
  disagreeing with the interface.)*
- `internal/apollographql/domain.Reconcile` keeps failing the build on the two
  cases that are defects — a field Conduit added that nothing backs, and a
  resolver claiming a coordinate the schema does not declare. Refusing a
  recovered coordinate is neither of those and stays legal.
- Serving any of the twelve later requires displacing this record, not
  amending it quietly.
- Issue #12 is closed by this decision rather than by code. Its premise about
  the documents was accurate; the conclusion did not follow from it.

## More information

- `docs/reference-platform/reference/graphql-api.md` — the recovered interface, its
  provenance tiers, and its bounds.
- `internal/apollographql/domain/refusal.go` — the refusal text and why a null
  would be worse.
- Issue #17 — the Space tenancy decision, closed 2026-08-16 as won't-do, which
  makes group 2's five refusals permanent rather than pending.
- Issue #26 — the namespace question split out of #17, still open, which is
  what group 3's single refusal now waits on.
