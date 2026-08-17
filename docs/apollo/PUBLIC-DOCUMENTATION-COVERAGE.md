# Apollo public-documentation coverage

This is the evidence index for the generic delivery platform. It records which
public Apollo contracts were reviewed, what those contracts imply, and where
our implementation intentionally differs. It does **not** copy Palantir's
documentation or claim private Apollo API compatibility.

## Snapshot

Re-crawled from the official Palantir Apollo documentation on **2026-08-15
UTC**, superseding the 2026-08-01 capture. The crawler discovers pages from the
current Documentation, Getting started, Reference, and What's New navigation
trees, then extracts the source Markdown embedded in each rendered page only for
local analysis.

| Measure | 2026-08-01 | 2026-08-15 |
| --- | ---: | ---: |
| Distinct public pages | 241 | **262** |
| Successful fetches | 241 | 262 |
| Failed fetches | 0 | 0 |
| Source Markdown words reviewed | 144,521 | **157,244** |
| Headings reviewed | 1,562 | **1,710** |

[`public-documentation-manifest.tsv`](public-documentation-manifest.tsv)
contains the URL, section, title, word count, heading count, and SHA-256 of the
Markdown for every page. It is sorted by URL so future captures produce a
reviewable drift diff. The source bodies are deliberately not committed.

### What the re-crawl found

Twenty-one pages were added and none removed. Eight changed.

Seven of the twenty-one were never a drift at all: they had been published all
along and the crawler could not see them. It read `href` attributes from four
seed pages, which is the rendered sidebar, so it saw exactly the pages the
navigation chooses to show. The seven `hidden_`-prefixed Apollo Product
Specification pages -- Endpoints, Liveness and Readiness, Replication,
Resources, Volumes, the Substitution Reference, and the full spec definition --
are excluded from that sidebar and reachable only through links in other pages'
body text. The crawler now scans whole documents and follows what it finds to
closure; both it and the drift gate were fixed together, because a gate built on
the narrower discovery would have called the incomplete corpus complete.

These seven turned out to be a **provenance gap rather than an implementation
gap**: `internal/controlplane/apollo_product_definition.go` already implements
endpoints, `sls-status-endpoint`, liveness and readiness paths, volumes,
resources and replication, from the CLI fixtures. What was missing was any
citable source, which also meant a `documentedBy` citation naming one of these
pages would have been rejected as an unknown URL. They are cited there now.

The other fourteen are genuine additions, all `apollo-cli`: `cve list`,
`entity get`, `environment setting` (with `edit` and `get`), `target-hub` (with
`get`, `list`, `pipeline`, `pipeline list`), and `terminal grant`/`policy` (with
their `list` forms). All are Experimental, and all are client surfaces over
contracts this repository already implements.

One content change is a contract change and is recorded in the capability map:
**Apollo now publishes five Environment connection settings, not four.**
`managing-environments/environment-connection-settings` grew from 444 to 584
words and gained a heading for **Intermittent agents**. The operational half of
that setting is now implemented (issue #15): an intermittent Environment issues
Plans, accepts agents, tracks health and promotes, exactly as the page
publishes. The one published difference — that its expected gaps are not an
outage — is carried as `Federation.SilenceIsExpected` and consumed by the
reporting fleet projection on both the console overview and the fleet API.
The promotion-evidence half is **not** implemented: `ValidateEvidence` rejects
evidence older than `MaximumEvidenceAgeSeconds` with no connection-setting
exemption, so an intermittent Environment's "as of last connection" health is
still refused by any age-bounded policy. That remains open on issue #65, and it
is a deliberate behavioural choice rather than plumbing -- the operator's stated
bound must not be exempted outright; what the published rule changes is the
default reading of silence.

The predicate was named `Federation.ExpectsContinuousReporting` here and in the
code comments. No such method exists; the real one is the inverted
`SilenceIsExpected`, and it is used rather than unused.

The other changes are informational: five `apollo-cli` parent pages gained
subcommand links, `core/whats-new` is a changelog (mined below), and
`managing-modules/update-module-installation` lost a section (460 → 303 words),
which narrows what Apollo publishes rather than what this repository must do.

### Maturity of the corpus

Not every published page is a commitment. Scanning all 262 for upstream markers:

| Marker | Pages | Where |
| --- | ---: | --- |
| `[Beta]` | 8 | every one in `apollo-product-specification` |
| `[Experimental]` | 60 | 55 `apollo-cli` pages plus 5 others |
| `[Deprecated]` | 62 | mostly `apollo-cli` command pages naming a deprecated form |

The Beta set is the one that reaches a served claim. **Two DOCUMENTED
coordinates in the Apollo-shaped GraphQL interface -- `ApolloEnvironment.health`
and `ApolloEntity.health` -- cite `apollo-product-specification/health/`, whose
own callout says the extension "may not be available on your Apollo Hub".** That
does not make the citation wrong, but it is weaker than a GA citation in a
stated way, and the DOCUMENTED tier alone does not express the difference.
`scripts/apollo-graphql/verify-apollo-interface.mjs` now reads maturity from the
manifest and discloses those citations by name on every run; it does not fail on
them, because citing Beta documentation is honest and citing it silently is not.
`scripts/verify-apollo-interface.test.sh` asserts the disclosure, so it cannot
be dropped without a test failing.

The other six Beta pages are the `hidden_*` specification extensions above,
whose contracts Conduit implements. Their Beta status bounds that claim rather
than voiding it.

### Releases and announcements

`core/whats-new` is the largest page in the corpus at 16,255 words, and it had
been captured since 2026-08-01 without ever being read. It is a dated changelog
of Apollo releases -- roughly one entry per CLI version -- and so it is the
densest contract-bearing page there is. Mining it produced two open items:

- **PH-7.** 2026-06-11 announces a constraint enforcing that a Release has
  passed a security scan before it can be installed, controlled by a Space
  setting. Conduit scans, scores risk, and gates the CVE-recall exception on
  scan freshness, but nothing blocks an install because a scan has not happened.
- **PH-8.** CLI 0.811.0 added `--values` and `--values-schema` to
  `product-release create`. Conduit has those flags elsewhere but not there.
  **Closed.** They are Apollo's spelling of `--default-config` and
  `--config-schema`, so they now fill the same bundle rather than opening a
  second path, and passing both spellings of one flag is refused instead of
  silently dropping one of the two files. Implementing it surfaced a defect
  older than the flags: Helm configuration passed with a `terraform.v1`
  manifest was accepted and then discarded, because the Terraform branch never
  builds the Helm bundle. That now errors, which is what Apollo published.

This paragraph used to begin *"Most other entries corroborate work already
done."* **That sentence was withdrawn on 2026-08-17. It was never checked, and
when it was checked it was false.**

The mining pass above was hypothesis-driven: it hunted for new contract-bearing
announcements, found two, and generalized about the rest without itemizing them.
An entry-by-entry pass over 165 announcements across 88 date headings classifies
them:

| | Count | Share |
| --- | ---: | ---: |
| Present | 50 | 30% |
| Partial | 35 | 21% |
| Deliberately refused | 18 | 11% |
| **Absent, unrecorded** | **37** | **22%** |
| Not applicable (cosmetics, deprecation notices, support-policy changes) | 25 | 15% |

Thirty per cent is not "most". The most charitable reading -- Present plus
deliberate refusals plus not-applicable, i.e. "nothing is owed" -- reaches 56%,
and only by counting icon changes and removal notices for commands that never
existed here as corroboration, which they are not. What the sentence concealed
is **72 entries, 44% of the page**, carrying an unclosed or half-closed
contract.

The largest of them was never raised. **Apollo Notifications**, announced
2025-06-16 with four dependent entries after it, has no implementation:
`docs/apollo/reference/notifications.md` reconstructs the model and states its
bounds, and the console rail records the omission as "external integration until
implemented" -- a deferral, not a decision. Nothing delivers anything. A Plan
fails, a recall opens, a grant changes, and nobody learns unless they were
already looking.

A caution on the denominator, recorded because it is the method finding rather
than a detail. **The entry count of this page is not a stable fact.** The
enumeration above reached 88 distinct date headings by summing four
date-scoped extractions; asked for a total directly in the same session, the
same tooling answered 130 once and 97 another time, neither consistent with its
own enumeration. This document recorded 120 on 2026-08-15, and
`ROSETTA-RELEASE-NOTES.md` states the page carries 229 distinct dates. Four
figures, none reproducible from the others. Treat 165 as a lower bound, and
anyone re-running this should capture the page's raw `pageProps.markdown` and
count headings offline rather than asking a model to count.

What does corroborate work already done: the 0.813.0/0.814.0 entries describe
the Product Release TGZ carrying `deployment/values.yaml` and
`deployment/values.schema.json`, and
`docs/apollo/cli-fixtures/reconstructed/0.814.0/deployment/` holds exactly those
files -- the fixture is pinned to the newest version the changelog names.

**A bound the changelog exposed.** It names commands that have no reference
page: `local bundle upload --delete-after-upload`, `terminal create-session
--session-id`, `module migrate`, `module installation unlink`, `registry
recover`, `vulnerability suppression modify`, `get-central-namespace` -- and,
found by the 2026-08-17 entry-by-entry pass and missing from the list above,
`product-release batch-get` (0.735.0), `release-channel import` and
`release-channel populate` (0.631.0), and `entity compare` and `entity sync`
(0.631.0). **Twelve, not seven.** The 105-command contract is therefore bounded
by what Apollo publishes as CLI reference pages, not by what the CLI has. `verify-conduit-cli-contract.mjs`
checks Conduit against the published reference and that remains the right
target; it just is not the whole CLI, and the "105/105" figure should be read
that way.

**A trap worth recording.** This site answers **HTTP 200 for paths that do not
exist**, serving a "Page Not Found" shell. Probing those seven command names by
status code reported all of them as live pages; they are not. The only reliable
existence signal is whether the response carries `pageProps.markdown`.
`verify-documentation-manifest.mjs` originally wrapped its whole crawl in one
`try`, so a page that had been unpublished would have been reported as "could
not reach the documentation site" and passed. It now separates the two: seed
failures skip, and a linked page answering without content is reported as drift.

### Images

The image audit was re-run against the enlarged corpus using the same rendered
`<img>` extraction the 2026-08-02 pass used, so the numbers are comparable:

| Measure | 2026-08-02 | 2026-08-15 |
| --- | ---: | ---: |
| Distinct image files | 391 | 389 |
| Total references | 399 | 396 |
| Pages carrying an image | 95 | 95 |

The corpus grew by 21 pages and the image set did not grow with it. The seven
`hidden_*` specification pages carry no images at all, and of the fourteen new
`apollo-cli` pages none does either -- generated CLI reference pages are text.
The small decrease is consistent with
`managing-modules/update-module-installation` losing a section.

So [`IMAGE-DERIVED-GAPS.md`](IMAGE-DERIVED-GAPS.md) remains accurate and no new
visual review is owed. That is a checked result rather than an assumption: a
broader extraction that also counts Markdown and JSON image references reports
496 distinct files, and the difference is extraction method, not new pictures.


### Why the drift was invisible

Nothing re-crawled. Several things *read* this manifest as evidence -- most
sharply `scripts/apollo-graphql/verify-apollo-interface.mjs`, which resolves
every `documentedBy` citation against it, so a citation counted as verified
whenever its URL appeared in this file rather than when the page existed. A
citation checked against a stale index is not checked.

`scripts/verify-documentation-manifest.mjs` now re-crawls and fails on any
added, removed, or content-changed page, with `--write` to refresh. It is not in
the ordinary build: it reaches a third party's website, so it skips cleanly when
that site is unreachable and belongs in a scheduled or dispatched job.


That gate needs a network and a person to run it, because A4 keeps
repository-authored workflows to manual dispatch and `manual-ci.md` names
`schedule` in its prohibition. So it was never scheduled, and nothing else ran
it either — the corpus aged with no signal at all.
`scripts/verify-documentation-freshness.mjs` is the half that needs neither: it
reads `public-documentation-capture.json`, reaches nothing, and fails once the
corpus is more than 45 days old or the manifest and the capture record stop
agreeing about its size. It cannot tell you the corpus has drifted. It can stop
"no drift reported" from being mistaken for "checked", which is the actual
failure that cost fourteen pages.

The first snapshot covered text only. A corrected rendered-HTML pass found and
visually reviewed all 391 distinct screenshots and diagrams referenced 399 times
by 95 pages; the earlier Markdown-only image parser had silently missed 110 raw
HTML images. The review found public contracts that page coverage had missed;
[`IMAGE-DERIVED-GAPS.md`](IMAGE-DERIVED-GAPS.md) records the capture, the
findings, and the resulting implementation. Page coverage below remained accurate
and showed no drift. The one embedded video was also sampled across its full
duration and locally transcribed; [`MEDIA-AUDIT.md`](MEDIA-AUDIT.md) records its
content-addressed evidence and disposition without committing copyrighted media.

### Coverage by section

| Section | Pages | Words |
| --- | ---: | ---: |
| Apollo CLI | 105 | 45,777 |
| Core concepts and workflows | 23 | 30,678 |
| Product specification | 16 | 11,605 |
| Export and import | 17 | 10,734 |
| Getting started | 13 | 8,431 |
| Managing Modules | 6 | 8,120 |
| Managing Environments | 12 | 7,275 |
| CEL policies | 4 | 6,004 |
| Managing Products | 8 | 4,340 |
| Managing Entities | 8 | 4,257 |
| Managing vulnerabilities | 6 | 3,913 |
| Managing Release Channels | 4 | 3,089 |
| Recall | 4 | 2,155 |
| All other sections | 36 | 10,866 |

## Reconstructed semantic kernel

The documentation describes a constraint-based release control plane, not a
plain desired-state synchronizer:

1. **Catalog:** a Product has an immutable Release identified by a Maven-style
   `group:name:version` coordinate. Release metadata declares artifacts,
   dependencies, incompatibilities, health, and labels.
2. **Installation:** an Entity is one Product installed in one Environment.
   Environment and Product settings, configuration ranges, secrets, dependency
   groups, maintenance windows, and suppressions constrain what may run.
3. **Selection:** an Entity follows a Release Channel rather than pinning a
   permanent target version. Apollo continuously selects the best eligible
   Release; it does not expose one global target state.
4. **Execution:** the Hub emits a Plan only after its constraints pass. A Spoke
   Agent polls outbound, executes the Plan, and reports observed state and
   health. Version and configuration changes are applied in lockstep.
5. **Governance:** most Environment mutations are Change Requests. CEL policies
   determine required approvers; Teams and resource-scoped RBAC determine who
   may propose, review, approve, and operate.
6. **Fleet safety:** health/soak gates promote Releases through channels.
   Recalls exclude bad Releases and choose roll-forward, bounded downgrade,
   temporary freeze, or selected-Environment roll-off behavior.
7. **Disconnected delivery:** Export Pipelines produce snapshot, incremental,
   and checkpoint Bundles containing catalog metadata and, when accessible,
   artifacts. Target Hubs admit Bundles and independently enforce their local
   security, permissions, and Change Requests.

## Contract matrix

`Implemented` means the behavior is exercised by repository tests. `Partial`
means a narrower contract exists but does not yet meet the public semantics.
`Boundary` means an external system is intentionally used instead of rebuilt.

| Apollo contract area | Status | Current platform behavior / missing work |
| --- | --- | --- |
| Generic Product, Release, Environment, Entity vocabulary | Implemented | The Go model is platform-neutral; application catalogs remain external. |
| Maven-style Product coordinates and exact versions | Implemented | Strict `group:name`, all four Apollo orderable version forms, signed 32-bit component limits, snapshot ordering, and `x` maximum-version matchers are tested. |
| Product and Product Release retirement | Implemented | [Governed retirement](reference/catalog-retirement.md) requires Product creator/Release creator proposal authority, an approval-policy Change Request, no live desired or reported Entity, no live dependency, and no Product reference from an installed or latest Module. Immutable tombstones prevent coordinate reuse at the Plan, Module, and public publication boundaries while preserving prior desired states, Plans, reports, rollback inputs, and execution evidence. This is a deliberate immutability-preserving adaptation; physical Apollo catalog deletion and private protocol compatibility are not claimed. |
| Immutable OCI Helm chart and workload images | Implemented | Apollo's public guidance requires semantically versioned registry charts and keeps Environment variance in values rather than divergent Git-hosted deployables. Platform strengthens this to tag-plus-SHA-256 chart and image coordinates; the Agent verifies the chart and rendered workload images. Git/Flux reconciles the control-plane source and never replaces Product artifact publication or duplicates Spoke-owned Helm execution. |
| Helm Product Release packaging and configuration inputs | Partial | `conduit helm-chart init` accepts either explicit fields or a bounded Helm v2 application `--chart-path`; `--as-tgz`, `create --product-tgz`, and separate `--default-config`/`--config-schema` normalize into one bounded semantic bundle. Exact bytes are content-addressed and bound to an immutable publication; hostile archive/chart fixtures cover traversal, links, duplicates, ambiguity, non-installable charts, and bounds. The public Apollo-shaped `/api/v0` adapter remains manifest-only; Platform bundles use `/v1`, and the pinned public verifier enforces the separation. Space targeting, the Product-page creation UI, and native archive-byte equivalence remain open. |
| Dependency-aware Plans | Implemented | Required/optional bounds, deterministic install ordering and reverse uninstall ordering, ordinary desired-state removal, Product incompatibilities, default/custom dependency groups, per-Product group overrides including `*`, and ignored dependencies are tested. Commands and recall roll-offs outrank routine work; new generations supersede non-executing stale Plans. Cycles are intentionally rejected deterministically. Flux/source adapters submit immutable generations; Apollo's private invalidation wire and four-hour recomputation timer are not claimed. |
| Helm rollout strategies, readiness, and image pre-pull | Implemented/boundary | `manageRollout` remains the production default; typed `applyChangesNoWait`, install/upgrade readiness policy, per-Release overrides, explicit apply-failure rollback, honest pre-readiness reporting, and bounded external exact-image preparation are tested. Image pre-pull defaults on with a 15-minute timeout, requires an explicit Environment opt-out, and fails closed without a configured native preparer. The shipped runtime is Helm 4.2.3 and CI retains a pinned Helm 3.21.1 compatibility lane; the executor deliberately maps their different rollback/wait flags and preserves client-side apply semantics. Private operator timeout-phase equivalence and an in-place Helm 3-to-4 release migration are not claimed. |
| Outbound-only Spoke execution | Implemented | Poll, lease, renew, execute, rollback, report, certificate-derived per-Environment identity, HTTPS proxy support, immutable artifact-repository pulls, cooperative cancellation, external material preparation, and active health probing are exercised through unit and ephemeral-cluster acceptance tests. Flux reconciles the runtime. Namespace-filtered kube-state-metrics is the preferred bootstrap-owned object-state feed when broader replica telemetry is required. Dynamic Pod registry/pull-secret mutation is an external admission-controller boundary (prefer the Environment's existing Kyverno installation), not Agent cluster-admin scope. The preferred identity profile uses the SPIFFE Workload API; neither Apollo's private PKI nor control wire is claimed. |
| Reported state and health | Implemented | The [reporting contract](contracts/reporting.md) keeps exact version/configuration and health, rollout, config, lifecycle, and freshness independent. Strict public ingestion plus Environment-authenticated five-second annotated HTTPS probing preserve full check evidence, enforce 10,000-byte bounds, and recompute quorum health. |
| Typed installation inputs and Universal targeting | Implemented | Literal, preset, secret references, templates, namespace/enrollment filters, and explicit ignore reasons are modeled. Secret values are not stored in desired state. |
| Environment configuration and version-ranged overrides | Implemented | Resolved typed values are hashed and sent atomically. Apollo-style YAML range selection, Environment Config and `apollo.secrets.*.k8sSecretName` preprocessing, versioned overrides, and authoritative Helm metadata are tested without exposing secret values. |
| Environment display, accreditation, and responsibility profile | Implemented | The [Environment profile contract](reference/environment-profile.md) keeps display name, description, README, accreditation, contact/co-contact teams, operational-responsibility routing, namespace registrations, and support override outside Git-owned desired state. Exact Environment RBAC protects updates, contact teams are validated, `UPDATE_ENVIRONMENT_PROPERTIES` Change Requests gate application and retain execution evidence, and immutable revisions preserve source/operator/rationale. The profile cannot change Plan identity or eligibility implicitly; CEL approval policies can name its bounded Environment resource projection explicitly. Notification delivery remains an external integration. |
| Modules and composite Modules | Implemented | Immutable Module releases, deterministic local publication, flattened locks, exact submodule versions, keyed repeated instances, variable and deep Entity overrides, restrictions, ownership/takeover, channel-tracked proposals, recall exclusion, pause/resume, unlink, relinquishment, and existing-Entity ENABLED/MERGE/DISABLED reconciliation are tested. A Module Change Manager can configure revisioned, rationale-bearing automatic update approval across compliance regimes; qualifying update-only shapes create immutable approved Change Requests, while initial installation and existing-Entity config-override edits remain manual. Native private lockfile byte equivalence is not claimed. |
| Change Requests and approvals | Implemented | The [governance contract](contracts/governance.md) creates durable Change Requests for Plans, Bundle imports, and per-installation bulk actions; evaluates every enabled CEL policy with AND semantics; persists diffs and reviewer requirements; supports approve/reject/cancel/reopen/error and `Do not merge`; and keeps execution eligibility in lockstep. The full published 34-type Change Type taxonomy is validated with the referenced-resource keys each type entitles a policy to read, and a type outside it fails closed; ordinary desired-state removal carries `DELETE_ENTITY`. All 12 published CEL predicates are implemented, including `diff_only_modifies_paths` over the Environment settings document's path vocabulary. Trusted service accounts are operator-declared rather than hardcoded, so an empty trust configuration auto-approves nothing. |
| Teams and resource-scoped RBAC | Implemented | External identity claims resolve into durable Teams with administrator/member users and groups. Exact and global-type grants model owner/co-owner relationships, Apollo-aligned canonical roles, and operations; Environment authority additively inherits to Entities, durable permission administrators supersede bootstrap, label application requires both label contribution and target write authority, and allow/deny audit events are tested. Connected publication independently requires Artifact Creator plus Product Creator and Release Creator authority, while the source OAuth secret and Marketplace whitelist remain external. SAML/OIDC administration remains with the external IdP. |
| Label definitions | Implemented | [Governed label definitions](reference/label-definitions.md) preserve immutable IDs through revisioned updates and tombstones, validate accepted values at the last safe persistence boundary, require label-contributor authority in addition to target write authority, and protect values or definitions referenced by latest desired state, Teams, and Export Pipelines. Existing arbitrary labels migrate incrementally; suggested resource types remain guidance. Private GraphQL and UI compatibility are not claimed. |
| Maintenance windows | Implemented | IANA-timezone weekly intervals, cross-week ranges, Product/Environment intersection, Entity windows, downtime/no-downtime semantics, Product-window bypass, temporary approved overrides, and lease-time re-evaluation are tested. |
| Suppression windows and Plan cancellation | Implemented | Durable manual, Plan-failure, promotion, and Environment-escalation suppressions block leasing; recovery, agent-visible cancellation, and reverse rollback are tested. Rollback and recall roll-off paths bypass ramping rather than being delayed as ordinary upgrades. |
| Emergency Entity commands | Implemented | Install, uninstall, enforce-config, enforce-config-and-version, and Deployment/StatefulSet restart are audited single-Entity command Plans. They bypass ordinary constraints, take priority without overlapping active execution, include attributable secret work, support cooperative cancel/abort, preserve unrelated reported state, and return to continuous desired-state evaluation afterward. This is semantic alignment over Platform's native Plan wire, not private Apollo command compatibility. |
| Entity uninstall, unmanage-only, and undo | Implemented | Ordinary removal uninstalls in reverse dependency order. An [explicit ownership handoff](reference/entity-ownership-handoff.md) uses a reviewed `DELETE_ENTITY` change with `unmanageOnly: true`, requires exact installed state and no retained required dependent, and produces an observe-only Spoke step that cannot call Helm apply/uninstall or rollback. The Hub retains exact installed state and durable external-ownership history. A dedicated undo is admitted only before lease; re-adoption must be explicit. Private Apollo wire compatibility is not claimed. |
| Release Channels and promotion | Implemented | Entities follow channels; immutable decisions enforce labels, health/soak, weekends, evidence freshness, overall timeout, source reachability, and an acyclic channel graph. Timed and canary stages are typed separately: a canary stage carries persisted Entity filters and OR-composed condition/action pairs over a percentage of the filtered set, where a met recall condition outranks a met promote condition, a minimum entity count prevents a one-Entity filter carrying a fleet-wide decision, and an empty canary set cannot pass. Label requirements accept a set of acceptable values per label, and timed stages support no-delay. Governed, attributable, revisioned pause/resume excludes evaluation on one exact Product channel edge and survives restart. Ramped rollout windows are per-channel Product settings, so a channel outside the promotion pipeline is still paced. Canonical `security-scanned=true` and `security-scan-outcome=pass\|fail` labels are derived only from complete exact-digest vulnerability evidence; authored values are rejected. Passed promotions snapshot deterministic upgrade-only fleet pacing; failed canaries suppress affected installations until recovery. |
| Recall and roll-off | Implemented | Single/closed/open ranges, exclusions, reversal, observed-state wait/roll-forward/bounded-downgrade, exact freeze expiry, and selected-Environment testing are exercised. Plans retain causal recall IDs and bypass upgrade ramping. Complete failing vulnerability assessments can derive stable single-Release security recalls. |
| Vulnerability and malware scanning gates | Implemented/boundary | A bounded external scanner seam obtains provider-neutral PASS/FAIL evidence over the exact immutable image digest set for vulnerabilities and chart-plus-image set for malware, then auto-enriches policy-gated Plan proposals. The included Grype normalizer scans exact image references, deduplicates CVEs, preserves strongest CVSS evidence, and rejects unproven ignores; CI independently publishes and attests Syft/SPDX SBOMs and Grype reports for Platform release images. Every suppressed finding preserves its exact image/image-prefix/distribution/scanner/global scope, category, rationale, requester, approver, Change Request, approval time, and active expiry; count-only or inactive provenance fails closed. Scanner-owned promotion labels are synthesized from that normalized evidence and cannot be spoofed in Product metadata. Bundle malware evidence requires zero findings. Credentials, vulnerability databases, raw provider responses, provider-side suppression administration, scheduling, and SLA dashboards remain external integrations; Grype is a local choice, not an Apollo scanner compatibility claim. |
| Artifact Registry assignment and mirrors | Implemented | Revisioned per-Environment assignments, deterministic mirror ownership, exact-digest availability, runtime repository rewriting, Apollo Helm-value injection, and a fail-closed Environment-side credential preparer are tested. Plans contain only opaque credential references. |
| Secrets | Implemented | Revisioned Environment assignments produce explicit create/verify/replace/delete operations, exact reported state, reverse rollback, and a single fail-closed Environment-side provider handoff. Published Apollo Agent secret-input variants and `secretKeyName` are validated against assignments. Values never enter desired state, Plans, Change Requests, or logs. |
| Kubernetes namespace management | Boundary | RFC 1123 inventory, fixed-placement immutability, separately named same-Product installations across namespaces, default-on two-generation deletion protection, and fail-closed reported-state emptiness are implemented and tested. Flux/bootstrap remains the separately authorized Namespace/RBAC reconciler; the Spoke never becomes a cluster administrator and private namespace-agent wire compatibility is not claimed. |
| Hub topology: connection setting and edit source | Implemented | Four connection states gate Agent connection, executable Plan issuance, health tracking, relayed-state admission, and promotion participation, so one Environment never has two writers and an Environment holding no observed state cannot vouch for a Release. Non-executing generations are stored as `RECORDED` desired-state records and cancel pending through running work from the previous writer. Environment selections carry signed exact reports; only explicitly relayed targets admit them, retaining both original observation attribution and Bundle/key provenance. Three declared-state-source states govern local mutability, Bundle overwrite rejection with the published error names, and whether break-glass per-Entity config is permitted. An undeclared connection setting resolves to agents-connected because Environment state is Git-owned and immutable per generation. |
| Product Definition documents (`manifest.yml` + `configuration.yml`) | Implemented | The [Product Definition contract](reference/product-definition.md) models the manifest workload extensions and the whole configuration document. `volumes-v2` follows the authoritative Conjure shape: a `volume-type` union whose only member is an empty `durable-volume`, alphanumeric-and-hyphen names, a `<positive-int><M|G|T>` `desired-size` grammar with a 10T ceiling, upsize-only overrides, and bidirectional manifest/configuration pairing. Also modelled: resources, `replication.desired`, endpoint definitions with `desired-port`/`external-port`/`path`, `sls-status-endpoint` and the probe paths derived from it, `public-proxy`/`tcp`/`udp` endpoint name lists with proxied-versus-raw exclusivity, `conf`, `managed_files` type/reload-type including the shorthand form, `generated_secrets`, keyed `discovery` produces/consumes with payload `select`, quorum requirement, and upgrade strategy. The double-curly substitution language (`secret` with `part`, `discovered`, `self_discovered`, `conf`, `ssl.*`, `volumes.<name>.PathOnDisk`, `endpoints.definitions.*`) is parsed, cross-referenced against what the definition declares, and resolved fail-closed. Cross-document rules JSON Schema cannot express are enforced in Go. Installation overrides merge maps recursively, replace lists and type mismatches wholesale, and refuse `generated_secrets` outright. `manifest-version` is optional here and constrained to `1.0` when present, matching the Product Manifest page's four required fields; the packaging validator that gates execution still requires it because `apollo-cli` emits it. `conduit product-release definition verify`/`schema` expose this — the former discovering `deployment/templates` beside the configuration so `tmpl` entries are checked against what was packaged — and tests hold the embedded schema's enums and volume size grammar equal to the validator's, and pin the two places the specification's worked example contradicts its own normative extension pages. Kubernetes pod-controller selection stays with the execution plane. The Annotations page's `apollo.palantir.com/*` label and five metadata annotations are agent-applied rather than author-written. They now have one definition, and a Helm post-renderer stamps them onto installed objects — every object annotated, Pod templates additionally labelled for the documented NetworkPolicy selector, unrecognized documents passed through rather than dropped. The flag is spelled per Helm major version (an executable path on 3, a `postrenderer/v1` plugin name on 4, which the Agent installs for itself pointing at its own binary); the plugin schema was established against a real Helm 4 binary because the published documentation does not yet specify that type, and an integration test exercises the whole chain against whatever `helm` is installed. The Entity identifier is refused rather than truncated at its 255-character bound, and an installation whose metadata cannot be derived proceeds unlabelled rather than failing. No private Apollo template engine or configuration service is claimed. |
| Environment settings document | Implemented | A named adapter over the published YAML shape (`managed-helm-charts`, `entity-locator`, `dependency-stack-overrides`, `global-maintenance-windows`, and the rest) converts both directions and projects a change into the document's `/`-separated path vocabulary, which is what CEL `diff_only_modifies_paths` policies are written against. Unmodelled fields survive a round trip; a partial document applies only the fields it states. Apollo's private settings service is not claimed. |
| Vulnerability risk score and CVE-recalled upgrades | Implemented | A 0-10 score over active unsuppressed out-of-grace findings, never below the worst single finding and rising with aggregate volume, gates an Environment-overridable Hub setting permitting upgrades between vulnerability-recalled Releases. Eligibility requires CVE-only recalls on the target, scan freshness inside the re-scan SLA, no worse target risk, and no malware findings, with absent evidence counting against the target. Plans record both scores only when this path selected the target. The scoring formula is Platform's own; Palantir's is unpublished and not configurable. |
| Export/Import Bundles | Implemented | The [Conduit-native Bundle contract](contracts/bundles.md) implements Environment, Module, Product, exact Release, and Team selection; none/all/minimal Release strategies with latest-only initialization and incremental history; strict or explicit best-effort missing-resource handling; type-scoped label inclusion/exclusion; refreshable/disableable unsigned pre-build diffs; snapshot/incremental/checkpoint lineage isolated per target; immutable Pipeline revisions with governed archive/unarchive/tombstone deletion; self-contained historical and standalone builds; autonomous validated IANA-timezone cron scheduling with durable single-flight results; the documented default checkpoint cadence of 10; vulnerability/malware evidence; dependency admission; immutable chunked artifact packaging; content deduplication; seven-day target-specific and rescindable transfer confirmation; optional cross-Pipeline target knowledge; ordered redacted import-task diagnostics; atomic target catalog application; and governed Change Requests. Recorded target Hub versions supplement the Bundle-history baseline so an incremental Bundle stays applicable when a disconnected source and its target diverges, suppressing only the exact Releases the target reports running. Private Apollo target-routing and wire-format compatibility are not claimed. |
| Debug terminal/JIT access | Boundary | [The integration contract](reference/debug-access.md) requires an isolated four-hour/read-only baseline, digest-pinned non-root profiles, non-self-approved time-bound elevation, immediate revocation, and durable audit. Prefer an existing Teleport Kubernetes Access deployment for resource authorization, Access Requests, impersonation, and session recording. Bootstrap owns the broker and profile Pod controller; the delivery Agent never becomes a general shell proxy or cluster administrator. |
| Notifications and UI workflows | Boundary | Emit durable domain events to existing notification and UI layers after the kernel contracts stabilize. |
| Fleet projections and bulk operations | Implemented | The [reporting contract](contracts/reporting.md) combines latest desired Plans and Environment reports, exposes exact filters, drift/freshness, per-axis summaries, and resource-filtered views. Bulk suppress/unsuppress expands into resumable, individually authorized and audited installation changes. |
| Apollo CLI/API boundary | Implemented, bounded | The pinned public OpenAPI has an exact 13/13 route adapter with tests. A strict Go YAML adapter validates the reconstructed CLI 0.814.0 Helm Product fixture. The `conduit` binary exposes every command page in the pinned public CLI corpus through native Conduit domain ports; executable publication requires exact OCI digests, while Terraform publications remain non-executable receipts. This is command/workflow parity, not byte-for-byte `apollo-cli` output or private Hub protocol compatibility. The pinned public `osdk-ts` Apollo Agent configuration types are implemented, including all secret variants and Universal ignore reasons. Exact-byte capture/comparison tooling exists, but no native capture is available; private Hub↔Agent transport compatibility remains unclaimed. |
| Foundry Connected Hubs publication | Implemented boundary | The [connected publisher contract](reference/connected-hubs.md) requires authenticated Artifact creation plus separate new-Product and Product-Release creation authority on the pinned public publication routes. Source-side client secrets, ingress, Marketplace store whitelists, folder tracking, remote-store import, and Product contents remain externally owned; this does not add inbound Spoke execution or claim the private integration protocol. |

## Important upstream ambiguities

These are recorded as compatibility tests or adapters, not resolved by naming
one side “correct”:

* The Product Types page lists `helm.v1`, while current Helm publishing examples
  use `helm-chart.v1`.
* The CLI exposes `product-release terraform init`, while the Product Types page
  says only Asset and Helm types are supported.
* Product dependency documentation does not guarantee ordering for circular
  dependencies. This platform rejects dependency cycles because deterministic
  rollback is safer; that is an intentional semantic difference.
* Public documentation and `osdk-ts` expose workflows, selected API/CLI
  contracts, and Agent-to-Foundry installation types, not the private
  Hub-to-Agent control wire. “Apollo-compatible” therefore
  applies only to a named, tested import/export adapter.
* The API Explorer is a GraphQL schema browser, while the pinned public contract
  Platform adapts is a 13-route OpenAPI definition. The two surfaces are not the
  same size and the OpenAPI adapter is not evidence about the GraphQL API. The
  authorized Connected Hubs capture is plan-gated and exposes no Hub origin;
  [`reference/graphql-api.md`](reference/graphql-api.md) records the exact
  observed-schema recovery gate once an Apollo Hub is authorized.
* The published error text names both `InvalidTargetDeclaredStateSource` and
  `CannotImportInEnvironmentWithAuthoritativeDeclaredStateSource` without saying
  which applies when. Platform reports the second when the target is deliberately
  authoritative and the first when the declared value is unrecognized, so an
  operator can tell a policy from a misconfiguration.

## Language and product boundary

The platform should remain generic. Names such as `platform-delivery`, Product,
Release, Environment, Entity, Plan, Channel, and Spoke express the domain
without binding the control plane to Example. Example-specific coordinates,
charts, defaults, and policies live in catalog/installation artifacts.

The precise claim is **Apollo-aligned**: the architecture and domain semantics
follow public Apollo contracts. Use **Apollo-compatible** only next to a tested
format and version, for example “Apollo Product manifest import compatible with
the public 2026-08-01 contract snapshot.” “Apollo-compliant” is too broad until
Palantir publishes or supplies a conformance suite.

## Closure record

An earlier version of this record claimed the public-semantic implementation
order was complete. That claim rested on a text-only review and was overstated:
ten public contracts stated in the documentation's screenshots had no
implementation, including the Change Type taxonomy that CEL policies dispatch on
and the two Hub topology state machines that the export/import story depends on.
All ten are now implemented with executable evidence and are recorded in
[`IMAGE-DERIVED-GAPS.md`](IMAGE-DERIVED-GAPS.md).

The corrected exhaustive 391-image review confirmed those ten, found and fixed
the missing relayed reported-state transport, topology-transition recording,
structured Plan diagnostics, Export Pipeline lifecycle/alternate builds, Bundle
import task diagnostics, and strict/opt-in missing-resource behavior. The full
disposition is in [`IMAGE-DERIVED-GAPS.md`](IMAGE-DERIVED-GAPS.md). The audit is
complete; the adaptation remains deliberately bounded. H43 is an external
scanner-administration boundary, while H46–H52 now close scheduling, metadata,
deletion, automatic approval, manual promotion control, Entity unmanagement,
and label-definition surfaces that are not inferred from narrower kernel
primitives.

Two limits are unchanged and one is newly stated. No tenant-native Apollo CLI
capture and no private Hub/Agent control protocol were available, so public Agent
configuration and reconstructed CLI adapters stay distinguished from unverified
wire and byte compatibility. Newly stated: Apollo's primary API is GraphQL, and
Platform's 13-route public OpenAPI adapter must not be read as covering it. As
of 2026-08-15 a bounded subset of that GraphQL surface has been recovered from
an authorized Hub and committed as observed evidence
([`reference/graphql-api.md`](reference/graphql-api.md)); it is one Hub, one
date and one candidate set, and it does not widen the OpenAPI claim.

## Primary source

The root of the reviewed corpus is the official
[Apollo documentation](https://www.palantir.com/docs/apollo/core/introduction/).
Individual source URLs and their content hashes are in the manifest.
