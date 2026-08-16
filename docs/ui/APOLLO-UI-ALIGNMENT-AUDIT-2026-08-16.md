# Apollo UI alignment audit

Status: bounded evidence record, 2026-08-16.

How far the Conduit console's composition tracks the recovered Apollo visual
evidence, what is deliberately unaligned, and where the gap is a real one. Read
with `HISTORICAL-APOLLO-EVIDENCE-2020-2022.md`, which bounds what the 2020
images may be used for, and with `docs/operations/TENANT-FIDELITY.md`.

## What the evidence is

Two bodies, different in kind and in force.

| Evidence | Form | Era |
| --- | --- | --- |
| `evidence/legacy-apollo-2020/{product-rollout,environment-events}.webp` | Two committed screenshots, viewed directly for this audit | 2020 |
| `upstream-screen-evidence.tsv` | 29 captures recorded as URL + SHA-256 + alt text; the images are not committed | Current docs |

The 2020 pair is the weaker source and the one most likely to be over-read.
`HISTORICAL-APOLLO-EVIDENCE-2020-2022.md` already states the bound, and this
audit does not reopen it:

> They do **not** justify copying the obsolete navigation, labels, private
> routes, dark-only palette, or visual table semantics.

The article's own editor note says the suite has since changed. So "align with
the images" cannot mean visual alignment with those two; what they legitimately
supply is **composition**. The 29 current-era captures are the stronger target
and are audited separately below.

## Part 1 — composition against the 2020 screenshots

Each row is a pattern visible in one or both images, against what the console
does today.

| Pattern | State | Where |
| --- | --- | --- |
| Compact status pills as the headline | Aligned | `SummaryBar.tsx` — total, per-health buckets, and an always-present STALE count |
| Dense table with per-column status glyphs | Aligned | `FleetPage` — Environment, Entity, Product, Desired, Observed, Health, Rollout, Config, Lifecycle |
| Resource-scoped tabs | Aligned | `EnvironmentDetailPage` — Overview, Plans, Commands |
| Server bounds on every large index | Aligned | `IndexPagination`, `useIndexQuery`, `contracts/pagination.md` |
| Selectable transition carrying promotion constraints | Partial | `ReleaseChannelGraph` renders selectable transitions with status and a blocker count, but from `/v1/promotions`; the dedicated `promotion-stages` endpoint is unread |
| Fixed summary pane beside a tabbed detail region | Absent | The console uses a horizontal `ObjectHeader`; no persistent side summary |
| Selected row opens an in-context detail region | Absent | The console navigates to a separate route. Import, Export and Risk reveal detail from the already-loaded row, which is the nearest equivalent |
| Faceted filters with counts and proportional bars | Absent | No `facet` symbol anywhere under `web/src/features`. Fleet has search and selects, but no counts |
| Event timeline paired with the event table | Absent | Only `PlanDetailPage` carries any timeline; there is no event timeline |
| Stacked version-distribution bar with a percentage | Absent | `SummaryBar` is a tag strip, not proportional |

Four of the ten absences are worth different treatment. The **dark-only palette,
the 2020 navigation, and its table semantics** are out of bounds by the rule
above and should stay unaligned. The **event timeline** has no endpoint to feed
it, so building one would mean inventing the data. **Faceted counts** are the one
2020 pattern that is genuinely good, genuinely absent, and blocked only by the
Hub not returning counts.

## Part 2 — the sharper finding: served but unsurfaced

The Hub registers **45 `GET /v1/*` routes**. The console reads **18**. The
remaining 27 are not a styling question — they are coordinates the Hub can
already answer and the UI does not ask for. Several are exactly the screens the
capture evidence documents.

| Unread endpoint | Upstream capture |
| --- | --- |
| `/v1/modules`, `/v1/modules/{name}/releases`, `/v1/modules/{module}/auto-update-policy`, `/v1/module-installations/{id}/update` | **266** Contents, **268** Installations, **270** Overview — three Module screens; the console has no Modules route at all |
| `/v1/environments/{id}/suppressions` | Suppressions is a tab in the 2020 Environment view |
| `/v1/products/{product}/releases/{version}` | **306** — the Release Overview tab; no Release detail route exists |
| `/v1/products/{product}/promotion-stages/{from}/{to}` | **314** — per-transition promotion constraints |
| `/v1/resource-grants`, `/v1/authorization-audit` | **353** Edit team roles, **356** Team settings |
| `/v1/labels`, `/v1/labels/{id}`, `/v1/labels/{id}/revisions` | **338** — label requirements in the manual-promotion sidebar |
| `/v1/approval-policies` | The governance surface behind Change Requests |
| `/v1/environments/{id}/` `profile`, `profile/revisions`, `health-targets`, `reported-state`, `target-hub-versions`, `module-installations` | **194** — metadata updates in the Environment Overview |
| `/v1/promotions/{id}`, `/v1/products/{product}/publications`, `/v1/export-pipelines/{id}/schedule-runs`, `/v1/bundles/{id}` | — |

This matters more than the 2020 styling, and it is the same discipline the
Apollo GraphQL interface already applies: MAPPED-or-REFUSED. Where the Hub holds
no data, an absent screen is the honest outcome. Here the data exists and the
console simply does not read it.

### Vulnerability detail: served, and the cheapest gap here

An earlier revision of this audit called captures **367** and **368**
"correctly absent — no endpoint serves them." **That was wrong in both cases**,
and wrong in the direction this repository most guards against: it read a screen
that was missing and concluded the data was missing, without grepping. Conduit
runs Grype, and the findings are domain data.

`internal/grypescanner` adapts Grype to the Hub's `ArtifactScanner` contract,
`cmd/platform-grype-scanner` is its binary, and `POST /v1/artifact-scans`
ingests the result. What lands on a Release
(`internal/catalog/domain/release.go:55`) is not a summary:

| Field | Content |
| --- | --- |
| `ActiveFindings` | **Per-CVE** detail — `id`, `cvss`, `severity`. Its comment: "what makes a Release's risk score computable, so a scanner that reports only counts cannot authorize an upgrade" |
| `SuppressedFindingDetails` | Per-finding suppression provenance — scope, category, rationale, `requestedBy`, `approvedBy`, `changeRequestId`, `validUntil` |
| `GracePeriodFindings`, `Provider`, `RunID`, `AssessedAt` | Scan identity and the freshness input for `MaximumScanAgeSeconds` |

All of it is served on `GET /v1/products/{product}/releases` — **an endpoint the
console already fetches**. What the console renders from it is
`AssessmentTag` (`ProductsPage.tsx:206`), whose props are exactly
`{ outcome, activeVulnerabilities }`: one tag and one integer. Every per-CVE
row, every suppression with its approval trail, and the scan's provenance are
already in the browser's hands and thrown away.

That makes **367** (a Vulnerabilities inventory) the cheapest real gap in this
audit — no new endpoint, no new contract, a projection over a payload already
fetched. **360** ("an active vulnerability is shown in the Security tab") is the
same data at Release scope.

**368** is also served, with a caveat worth stating precisely. The scores are
`UpgradeRationale.CurrentRiskScore` / `TargetRiskScore`
(`internal/delivery/plan/plan.go:92-93`), set at `internal/controlplane/risk.go:245`
and carried on a **plan step**, not on the Plan. They are **conditional**:
`UpgradeRationale`'s own comment says "its presence is the evidence that the
narrower selection path ran", so they exist only for upgrades chosen through the
vulnerability-recall exception. The console's `PlanStep` interface does not carry
them. A Plan detail view can show them honestly — as evidence that appears when
that path ran and is absent otherwise — but must not imply every Plan has a
score.

`ReleaseRiskScore` (`internal/controlplane/risk.go:89`) already grades a Release
0–10 from the unsuppressed findings, so the scale a UI would render is the Hub's,
not a new invention.

### What the public Tenable/Nessus client establishes, and the trap it exposes

`VulnerabilityAssessment` names Tenable.sc as a normalizing provider alongside
OCI and SBOM scanners, so the public `palantir/tenablesc-client` was read
directly (HEAD `dc63ef0`; `security-scanning.md` pins its review at `35e868a`,
so that note is a commit behind).

That note describes the client as the `AgentScan` API for "Nessus Agent host
scan definitions and Agent Groups". **It is substantially more than that.**
`tenablesc/analysis.go` wraps the Tenable.sc Analysis API and `VulnDetailsResult`
carries genuine per-finding detail — `cve`, `cvssV3BaseScore`,
`cvssV3TemporalScore`, `cvssV3Vector`, `pluginID`, `pluginName`, `severity`,
`stigSeverity` — with `scanresult.go` alongside it.

The note's **conclusion still holds, and this strengthens it**: every result type
is keyed to a *host*, not an artifact. `VulnSumIPResult` and
`VulnSumDNSNameResult` key on `ip`, `dnsName`, `macAddress`, `netbiosName`,
`osCPE` and a `repository`; `VulnHosts` and `VulnIPSummaryResult` are the same
axis. Nothing binds a finding to an immutable OCI digest. Conduit's assessment is
exact-artifact by construction (`ArtifactDigests`, digest-coverage validation),
so this is a different axis rather than a competing contract — useful
deployment-side scanner administration, not an ArtifactScan result boundary.

**The trap is in the severity vocabulary, and it bears directly on the
Vulnerabilities view recommended above.** Tenable publishes
Critical / High / Medium / Low / Info, as `BaseInfo` in detail results and as
`severityCritical`…`severityInfo` counts in summaries. Conduit's
`VulnerabilityFinding.Severity` is an **unconstrained `string` with no Go const
set anywhere**; its only interpretation is the switch in
`internal/controlplane/risk.go:113`, which accepts `CRITICAL`, `HIGH`,
`MEDIUM`/`MODERATE`, `LOW`, `NONE`/`INFORMATIONAL`/`INFO`, and falls through to
an unknown tier. Two consequences for the UI:

1. **A severity → colour map would be ungateable.**
   `scripts/verify-console-contract.mjs` extracts the Hub set from *named Go
   consts*, and there are none here. A hand-written severity map is exactly the
   shape that drifted six times before the gate existed, and the gate could not
   catch a seventh.
2. **Colouring on severity would contradict the Hub's own ranking.**
   `findingScore` uses `CVSS` whenever it is `> 0` and consults `Severity` only
   as the fallback. A view ranked or coloured by the severity string can order
   findings differently from how the Hub scores the very same Release.

So the Vulnerabilities view should rank and colour on **CVSS**, which is numeric
and bounded 0–10 by `RiskScoreMaximum`, and render `Severity` as the
provider-supplied text it is. If it must bucket at all, it should mirror
`findingScore`'s arms exactly — including the `MODERATE`, `INFORMATIONAL` and
`INFO` aliases that exist because providers disagree — rather than adopt
Tenable's five-value set, which is one provider's vocabulary and not Conduit's.

Syft appears only in `.github/workflows/*` and `scripts/*` — the optimizer
candidate build and release path, where it produces SBOMs committed as evidence
under `docs/evidence/`. There is **no SBOM domain type and no Hub endpoint**; the
single mention under `internal/` is a comment on `VulnerabilityAssessment`
noting that SBOM scanners are one *provider class* normalizing into that record.
So syft is build-time supply-chain evidence, not a runtime contract, and it
implies no console surface. Recorded so the next reader does not go looking for
an SBOM screen and mistake its absence for a gap.

## Part 3 — a defect found while auditing

`web/src/features/environments/EnvironmentDetailPage.tsx` claims its tab set is
evidence-backed:

> The tab set is the upstream one, evidenced by captures 070 (Overview), 069
> (Changes) and 066 (Commands) in docs/ui/upstream-screen-evidence.tsv.

**None of 066, 069 or 070 exist in that file.** The captures that carry those
descriptions are **206** ("The Environment Overview tab is displayed"), **200**
("Changes Tab") and **157** ("The Commands tab displays all commands that were
executed for this Entity"). The same file cites 206 correctly seventy lines
later, so the stale identifiers are a leftover from an earlier numbering rather
than a different claim. Corrected in the same change as this audit.

The comment is also right about something the console does not do. Upstream's
Environment tab set is Overview / **Changes** / Commands. The console ships
Overview / **Plans** / Commands. Plans is independently evidenced (capture 058,
an Environment-level Plans view), so it belongs — but **Changes is absent**,
while `/v1/change-requests` exists and is already read for the global index. The
gap is scoping those to an Environment, not new data.

## Ranked recommendations

1. **Vulnerability detail** (captures 367, 360). The cheapest by a wide margin:
   the per-CVE findings and their suppression provenance are already in the
   payload `ProductDetailPage` fetches, and the console discards all but a tag
   and a count. No endpoint, no contract change. **Rank and colour on `CVSS`,
   not on `Severity`** — see the Tenable section above; severity is an
   unconstrained string the contract gate cannot guard, and the Hub itself
   prefers CVSS whenever it is present.
2. **Modules.** Three upstream screens, four live endpoints, no UI. The largest
   single gap, and the one least dependent on judgement.
3. **The Environment Changes tab**, plus the citation fix. Both small; the data
   is already read elsewhere.
4. **A Release detail route** (`/products/:product/releases/:version`) for
   capture 306, whose endpoint is live and unread. Natural home for the
   Release-scoped Security tab in 360.
5. **Plan risk scores** (capture 368) on `PlanStep`, rendered as conditional
   evidence rather than a field every Plan has.
6. **Faceted filter counts.** Needs `/v1/fleet/installations` to return counts
   before the UI can show them, so it is a contract change first.
7. **Leave alone:** the dark-only palette, the 2020 navigation and labels, an
   event timeline, and any SBOM screen. The first two are out of bounds by the
   rule this document opens with; the third has no data behind it; the fourth
   has no contract, only CI artifacts.

## Method

Both `.webp` files were viewed directly rather than read through the existing
prose summary. Console composition was read from `web/src/features/**` and
`web/src/components/**`. The route surface was extracted from
`internal/controlplane/http.go` and diffed against the endpoints reached from
`web/src/api/*.ts`.

One correction is recorded rather than quietly fixed, because the mistake is the
instructive part. The first revision called captures 367 and 368 "correctly
absent — no endpoint serves them", reasoning from the absence of a *screen* to
the absence of *data*. Both were wrong: `grep -rn "VulnerabilityFinding"` and
`grep -rn "RiskScore"` settle it in one command each. This is the same failure
`APOLLO-WORK-QUEUE.md` records five times over — reasoning from plausibility
instead of grepping — and it is worth noting that it recurs even in an audit
whose whole purpose is to check claims against source. An absent screen is
evidence about the UI and nothing else; only the Go source is evidence about
what the Hub can answer.
