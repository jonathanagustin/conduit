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

Three items, all unblocked by checking the code rather than by a decision.

| Issue | Work |
| --- | --- |
| [#13](https://github.com/jonathanagustin/conduit/issues/13) | Surface a reviewer requirement no *current* grant can satisfy, as a **query-time computed diagnostic**. Not `ChangeError`: satisfaction is evaluated live (`RequirementSatisfied` reads `EffectiveGrants` at call time), `ChangeError` is terminal, and grant-after-policy is ordinary sequencing — flagging at submission would strand changes that were merely early. A computed, unstored field also cannot perturb `ReviewerRequirement.ID`'s digest. |
| [#25](https://github.com/jonathanagustin/conduit/issues/25) | Intermittent Environment silence must not read as stale. Two halves: **display** (the `FleetQuery`/`VisibleInstallations` change) and **behaviour** (a sibling of `nonParticipatingEvidenceEnvironments` in `internal/controlplane/federation.go`, *not* a new port contract). Must **not** exempt anything from `MaximumEvidenceAgeSeconds` — that bound is the operator's. |
| [#28](https://github.com/jonathanagustin/conduit/issues/28) | Comprehensive mutation testing. Today CI validates the *harness*, not the suite, so the Go mutation score is unknown and the configured Stryker setup never runs. Wants a measured score with a **ratchet**, in a dispatched workflow (A4 forbids a timer). |

## Blocked on a decision

| Issue | The decision |
| --- | --- |
| [#17](https://github.com/jonathanagustin/conduit/issues/17) + [#16](https://github.com/jonathanagustin/conduit/issues/16) | **One decision, not a chain.** Adopt a minimal Foundry Space grouping (`spaceId`, 5 refused coordinates)? The evidence flows #16 → #17, not the reverse: Apollo publishes a scan-before-install constraint "controlled by a Space setting", which is unimplementable-as-published without a Space to attach it to. The Space's published job is to be the *settings attachment point*, which answers the "discriminator that discriminates nothing" objection. Scope it opaque, optional, not wired into grant scopes. |
| [#19](https://github.com/jonathanagustin/conduit/issues/19) + [#20](https://github.com/jonathanagustin/conduit/issues/20) | **One decision in two costumes.** Both require overturning A4. Answering A4 once settles both. #19's offline half already shipped, so nothing is lost by keeping A4. |
| [#21](https://github.com/jonathanagustin/conduit/issues/21) | Which principal proposes a Gitless convergence change. A dedicated `change:propose`-only service account, not `OnBehalfOf` — attributing autonomous changes to whoever enabled the flag falsifies the audit trail. Three unresolved sub-parts make this the *last* to start, not the least important: the `Metadata["source"]` stamp policies need to branch on, the **deterministic `crq_` id collision** on re-proposal (`crq_` = SHA-256 of TargetKind+TargetID, content-independent — so re-proposing a target collides with a possibly-terminal prior request), and coalescing so propose-only does not flood review. |

### Deferred

| Issue | Trigger to reopen |
| --- | --- |
| [#26](https://github.com/jonathanagustin/conduit/issues/26) | Adopt Apollo **Namespace** as a governed resource (`namespaceId`, 1 coordinate)? Defer — one coordinate against a five-role family is nowhere near threshold, and the full RBAC evidence *strengthens* deferral: adopting honestly means the whole family or a misleading sliver. |

## Closed

| Issue | Outcome |
| --- | --- |
| [#10](https://github.com/jonathanagustin/conduit/issues/10) | Cardinality for the five collection fields, via a second directive argument so `OBSERVED` existence and `DOCUMENTED` cardinality are stated separately. Apollo's published example query now answers. |
| [#11](https://github.com/jonathanagustin/conduit/issues/11) | `ApolloEnvironment.id`, served from `projection.ID`. |
| [#12](https://github.com/jonathanagustin/conduit/issues/12) | **Won't do.** Its premise was right — the bundle documents disclaim the wire format, not the concept — and the conclusion did not follow: `rid` is the identity, not the concept, and Conduit's is `bnd_<hex>`. ADR 0005. |
| [#14](https://github.com/jonathanagustin/conduit/issues/14) | `--values` / `--values-schema` as aliases onto the existing bundle, plus the older defect it exposed: Helm configuration passed with a `terraform.v1` manifest was accepted and silently dropped. |
| [#15](https://github.com/jonathanagustin/conduit/issues/15) | **Intermittent agents recognised.** Re-reading the published page turned a decision back into conformance: it is operationally identical to Connected. Wired through a shared `ExpectsDirectAgents` so no path can omit it; the one difference is `ExpectsContinuousReporting`, consumed by #25. |
| [#18](https://github.com/jonathanagustin/conduit/issues/18) | The Rosetta-only notes read one by one. 55, not 58 — the public page moved. 49 are console notes with no contract here; of the 6 CLI notes, two are Space, two are deliberate divergences, one carries no contract, and one became #24. |
| [#22](https://github.com/jonathanagustin/conduit/issues/22) | **Won't do yet.** Trigger is a multi-replica deployment; every manifest under `deploy/` is `replicas: 1`. The bound lives in the `resolutionlog` package comment, where a deployer will meet it — a better place than a board. |
| [#23](https://github.com/jonathanagustin/conduit/issues/23) | [ADR 0005](../decisions/0005-apollo-coordinates-that-stay-refused.md): all twelve refused coordinates, split by what would close each, pinned to the resolver by `TestRefusedCoordinatesMatchTheDecisionRecord`. |
| [#27](https://github.com/jonathanagustin/conduit/issues/27) | **Won't do.** A per-binding portability matrix validates nothing until a workload class declares bindings, and the matrix actually needed belongs to the adopting project's runtime ADR. Re-file inside whichever future issue introduces a binding-declaring workload class. Its closure ends the Knative / `function.v1` / shared-runtime thread. |
| [#24](https://github.com/jonathanagustin/conduit/issues/24) | **Contact team optional.** The decision answered itself in the code: the profile model already permits an ownerless Environment, so the requirement was incidental to the CLI, not policy. Removed an invented constraint rather than relaxing a real one. |

The work for the closed issues is on `claude/apollo-documented-tier` and has not
been merged to `main`. They are closed on the strength of their commits.

## How the grounding was done

Several of these were labelled decisions when a published contract had already
decided them, or were conflated with a different question. The evidence used to
tell the two apart, in descending force:

1. **The captured tenant authorization graph** (`docs/evidence/apollo-authorization-graph.json`) — Apollo's live RBAC. It proved `namespace` and `space` are distinct concepts (Namespace has a full role family; space does not), which split #17 from #26.
2. **The pinned Conjure interop IR and OpenAPI** (`api/conjure/`, `docs/apollo/snapshots/`) — the wire types and the `Namespace{id,owners,editors,viewers}` shape.
3. **Foundry's own published docs** — the Space concept Apollo inherits but does not document.
4. **The live Apollo documentation pages** — which fully specify Intermittent agents (#15) and the optional contact team (#24).

The rule that held throughout: a published contract displaces an invented
constraint (#15, #24), but never displaces an honest refusal into a false
answer (#12, #17, #26).

**And the failure mode it kept catching, five times over: reasoning from
plausibility instead of grepping.** Twice a *validation surface* was mistaken
for a runtime — the workload-type list (only `helm-chart.v1` executes) and the
binding machinery (`ResolveApolloSubstitutions` and
`ApolloAugmentProducedDiscovery` have no non-test callers). Once a *terminal*
state was proposed for a *transient* condition (#13: satisfaction is evaluated
live against the grant table, and `ChangeError` cannot be left). Once two
unrelated systems sharing a word were treated as one causal chain (#25: the
reporting fleet's `Stale` and promotion's `EvidenceStale` — `grep -rn
MaximumEvidenceAgeSeconds internal/reporting/` returns nothing). Each was found
by reading the code, and each had survived a confident written argument first.

## A question that closed itself: invocation-shaped workloads

Recorded because it consumed real effort and the conclusion is reusable, not
because anything is outstanding.

The ask was for Conduit to deploy "a thing that isn't a container that's always
on" — a workload whose unit of execution is an invocation rather than a process.
It looked like a large capability gap. It was not one. Conduit can already do it:

| Trigger | Shape | Status |
| --- | --- | --- |
| Scheduled only | `CronJob` in the chart | works today |
| Queue / event | KEDA `ScaledJob` | works today (needs KEDA) |
| HTTP | the workload bundled into a digest-pinned image, installed as `helm-chart.v1` | works today |

Three things were proposed along the way and all three were wrong. **A Knative
interface** — rejected: something must always listen, so Knative's floor is paid
first, and it reintroduces the cold starts the workloads are chosen to avoid
(see [`docs/knative/README.md`](../knative/README.md)). **A portable
`function.v1` contract** — unnecessary: the gap was a *host for invocations*,
not a portable function format. **A shared runtime fed by objects naming
artifacts to fetch** — rejected on a repository-specific ground worth
remembering: Helm would succeed, Conduit would record the release deployed, and
a later fetch failure would leave the record false with nothing to surface it. A
silent lie in the system of record is worse, here, than an honest new verb.

The pattern that works was found already built in a consuming project: a bundled
workload running under a compatibility runtime in a container, published by
digest, installed by Conduit as an ordinary immutable release, with the
platform boundary drawn at **routes rather than at workloads**. Nothing in
Conduit had to change. The only piece that belongs in this repository is #27.

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
