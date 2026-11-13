# Production readiness

The go/no-go record for running Conduit in production. Every `READY` row
cites the executable gate or committed evidence that proves it — a row
without a passing check does not belong here.

The residual items at the bottom are things nothing in this repository can
complete. They are not all owed by the same person, and the list used to read as
though they were (#265). Two distinct humans appear:

- the **deploying operator**, who runs Conduit somewhere and owns the choices
  that belong to that deployment — signing keys, backup schedule and retention,
  tenant authorization;
- the **repository administrator**, who owns settings on
  `jonathanagustin/conduit` itself and whom a deployment never involves.

Item 3 is the second kind. An operator standing up a Hub cannot install a GitHub
App on somebody else's repository, and reading it as their obligation is a
blocker they can never clear. Marked in place rather than moved, so the
numbering the audit record cites stays stable.

Status date: 2026-08-17, trunk `d8315ed`. Externally audited 2026-08-18 by two
reviewers with no repository access; the resulting corrections to this document,
the findings refuted against the code, and the findings left open are recorded
in [`EXTERNAL-AUDIT-2026-08-18.md`](EXTERNAL-AUDIT-2026-08-18.md).

**Scope.** Two different questions get asked of this document and they have
different answers. *Can Conduit run as a delivery control plane in production?*
is what the rows below answer. *Can Conduit replace Apollo?* is a larger
question the [blocking gaps](#blocking-gaps) and the Apollo parity backlog
answer, and nothing here should be read as claiming it.

## Verified by gates

| Area | Claim | Evidence |
| --- | --- | --- |
| Correctness | **Every** local gate passes at the trunk tip (tests, vet, lint, formatting, domain boundaries, contract validation, GitOps acceptance). The count is deliberately not restated here: `verify-local.sh` reads the step names out of `.github/workflows/platform-ci.yml` and **fails when CI runs a step it does not**, so the set is reconciled by the script rather than by a number in this table. | `./scripts/verify-local.sh` — "all CI gates passed locally"; hosted `Trunk gate` status green at the same tree. **Qualified by #128**: the hosted Apollo step runs fewer gates than the local script, so a green Trunk gate is not yet equivalent to a green `verify-local.sh`. Corrected on 2026-08-18: this row read "All 30 local gates" and the script ran 35 — a hand-maintained count in a row that is supposed to be checkable, drifted, which is the failure mode the closing rule of this document names. |
| Concurrency and replica correctness | The Hub runs two replicas over a transactional PostgreSQL repository, with a PodDisruptionBudget protecting one replica; concurrent lease, approval, idempotency, and crash-recovery are proven against real PostgreSQL. | A7; `scripts/test-postgres-repository.sh`; `ruby scripts/verify-helm-runtime.rb` (aborts unless replicas == 2 and the PDB exists). **Bounded by the two Missing rows in [Test types](#test-types)**: failure injection and load/soak. What is proven is that two replicas do not corrupt each other and that one dying mid-commit loses nothing. Availability *when the infrastructure underneath fails* — PostgreSQL failover, Agent disconnect mid-Plan, partition — is untested, and behaviour at depth is unmeasured. This row was titled "Availability" until the 2026-08-18 external audit; the title asserted more than the evidence carries. |
| Identity | Production identity is OIDC for operators and mTLS or SPIFFE for Agents. Insecure identity headers exist only behind explicit `ALLOW_INSECURE_*` opt-ins; `cmd/platform-delivery` exits rather than combine them with any secure mechanism, and the chart gate aborts if any `ALLOW_INSECURE_` variable is set. | `cmd/platform-delivery/main.go` startup checks; `verify-helm-runtime.rb` ("chart enabled insecure identity"). |
| Bundle signing | A Hub refuses to start with no `BUNDLE_SIGNING_KEY_FILE`. The ephemeral development key survives only behind an explicit `ALLOW_INSECURE_EPHEMERAL_BUNDLE_SIGNING_KEY=true`, which is itself refused in combination with a mounted key or with production identity — the fail-closed shape the insecure identity headers already have — and which no chart profile can set. Which signer a Hub came up with is on its startup line as `bundleSigner`: the base64 Ed25519 **public** key, the same encoding `BUNDLE_TRUSTED_KEYS_FILE` uses, so a changed signer is readable rather than only detectable by a target refusing a Bundle. | `checkBundleSigningPosture` in `cmd/platform-delivery/main.go`; `TestBundleSigningPostureRefusesAnUndeclaredEphemeralKey`, `TestBundleSigningKeyIdentifierPublishesThePublicKeyOnly`; `verify-helm-runtime.rb` ("Hub renders no BUNDLE_SIGNING_KEY_FILE and would refuse to start", "Hub enabled insecure opt-ins", and the render that fails closed on an empty `bundleSigningSecretName`). |
| Delivery | A clean production cluster reconciles the signed private runtime artifact from OCI with no Git source or credential: pull-only registry authority, trusted-signature success, wrong-signer rejection, exact-digest application. | A8; `scripts/test-gitless-clean-cluster.sh`; ADR 0001. |
| Agent privilege | The Spoke chart renders the Agent's Kubernetes authority instead of leaving it to whatever binding a tenant happened to create: one namespaced `Role` and `RoleBinding` per entry in `rbac.managedNamespaces`, every rule derived from a call site in `internal/agent` and carrying that citation in the template, no cluster-scoped rule of any kind, no wildcard apiGroup/resource/verb, no `escalate`/`bind`/`impersonate`, and the binding provably naming the ServiceAccount the Deployment runs as. The chart fails to render when the namespace list is absent or empty rather than defaulting to the Agent's own namespace, which is not where it installs. | `ruby scripts/verify-helm-runtime.rb` (`assert_spoke_rbac_floor!`, over the default, external-identity and multi-namespace profiles, plus both fail-closed renders and the `rbac.create=false` escape hatch). **Bounded by gap 8**, which owns what this does not claim: this is a floor on what the chart grants, not a ceiling on the Agent's effective privilege, and Bundle reach between tenants is a separate question. Read the two together. |
| Reconciliation | Tenant intent converges through Conduit-owned durable work (leases fenced by input revision, atomic decision/audit/outbox, crash-safe receipts); Flux owns only runtime convergence. | A9; ADR 0003; `scripts/test-postgres-repository.sh`. |
| Air-gap packaging | The Zarf package assembles and validates for disconnected **installation**: image digests pinned, one confined namespace, non-root runtime with every capability dropped, no credential bytes in the manifest, HTTPS probes, and a required `BUNDLE_SIGNING_KEY_FILE`. It claims nothing about **operating** a disconnected estate after that install. | `Validate Zarf air-gap package` gate (`scripts/verify-zarf-package.rb`). **Bounded by gap 5**: a Release recalled on the source keeps running on a disconnected target, so lifecycle control of a disconnected estate is outside this row. Read the two together — see [Air gap: what this claims and what it does not](#air-gap-what-this-claims-and-what-it-does-not). |
| Supply chain | Every workflow action, container base, and tool is digest- or checksum-pinned; images carry SBOM and vulnerability evidence with zero high/critical matches; Hadolint and the trusted-technology policy gate the Containerfiles. | `Validate trusted supply-chain policy`, `Scan for known vulnerabilities`, `Lint Containerfiles` gates. Image SBOM and vulnerability evidence is **digest-bound and produced at publication**, by the "Generate digest-bound SBOM and vulnerability evidence" step in `.github/workflows/platform-images.yml` (pinned Syft 1.50.0 and Grype 0.116.1); it is not a committed artifact, so this half of the row is proven per-publication rather than per-commit. Corrected on 2026-08-18: this row previously cited `docs/evidence/highs-1.15.1-shim-candidate-amd64.json`, which is the `UNADMITTED` optimizer candidate the [out-of-scope section](#explicitly-out-of-production-scope) excludes from production — evidence about an image this document says production does not run. |
| CI provenance | Publication, release, and deployment run only on explicit human dispatch; verification and maintenance workflows may be automated under ADR 0006's guardrails (cancel superseded runs, cap runtime, least-privilege, never publish). The Trunk gate runs on `pull_request` under those guardrails (#195) and stays dispatchable for a re-run on an unchanged tree; it authenticates the exact pull-request head on both paths and publishes a commit status against it; each run writes a per-gate summary with failure annotations. | A4 (ADR 0006); `docs/operations/manual-ci.md`; `.github/workflows/platform-ci.yml`; `scripts/verify-pr-fidelity.test.sh`. |
| Metrics exposition | The Hub exports queue depth, lease occupancy, exhaustion and reconciliation lag on an opt-in listener, with no tenant identifier in any label — enforced on the publishing path rather than by convention. | `internal/observability`; `TestALabelNamingATenantResourceIsRefused`; `TestAWedgedHubIsDistinguishableFromABusyOne`. **Bounded by gap 2**, which owns everything this row does not claim: no tracing, nothing exported under `REPOSITORY_KIND=file`, the listener off until `METRICS_ADDRESS` is set, and no alerting rules. This row was titled "Observability" until the 2026-08-18 external audit, which read the title as contradicting gap 2; the claim never did, but the title invited the reading. |
| Frontend | The operator console ships one tenant-neutral bundle; tenant runtime overrides pass two independent closed validators that reject executable content and policy weakening; browser tests cover URL-owned pagination state. | A3; `Validate operator console` gate; `docs/ui/contracts/runtime-bootstrap.md`. |
| Disaster recovery | A `pg_dump` taken while decisions are committing restores into an empty database with the migration ledger intact, state readable through the real repositories including artifact bytes, and no decision, audit record or queued proposal torn from the transaction that wrote all three. | `docs/operations/backup-and-restore.md`; `TestPostgresBackupAndRestoreDrill` (`scripts/test-postgres-repository.sh`). **Bounded by gap 4**: the schedule, retention, PITR and secrets backup belong to the deployment that owns the database. |
| Fidelity boundary | No private Apollo behavior is inferred: the public OpenAPI is authoritative only for published operations, and the private Hub-to-Agent protocol remains an explicit non-claim. | A1, A2; H9 (BLOCKED by design); `docs/apollo/conformance.md`. |

### Air gap: what this claims and what it does not

Two external reviewers reading only this document on 2026-08-18 found the same
defect independently, and it was a real one. The row then titled `Air gap`
claimed the package "validates for disconnected installation" while gap 5 recorded that
"a Release recalled on the source keeps running on a disconnected target with no
roll-off instruction". Those are assertions about the same estate, and the
tiering hid the collision by putting them on different pages: readiness above,
Apollo-parity below. As one reviewer put it, either disconnected estates are in
scope — and gap 5 is Tier 1 — or they are not — and the row is marketing.

**Resolved by narrowing the row, not by re-tiering the gap.** The reasoning,
stated here so a later reader can disagree with it on the record:

- The gate proves something real and something narrow.
  `scripts/verify-zarf-package.rb` reads the package definition and the runtime
  manifest and runs `zarf dev lint`. It establishes that the package is
  well formed, digest-pinned, confined to one namespace, non-root, free of
  credential bytes, and that it requires a persistent `BUNDLE_SIGNING_KEY_FILE`.
  It installs nothing, reaches no cluster, and rehearses no disconnected
  operate, upgrade, restore or key rotation. Deleting the row would discard a
  true and useful claim; leaving the old wording asserted a false one.
- Promoting gap 5 to Tier 1 would answer a question this document is not
  entitled to answer for the project. Tier 1 means "blocks any production use",
  and whether disconnected estates are the product is a scope decision, not a
  finding. What this document *is* obliged to do is stop asserting both sides at
  once. The narrowed row does that: it no longer claims readiness for the thing
  gap 5 says does not work, so gap 5 sitting in Tier 2 is consistent rather than
  convenient.
- The consequence is now stated where an operator will hit it. Recall roll-off
  *is* modelled on the connected path — `RecallRollOff` is a field of the Plan
  step, written in `internal/controlplane/plan.go` and consumed in
  `lifecycle.go` — and it is the bundle path that drops it, not the model. A
  disconnected target therefore installs correctly and cannot
  be told to stop. Anyone evaluating Conduit for a disconnected estate should
  read gap 5 as a condition on this row, not as a parity nicety.

**The rule that keeps them from drifting apart again.** A `READY` row and a gap
that speak about the same estate, resource or failure must cite each other by
number, in both directions, in the same change that creates either of them. A
one-way reference is how this pair diverged: gap 5 never mentioned the air-gap
row, so editing one left the other untouched and unreviewed. Applied here to
Air-gap packaging ↔ gap 5, Metrics exposition ↔ gap 2, Disaster recovery ↔
gap 4, Agent privilege ↔ gap 8, and Concurrency and replica correctness ↔ the
Missing rows in [Test types](#test-types).

The full record of that audit, including the findings that were refuted against
the code and the ones deliberately left open, is
[`EXTERNAL-AUDIT-2026-08-18.md`](EXTERNAL-AUDIT-2026-08-18.md).

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
   with no webhook, logging, or native SMTP/Slack delivery settings set,
   no transport is built, no dispatcher is constructed, no loop runs and no
   request leaves the process. Upgrading an existing deployment changes nothing.

   **Reading and editing now have an HTTP surface.** The Hub serves
   `GET /v1/notifications/inbox`, `GET /v1/notifications/events`, and `GET` and
   `PUT /v1/notifications/preferences`, mounted from `internal/hub/composition`
   rather than from `internal/controlplane` — originally because that package was
   at a line ceiling, and still, because composition is where a cross-context
   HTTP surface belongs. They
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

   **Native recipient delivery (#119).** SMTP and Slack now submit directly to
   the selected owner/channel contact through the durable dispatcher. SMTP
   requires verified TLS and final DATA acceptance; Slack requires `ok: true`.
   Mounted credential files, bounded timeouts and provider-aware retries are
   described in `native-notification-delivery.md`. Isolated protocol servers and
   real PostgreSQL tests establish submission and durable routing, not real
   mailbox receipt. Contact reachability is still discovered on delivery; team
   inbox UI and unread-state writes remain absent.
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
   available are all raised inside `internal/controlplane`. Each needed an
   extraction before it could emit while that package was at a line ceiling;
   **that ceiling was removed on 2026-08-19**, so the extraction is no longer a
   precondition and these three are ordinary unstarted work. Subscription
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

   **This gap bounds the Metrics exposition row above, and the two must be read
   together.** The row claims one thing — that the four reconciliation gauges
   and `conduit_build_info` are exported on an opt-in listener with no tenant
   identifier in any label — and this gap owns everything the broader word
   "observability" would imply. Nothing exported here covers notification
   delivery either: `conduit_notification_inbox.delivery_exhausted` has a reader
   (`internal/notifications/adapters/postgres.Store.Deliveries`) and no
   production caller, no metric and no alerting rule, so a notification that
   runs out of retries ends in a database row nobody queries.

3. **Schema migrations are versioned and verifiable on request; rehearsal is
   not.** An earlier draft of this row said there was no migration path at all.
   That was wrong, and a test written to check it found the error: the schema
   already carried one, for the proposal outbox — two `ADD COLUMN IF NOT
   EXISTS`, a backfill `UPDATE` guarded on `IS NULL`, then `ALTER COLUMN … SET
   NOT NULL`.

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
   than a gap. The manual **Old data upgrade** rehearsal now builds the previous
   stable release and current source separately, seeds 500 Teams, 100 policies,
   100 Plans, 50 reports, 1,000 audit events, ten artifacts, 100 reconciliation
   decision groups and 50 Gitless resolutions through old constructors, then
   compares current reads, exact artifact bytes and physical decision/outbox
   records. Its receipt measures constructor migration/adoption time at those
   stated counts. This synthetic dataset is not a production capacity bound;
   see `old-data-upgrade-rehearsal.md` for the source and compiler boundary.

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
   `scripts/verify-helm-runtime.rb:157-158` pins the rendered kinds, so a
   database StatefulSet cannot be added without failing that gate) — so **no
   backup schedule, retention policy, WAL archiving or PITR configuration ships
   here**, and none can. **No secrets backup or rotation procedure exists**
   either, and providing and rotating the Bundle signing key stays an operator
   action.

   What no longer remains: a restore that forgot the signing Secret used to come
   up **healthy** and sign with an ephemeral key, because
   `BUNDLE_SIGNING_KEY_FILE` was the one input whose absence did not stop the
   Hub. It now does. `checkBundleSigningPosture` in
   `cmd/platform-delivery/main.go` refuses that start unless the Hub declared
   itself a development Hub, so a forgotten Secret is a Hub that will not serve
   rather than a Hub that quietly rejoins the fleet under a new signing
   identity. That is a split-brain supply chain, and it is why the fix was a
   startup refusal rather than a louder warning: a recovery path that silently
   changes the signer turns a recoverable backup mistake into a fleet-wide
   trust failure that presents as health.

   **This gap bounds the Disaster recovery row above, and the two must be read
   together.** That row already said so; this is the return reference the
   2026-08-18 external audit's drift rule requires. Both reviewers read the
   ephemeral-key sentence as the sharpest item in the document and both were
   right to: it is a code choice, not an operator residual, and it is being
   fixed rather than documented — the citation above is stale, the behaviour now
   lives at `cmd/platform-delivery/main.go:88-100`.

   **Authorization audit integrity and retention (#261).** PostgreSQL now
   keeps audit events in governance-owned append-only tables outside the
   metadata snapshot. A versioned SHA-256 chain binds the exact stored bytes;
   events and metadata decisions share one transaction, including nested
   rollback. SQL guards refuse ordinary rewrites and truncation. Records are
   retained indefinitely without automatic pruning, so the deployment owns
   storage capacity and backup retention.

   `platform-delivery verify-authorization-audit` checks one read-only database
   snapshot against a separately retained checkpoint. A database owner can
   recompute an unanchored chain; independent checkpoint custody is therefore
   required to detect coherent history rewrites or rollback. Bootstrap proves
   consistency only, and imported legacy records attest cutover bytes rather
   than historical authenticity. Old Hub audit readers remain incomplete during
   a mixed-version rollout. `authorization-audit-integrity.md` records these
   bounds and the upgrade procedure. The restore drill verifies the saved
   checkpoint before opening a repository, then reads the restored audit event
   through the real adapter.

   **Live Hub self-upgrade remains blocking (#263).** The populated-data
   fixture now has a separate wire-version counterpart: previous Agent/current
   Hub and current Agent/previous Hub execute real explicitly reviewed Helm
   Plans and assert exact observed state against an isolated Kubernetes API.
   `version-skew-rehearsal.md` bounds this to the pinned ConfigMap-only fixture,
   current compiler and tools; it is not a workload scheduling or released-image
   identity claim. Neither fixture rolls two serving Hub replicas with work in
   flight. Until that test establishes preserved leases, atomic decisions and
   proposals, this delivery platform has incomplete evidence for its own upgrade.

### Tier 2 — blocks Apollo-parity production

5. **Recalls never reach Target Hubs** (#126). A recall is a source-side
   selection filter; `Recalls` is a field of `DesiredState`, not `Environment`,
   and none of the five `addBundleResource` call sites carries it. A Release
   recalled on the source keeps running on a disconnected target with no
   roll-off instruction, and `Source: SECURITY` provenance is lost.

   **This gap bounds the Air-gap packaging row above, and the two must be read
   together** — see [Air gap: what this claims and what it does not](#air-gap-what-this-claims-and-what-it-does-not).
   It stays in Tier 2 only because that row was narrowed to installation. What
   recalls *do* reach on the bundle path is selection: `selectResources` gathers
   every live recall and `selectReleases` refuses to newly select a recalled
   Release, so a bundle built after a recall does not carry the bad Release. No
   `RECALL` resource type is ever added, so an already-installed Release on a
   disconnected target is never told to roll off. If this row is ever closed, or
   if the Air-gap packaging row is ever widened past installation, change both
   in the same commit.

6. **Unsafe rollback of stateful services is not representable** (#134). Apollo
   declares, at compile time, the schema set a service can read and write, the
   migration steps it supports, and a runtime-reported current version. Conduit
   models none of the three, so rolling a storage service back to a version that
   cannot read its on-disk schema is not refused — it cannot be expressed.

7. **Monitors are unmodelled** (#131). Apollo's product-declared, severity-
   carrying SLO monitors drive automatic recall with a stated roll-off strategy.
   Conduit has the word only as two mute permissions in the generated role table
   and an unrelated internal prober.

8. **The Agent's RBAC floor is not a ceiling, and cross-tenant Bundle reach is
   still unanswered** (#264). Two halves, both bounding the Agent privilege row
   above; read them together.

   The chart's Role covers what `internal/agent` demonstrably calls. It does not
   cover what the Agent is *asked* to apply: `helm upgrade --install` in
   `internal/agent/helm.go` applies whatever the tenant's chart rendered, and
   that path validates rendered *images* against the Plan's declared artifacts
   while validating nothing about kinds — `apollo_annotations.go` states the
   consequence directly, "Kinds outside this table still receive object
   annotations". So a product chart carrying a Service, a PVC, a CRD or any
   cluster-scoped object needs grants the tenant's cluster admin adds and this
   repository cannot derive, because the chart is a Plan input rather than a
   fact about Conduit. Nothing here measures the resulting effective privilege
   in a real tenant cluster; the gate proves what the chart renders, not what
   the Agent ends up holding.

   The second half is untouched by any of this. Whether one Environment's
   compromised Agent can lease or read another Environment's Plans and Bundles
   is a Hub-side authorization question over the private Hub-to-Agent protocol,
   not a Kubernetes RBAC one. The Fidelity boundary row records that protocol as
   an explicit non-claim *for Apollo reconstruction*; that is not the same as it
   having been specified and adversarially tested for Conduit, and it has not
   been. Closing this half needs its own issue and its own gate.

   **Tier 2 rather than Tier 1**, on the record so it can be argued with: a
   rendered floor plus a tenant-owned grant for the tenant's own chart is an
   ordinary and workable Kubernetes posture, so neither half blocks a
   single-tenant production use. What the second half does block is a
   *multi-tenant* claim, which is where Apollo parity lives.

9. **The artifact security gate derives no evidence of its own** (#54). A
   `VulnerabilityAssessment` arrives inside the caller-submitted `DesiredState`
   and carries no signature or attestation; its provenance is two free strings,
   `Provider` and `RunID` (`internal/catalog/domain/release.go`). Where a
   supplied assessment is present and inside the freshness window,
   `enrichRequiredArtifactAssessments` keeps it and the scanner never runs, so a
   principal holding `change:propose` can attach `Status: COMPLETE`,
   `Outcome: PASS`, `ActiveVulnerabilities: 0` over the exact digest set and the
   gate reports a pass it did not derive. Suppression provenance has the same
   shape at a smaller scale: `approvedBy` and `changeRequestId` are checked for
   being present and ordered, never resolved against an approver's authority —
   `docs/apollo/reference/security-scanning.md` now says so in those words.

   **Narrowed, not closed.** The freshness half is fixed: a policy stating no
   `maximumScanAgeSeconds` used to accept evidence of unbounded age and now
   resolves to a seven-day floor, so stale evidence is re-derived where a
   scanner exists and refused where none is. What that buys is a bound on *how
   long* a forged assessment survives, not on whether one is admitted — a
   fabricated `assessedAt` of now sits inside any window.

   **Tier 2 rather than Tier 1**, and the reason is a mitigation rather than an
   absence: forging requires `change:propose`, and the Plan still traverses the
   approval policy, so a human reviewer sees the assessment. What it does block
   is the claim that the *machine* gate is independent assurance against an
   authorized proposer, which is what a governance product sells. It compounds
   with self-approval: a proposer who can approve their own change request faces
   no second party at all.

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
| **A rolling upgrade meets its own past** | **Version skew** — old Agent against new Hub, old data against new code | **Partial; live rollout blocking.** Previous-release data preservation and both Agent/Hub wire-version directions have separate executable rehearsals. The latter uses real Helm and exact reports with a ConfigMap-only Kubernetes API fixture. A two-replica live rollout remains missing (#263). See `old-data-upgrade-rehearsal.md` and `version-skew-rehearsal.md`. |
| Two replicas race, or one dies mid-commit | **Concurrency and crash durability** | Present against real PostgreSQL: leases, fencing, atomic commit, crash-safe receipts. |
| Applying the schema again destroys data | **Idempotent re-application** | Present: a source-level guard on the DDL, and a populated-database re-apply test. |
| A caller does what they are not entitled to | **Authorization, negative path** | Present, and the default is now stated. This row used to read "nothing enumerates route × role, so a new route defaults to untested". The unasked question underneath it was what a route with *no* registered decision does, and the measured answer was **allow**: it served its body to a caller holding no credential while the route beside it answered 401. `internal/hub/composition/authorization.go` registers all 134 routes with the decision each makes, the Hub refuses to route a pattern the register does not name, and `scripts/verify-route-authorization.mjs` fails the build when a mounted route has no entry. See [route authorization](route-authorization.md). |
| A caller reads another tenant's rows | **Cross-tenant isolation** | Present. Two tenants, one read grant each, every filtered index and by-name read driven as both, plus the write half — `TestFilteredRoutesShowOneTenantNothingOfAnother`. Nothing was wrong when it was written; it had simply never been asserted for Environments, Products, Releases, Plans, change requests or the fleet. Plan approval and cancellation already had cross-Environment refusal tests, so the inbox was not the only one. |
| Wrong bytes are admitted | **Adversarial supply chain** | Strong. Wrong-signer, tampered-digest and unsigned all refused, proven against a real registry and signature. |
| It cannot be installed in the target topology | **Clean-room delivery** | Strong. Clean cluster, no Git source or credential, Zarf air-gap. |
| The tests do not actually test | **Mutation** | Present on both Go and web, with committed scores. |
| **Infrastructure fails underneath it** | **Failure injection** — PostgreSQL failover mid-transaction, Agent disconnect mid-Plan, partial network | **Missing.** Bounds the Concurrency and replica correctness row above: that row proves replicas do not corrupt each other, not that the Hub stays available when the infrastructure under it fails. |
| **It degrades under its own backlog** | **Load and soak** — sustained multi-replica reconciliation, lease contention, no starvation | **Missing.** Concurrency tests prove correctness at small N, not behaviour at depth. Bounds the Concurrency and replica correctness row above. |
| It is running and nobody can tell | **Observability assertion** | Present for metrics: the exposition format, the label allowlist and the wedge-versus-busy distinction are all asserted. Tracing remains unbuilt. |
| Nobody is told what happened | **Delivery end-to-end** | **Partial.** Ports exist and are driven: one emit site records an exhausted reconciliation proposal, fans it out to the preferences stored for the Environment, and a read path returns it — asserted against a real database in `internal/notifications/adapters/postgres` — and, when an operator configures one, a transport leases the entry and POSTs it or logs it; the recipient can now read their own inbox in the console rather than by hand. Native SMTP and Slack also submit to the selected recipient contact (#119), verified with isolated protocol servers and the PostgreSQL queue. Real mailbox receipt and human acknowledgement are not established. |
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
   Hub without them, and the Hub itself refuses to start without them —
   including the Bundle signing key, whose absence used to be tolerated. What
   remains an operator action is narrower than it was and is stated exactly:
   **producing the key material and mounting it**, and **rotating it**
   (generating the successor, publishing its public half into every consumer's
   `BUNDLE_TRUSTED_KEYS_FILE` before it signs, and retiring the predecessor).
   Nothing here generates, distributes or rotates a signing key; what the
   repository now guarantees is that a Hub missing one says so instead of
   inventing one.
3. **Renovate activation.** *(Repository administrator, not the deploying
   operator — see the preamble.)* `renovate.json` is committed and gated; the
   Renovate GitHub App still needs an admin install on the repository
   before update PRs and the Dependency Dashboard appear. Nothing about a
   deployment depends on it.
4. **Tenant deployment authorization.** Deploying to any tenant requires
   its own explicit approval through the owning deployment repository;
   a successful build or upload never authorizes it.
5. **Backup schedule, retention and secrets copies.** Follow
   `docs/operations/backup-and-restore.md`: choose the dump interval (it *is*
   the RPO unless the deployment's PostgreSQL runs PITR), decide retention, and
   back up the Bundle signing key and Agent client CA alongside the database.
   Nothing in this repository can do any of it — the chart does not create the
   database or the Secret. A restore that forgets the signing key is no longer
   silent, which changes the cost of getting this wrong but not the ownership:
   the Hub refuses to start, and someone still has to hold the copy that lets
   it start again.

Update this document in the same change as anything that invalidates a row,
and treat a stale row exactly like a failing gate.
