# Apollo work queue

**Open work is tracked in GitHub issues, not here.** This file is an index, so
the repository carries a map of what is outstanding without becoming a second
place to update it.

The split is deliberate. Issues track *what is open*; the documents under
`docs/apollo/` and `docs/operations/` remain the *evidence record* — what was
found, when, from which source, and why a decision went the way it did. A closed
issue disappears from a board; the reasoning behind it should not disappear from
the repository.

Every issue below carries its own evidence inline, so it can be read without
this file.

## Ready to build

| Issue | Work |
| --- | --- |
| [#10](https://github.com/jonathanagustin/conduit/issues/10) | Serve cardinality for the five recovered collection fields. Makes Apollo's own published example query work. Needs a second directive argument so `OBSERVED` existence and `DOCUMENTED` cardinality can both be stated. |
| [#11](https://github.com/jonathanagustin/conduit/issues/11) | Serve `ApolloEnvironment.id` from `projection.ID`. Five sources name it; Conduit already keys on it. |
| [#12](https://github.com/jonathanagustin/conduit/issues/12) | Serve the bundle coordinates. The bundle documents disclaim the wire format, not the concept. |
| [#13](https://github.com/jonathanagustin/conduit/issues/13) | Make `require_operation` fail loudly on an operation Conduit cannot satisfy, instead of producing a silently unapprovable change. |
| [#14](https://github.com/jonathanagustin/conduit/issues/14) | `product-release create --values / --values-schema`. A missing entry point to a path that already works. |

## Blocked on a decision

These are listed so the decision is visible, not so the work looks planned.

| Issue | Decision |
| --- | --- |
| [#15](https://github.com/jonathanagustin/conduit/issues/15) | How does an *intermittent* Environment vouch for a Release? Apollo published a fifth connection setting; adding the constant alone would invert its behaviour. |
| [#16](https://github.com/jonathanagustin/conduit/issues/16) | Scan-before-install: where does a Space setting live when there is no Space, and what makes a scan record fresh enough? |
| [#17](https://github.com/jonathanagustin/conduit/issues/17) | Should Conduit have a Space / organization tenancy boundary at all? Gates #16 and five coordinates. |
| [#20](https://github.com/jonathanagustin/conduit/issues/20) | A merge queue requires overturning A4, which is CONFIRMED. |
| [#21](https://github.com/jonathanagustin/conduit/issues/21) | Which principal proposes a Gitless convergence change. |

## Evidence and hygiene

| Issue | Work |
| --- | --- |
| [#18](https://github.com/jonathanagustin/conduit/issues/18) | Read the 58 Rosetta-only release notes against the implementation. Do this before #15 is scoped — it may widen it. |
| [#19](https://github.com/jonathanagustin/conduit/issues/19) | Run the documentation drift gate on a schedule. Nothing runs it today, which is how the corpus aged two weeks and a contract change went unnoticed. |
| [#22](https://github.com/jonathanagustin/conduit/issues/22) | Durable Gitless resolution log. Trigger is the first multi-replica deployment. |
| [#23](https://github.com/jonathanagustin/conduit/issues/23) | ADR recording the ten coordinates that stay refused, and why. Several look closable and are not. |

## Where the evidence lives

- [`apollo/PUBLIC-DOCUMENTATION-COVERAGE.md`](../apollo/PUBLIC-DOCUMENTATION-COVERAGE.md) — the 262-page corpus, its maturity markers, the image audit, and what the 2026-08-15 re-crawl found
- [`apollo/ROSETTA-RELEASE-NOTES.md`](../apollo/ROSETTA-RELEASE-NOTES.md) — the tenant release-note record and how it differs from the public one
- [`apollo/reference/graphql-api.md`](../apollo/reference/graphql-api.md) — the recovered interface, its provenance tiers, and its bounds
- [`HYPOTHESIS-BACKLOG.md`](HYPOTHESIS-BACKLOG.md) — the PH rows, each now carrying its issue number
- [`CAPABILITY-MAP.md`](CAPABILITY-MAP.md) — the H rows, which record solved capabilities and are not tracked as issues
