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
| [#24](https://github.com/jonathanagustin/conduit/issues/24) | Conduit cannot represent an Environment with no contact team. `newEnvironmentDescriptor` rejects an empty one; Apollo permits it, which is why `apollo-cli` 0.463.0 made the flag optional on `environment get`. Surfaces as an import failure, not a missing feature. Decide first whether the requirement is policy or incidental — the easy fix removes a governance constraint that may be load-bearing. |

### Re-scoped

| Issue | Work |
| --- | --- |
| [#13](https://github.com/jonathanagustin/conduit/issues/13) | **Its premise was wrong; the body needs rewriting before anyone starts.** "Reject an unknown operation at policy-authoring time" would delete a working feature — operator-defined approval tokens in `require_operation` are satisfied against `ResourceGrant.Operations`, which is open on purpose. The real defect is narrower: a requirement *no grant anywhere* can satisfy sits pending forever rather than being reported. That is a grant question at evaluation time, not a vocabulary question at authoring time. See PH-6. |

### Shipped

| Issue | Work |
| --- | --- |
| [#10](https://github.com/jonathanagustin/conduit/issues/10) | Cardinality for the five recovered collection fields, via a second directive argument so `OBSERVED` existence and `DOCUMENTED` cardinality are stated separately. Apollo's published example query now works. |
| [#11](https://github.com/jonathanagustin/conduit/issues/11) | `ApolloEnvironment.id`, served from `projection.ID`. |
| [#14](https://github.com/jonathanagustin/conduit/issues/14) | `product-release create --values / --values-schema`, as aliases onto the existing bundle. Also fixed the older defect it exposed: Helm configuration passed with a `terraform.v1` manifest was accepted and silently dropped. |
| [#12](https://github.com/jonathanagustin/conduit/issues/12) | **Closed by decision, not by code.** Its premise was right — the bundle documents disclaim the wire format, not the concept — and the conclusion did not follow: `rid` is the identity, not the concept, and Conduit's is `bnd_<hex>`. See ADR 0005. |
| [#23](https://github.com/jonathanagustin/conduit/issues/23) | [ADR 0005](../decisions/0005-apollo-coordinates-that-stay-refused.md) records all twelve refused coordinates (not ten — the count had drifted), split by what would actually close each. Pinned to the resolver by `TestRefusedCoordinatesMatchTheDecisionRecord`. |
| [#18](https://github.com/jonathanagustin/conduit/issues/18) | Read the Rosetta-only release notes against the implementation. 55 today, not 58 — the public page moved. 49 are Apollo console notes with no contract for a control plane; 6 are CLI. Two are Space (widens the *cost* of #17, not its scope), two are deliberate architectural divergences, one is a real unrecorded gap (Conduit cannot represent an Environment with no contact team), one carries no contract. It does not widen #15. |

## Blocked on a decision

These are listed so the decision is visible, not so the work looks planned.

| Issue | Decision |
| --- | --- |
| [#15](https://github.com/jonathanagustin/conduit/issues/15) | How does an *intermittent* Environment vouch for a Release? Apollo published a fifth connection setting; adding the constant alone would invert its behaviour. |
| [#16](https://github.com/jonathanagustin/conduit/issues/16) | Scan-before-install: where does a Space setting live when there is no Space, and what makes a scan record fresh enough? |
| [#17](https://github.com/jonathanagustin/conduit/issues/17) | Should Conduit have a Space / organization tenancy boundary at all? Gates #16 and five coordinates. |
| [#20](https://github.com/jonathanagustin/conduit/issues/20) | A merge queue requires overturning A4, which is CONFIRMED. |
| [#21](https://github.com/jonathanagustin/conduit/issues/21) | Which principal proposes a Gitless convergence change. |
| [#19](https://github.com/jonathanagustin/conduit/issues/19) | **Re-filed from "evidence and hygiene", where it read as ready work.** "Run the drift gate on a schedule" requires overturning A4, exactly as #20 does — `manual-ci.md` names `schedule` in its prohibition. The decision is whether a third-party crawl is the exception to manual-only CI. The offline half needed no decision and is done: `verify-documentation-freshness.mjs` fails a gate that already runs once the corpus is older than 45 days. |

## Evidence and hygiene

| Issue | Work |
| --- | --- |
| [#22](https://github.com/jonathanagustin/conduit/issues/22) | Durable Gitless resolution log. Trigger is the first multi-replica deployment, which has not happened — every manifest under `deploy/` is `replicas: 1`, checked 2026-08-15. Correctly deferred, not forgotten. |

## A standing gap: label appearance

Every label in [`.github/labels.yml`](../../.github/labels.yml) exists on the
repository, because applying a label to an issue creates it — but implicitly
created labels carry a name and nothing else. All of them are still the default
grey (`ededed`) with no description.

Applying the file needs a label write, and the GitHub MCP server exposes only
`get_label`. It is not a permission that can be widened from a session; it is a
tool that is not there. Run it from anywhere with ordinary repository access:

```sh
gh label create status:ready --color 0e8a16 \
  --description "No decision outstanding. An agent may claim this." --force
```

one line per entry, or point any label-sync action at the file. Nothing depends
on the colours — the `status:` names are the protocol and they are correct — so
this is cosmetic, and it is recorded here rather than filed as an issue because
it is a fact about the tooling rather than work on the product.

## Where the evidence lives

- [`apollo/PUBLIC-DOCUMENTATION-COVERAGE.md`](../apollo/PUBLIC-DOCUMENTATION-COVERAGE.md) — the 262-page corpus, its maturity markers, the image audit, and what the 2026-08-15 re-crawl found
- [`apollo/ROSETTA-RELEASE-NOTES.md`](../apollo/ROSETTA-RELEASE-NOTES.md) — the tenant release-note record and how it differs from the public one
- [`apollo/reference/graphql-api.md`](../apollo/reference/graphql-api.md) — the recovered interface, its provenance tiers, and its bounds
- [`HYPOTHESIS-BACKLOG.md`](HYPOTHESIS-BACKLOG.md) — the PH rows, each now carrying its issue number
- [`CAPABILITY-MAP.md`](CAPABILITY-MAP.md) — the H rows, which record solved capabilities and are not tracked as issues
