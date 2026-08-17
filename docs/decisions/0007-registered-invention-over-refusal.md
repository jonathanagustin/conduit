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

### Group 1 — no Apollo resource identifiers (6)

`ApolloBundle.rid`, `ApolloProduct.rid`, `ApolloProductRelease.rid`,
`ApolloQuery.bundle`, `ApolloQuery.bundles`, `ApolloQuery.productRelease`.

Conduit holds all three resources and lacks only Apollo's identifier for them.
Under this record it mints one, in a form that cannot be mistaken for Apollo's.

The invention is bounded by the one structural fact the evidence does give:
`ApolloPlanId` is documented as *"a uuid, in practice always equal to the locator
of the corresponding PlanRid"*, which establishes that an Apollo RID has a
**locator** segment and that a locator can be a UUID. It establishes nothing
about the segments before it, and `ri.apollo` appears nowhere in the corpus.

The minted identifier must therefore name Conduit in the position where Apollo
would name its own service, so that a value leaking into a log or a bug report is
self-evidently not an Apollo RID even with no access to `valueTier`. It is an
invention twice over — the segment grammar as much as the values — and both parts
get registered.

**Displaced by:** a capture or published statement giving an `ri.apollo.*`
grammar. On arrival the minting is reimplemented against it, not bent toward it.

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

- Today the interface has 41 recovered coordinates declared, of which 29 are
  served and 12 are unanswerable. **This record changes no code**, so that figure
  is unmoved by it; it falls to five as groups 1 and 3 land. It is pinned by
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
