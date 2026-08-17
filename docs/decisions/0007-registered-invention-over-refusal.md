---
status: accepted
date: 2026-08-17
supersedes: 0005-apollo-coordinates-that-stay-refused
decision-makers:
  - repository owner
consulted: []
informed: []
---

# Serve every Apollo coordinate, and register what Conduit invented to do it

## Context and problem statement

[ADR 0005](0005-apollo-coordinates-that-stay-refused.md) decided that twelve
recovered Apollo coordinates would refuse at request time rather than resolve,
because Conduit holds no data behind them and a GraphQL null is itself a claim.
It also decided, in its third driver, that serving Conduit's nearest equivalent
under an Apollo field name "would invent a correspondence and then present it as
recovered".

The repository owner has overruled the first half of that. The instruction is
that Conduit does not deliberately refuse: where there is no ground truth, it
implements against an educated guess, marks the guess, and reimplements when
evidence arrives. That instruction is a decision about what this product is for,
and it is not something evidence can settle.

This record supersedes ADR 0005 rather than amending it, because ADR 0005
requires exactly that: *"Serving any of the twelve later requires displacing this
record, not amending it quietly."*

## The conditions were not met, and are being overruled

This is the part most likely to be misremembered later, so it is stated before
the decision rather than after it.

**ADR 0005's close conditions are unmet as of today**, and two independent
captures landed since say so in their own words:

- [`apollo-identifier-grammar.json`](../evidence/apollo-identifier-grammar.json)
  records that `ApolloPlanRid` and `ApolloPlanTaskRid` are documented as rids
  with no form given, and states outright: *"ADR 0005 group 1's close condition
  remains unmet."*
- [`apollo-control-panel-seam.json`](../evidence/apollo-control-panel-seam.json)
  records that *"the string `ri.apollo` does not occur anywhere in the corpus."*

> **Correction, 2026-08-17, same day this record landed.** The second bullet is
> quoted accurately and was used to support more than it says. "The corpus" there
> is the Java control-panel artifact that capture projected — not every source
> this repository holds. `ri.apollo` occurs **twelve times in the pinned public
> OpenAPI snapshot**, which publishes three RID grammars outright:
> `ri.apollo.main.environment.123`, `ri.apollo.main.installation.123`, and
> `ri.apollo.main.package.123`. A fourth, `ri.autopilot.main.team.123`, shows the
> service segment varying by owning service, so the shape
> `ri.<service>.main.<type>.<locator>` is published rather than inferred.
>
> This does not change the decision, and it does not revive ADR 0005's condition
> for group 1 — `bundle`, `product` and `productRelease` have no published RID
> grammar, which is exactly why those six were refused while
> `ApolloEnvironment.rid` was served. ADR 0005 was internally consistent on
> this point and the correction is to this record, not to that one.
>
> What it does change is group 1's minting guidance below, which is amended in
> place, and one fact this record got backwards: Conduit is **already** minting
> `ri.apollo.*` identifiers against the published grammar, and serving them under
> `OBSERVED` coordinates with no disclosure. That is the first thing `valueTier`
> is applied to.
>
> The error is the one this repository keeps making: a source-scoped absence read
> as a general one. It is the same shape as the `notifications.md` claim that no
> public documentation page existed, and it was made in the record that added a
> gate against that exact class.

For group 2 the evidence went further and cut **against** serving. The same
identifier capture documents `ApolloSpaceId` as originating *external to
Foundry*, stored so Foundry knows which Apollo space manages an installation, and
*obtained when an agent reports its information to Apollo*. Palantir's own words
say the value is minted elsewhere and arrives by report. A Hub that is not Apollo
has nothing to put in that field — not because nobody has captured the answer,
but because the answer is another system's.

So this record does not claim a condition was satisfied. ADR 0005 pre-refuted
that move at its own lines 120–124, warning against softening "what an Apollo
Space id is for a resource Conduit holds" into the weaker "how a Space id is
formed". Nothing here softens it. The condition is unmet, and it is being
overruled by the owner's decision about the product.

## What actually changed

Two things, neither of them evidence.

**1. The owner's instruction.** A gap that cannot be closed by evidence is now
closed by construction. The reasoning is that a refusal is only honest about
Conduit; it is useless to the operator holding the query. An interface that
refuses twelve coordinates is not a more truthful interface than one that
answers them and says which answers are its own — it is a less useful one that
has moved the cost onto the reader.

**2. A disclosure mechanism that did not exist when ADR 0005 was written.**
This is the substantive change, and it is why superseding does not require
calling ADR 0005 wrong on its own terms.

Read ADR 0005's third driver precisely. It forbids serving an invented value
**and presenting it as recovered**. It is one objection, not two, and the second
half is doing the work. In 2026-08-15 the interface had no way to express "here
is a value, and it is Conduit's own": `@provenance(tier:)` describes why a
coordinate is *declared*, and a client reading `ApolloBundle.rid` had no way to
learn whether the string came from an Apollo Hub or from us. Given that, refusal
really was the only honest option available.

This record adds the missing expression, so the objection stops applying:

```graphql
directive @provenance(
  tier: ProvenanceTier!
  cardinality: ProvenanceTier
  valueTier: ProvenanceTier
) on FIELD_DEFINITION
```

- `tier` keeps its meaning: the provenance of the **declaration** — why this
  field exists in Conduit's schema at all.
- `valueTier` is new: the provenance of the **value served**. Absent means it
  equals `tier`, so every coordinate served today is unchanged.
- `@provenance(tier: OBSERVED, valueTier: CONDUIT_NATIVE)` reads: *Apollo
  declares this field; the value you are receiving is Conduit's own.*

The two-argument split is not a new idea here — `cardinality` already carries a
second, independent provenance on the same directive, from issue #10.

## Decision drivers

- An operator's query should be answered. A refusal is a cost moved, not a cost
  avoided.
- Invention is acceptable; **undisclosed** invention is not. Every invented value
  must be distinguishable from a recovered one in band, by a client that never
  reads this repository.
- Displacement still runs one way. Ground truth displaces invention, never the
  reverse, and arriving evidence is an obligation to reimplement rather than a
  licence to keep the guess that now looks close.
- The counts must stay honest. They must now distinguish served-from-evidence
  from served-by-invention, or the interface will look more recovered than it is.
- A guess made without exhausting the sources already in the repository is not an
  educated guess. This is written in because the invention register's first three
  entries were displaced the day after they were written, by evidence that had
  been sitting in this repository's own manifest.

## Considered options

- **Keep refusing** (ADR 0005). Rejected by the owner.
- **Serve invented values under the existing directive.** Rejected: this is
  precisely the "present it as recovered" failure ADR 0005 identified, and
  nothing about the owner's instruction requires it. Marking the invention only
  in `docs/` leaves the wire silent, and the wire is what most readers get.
- **Serve invented values, disclosed in band by `valueTier`, and registered in
  `docs/evidence/conduit-inventions.json` with the evidence that would displace
  each.** Chosen.

## Decision outcome

Chosen option: **serve, disclose, register**. Concretely:

1. No recovered coordinate refuses for want of data. `RefuseUnserved` survives
   only for the defect case — a coordinate Conduit declared that nothing backs —
   which `Reconcile` already fails the build on.
2. Any coordinate served from a value Conduit constructed carries
   `valueTier: CONDUIT_NATIVE`.
3. Any such construction is registered in
   [`conduit-inventions.json`](../evidence/conduit-inventions.json) with what it
   is, why, the evidence that would displace it, and the risk if wrong. The gate
   at `scripts/verify-conduit-inventions.mjs` already enforces both directions.
4. **Before inventing, the sources already in this repository must be
   exhausted** — the documentation manifest and the images referenced from it
   included. `scripts/verify-documentation-claims.mjs` enforces the cheapest part
   of this automatically.

### Group 1 — no Apollo resource identifiers (3)

`ApolloBundle.rid`, `ApolloQuery.bundle`, `ApolloQuery.bundles`.

Conduit holds all three resources and lacks only Apollo's identifier for them.
Under this record it mints one, in a form that cannot be mistaken for Apollo's.

**Three of the original six are served as of 2026-08-17.** `ApolloProduct.rid`,
`ApolloProductRelease.rid` and `ApolloQuery.productRelease` now answer from
Conduit-minted identifiers under `ri.conduit.main.<type>.<locator>`, disclosed as
`valueTier: CONDUIT_NATIVE`. The open design question this record left — whether
to guess a type token inside `ri.apollo` or take a namespace that cannot collide
— was answered in favour of the second, because the failure modes are not
symmetric: a guessed token that turns out to be Apollo's real token for
something else is a silent collision, while a Conduit-namespaced identifier can
only be wrong in the direction of being obviously ours. It is also the more
literal reading of the grammar, whose service segment names the service that
owns the resource.

The three that remain are the Bundle coordinates, and they are not waiting on an
identifier. Conduit builds, signs, imports and transfers Bundles, but none of
that is reachable from this interface: `ports.ApolloProjection` has no bundle
read, so there is nothing to mint an identifier *for* until one exists. That is
a wiring gap rather than an evidence gap, and it is the reason these three did
not move with the others.

*Amended by the correction above.* The grammar is **not** the invention here. The
pinned OpenAPI publishes `ri.<service>.main.<type>.<locator>` and three Apollo
instantiations of it, and `ApolloPlanId` adds that a locator can be a UUID
(*"a uuid, in practice always equal to the locator of the corresponding
PlanRid"*). Conduit already mints against that published grammar for the three
published types.

Two things are invented, and only two:

1. **The type token** for `bundle`, `product` and `productRelease`, which Apollo
   has not published. Minting `ri.apollo.main.bundle.<locator>` guesses a token
   inside Apollo's own namespace, where a future real Apollo type of the same
   name would collide with it silently.
2. **The locator**, for every type including the published three. Apollo's
   examples use the placeholder `123`, so no locator format is established;
   Conduit uses a truncated SHA-256 of a stable input, where the one documented
   Apollo locator is a UUID.

Both are registered. Naming Conduit in the service segment is **not** the answer
for the three published types — Conduit correctly follows Apollo's grammar there,
and substituting `ri.conduit.*` would break compatibility to make a point that
`valueTier` already makes. Whether the three unpublished types should follow the
same namespace or take a distinguishable one is the live design question, and it
turns on collision risk rather than on honesty, since `valueTier` carries the
honesty either way.

**Displaced by:** a published statement giving the `bundle`, `product` and
`productRelease` type tokens, and separately by any statement of Apollo's locator
format. On arrival the minting is reimplemented against it, not bent toward it.

Note preserved from ADR 0005 because it remains a live trap: `ri.apollo.main.bundle`
appears in this repository only in Conduit's own GraphQL fixtures. It is our test
data, not evidence, and citing it would be circular.

### Group 2 — no Space tenancy boundary (5)

`ApolloBundle.spaceId`, `ApolloEntity.spaceId`, `ApolloEnvironment.spaceId`,
`ApolloProduct.spaceId`, `ApolloProductRelease.spaceId`.

This is the group where the evidence argues hardest against serving, and saying
so is part of deciding it.

`ApolloSpaceId` is documented as external in origin and delivered by agent
report. Minting one is not filling a gap in our knowledge; it is Conduit issuing
an identifier whose defined meaning belongs to a system Conduit is not. Under
ADR 0005 that ended the discussion. Under this record it does not, because
`valueTier: CONDUIT_NATIVE` says the necessary thing on the wire: this is
Conduit's tenancy identifier occupying Apollo's field, and it is not an
`ApolloSpaceId`.

**This reopens issue #17.** It closed 2026-08-16 as won't-do, and its stated
reason was "the honest refusal over a single default Space" — the exact
preference the owner has now overruled. #17's decision therefore does not survive
this record on its own reasoning, and the question it answered has to be asked
again under the new rule. Its substantive objection does survive and must be
answered rather than dismissed: **a single default Space is a discriminator that
discriminates nothing until a second exists.** An invented Space that is always
the same constant is a synthesized value wearing a tenancy field, and it is worse
than a refusal because it looks like data.

The consequence is that group 2 does not close by minting a constant. It closes
by Conduit having a real tenancy boundary of its own to report — one that differs
between deployments and can differ within one. Until that exists, these five stay
refused under this record too, and they are the only coordinates that do.

**Displaced by:** a capture establishing what an Apollo Space id is for a
resource Conduit holds, which would replace the invention outright rather than
disclosing alongside it.

### Group 3 — no namespace (1)

`ApolloEnvironment.namespaceId`.

Split out to issue #26, which recommended deferring. Deferral was a form of
refusal and is no longer available. Conduit registers Kubernetes namespaces
already, so unlike group 2 there is a real, deployment-varying value to report,
and unlike group 1 there is no identifier grammar to invent.

The care needed is that the Apollo concept and the Kubernetes concept may not be
the same thing, and serving one under the other's name is the invented
correspondence this record still refuses to make silently. Served with
`valueTier: CONDUIT_NATIVE` and registered.

**Displaced by:** a capture establishing what an Apollo namespace id is and
whether it corresponds to a Kubernetes namespace.

## Consequences

- Today the interface has 41 recovered coordinates declared, of which 32 are
  served and 9 are unanswerable. Three of group 1 landed on 2026-08-17; the
  figure falls to five once the Bundle coordinates and group 3 follow. It is
  pinned by
  `TestPublishedCoordinateCountsMatchTheInterface`, which now reads this record
  rather than the superseded one, so the number cannot drift ahead of the
  interface or behind it.
- The published counts gain a dimension. It is no longer enough to say how many
  recovered coordinates are served; the split between served-from-evidence and
  served-by-invention has to be published too, or the interface reads as more
  recovered than it is. Every document carrying these figures moves together.
- **Five coordinates stay refused** — group 2 — and they are refused for a
  reason this record states rather than inherits: there is no Conduit value to
  disclose, and a constant would be a synthesized value in a tenancy field. That
  count falls when Conduit has a tenancy boundary, not when someone decides to
  mint one.
- `internal/apollographql/domain.Reconcile` keeps failing the build on the two
  real defects: a field Conduit added that nothing backs, and a resolver claiming
  a coordinate the schema does not declare.
- A served coordinate whose `valueTier` is `CONDUIT_NATIVE` and which is absent
  from the invention register is a build failure. Disclosure and registration are
  one obligation, not two that can drift apart.
- ADR 0005 becomes historical. Its group memberships and its reasoning about
  nulls are still correct and still worth reading; its outcome is replaced.
- Issue #17 is reopened. Issue #26 is superseded by group 3 above.

## More information

- [ADR 0005](0005-apollo-coordinates-that-stay-refused.md) — the superseded
  record, including why a null is worse than a refusal, which this record keeps.
- [`../evidence/conduit-inventions.json`](../evidence/conduit-inventions.json) —
  the register, its displacement procedure, and what the first three displaced
  inventions got wrong.
- [`../evidence/apollo-identifier-grammar.json`](../evidence/apollo-identifier-grammar.json)
  — the AEID grammar Conduit already mints against, the Plan RID locator fact
  that bounds group 1, and the `ApolloSpaceId` origin that bounds group 2.
- [`../apollo/reference/graphql-api.md`](../apollo/reference/graphql-api.md) —
  the recovered interface, its provenance tiers, and its bounds.
- `internal/apollographql/domain/refusal.go` — the refusal text, now reduced to
  the defect case.
