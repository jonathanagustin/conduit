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

#65 — intermittent Environment silence, the half of #25 that was not done.
The fleet API now agrees with the console overview about an Environment whose
gaps are expected, which was the plumbing. What remains is the half PH-5
actually named: `ValidateEvidence` rejects evidence older than
`MaximumEvidenceAgeSeconds` with no connection-setting exemption, so an
intermittent Environment's "as of last connection" health is refused by any
age-bounded policy. It is a behavioural choice rather than plumbing, and the
operator's stated bound must not be exempted outright.

#25 was recorded here as shipped on 2026-08-16 and it was not. The symbols
existed and were wired on one surface of two; verifying that a predicate exists
is not verifying that every path consults it.

Four more shipped on 2026-08-17, each the part of a blocked decision that did
not depend on the decision. Recorded because in every case the code answered a
question the issue had framed as open.

**#56's ERROR wedge** (#102). `ERROR` is not a decision anybody took —
`evaluateChangeRequest` writes it when no approval policy is enabled at all —
yet `Reopen` refused it, so a deployment that proposed before configuring
policies burnt every target it touched, with no recourse open to any principal
including the author. Correct under either answer to the standing-veto question,
so it did not wait on it. What was deliberately left out is #103: `Reopen`
rebuilds the record without `Reviews`, destroying the only durable copy of a
rejection's rationale, and the obvious fix interacts — `approveOnce` refuses a
principal already in `Reviews`, so carrying a rejection forward would bar the
rejecting reviewer from approving the revision.

**#65's second gap** (#104). The refusal was correct and its sentence was not:
an intermittent Environment cannot satisfy a bound shorter than its connection
cadence, but "stale health evidence" reads as an outage.
`ENVIRONMENT_EVIDENCE_BOUND_CONFLICT` names the pair instead. The bound is not
exempted — and it turns out it could not be: `validatePromotions` has no
repository, so it takes Environment-dependent blockers on trust *because they
only ever make a promotion fail to pass*. A record that passed on excused
evidence would fail replay on the next Hub that read it.

**#57's health joins** (#105). `ApolloEnvironment.health` and
`ApolloEntity.health` answered `UNKNOWN` for everything, however healthy the
fleet was, because they compared minted rids against the plain names reporting
records. Demonstrated against a running Hub, not traced. The contract defect is
that the provenance extension listed both under `documentedServed` — the
interface reported having answered them, from a constant no Conduit data
produced. It survived because the test fixture had been aligned to the broken
code; the fixture is corrected from the projection, and a test now asserts the
two vocabularies are different strings so no fixture can hide the join again.

**#54's naming** (#106). `VulnerabilitySuppressionProvenance.ChangeRequestID` is
the one `ChangeRequestID` never resolved against `GetChangeRequest`, which reads
like a forgotten check. It is not: Conduit does not create provider-side
suppressions, so no local record could be named, and a relayed assessment
carries a reference only the approving Hub can resolve. Documented rather than
verified, because verifying it would be wrong.

**Supply-chain pin shape** (#101). The cosign gate scanned for a literal version
in one workflow and ignored a third occurrence in another, so the signing
toolchain could already drift. The version is now tethered to the one cosign the
repository pins by sha256 and actually executes, `actions/cache` is asserted as a
property rather than a literal commit, and `mise.toml` must agree with the
workflows about Node. cosign v3.1.3 had been in the repository, checksum-pinned,
since `bf1d7b5` — the bump closed a drift rather than opening one.

#13 (reviewer-requirement satisfiability) and #28 (comprehensive mutation
testing — now a *weekly* sweep, since ADR 0006 permits the `schedule:` trigger
A4 once forbade) did ship on 2026-08-16.

The **replay half of #56** shipped on 2026-08-17 as #89. An approved Change
Request was a replayable pre-approval token: a `crq_` id is a digest of the
target alone, so a re-proposal received the stored record and every handler that
auto-applied on `Status == APPROVED` performed the action again under whatever
policy applied now. Reproduced at three places rather than argued — the
export-pipeline routes, `PUT /v1/change-requests/{id}/labels` (which takes no
decision at all), and the deployment path, where `supersedePendingPlans` cancels
an approved Plan without touching its Change Request, so re-proposing superseded
desired state resurrected a `CANCELLED` Plan to `APPROVED` and cleared its
cancellation. The guard is the verdict of the evaluation the call performed, not
whether the call created the record: creation was the first design and it
refused an unchanged-policy repeat, which is now a test so it cannot come back
quietly. **The terminal-record wedge — the other half — is untouched.**

The **console live-simulation stack** shipped the same day as #92 and #93. The
stubbed console suite establishes that the console renders correctly *given* a
response, never that the Hub produces it, and says so in its own harness. #92
adds `scripts/simulate-console.sh`, which builds a Hub, seeds it through the
Hub's own API and drives the browser against the console that Hub serves from
its embedded bundle. #93 adds `internal/agent/adapters/simulator` behind the
Agent's existing ports and `cmd/platform-simulated-agent` — deliberately a
separate command rather than a mode of the shipped Agent, because a binary that
can fabricate healthy reports would let the control plane lie about the state of
the world; `scripts/assemble-oci-manifest.sh` already asserts the release
manifest carries exactly `hub` and `spoke-agent`, so the exclusion is enforced.

Two defects the simulation found in the course of being built, both recorded in
the code where they bit: the Hub refuses a successful Plan that reports a
degraded Entity (so degradation belongs on the health-observation path, not the
delivery report), and a Change Request evaluated before any approval policy
exists sits in `ERROR`, which made the Plans screen and the Overview disagree
while both told the truth.

## Blocked on a decision

| Issue | The decision |
| --- | --- |
| [#17](https://github.com/jonathanagustin/conduit/issues/17) | Adopt a minimal Foundry Space grouping (`spaceId`, 5 refused coordinates)? Opaque, optional, Maven-group-keyed, an administrative grouping rather than a security boundary and not wired into grant scopes — or accept the five refusals permanently. **This is no longer a chain with #16.** The argument that made them one decision was that Apollo's scan-before-install constraint is "controlled by a Space setting" and so needs a Space to attach to; #35 shipped that constraint without one, so the settings-attachment-point premise is spent. Recommended **won't-do**: serving a synthesized `spaceId` behind an `OBSERVED` coordinate would make the provenance disclosure lie. |
| [#20](https://github.com/jonathanagustin/conduit/issues/20) | **Answered, then blocked again on something else.** ADR 0006 overturned A4, and the queue shipped in #33 as `.github/workflows/merge-queue.yml`. It cannot run: GitHub offers merge queues on public repositories, and on private ones only under Team or Enterprise Cloud, so this private repository under a personal account has no "Require merge queue" control to enable — confirmed 2026-08-16 against `main`'s branch-protection form, where the option is absent between "Require linear history" and "Require deployments to succeed before merging". Rulesets do not route around it; the `merge_queue` rule type is gated the same way. What remains is a decision about repository ownership, not about CI. |
| [#21](https://github.com/jonathanagustin/conduit/issues/21) | Which principal proposes a Gitless convergence change. A dedicated `change:propose`-only service account, not `OnBehalfOf` — attributing autonomous changes to whoever enabled the flag falsifies the audit trail. Three unresolved sub-parts make this the *last* to start, not the least important: the `Metadata["source"]` stamp policies need to branch on, the **deterministic `crq_` id collision** on re-proposal — but on the `ProposeChanges` path only, and the distinction matters to whoever implements this. `crq_` is SHA-256 of TargetKind+TargetID (`governance.go:529-530`), so it collides exactly when `TargetID` is stable across re-proposals of the same target. That is true of every `ProposeChanges` caller, which pass a caller-supplied id — `EXPORT_PIPELINE_LIFECYCLE_*` with a pipeline id (`http.go:860`), `BUNDLE_IMPORT` with a record id (`http.go:1059`), the label definition target (`http.go:378`). It is **not** true of the plan pipeline: `evaluatePlan` sets `TargetID: record.ID`, and `pln_` is SHA-256 of the desired-state digest plus the plan JSON (`lifecycle.go:196-205`), so a re-proposal with different content gets a different `pln_` and therefore a different `crq_`. Designing Gitless around a collision the plan path does not have would be building for the wrong shape, and coalescing so propose-only does not flood review. **Updated 2026-08-17:** #89 closed the dangerous half of that collision — a stale `APPROVED` record can no longer be replayed into an apply, because auto-apply now asks the verdict of the evaluation the call performed. What remains for whoever implements this is the *wedge*: a terminal record still occupies its target's id. Design for that, not for replay. |

| [#56](https://github.com/jonathanagustin/conduit/issues/56) | **Is a rejection a standing veto, or advisory per attempt?** The replay half shipped as #89; this is the remainder. A `crq_` id is derived from the target alone, so a terminal record occupies that target permanently and only its author can `Reopen` it — and `Reopen` excludes `ERROR`, so a request that errored because no policy was configured was unrecoverable even by its author — **that half shipped as #102**, since `ERROR` is machine output rather than a judgement and admitting it is correct under either answer. What is left is the semantics for a *rejection*. *Standing veto* → re-proposal after rejection stays privileged, and the fix is making that path reachable. *Advisory per attempt* → any authorized proposer may mint a successor, with the rejection preserved and linked rather than overwritten. Nothing in the code answers this; today's behaviour is a standing veto enforced by accident, via id collision. An adversarial review recommends **standing veto** on structural grounds: advisory-per-attempt requires abandoning the target-derived id, and the replay fix, the deliberate re-proposal idempotency, and `LinkImportChangeRequest`'s `PENDING_APPROVAL` equality check all rest on it. Whichever way it goes, #103 has to be fixed too. |
| [#54](https://github.com/jonathanagustin/conduit/issues/54) | **Half answered.** The `ChangeRequestID` question resolved itself in the code and shipped as #106: it is an external reference and could not be anything else. What remains is whether an Environment with a `RequiredOutcome` may run with no scanner configured. Today it refuses honestly at proposal — "artifact security policy requires vulnerability evidence but no scanner is configured" — so the behaviour is defensible as it stands; what it cannot do is distinguish hub-scanned evidence from operator-asserted evidence in the record a reviewer approves. Both of my first two proposed fixes here were wrong and neither should be retried: a freshness floor does not defend against a forged `AssessedAt`, which is a plain client-supplied field, and "always scan" would delete the suppression feature outright, since grype hard-refuses a report carrying ignored findings. |
| [#57](https://github.com/jonathanagustin/conduit/issues/57) | The minted `ri.apollo.main.*` RIDs cut against ADR 0005's refusal of `ApolloProduct.rid`. Both cannot stand: either the ADR relaxes or the RIDs go. The defect they were *entangled* with is fixed and out of the way (#105), so this is now purely the disclosure question. An adversarial review recommends **the RIDs go**, on the ADR's own grounds rather than its letter: it refuses "serving Conduit's own identifier under an Apollo field name" and records that `ri.apollo.main.bundle` appears in this repository only as our own test data, so minting the grammar is worse than serving `bnd_<hex>` would have been. Consequence to accept if so: `ApolloQuery.environment(rid:)` becomes unreachable in practice, since a client can no longer learn a rid to pass. |
| ~~[#96](https://github.com/jonathanagustin/conduit/issues/96)~~ | **Closed won't-do on 2026-08-17, with #94 and #95.** Kept here because the reasoning is the record. Should the console tenant become a per-request fact? The framing that prompted it — "hydrated via headers" — is not what Foundry does: it exposes tenants per *provisioned hostname* (`<enrollment>.<stack>.palantirfoundry.com`) with OAuth2 on every call, so the binding is DNS and TLS established at provisioning time plus authentication before the hydrated document is served. Conduit has none of those three: no `TenantID` anywhere in `internal/`, and `GET /console/` is mounted bare with no auth wrapper, so a header-selected tenant would let any anonymous client choose what identity the console asserts. The Hub is also single-tenant by construction everywhere else, so the capability would have no consumer. |

### Open only because closing is a human step

The work is on `main`; the issue has not been closed.

| Issue | State |
| --- | --- |
| [#13](https://github.com/jonathanagustin/conduit/issues/13) + [#25](https://github.com/jonathanagustin/conduit/issues/25) | Both shipped in #29 — `Authorizer.RequirementSatisfiable` and `AnnotateSatisfiability` in `internal/governance/application/authorizer.go`; `FleetQuery.SilenceExpected` in `internal/reporting/domain/fleet.go` with `expectedSilenceEnvironments` in `internal/controlplane/federation.go`. Both still carry `status:needs-review`. |
| [#16](https://github.com/jonathanagustin/conduit/issues/16) | **Its premise was false.** `ArtifactSecurityPolicy` already refused to install an unscanned or non-PASS Release at every plan-producing path, and the Hub auto-scans missing evidence at proposal. The one real gap was *freshness*, shipped in #35 as `ArtifactSecurityPolicy.MaximumScanAgeSeconds` (`internal/environments/domain/configuration.go:50`), enforced by rescanning stale evidence at proposal time rather than in the clock-free `Validate`. No Space was required, which is what unpicked it from #17. |

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
| [#22](https://github.com/jonathanagustin/conduit/issues/22) | **Implemented.** The deferred trigger fired when the Ironcase adoption selected the production chart's two-replica Hub. The PostgreSQL adapter gives all replicas one append-only resolution history, refuses a split repository profile, and is covered by concurrent independent-store tests plus the real backup/restore drill. |
| [#23](https://github.com/jonathanagustin/conduit/issues/23) | [ADR 0005](../decisions/0005-apollo-coordinates-that-stay-refused.md): all twelve refused coordinates, split by what would close each, pinned to the resolver by `TestRefusedCoordinatesMatchTheDecisionRecord`. |
| [#27](https://github.com/jonathanagustin/conduit/issues/27) | **Won't do.** A per-binding portability matrix validates nothing until a workload class declares bindings, and the matrix actually needed belongs to the adopting project's runtime ADR. Re-file inside whichever future issue introduces a binding-declaring workload class. Its closure ends the Knative / `function.v1` / shared-runtime thread. |
| [#24](https://github.com/jonathanagustin/conduit/issues/24) | **Contact team optional.** The decision answered itself in the code: the profile model already permits an ownerless Environment, so the requirement was incidental to the CLI, not policy. Removed an invented constraint rather than relaxing a real one. |
| [#19](https://github.com/jonathanagustin/conduit/issues/19) | **A schedule is justified.** Answered by ADR 0006 along with #20, since both turned on A4. The crawl now runs weekly in `.github/workflows/documentation-drift.yml`; the offline 45-day freshness gate it complements is unchanged, so drift surfaces long before it can block a candidate. |
| [#28](https://github.com/jonathanagustin/conduit/issues/28) | Real per-domain and per-file efficacy baselines in `docs/operations/mutation-scores.json`, measured by a **weekly** `mutation-sweep.yml` (a timer ADR 0006 permits and A4 had forbidden) that ratchets, and guarded on every candidate by the cheap offline `verify-mutation-scores.mjs`. Weakest domain at baseline: `internal/catalog/domain`. |

The work behind the earliest closures landed on `claude/apollo-documented-tier`,
which merged as #29; everything above is now on `main`.

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
