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

## Everything open is a decision

Nothing is claimable. Every open issue below needs a person to choose, and each
row states the choice rather than the task — an issue that is really a decision
wastes an agent's run when it is labelled ready.

| Issue | The decision |
| --- | --- |
| [#13](https://github.com/jonathanagustin/conduit/issues/13) | Where does a reviewer requirement that **no grant can satisfy** surface — push the change to `ChangeError`, or attach a non-fatal diagnostic? A grant created after the policy is legitimate ordering, so the loud option can fail a change that is merely early. |
| [#15](https://github.com/jonathanagustin/conduit/issues/15) | How does an **intermittent** Environment vouch for a Release? Adding Apollo's fifth connection setting as a bare constant would invert its behaviour. |
| [#16](https://github.com/jonathanagustin/conduit/issues/16) | Scan-before-install: where does a Space setting live when there is no Space, and what makes a scan record fresh enough? Gated on #17. |
| [#17](https://github.com/jonathanagustin/conduit/issues/17) | Should Conduit have a **Space / organization tenancy boundary** at all? The largest of these: it gates #16 and six refused coordinates, and two independent `apollo-cli` notes make Space a first-class publishing argument. |
| [#19](https://github.com/jonathanagustin/conduit/issues/19) | Is a third-party documentation crawl **the** exception to manual-only CI (A4)? The offline half is already done and needs no answer. |
| [#20](https://github.com/jonathanagustin/conduit/issues/20) | Same shape as #19: a merge queue requires overturning A4, which is CONFIRMED. |
| [#21](https://github.com/jonathanagustin/conduit/issues/21) | Which principal proposes a Gitless convergence change. |
| [#24](https://github.com/jonathanagustin/conduit/issues/24) | Is requiring a contact team on every Environment **policy or incidental**? Apollo permits Environments without one. Relaxing the validation is two lines and would quietly remove a governance constraint if the constraint is deliberate. |

Two of these — #19 and #20 — are the same question about A4 wearing different
clothes, and answering A4 once settles both.

## Closed

| Issue | Outcome |
| --- | --- |
| [#10](https://github.com/jonathanagustin/conduit/issues/10) | Cardinality for the five collection fields, via a second directive argument so `OBSERVED` existence and `DOCUMENTED` cardinality are stated separately. Apollo's published example query now answers. |
| [#11](https://github.com/jonathanagustin/conduit/issues/11) | `ApolloEnvironment.id`, served from `projection.ID`. |
| [#12](https://github.com/jonathanagustin/conduit/issues/12) | **Won't do.** Its premise was right — the bundle documents disclaim the wire format, not the concept — and the conclusion did not follow: `rid` is the identity, not the concept, and Conduit's is `bnd_<hex>`. ADR 0005. |
| [#14](https://github.com/jonathanagustin/conduit/issues/14) | `--values` / `--values-schema` as aliases onto the existing bundle, plus the older defect it exposed: Helm configuration passed with a `terraform.v1` manifest was accepted and silently dropped. |
| [#18](https://github.com/jonathanagustin/conduit/issues/18) | The Rosetta-only notes read one by one. 55, not 58 — the public page moved. 49 are console notes with no contract here; of the 6 CLI notes, two are Space, two are deliberate divergences, one carries no contract, and one became #24. |
| [#22](https://github.com/jonathanagustin/conduit/issues/22) | **Won't do yet.** Trigger is a multi-replica deployment; every manifest under `deploy/` is `replicas: 1`. The bound lives in the `resolutionlog` package comment, where a deployer will meet it — a better place than a board. |
| [#23](https://github.com/jonathanagustin/conduit/issues/23) | [ADR 0005](../decisions/0005-apollo-coordinates-that-stay-refused.md): all twelve refused coordinates, split by what would close each, pinned to the resolver by `TestRefusedCoordinatesMatchTheDecisionRecord`. |

The work for the closed issues is on `claude/apollo-documented-tier` and has not
been merged to `main`. They are closed on the strength of their commits.

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
