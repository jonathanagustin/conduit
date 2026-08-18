# External audit, 2026-08-18

A record of the first review of this repository's readiness claims by people who
do not work on it, and of what the repository did about it.

This is a record rather than a defence. Both reviewers were sharply critical and
several of their findings were correct; those are written down here in their own
words. Several others were wrong, and those are written down here too, with the
code that shows it — a finding that can be refuted is as useful as one that can
be confirmed, and a repository that only records the flattering half of an audit
has not been audited.

## What was audited, and how

| | |
| --- | --- |
| Subject | [`PRODUCTION-READINESS.md`](PRODUCTION-READINESS.md), the go/no-go record, at status date 2026-08-17, trunk `d8315ed` |
| Method | The document verbatim, and nothing else. Neither reviewer had repository access, checkout, CI logs, or the ability to run a gate |
| Reviewers | Kimi K3 Max (Moonshot AI) and Grok 4.6 (xAI), asked independently, from different labs, with no knowledge of each other's answers |
| Date | 2026-08-18 |
| Verification of the findings | Done afterwards, in this repository, by a reader with the tree. Every claim below is labelled by what that verification found |

Two reviewers were used deliberately. A single reviewer's objections are hard to
separate from a single reviewer's temperament; findings both of them reached
independently, from the same text, are the ones worth acting on first. The
tiering contradiction that this audit's document changes resolve is exactly
that: both of them found it, neither knew the other had.

### What each reviewer said about its own limits

Both stated the limitation unprompted and up front, which is why their output is
usable at all. Quoted exactly.

**Kimi K3 Max:**

> I have read one self-authored document and nothing else. I cannot verify that
> any gate does what its name says; I would need the repository
> (`scripts/verify-local.sh`, gate definitions, thresholds, CI logs), not its
> description.

and, in the same section, that it could not assess the OIDC/mTLS
implementation, the Hub–Agent protocol, the chart's own pod security posture,
audit-record tamper-evidence, actual mutation scores, migration behaviour at
production table sizes, or performance at any scale.

**Grok 4.6:**

> I have no tree at `d8315ed`. Every "READY" row below is a claim I cannot
> execute. The arguments that follow are about what the document *does even if
> the gates are green*.

and:

> I cannot run `./scripts/verify-local.sh`, read `main.go:75-77`, confirm 230
> files / 1,355 tests, mutation scores, 13/13 route tests, or that hosted Trunk
> is green at `d8315ed`.

**What follows from that.** Neither audit is evidence about the code. Both are
evidence about the document's reasoning, framing and internal consistency — which
is a real thing to audit, and the thing this document is most able to get wrong
on its own. Every finding below was therefore re-checked against the tree before
being accepted, and several did not survive the check.

## Findings

Organised by what the verification found, not by how forcefully it was argued.

### Verified

Claims re-checked against the repository and confirmed.

#### V1 — The Air-gap row and gap 5 contradicted each other

Both reviewers, independently. Kimi:

> You cannot claim "validates for disconnected installation" as a readiness
> property while filing "recalls never reach disconnected targets" as a parity
> issue. Either disconnected estates are in scope — and gap 5 is Tier 1 — or they
> are not — and the Zarf row is marketing.

Grok, separately:

> Put recalls on the bundle / Target Hub path, or retract the Air gap READY row.
> You do not get both "disconnected installation" and "recall is source-side
> only."

**Verified.** In `internal/controlplane/bundles.go`, `selectResources` gathers
every live recall across desired states and hands them to `selectReleases`,
which uses them only to *exclude* a recalled Release from a new selection. No
`RECALL` resource type exists: the `addBundleResource` call sites emit
`ENVIRONMENT`, `ENVIRONMENT_REPORT`, `MODULE_RELEASE`, `TEAM` and
`PRODUCT_RELEASE` and nothing else. Roll-off *is* modelled on the connected
path — `RecallRollOff` is a field of the Plan step (`internal/delivery/plan`),
written in `internal/controlplane/plan.go` and consumed in
`internal/controlplane/lifecycle.go` — so the capability exists and the bundle
path drops it. A disconnected target installs correctly and cannot be told to
stop.

**Consequence.** For the estate the Zarf row exists to serve, the safety
response to a bad Release does not arrive.

**Changed.** Resolved in the document by narrowing the row rather than
re-tiering the gap, with the reasoning stated in place — see
[Air gap: what this claims and what it does not](PRODUCTION-READINESS.md#air-gap-what-this-claims-and-what-it-does-not).
Narrowing was chosen because the gate proves something real and narrow (the
package is well formed, digest-pinned, namespace-confined, non-root, free of
credential bytes, and requires a persistent signing key) and nothing wider;
deleting the row would have discarded a true claim, and promoting gap 5 to
Tier 1 would have decided a scope question — whether disconnected estates are
the product — that an audit response is not entitled to decide. What the
document was obliged to stop doing was asserting both sides at once, and it has.

#### V2 — The Hub starts and signs with an ephemeral key

Grok:

> **the Hub starts and signs with an ephemeral key if `BUNDLE_SIGNING_KEY_FILE`
> is missing** (`cmd/platform-delivery/main.go:75-77`). That last one is not an
> operator residual. It is fail-open cryptography.

Kimi reached the same place from the recovery side: "the recovery path silently
changing the signer converts a recoverable backup mistake into a fleet-wide
trust failure that presents as 'healthy'."

**Verified, and it is the one finding here that is about code rather than about
prose.** `cmd/platform-delivery/main.go` loads the key and, when it is absent,
emits `slog.Warn` and continues. Three details neither reviewer could have had:

- The line citation in the readiness document is **stale by thirteen lines**.
  The behaviour is at `main.go:88-100`, not `75-77`. Both audits repeated the
  wrong coordinate because the document gave it to them. That is its own small
  documentation-integrity defect and it is noted in gap 4.
- The fail-open is narrower than "always". The air-gap path already refuses it
  at the manifest layer: `scripts/verify-zarf-package.rb` aborts unless the Zarf
  runtime manifest names `BUNDLE_SIGNING_KEY_FILE`. `main.go` also refuses the
  SPIFFE configuration without a key. What is left open is the Helm and bare
  paths.
- That narrowness makes the finding worse, not better, because it means the
  binary already knows how to refuse in one configuration and does not in the
  others.

**Changed elsewhere.** Being fixed on a separate branch, in code. Not touched
here, to keep one writer per file set.

#### V3 — An exhausted notification delivery ends in a row nobody reads

Kimi:

> the dispatcher "retries with the same exponential curve and budget the
> reconciliation worker uses" and terminates in `delivery_exhausted` — a state
> visible only to someone who queries it, since "no alerting rules ship". So the
> exhausted-recall-notification path ends in a silent database row.

**Verified, and worse than stated.** There is not merely no alerting rule; there
is no metric. `internal/notifications/adapters/postgres.Store.Deliveries`
computes pending, delivered and exhausted counts and **has no production
caller** — only tests. The metrics exporter in
`internal/observability/application` publishes five series, all reconciliation
or build info: `conduit_reconciliation_pending`, `_leased`, `_exhausted`,
`_oldest_pending_seconds`, `conduit_build_info`. Nothing exports notification
delivery at all, so the alerting rule that does not ship would have nothing to
read if it did.

**Changed.** Written into gap 2, which is the row that owns it. Not filed as a
separate issue: gaps 1 and 2 are the tracker for this area and a second one
would fragment it.

#### V4 — A stored PagerDuty contact is a trap

Kimi:

> A stored PagerDuty contact that "is never a destination" is a trap, not a
> fidelity decision. […] an operator will set their PagerDuty contact during an
> incident and expect to be paged, and nothing will happen. A no-op field that
> implies paging capability is worse than no field.

Grok independently: "PagerDuty is stored and never used […] faithfully shipping
a known dead letter."

**Verified.** `domain.ContactKind.Deliverable()` returns true for `EMAIL` and
`SLACK` only. `domain.ResolveDestination` skips every non-deliverable kind, so a
principal whose only contact is PagerDuty resolves to no destination and falls
back to the operator-configured endpoint. `application.Contacts.List`
deliberately does *not* filter undeliverable kinds — with a reason given in
place, that a contact vanishing from its owner's settings page would be one they
cannot delete — and nothing on that read path marks the contact as
undeliverable. The stored value is accepted, echoed back, and silently inert.

The fidelity reasoning behind it is real and is recorded in
`docs/apollo/reference/notifications.md`: Apollo's own surface both offers a
PagerDuty contact and says PagerDuty is not supported. The audit's point is that
fidelity to a contradiction is a defensible archival choice and an indefensible
operational one, and that is correct.

**Filed.** See the issue table below.

#### V5 — Version skew and Hub self-upgrade are untested

Grok ranked it second of twelve:

> A delivery platform with no old-Agent/new-Hub test, no down migrations, and
> untested self-upgrade is not ready to be the thing that upgrades other
> software. They call this "the one a delivery platform is least entitled to get
> wrong," then do not put it in Tier 1.

**Verified.** No version-skew harness exists. The only `old-agent` string in the
tree is an Agent *identifier* in `internal/controlplane/diagnostics_test.go`,
which exercises a legacy execution phase, not a build-version boundary. Nothing
runs an old Agent against a new Hub, and nothing rehearses the Hub upgrading
itself. The document already says so, in the Test types table; the audit's
addition is that saying so in an appendix while ranking it as the thing a
delivery platform is least entitled to get wrong is not consistent.

**Filed.**

#### V6 — The Spoke agent has no privilege boundary and no readiness row

Kimi:

> The Spoke agent is an unaudited privilege concentration. The entire document
> is Hub-centric. Agents hold deploy credentials inside tenant clusters, and
> nothing is said about their least-privilege posture, their blast radius if
> compromised, or whether one tenant's compromised agent can pull another
> tenant's bundles.

Grok, independently: "No READY row on Agent privilege, Agent upgrade, or what a
compromised Spoke can do."

**Verified, and the repository is quieter than the document.**
`deploy/helm/platform-spoke/templates/` contains exactly `serviceaccount.yaml`,
`deployment.yaml` and `_helpers.tpl` — **no `Role`, `RoleBinding`,
`ClusterRole` or `ClusterRoleBinding` anywhere in the Spoke chart**. Both the
chart's ServiceAccount and its pod set `automountServiceAccountToken: true`, as
does the Zarf runtime manifest's agent pod. The one RBAC file in `deploy/`
(`deploy/flux/runtime-rbac.yaml`) scopes the *Flux runtime reconciler*, not the
agent. The agent drives Helm against tenant clusters
(`internal/agent/helm.go`), so its effective privilege is whatever the tenant
binds to that ServiceAccount — a decision the repository neither states,
constrains, nor tests, for the one component that mutates a tenant.

**Filed.**

#### V7 — The residual list fails its own admission criterion

Kimi:

> The preamble: "decisions or actions only a human operator can take; nothing in
> this repository can complete them." Item 3, **Renovate activation**, is a
> five-minute GitHub App install by the *repository admins* — it is the authors'
> undone backlog item wearing an operator costume; the deploying operator cannot
> install an app on the authors' repo.

Grok reached it separately: "'Nothing in this repository can complete them' is
false for 3 and 5. Those are choices."

**Verified for item 3.** `renovate.json` is committed and gated (`Validate
Renovate configuration` runs `jq --exit-status . renovate.json`). What remains
is installing a GitHub App on `jonathanagustin/conduit` — an act available only
to this repository's own admins, and unavailable to the deploying operator the
residual list addresses. Two different parties are being called "a human
operator", and the list's criterion does not distinguish them.

**Partly verified for item 5.** Kimi's split is right: the backup *schedule* is
genuinely the deployment's, and the signing-key *semantics* are the
repository's. The second half is V2 and is being fixed in code, which is itself
the demonstration that it was never an operator residual.

**Filed** (item 3 and the criterion). Item 5's key half is already covered by
the signing-key work.

#### V8 — The Supply chain row cited out-of-scope evidence

Grok:

> The evidence file named in that row is
> `docs/evidence/highs-1.15.1-shim-candidate-amd64.json` — the UNADMITTED
> optimizer candidate. That is the wrong artifact to cite for production image
> hygiene, or it is the only artifact they have. Either way the row is
> contaminated.

**Verified.** That file carries `"admission": "UNADMITTED"`, and the same
document's *Explicitly out of production scope* section excludes the HiGHS
candidate from production. A READY row was resting its image-hygiene half on
evidence about an image the document says production does not run. Grok's
either/or is also answerable: it is the second branch. No committed evidence
file covers the production Hub or Agent images, because that evidence is
digest-bound and generated at publication by
`.github/workflows/platform-images.yml`.

**Changed.** The row now cites the publication step and states that this half is
proven per-publication rather than per-commit, and records what it used to cite
and why that was wrong.

#### V9 — The audit trail has no tamper-evidence at rest and no retention

Kimi:

> nothing states audit records are append-only or tamper-evident, nothing states
> retention, nothing states who can read them. An audit log that a DBA can
> silently rewrite is decorative in an investigation.

**Partly verified; see R2 for the parts that are refuted.** What survives
verification is the tamper-evidence half, and it is worse than "nothing states".
The PostgreSQL control-plane repository keeps its entire metadata state —
authorization audit records included — in **one row**:

```
CREATE TABLE IF NOT EXISTS conduit_repository_state (
    singleton boolean PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    generation bigint NOT NULL,
    state jsonb NOT NULL
);
```

Audit events are a map inside that `jsonb`, rewritten in full on every
control-plane write. There is no hash chain, no signature, no separate
append-only table, and no write-once storage, so anyone with database access can
edit an audit record with an ordinary `UPDATE` and leave no artefact behind.
Nothing in `internal/` implements retention or pruning either, in any direction:
records accumulate in that single row indefinitely, which is a growth problem as
well as a policy silence.

**Filed**, narrowed to the two halves that survived.

#### V10 — The Correctness row's gate count had drifted

Not a finding either reviewer could reach — both said outright that they could
not run `verify-local.sh` — but found while verifying the ones they did reach,
and it belongs here because it is the exact failure mode Kimi's closing
paragraph predicts for a document whose rows are enforced only by their authors:

> "Treat a stale row exactly like a failing gate" has no enforcement mechanism
> named other than the same people who write the rows.

**Verified.** The Correctness row read "All 30 local gates pass at the trunk
tip". `./scripts/verify-local.sh` on this branch ran **35**. Five gates were
added and the row was not updated, which is precisely what its own closing
sentence forbids.

**Changed, and changed so it cannot recur.** The count is removed rather than
corrected. `verify-local.sh` already reconciles itself against CI — it reads
step names out of `.github/workflows/platform-ci.yml` and exits non-zero when
"ci.yml runs steps this script does not", unless the step is listed in
`not_run_as_written` with a reason. The set of gates is therefore enforced by a
mechanism; a number in a table is a second, unenforced copy of the same fact,
and it was the copy that rotted. The row now cites the mechanism.

### Plausible and unverified

Findings not contradicted by anything checked, and not settled by anything
checkable within this change. Recorded so a later reader can see they were
weighed rather than dropped.

| Finding | Reviewer | Why it is unsettled |
| --- | --- | --- |
| "The document grades **components**, and production failures are **scenarios**" — the assembled 2 AM CVE walk in which each row reads as honestly bounded and the mission still fails end to end | Kimi §1 | A claim about the document's *shape*. No gate can settle it, and no rewrite here would test it. It is the sharpest paragraph in either audit and it stands unanswered |
| Route × role authorization is unenumerated, and the fail-closed default for an unregistered route is unstated | Both; ranked first by Kimi, eighth by Grok | Being built on a separate branch. Not duplicated here, and not refuted — the document's own "a new route defaults to untested" is accurate |
| Cross-tenant isolation of core resources (Releases, Plans, Environments, approvals) is never claimed | Kimi | Accurate about the document. Whether the code isolates them is the same question the route × role matrix answers, so it is deferred to that work rather than guessed at here |
| Gap 6, "not representable", is allow-by-omission: the control loop can issue a Plan that un-reads a disk | Grok #4 | The document says the constraint cannot be expressed. Nothing checked contradicts that, and the coarse refusal Grok proposes is a design decision, not a correction |
| The `internal/controlplane` line ceiling is steering the product — three named events do not emit because the package cannot grow | Grok #2 | The ceiling and its gate are real (`docs/operations/controlplane-ceiling.json`, `Validate controlplane ceiling`), and the document itself gives the ceiling as the reason. That it is the *cause* rather than the occasion is a claim about the authors' choices |
| OIDC/mTLS validation correctness; Hub–Agent protocol soundness; migration cost at production table sizes; behaviour at any scale | Both, explicitly | Neither reviewer could assess these and this verification did not either. They remain open in the same state the document already puts them |
| The whole edifice is self-attestation with no external review sought | Kimi | True as a description of the repository before 2026-08-18, and only partly less true now. This record is not a substitute for a commissioned security review of the Hub–Agent trust boundary |

### Refuted

Findings checked against the code and found wrong. The reviewers had no way to
check them; that is the point of writing the refutations down rather than
quietly ignoring them.

#### R1 — "Approvals have no stated integrity properties"

Kimi:

> The system models approvals, promotions, and change requests; the document
> never says whether a principal can approve their own change request, whether
> quorum or segregation-of-duties exists, or whether promotion paths (staging
> before production) are enforced by the Hub or by convention. For regulated
> tenants this is table stakes, and the silence suggests the approval model was
> built as workflow, not as control.

**Refuted as a claim about the system. The silence is the document's, not the
code's, and the inference drawn from it is wrong on every clause.**

`GovernanceService.approveOnce` in `internal/controlplane/governance.go`:

- **Self-approval is refused, and refused twice.** It computes
  `authorApproving := governance.SameHuman(principal, candidate.Author)` and
  skips every requirement for the author, leaving no eligible requirement and a
  named refusal, "the author of a change request cannot approve it". The same
  rule is re-asserted *inside* the serialized `UpdateChangeRequest` callback,
  against the stored record rather than the copy read before it, with a comment
  saying it is there for a future mutation path rather than for today.
- **The rule was written as a correction, and says so.** The comment above it
  records the defect it fixes: eligibility used to be "is this principal in the
  required team", the author usually *is* in that team, and the only identity
  guard was a duplicate-review check that "never stopped the author approving
  once". It also records why it is not configurable: a deployment wanting no
  independent review raises no requirement, whereas a self-approval switch would
  let a reviewer requirement "appear to exist while being void".
- **Segregation of duties is stronger than username equality.**
  `governance.SameHuman` compares identity *sets* through `EffectivePrincipal`,
  so a service account acting on behalf of the author is the author, in both
  directions of the comparison — the delegation hole is closed deliberately and
  documented as the conservative direction.
- **Quorum is requirement-shaped and enforced.** Each requirement is checked by
  `requirementSatisfied`, satisfied requirements accumulate `SatisfiedBy`, a
  duplicate reviewer is refused with `ErrConflict`, and the request reaches
  `ChangeApproved` only when `allRequirementsSatisfied` holds and no
  do-not-merge label blocks it.
- **Refusals are audited.** The deny path files a `CHANGE_REQUEST_APPROVE` /
  `DENY` record with the reason, explicitly so an operator can find out which
  rule stopped them.
- **Promotion order is enforced by the Hub, not by convention.**
  `Lifecycle.EvaluatePromotion` in `internal/controlplane/promotion.go` refuses
  a Release "not reachable on source channel", refuses a promotion that "would
  create a channel cycle", and refuses outright when the stage control is
  `PromotionStagePaused`.
- **One exemption exists and is reasoned in place.** The
  `OPTIMIZER_REVALIDATION` requirement is exempt from the author rule because it
  is unconditionally appended by the planner, waivable by no policy, and asks
  for machine-verified evidence freshness rather than for a second party.

What survives is the documentation gap Kimi actually observed: the readiness
document says none of this. That is worth fixing and is not a control gap, so no
issue was filed for a missing control that is present. This entry is the record.

#### R2 — "Nothing states who may read the audit trail, or that it is append-only"

**Refuted in both halves; the surviving half is V9.**

- **Read authorization exists and is two-stage.**
  `GET /v1/authorization-audit` in `internal/controlplane/http.go` requires
  `requireOperatorPrincipal` *and then* `requireGovernanceAdministrator`. It is
  not readable by an ordinary authenticated principal.
- **The write path is append-only at the application layer.**
  `AppendAuthorizationAudit` validates the `audit.3` category contract — unknown
  categories fail closed rather than "silently acquiring invented semantics" —
  and on a duplicate identifier **returns the existing record instead of
  overwriting it**. The repository interface exposes no update and no delete for
  audit events, in any of the three implementations.

What Kimi's sentence gets right is the part about the DBA, and only that part:
append-only at the application layer is not tamper-evidence at rest. That
narrowed claim is V9 and is filed.

#### R3 — "The Mutation row cites 'committed scores' without stating them"

Kimi, offering it as an instance of a pattern: "existence of a mechanism offered
as evidence of a property".

**Refuted.** `docs/operations/mutation-scores.json` states them: efficacy,
killed, lived, total and line coverage for five Go bounded-context domains and
three console source files, with a capture date, the generating command, and an
explicit ratchet — "an entry may only improve". Two separate gates enforce it,
`Validate mutation harness` (which proves the harness kills a known planted
mutant, i.e. that the mechanism is not decorative) and `Validate mutation
scores`. The row citing a committed file rather than inlining eight numbers is a
formatting choice. This is the opposite of the pattern it was offered as an
instance of.

#### R4 — "Observability is listed READY and again as Tier 1 gap 2"

Grok, as an instance of the READY table violating its own rule.

**Refuted as an assertion contradiction; accepted as a titling defect.** The
row's Claim never asserted tracing, alerting, or a default-on endpoint — it says
"on an opt-in listener" in the same sentence, and names exactly the four gauges
plus build info that gap 2 concedes exist. The word doing the overclaiming was
the *Area* title, "Observability", which is a larger word than the claim
underneath it.

Unlike V1, there was nothing here for the tiering to hide: both statements were
true and compatible. The title has been narrowed to "Metrics exposition" and the
two are now cross-referenced, because a title that invites a false reading is
still a defect even when the sentence under it is honest.

#### R5 — "Disaster recovery is READY, 'bounded by gap 4', which was then removed from Tier 1"

Grok, as a second instance of the same rule violation.

**Refuted.** The bound is stated inline, in the row, in the same cell as the
evidence, and gap 4's remaining text is still in the document under its own
number. This is the document disclosing a limit on a claim, which is the
behaviour the READY table's rule is *for*. A return reference has been added
from gap 4 so the pair cannot drift apart, but no claim was withdrawn because
none was overstated.

## What changed

In [`PRODUCTION-READINESS.md`](PRODUCTION-READINESS.md), in this change:

| Change | Finding |
| --- | --- |
| `Air gap` row renamed **Air-gap packaging**, narrowed to what the gate proves, and bounded by gap 5 with a two-way cross-reference | V1 |
| New section, *Air gap: what this claims and what it does not*, stating the contradiction, the resolution, and the reasoning for narrowing rather than re-tiering | V1 |
| Gap 5 gains a return reference to that row, plus what recalls *do* reach on the bundle path (selection) and what they do not (roll-off) | V1 |
| `Availability` row renamed **Concurrency and replica correctness** and bounded by the two Missing rows in Test types; those rows now name it back | Kimi §2, "Row titles overclaim their evidence" |
| `Observability` row renamed **Metrics exposition** and bounded by gap 2; gap 2 gains the return reference | R4 |
| Gap 2 gains the notification-delivery silence: `delivery_exhausted` has a reader with no production caller, no metric, and therefore nothing for an alert to read | V3 |
| Gap 4 gains a return reference to the Disaster recovery row, and the note that its `main.go:75-77` citation is stale by thirteen lines | V2, R5 |
| `Supply chain` row stops citing the `UNADMITTED` HiGHS candidate for production image hygiene, and cites the publication step that actually produces it | V8 |
| `Correctness` row stops restating a gate count that had drifted from 30 to 35, and cites the reconciliation in `verify-local.sh` that already enforces the set | V10 |
| Status date line points here | — |
| A stated **drift rule**: a READY row and a gap that speak about the same estate, resource or failure must cite each other by number, in both directions, in the change that creates either | V1 |

The drift rule is the durable part. The Air-gap row and gap 5 diverged because
the reference ran one way only: gap 5 never named the row it undercut, so
editing either left the other unreviewed. Two-way citation is what makes the
next such pair visible to the next reader.

## What did not change, and why

- **The signing key.** V2 is real and is the highest-value item in either audit,
  and it is being fixed in code on a separate branch. Documenting it again here
  would have been the exact move both reviewers criticised.
- **Route × role authorization.** Ranked first by Kimi and eighth by Grok, and
  being built on a separate branch. Not duplicated, not restated as a document
  change, and not closed here.
- **Gap 5 stayed in Tier 2.** Re-tiering it would answer "are disconnected
  estates the product?", which is a scope decision for the project rather than a
  finding for an audit response. Narrowing the row removes the contradiction
  without pretending to have made that decision. If the decision is later made
  the other way, the two-way reference is what will make both places move
  together.
- **The residual list's preamble.** V7 is verified and the fix is a
  categorisation decision — does Renovate belong in the authors' backlog, or does
  the criterion need to name two different humans? — so it is filed for a
  decision rather than made unilaterally.
- **No new READY row was added for approvals or promotions,** even though R1
  found real controls the document does not mention. A row must cite a passing
  gate, and inventing rows in the same change that narrows three others would
  make this change harder to read as what it is. The controls are recorded here.
- **Contact deletion, and the absence of an administrator override.** Grok is
  right that a hard `DELETE` with no break-glass loses offboarding, typo
  recovery and incident response, and the document already discloses both the
  hard delete and the missing override, with the personal-data reasoning and the
  tests that hold them down. It is a live disagreement about a reasoned
  decision, not an undisclosed gap, so it is recorded rather than reversed.
- **The document's candour.** Nothing was softened. The audits were possible
  only because the document says things like "'told' means 'recorded where the
  recipient can look'" and "a new route defaults to untested" about itself. A
  readiness record that reads better to an auditor is worth less to an operator.

## Issues filed

Substantive findings not fixed in this change, one issue per finding. Each
carries the reviewer's claim quoted, what verification found, the concrete
consequence, and what would close it.

| Issue | Finding | Reviewer | Verification outcome |
| --- | --- | --- | --- |
| [#261](https://github.com/jonathanagustin/conduit/issues/261) | Audit trail has no tamper-evidence at rest and no retention policy | Kimi | V9 verified; the read-authorization and append-only halves of the same claim are refuted in the issue body (R2) |
| [#262](https://github.com/jonathanagustin/conduit/issues/262) | A stored PagerDuty contact is accepted, listed back, and never paged | Both | V4 verified |
| [#263](https://github.com/jonathanagustin/conduit/issues/263) | No version-skew test and no Hub self-upgrade rehearsal | Both; Grok ranked it second | V5 verified; the "no down migrations" clause is refuted in the issue body as a recorded decision rather than an omission |
| [#264](https://github.com/jonathanagustin/conduit/issues/264) | The Spoke agent mutates tenant clusters, ships no RBAC, has no readiness row | Both | V6 verified; Zarf-path pod hardening is refuted as a substitute for it |
| [#265](https://github.com/jonathanagustin/conduit/issues/265) | The residual list's admission criterion is false for Renovate, and conflates two different humans | Both | V7 verified for items 3 and 5; items 1, 2 and 4 refuted as honest boundaries |

Each issue quotes the reviewer, states what verification found — including the
parts of the same finding that did not survive — names the consequence, and says
what would close it.

Candidates considered and **not** filed:

| Candidate | Why not |
| --- | --- |
| Approval integrity — self-approval, segregation of duties, quorum, promotion order | Refuted; the controls exist and are enforced by the Hub. See R1 |
| Audit trail read authorization and append-only writes | Refuted; both exist. See R2. Only the tamper-evidence and retention halves were filed |
| `delivery_exhausted` has no metric and no alert | Gaps 1 and 2 in the readiness document are the tracker for this area; recorded there rather than fragmented into a separate issue |
| Signing key, route × role authorization | Being fixed on separate branches |
