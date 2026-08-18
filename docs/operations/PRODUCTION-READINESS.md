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

   **Partly closed again (delivery channel).** A notification now leaves the
   process. `ports.Transport` and `ports.DeliveryQueue` state the two halves;
   `adapters/webhook` POSTs a rendered message to one operator-configured
   endpoint with an injectable `http.Client`, and `adapters/logging` writes it
   to the structured log; `application.Dispatcher` drains undelivered inbox
   entries, retries with the same exponential curve and budget the
   reconciliation worker uses, and marks a delivery terminal when the endpoint
   answers with something a retry cannot change. The queue is the reconciliation
   proposal outbox's shape transposed onto the inbox — lease with an expiry,
   attempt counter, `SKIP LOCKED`, and a durable `delivery_exhausted` — added by
   migration `0002-notification-delivery`.

   It is **off unless configured**, on the same reasoning as `METRICS_ADDRESS`:
   with neither `NOTIFICATION_WEBHOOK_URL` nor `NOTIFICATION_DELIVERY_LOG` set,
   no transport is built, no dispatcher is constructed, no loop runs and no
   request leaves the process. Upgrading an existing deployment changes nothing.

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

   **A recipient now has an address of their own.** The sentence this row
   carried for its whole life — nothing in this repository stores a person's
   email address or a team's Slack channel — is no longer true. `ports.Contacts`
   states the address book, `adapters/postgres` persists one destination per
   (owner, kind) under migration `0003-notification-contacts`, and
   `domain.Contact` is keyed on an `Owner` so a person holds one and not only a
   team. `domain.ResolveDestination` picks the recipient's own destination at
   delivery time and the dispatcher puts it on the message, so a notification is
   delivered *to* a recipient rather than only *about* one.

   **The recipient's contact wins; the operator-configured endpoint stays both
   the transport and the fallback.** A recipient holding no deliverable contact
   is delivered exactly as before, which is what leaves an existing deployment
   unchanged by the upgrade, and a failed read of the address book retries
   rather than falling back — a query that failed is not a person with no
   address. A PagerDuty contact is storable and is never a destination, which is
   Apollo's own contradiction carried rather than resolved. Authorization is the
   inbox's: a principal manages their own contact or that of a team they belong
   to, with **no administrator override**, held down by
   `TestThroughTheHubOnePrincipalCannotWriteAnotherPrincipalsContact` and
   `TestAnAdministratorCannotReadOrRedirectAnybodysContact`. The routes are
   `GET`, `PUT` and `DELETE /v1/notifications/contacts`, registered as
   `notifications.ContactAddressBook` and `notifications.DestinationResolution`
   in `docs/evidence/conduit-inventions.json`. An address is treated as personal
   data throughout: it is never logged, never put in an error, never a metric
   label, and `DELETE` removes the row rather than tombstoning it.

   **Reading and editing now have a console screen.** `/console/notifications`
   serves the calling principal's inbox on a chosen channel and edits the
   preference for one resource, so an operator no longer needs `curl` to find out
   what they were told. It states the four states a real inbox has — empty (which
   arrives as `{"entries":null}`, because Go encodes an empty slice as null),
   loading, a failed request that reads as a failed request rather than as an
   empty inbox, and a bounded page that says it filled the applied limit rather
   than implying it is the end. The console names no owner on any request, so it
   cannot ask for somebody else's inbox; the type checkboxes are held to the
   `domain.EventType` consts in both directions by
   `scripts/verify-console-contract.mjs`; and a type this build has no checkbox
   for is carried through a save unchanged. The contract is
   `docs/ui/contracts/notification-inbox.md`.

   **What still blocks the row below:** there is **no SMTP transport and no
   Slack transport**, so a resolved address reaches a webhook receiver that must
   itself know how to reach a mailbox or a channel; **nothing verifies that an
   address is reachable**, so a typo is indistinguishable from a working
   destination until a delivery fails; and the console reads the caller's own
   inbox only — a team inbox is now storable but has no screen — with **no unread
   state**, because `InboxEntry.ReadAt` exists and no route writes it.
   Notification authorization decisions *are* audited as of
   #241: the resource half of the check is `ResourceDecision.Allow` in
   `internal/governance/application`, which both these routes and the
   controlplane REST routes call, and which files the `audit.3`
   `authorizationCheck` record for ALLOW and DENY alike. That did not need
   `internal/controlplane` to grow — moving the decision out left it two lines
   smaller. The owner half is still unrecorded, and so is the administrator
   predicate on its own. **A recall opening now emits (#253).** A recall in an
   accepted `DesiredState` is announced through `catalog/ports.RecallOpenings`
   and `catalog/adapters/notifications`, the shape reconciliation's escalation
   established: the control plane hands over the recalls in force and learns
   nothing about notifications. It is typed with the two recall event types the
   capture observes — `PRODUCT_IN_OPEN_ENDED_RECALL` for a recall with no upper
   bound, `PRODUCT_RECALLS` otherwise — and the split between them is registered
   as `catalog.RecallEventTypeSelection`. That did not need
   `internal/controlplane` to grow either: the rule for what an automatic
   security recall withdraws moved to `internal/catalog/domain`, leaving the
   package nine lines smaller. The other three events named above are still
   emitted by nobody: a grant changing, a Plan failing and a Release becoming
   available are all raised inside `internal/controlplane`, which is at its line
   ceiling, so each needs its own extraction before it can emit. Subscription
   grain is the Environment, for recalls as for reconciliation: a preference is
   joined by resource identifier, and the Environment is the identifier Conduit
   publishes for the thing a recall is submitted against — #126 records that
   `Recalls` is a field of `DesiredState` rather than of a Product.

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

3. **Schema migrations are versioned and verifiable on request; rehearsal is
   not.** An
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

   Verification now exists too. `pgmigrate.Verify` rebuilds the expected
   catalogue by replaying the migrations **the ledger records** into this
   session's temporary schema, inside a transaction that is always rolled back,
   and diffs the live schema against it object by object — so it reports that
   `conduit_reconciliation_proposal_outbox.attempt` is missing rather than that
   something does not match. It is derived from the DDL rather than from a
   fingerprint taken by the code that ran the DDL, which is what lets it describe
   a difference instead of only detecting one. `platform-delivery verify-schema`
   is the operator command, reading the same `POSTGRES_DSN_FILE` a start reads
   and passing every component that shares it. Nine planted alterations are held
   down in `internal/pgmigrate/verify_postgres_test.go` and
   `cmd/platform-delivery/schema_verification_test.go` — a dropped column, a
   column nothing built, a retyped column, a dropped `NOT NULL`, a dropped index,
   a redefined index, a dropped `CHECK`, a dropped primary key, and drift in the
   ledger table itself — beside the control that an untouched database reports
   nothing at all.

   What remains: **it is invoked, not scheduled and not a start-time check**, so
   drift is found when somebody thinks to look. A Hub refusing to start on a
   schema difference would convert an anomaly into an outage, and half the
   differences a diff finds cannot be judged without a human — an operator's own
   index and a component the caller did not enumerate are indistinguishable from
   drift — so an object only the database has is reported as an advisory rather
   than a fault. **The diff is bounded**: tables, columns with type, nullability,
   default and identity, constraints and indexes, and not ordinal position,
   storage settings, privileges, triggers, views, functions or row contents.
   **No down path**, which is deliberate and is recorded as a decision rather
   than a gap; and **no upgrade rehearsal against a production-shaped dataset** —
   the tests upgrade a populated database, but with tens of rows, so a migration
   whose cost is proportional to table size is not measured anywhere before it
   runs.

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
| Nobody is told what happened | **Delivery end-to-end** | **Partial.** Ports exist and are driven: one emit site records an exhausted reconciliation proposal, fans it out to the preferences stored for the Environment, and a read path returns it — asserted against a real database in `internal/notifications/adapters/postgres` — and, when an operator configures one, a transport leases the entry and POSTs it or logs it; the recipient can now read their own inbox in the console rather than by hand. Still **missing** the end: the only *outbound* destination is that one configured endpoint, so "told" means "recorded where the recipient can look, and sent somewhere an operator watches", not "sent to the recipient". |
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
