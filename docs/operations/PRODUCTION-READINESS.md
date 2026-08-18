# Production readiness

The go/no-go record for running Conduit in production. Every `READY` row
cites the executable gate or committed evidence that proves it — a row
without a passing check does not belong here. The residual items at the
bottom are decisions or actions only a human operator can take; nothing in
this repository can complete them.

Status date: 2026-08-17, trunk `d8315ed`.

**Scope.** Two different questions get asked of this document and they have
different answers. *Can Conduit run as a delivery control plane in production?*
is what the rows below answer. *Can Conduit replace Apollo?* is a larger
question the [blocking gaps](#blocking-gaps) and the Apollo parity backlog
answer, and nothing here should be read as claiming it.

## Verified by gates

| Area | Claim | Evidence |
| --- | --- | --- |
| Correctness | All 30 local gates pass at the trunk tip (tests, vet, lint, formatting, domain boundaries, controlplane ceiling, contract validation, GitOps acceptance). | `./scripts/verify-local.sh` — "all CI gates passed locally"; hosted `Trunk gate` status green at the same tree. **Qualified by #128**: the hosted Apollo step runs fewer gates than the local script, so a green Trunk gate is not yet equivalent to a green `verify-local.sh`. |
| Availability | The Hub runs two replicas over a transactional PostgreSQL repository, with a PodDisruptionBudget protecting one replica; concurrent lease, approval, idempotency, and crash-recovery are proven against real PostgreSQL. | A7; `scripts/test-postgres-repository.sh`; `ruby scripts/verify-helm-runtime.rb` (aborts unless replicas == 2 and the PDB exists). |
| Identity | Production identity is OIDC for operators and mTLS or SPIFFE for Agents. Insecure identity headers exist only behind explicit `ALLOW_INSECURE_*` opt-ins; `cmd/platform-delivery` exits rather than combine them with any secure mechanism, and the chart gate aborts if any `ALLOW_INSECURE_` variable is set. | `cmd/platform-delivery/main.go` startup checks; `verify-helm-runtime.rb` ("chart enabled insecure identity"). |
| Delivery | A clean production cluster reconciles the signed private runtime artifact from OCI with no Git source or credential: pull-only registry authority, trusted-signature success, wrong-signer rejection, exact-digest application. | A8; `scripts/test-gitless-clean-cluster.sh`; ADR 0001. |
| Reconciliation | Tenant intent converges through Conduit-owned durable work (leases fenced by input revision, atomic decision/audit/outbox, crash-safe receipts); Flux owns only runtime convergence. | A9; ADR 0003; `scripts/test-postgres-repository.sh`. |
| Air gap | The Zarf package assembles and validates for disconnected installation. | `Validate Zarf air-gap package` gate. |
| Supply chain | Every workflow action, container base, and tool is digest- or checksum-pinned; images carry SBOM and vulnerability evidence with zero high/critical matches; Hadolint and the trusted-technology policy gate the Containerfiles. | `Validate trusted supply-chain policy`, `Scan for known vulnerabilities`, `Lint Containerfiles` gates; `docs/evidence/highs-1.15.1-shim-candidate-amd64.json`. |
| CI provenance | Publication, release, and deployment run only on explicit human dispatch; verification and maintenance workflows may be automated under ADR 0006's guardrails (cancel superseded runs, cap runtime, least-privilege, never publish). The Trunk gate runs on `pull_request` under those guardrails (#195) and stays dispatchable for a re-run on an unchanged tree; it authenticates the exact pull-request head on both paths and publishes a commit status against it; each run writes a per-gate summary with failure annotations. | A4 (ADR 0006); `docs/operations/manual-ci.md`; `.github/workflows/platform-ci.yml`; `scripts/verify-pr-fidelity.test.sh`. |
| Observability | The Hub exports queue depth, lease occupancy, exhaustion and reconciliation lag on an opt-in listener, with no tenant identifier in any label — enforced on the publishing path rather than by convention. | `internal/observability`; `TestALabelNamingATenantResourceIsRefused`; `TestAWedgedHubIsDistinguishableFromABusyOne`. |
| Frontend | The operator console ships one tenant-neutral bundle; tenant runtime overrides pass two independent closed validators that reject executable content and policy weakening; browser tests cover URL-owned pagination state. | A3; `Validate operator console` gate; `docs/ui/contracts/runtime-bootstrap.md`. |
| Disaster recovery | A `pg_dump` taken while decisions are committing restores into an empty database with the migration ledger intact, state readable through the real repositories including artifact bytes, and no decision, audit record or queued proposal torn from the transaction that wrote all three. | `docs/operations/backup-and-restore.md`; `TestPostgresBackupAndRestoreDrill` (`scripts/test-postgres-repository.sh`). **Bounded by gap 4**: the schedule, retention, PITR and secrets backup belong to the deployment that owns the database. |
| Fidelity boundary | No private Apollo behavior is inferred: the public OpenAPI is authoritative only for published operations, and the private Hub-to-Agent protocol remains an explicit non-claim. | A1, A2; H9 (BLOCKED by design); `docs/apollo/conformance.md`. |

## Blocking gaps

Found by audit on 2026-08-17 and not covered by any row above. Each cites what
was checked, because "we should probably have X" is not a finding.

### Tier 1 — blocks any production use

Gap 4 no longer blocks this repository; what is left of it is an operator action,
carried to the residual list as item 5.

1. **Nothing delivers notifications.** `PUBLIC-DOCUMENTATION-COVERAGE.md` states
   the consequence exactly: *"A Plan fails, a recall opens, a grant changes, and
   nobody learns unless they were already looking."* For a control plane that is
   an operability failure, not a missing feature.

   **Partly closed (#197).** `internal/notifications` is now a bounded context:
   `ports` states the store and the emitter, `application` fans an event out to
   the preferences that exist and records it either way, and
   `adapters/postgres` persists events, preferences and per-recipient inbox
   entries under the migration ledger as component `notifications`. One emit
   site is live —
   `internal/delivery/reconciliation/application.Worker.failProposal` escalates
   a proposal whose retry budget is spent, through `ports.Escalations` and
   `adapters/notifications`, which is the escalation #207 records as reaching
   nobody. Reading is through `application.Reader`.

   **Reading and editing now have an HTTP surface.** The Hub serves
   `GET /v1/notifications/inbox`, `GET /v1/notifications/events`, and `GET` and
   `PUT /v1/notifications/preferences`, mounted from `internal/hub/composition`
   rather than from `internal/controlplane`, which is at its line ceiling. They
   are a Conduit extension: the pinned Apollo OpenAPI publishes no notification
   operation, so no shape here is claimed to be Apollo's, and it is registered
   as `notifications.HTTPSurface` in `docs/evidence/conduit-inventions.json`.
   Authorization is two independent checks — Governance decides `resource:read`
   on the resource a route names, and the adapter decides that the owner named
   is the caller or a team the caller is on. The second admits no administrator
   override, and
   `TestThroughTheHubOnePrincipalIsRefusedAnotherPrincipalsInboxAndPreference`
   holds it down against the assembled Hub.

   **What still blocks the row below:** no delivery channel — nothing sends an
   email or posts to Slack, so an operator learns only by reading — no console
   inbox, and no audit record of a notification authorization decision, because
   `GovernanceService.audit` is unexported and the package holding it cannot
   grow. The other four events named above are still emitted by
   nobody: a recall opening, a grant changing, a Plan failing and a Release
   becoming available are all raised inside `internal/controlplane`, which is at
   its line ceiling, so each needs its own extraction before it can emit.
   Subscription grain is the Environment, because reconciliation and the Apollo
   projection mint different identifiers for the same installation.

2. **Observability is partial.** Metrics now exist and the wedge signal is
   exported: `conduit_reconciliation_pending`, `_leased`, `_exhausted` and
   `_oldest_pending_seconds`, each labelled by queue, plus `conduit_build_info`.
   The lag gauge is the one that matters — depth alone cannot separate a busy Hub
   from a stuck one, because both sit at some number; how long the oldest due
   item has waited can.

   What remains: **no tracing**, so a slow decision cannot be attributed to a
   stage; metrics are available **only under `REPOSITORY_KIND=postgres`**, since
   the file repository has none of the queues they describe; the endpoint is
   **off unless `METRICS_ADDRESS` is set**, on its own listener, so an existing
   deployment gains no reachable surface until an operator asks; and **no
   alerting rules ship**, so the signal exists but nobody is woken by it.

3. **Schema migrations are versioned; verification and rehearsal are not.** An
   earlier draft of this row said there was no migration path at all. That was
   wrong, and a test written to check it found the error: the schema already
   carried one, for the proposal outbox — two `ADD COLUMN IF NOT EXISTS`, a
   backfill `UPDATE` guarded on `IS NULL`, then `ALTER COLUMN … SET NOT NULL`.

   What was missing was everything around it, and most of it now exists.
   `internal/pgmigrate` applies an ordered slice of named migrations **once per
   database** and records them in `conduit_schema_migrations`; each commits in
   one transaction with the row that claims it ran; a session advisory lock,
   taken before the ledger table is created, keeps two replicas from racing the
   DDL. Both schemas are on it — `reconciliation` and `controlplane` — so the
   backfill no longer re-runs on every start, ordering is slice position rather
   than textual position in one constant, and the idiom is written down in
   `docs/operations/schema-migrations.md`.

   The dry run now exists. `pgmigrate.Pending` reports what a start would apply
   without applying it, and — the part that makes it a dry run rather than a
   report — without creating the ledger table it reads, since the database an
   operator most wants to ask about before a first start is the one that has
   never been migrated.

   What remains: **nothing verifies a database's schema against the migrations
   that claim to have built it**, so a ledger row asserts a migration ran and
   does not detect a column dropped by hand; **no down path**, which is
   deliberate and is recorded as a decision rather than a gap; and **no upgrade
   rehearsal against a production-shaped dataset** — the tests upgrade a
   populated database, but with tens of rows, so a migration whose cost is
   proportional to table size is not measured anywhere before it runs.

4. **Backup and restore are stated and rehearsed; the schedule is the
   deployment's.** `docs/operations/backup-and-restore.md` states the method,
   what a restore needs beyond the database, and what bounds RPO and RTO.
   `TestPostgresBackupAndRestoreDrill` in `cmd/platform-delivery` holds it down:
   it seeds both schemas through their real constructors, takes a
   `pg_dump --format=custom` **while decisions are being committed**, restores
   into a fresh database, and asserts nothing is pending for either component,
   that state reads back through the real repositories including the artifact
   bytes, and that no decision, audit record or queued proposal was torn from
   the transaction that wrote all three.

   What remains, and it is a deployment's rather than this repository's: the
   database is BYO — the chart consumes a DSN from a Secret it does not create
   (`Chart.yaml` declares no dependencies and
   `scripts/verify-helm-runtime.rb:130-131` pins the rendered kinds, so a
   database StatefulSet cannot be added without failing that gate) — so **no
   backup schedule, retention policy, WAL archiving or PITR configuration ships
   here**, and none can. **No secrets backup or rotation procedure exists**
   either; `BUNDLE_SIGNING_KEY_FILE` is the one input whose absence does not
   stop the Hub, so a restore that forgets it comes up healthy and signs with an
   ephemeral key (`cmd/platform-delivery/main.go:75-77`).

### Tier 2 — blocks Apollo-parity production

5. **Recalls never reach Target Hubs** (#126). A recall is a source-side
   selection filter; `Recalls` is a field of `DesiredState`, not `Environment`,
   and none of the five `addBundleResource` call sites carries it. A Release
   recalled on the source keeps running on a disconnected target with no
   roll-off instruction, and `Source: SECURITY` provenance is lost.

6. **Unsafe rollback of stateful services is not representable** (#134). Apollo
   declares, at compile time, the schema set a service can read and write, the
   migration steps it supports, and a runtime-reported current version. Conduit
   models none of the three, so rolling a storage service back to a version that
   cannot read its on-disk schema is not refused — it cannot be expressed.

7. **Monitors are unmodelled** (#131). Apollo's product-declared, severity-
   carrying SLO monitors drive automatic recall with a stated roll-off strategy.
   Conduit has the word only as two mute permissions in the generated role table
   and an unrelated internal prober.

## Test types

The levels below are organised by **what class of failure each one catches that
no other type catches**. A ladder borrowed from elsewhere — unit, integration,
end-to-end — describes where a test runs rather than what it is for, and this
system's risks do not divide that way: its most dangerous failure is a control
loop deciding something unsafe, which is not a level at all.

**The existing posture is strong and should not be rebuilt**: 230 test files,
1,355 test functions, thirteen `scripts/test-*.sh` harnesses, Playwright
end-to-end and live suites, and mutation scores on both sides.

| Risk it uniquely catches | Type | State |
| --- | --- | --- |
| The reconstruction is wrong about Apollo | **Evidence binding** — source compared against a committed capture, failing the build when they disagree | Strong. Ten `verify-*` gates, the invention register, and the provenance reconciler. |
| A published contract is not honoured | **Contract conformance** — pinned OpenAPI, CLI corpus, GraphQL provenance | Strong. 13/13 routes with named tests. |
| A unit computes the wrong answer | **Unit** | Strong, and mutation-scored. |
| **The control loop decides something unsafe** | **Property / invariant** — generate fleets and assert no run ever produces a forbidden Plan | **Missing.** Example-based tests sample this thinly, and it is the core risk of a control plane. |
| **A rolling upgrade meets its own past** | **Version skew** — old Agent against new Hub, old data against new code | **Missing.** Acute for a system whose job is upgrading other software; it has no test that it can upgrade itself. |
| Two replicas race, or one dies mid-commit | **Concurrency and crash durability** | Present against real PostgreSQL: leases, fencing, atomic commit, crash-safe receipts. |
| Applying the schema again destroys data | **Idempotent re-application** | Present: a source-level guard on the DDL, and a populated-database re-apply test. |
| A caller does what they are not entitled to | **Authorization, negative path** | Partial. Refusals are tested where someone thought to; nothing enumerates route × role, so a new route defaults to untested. |
| Wrong bytes are admitted | **Adversarial supply chain** | Strong. Wrong-signer, tampered-digest and unsigned all refused, proven against a real registry and signature. |
| It cannot be installed in the target topology | **Clean-room delivery** | Strong. Clean cluster, no Git source or credential, Zarf air-gap. |
| The tests do not actually test | **Mutation** | Present on both Go and web, with committed scores. |
| **Infrastructure fails underneath it** | **Failure injection** — PostgreSQL failover mid-transaction, Agent disconnect mid-Plan, partial network | **Missing.** |
| **It degrades under its own backlog** | **Load and soak** — sustained multi-replica reconciliation, lease contention, no starvation | **Missing.** Concurrency tests prove correctness at small N, not behaviour at depth. |
| It is running and nobody can tell | **Observability assertion** | Present for metrics: the exposition format, the label allowlist and the wedge-versus-busy distinction are all asserted. Tracing remains unbuilt. |
| Nobody is told what happened | **Delivery end-to-end** | **Partial.** Ports exist and are driven: one emit site records an exhausted reconciliation proposal, fans it out to the preferences stored for the Environment, and a read path returns it — asserted against a real database in `internal/notifications/adapters/postgres`. Still **missing** the end: no transport sends anything, so "told" currently means "could have read it". |
| A recall does not reach a disconnected target | **Bundle round trip** | **Blocked** by the design decision in #126, not by effort. |

Three of these cannot be written today, and saying so is the point: listing them
as available work would imply someone could pick them up this week.

The two most valuable that *can* be written are the top two marked missing.
**Property-based invariant testing** attacks the failure this system exists to
prevent, and no amount of example-based coverage substitutes for it. **Version
skew** is the one a delivery platform is least entitled to get wrong.

## Explicitly out of production scope

- **HiGHS optimizer candidate** — `UNADMITTED` by construction. The native
  backend serves production; the candidate runs shadow-only. Admission is a
  separate release decision with its own four open gates recorded in
  `docs/evidence/highs-1.15.1-shim-candidate-amd64.json`.
- **File-mode repository** — supports exactly one replica (chart enforces
  `Recreate` and `replicas: 1`); production uses the PostgreSQL repository.

## Residual items owned by a human operator

1. **Release instruction.** Passing CI does not authorize release,
   publication, or deployment (A5). Follow
   `docs/operations/release-and-publication.md`: pick the commit and tag,
   dispatch the release gate, publish immutable digests with SBOM,
   provenance, signature, and attestation, and verify from a clean client.
2. **Production secrets.** Provision in the target environment: PostgreSQL
   DSN, TLS certificate and key (or a SPIFFE workload endpoint), the Agent
   client CA, the Bundle signing key, OIDC issuer configuration, and the
   bootstrap administrator list. The chart refuses to render a production
   Hub without them.
3. **Renovate activation.** `renovate.json` is committed and gated; the
   Renovate GitHub App still needs an admin install on the repository
   before update PRs and the Dependency Dashboard appear.
4. **Tenant deployment authorization.** Deploying to any tenant requires
   its own explicit approval through the owning deployment repository;
   a successful build or upload never authorizes it.
5. **Backup schedule, retention and secrets copies.** Follow
   `docs/operations/backup-and-restore.md`: choose the dump interval (it *is*
   the RPO unless the deployment's PostgreSQL runs PITR), decide retention, and
   back up the Bundle signing key and Agent client CA alongside the database.
   Nothing in this repository can do any of it — the chart does not create the
   database or the Secret.

Update this document in the same change as anything that invalidates a row,
and treat a stale row exactly like a failing gate.
