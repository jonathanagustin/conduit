# Troubleshooting: refusals, and what a tenant cannot do yet

Conduit refuses in sentences rather than in status codes, so the message is
usually the whole diagnosis. This file is the ones worth knowing in advance,
and then the honest list of what it will not do for you at all.

## Refusals, and what they mean

| Message | Cause | What to do |
| --- | --- | --- |
| `all enabled policies returned no_op for change chg_…; no enabled approval policies` | The Hub has no enabled approval policy, so nothing decided what review the change needs. The Change Request lands in `ERROR` | `conduit approval-policy apply … -f policy.yaml`, then propose again. This is the first thing to configure on a new Hub |
| `forbidden: the author of a change request cannot approve it` | Self-approval, refused unconditionally | Have a second principal who satisfies the requirement approve it |
| `configure static mTLS, SPIFFE, bearer-token, or OIDC identity` | A profile with a server and no credential | Add one of `--client-cert-file`+`--client-key-file`, `--spiffe-endpoint`+`--hub-spiffe-id`, `--bearer-token-file`, or `--oidc-issuer`+`--oidc-client-id`+`--oidc-client-secret-file` |
| `bearer token file permissions must not grant group or other access` | The token file is group- or world-readable. Checked at **use** time, not at profile creation | `chmod 0600` the token file |
| `authenticated CLI connections must use https` | A credential pointed at an `http://` server | Use `https://`. Loopback development uses `--development-subject` instead of a credential |
| `--product-id is required by Conduit's bounded catalog projection` | `conduit product-release list` with no `-p` | Name the Product. There is no whole-catalog release listing |
| `must pin a Git commit or other immutable generation` | `environment.generation` blank | Pin the source revision the document came from. A mutable generation is the thing this refuses |
| `--image must be an exact OCI reference ending in @sha256:<64 lowercase hex characters>` | A tag-only agent image | Resolve the digest |
| `--control-plane-url must use https` | Plain-HTTP Hub URL in a Spoke manifest | Use `https://` |
| `Environment and Namespace must be lowercase DNS-style IDs` | An uppercase or otherwise non-DNS ID | Rename |
| `REPOSITORY_KIND must be file or postgres, got …` | Typo in Hub configuration; a startup failure | Fix it. The Hub refuses to start rather than pick one |
| `NOTIFICATION_WEBHOOK_URL and NOTIFICATION_DELIVERY_LOG are both set; choose one notification transport` | Two destinations configured | Pick one. A Hub that quietly picked would deliver to the loser's operator exactly never |
| `METRICS_ADDRESS is set but the repository reports no queue statistics; metrics are available only with REPOSITORY_KIND=postgres` | A warning, not a failure. Nothing is served | Use the PostgreSQL repository, or stop expecting metrics |
| `cve list is unavailable: …` / `target-hub list is unavailable: …` / `terminal grant is intentionally unavailable: …` | Deliberate refusals with a stated reason | Use the alternative each message names |
| `404 not found` from `conduit environment reported-state ENV` | No Spoke has reported for that Environment yet | Install the Spoke; check it can reach the Hub URL and that its client certificate is signed by the Hub's `AGENT_CLIENT_CA_FILE` |

## A Change Request that will not move

Read `requirements[]` on `conduit changerequest get`. Each entry carries:

- `kind` — `TEAM`, `ROLE`, `OPERATION` or `ANY`
- `satisfiable` — computed at read time, **independent of who is asking**
- `unsatisfiable` — a one-line reason when `satisfiable` is false

`satisfiable: false` means **no current grant conveys this**, so nobody can
approve it until a grant is made. That is a different problem from "the right
person has not looked yet", and it is the one that silently wastes a day.
Alternatives inside a requirement are disjunctive: satisfying any one satisfies
the requirement.

## Nobody was told

This is the sharp edge of running Conduit today, and it is worth stating plainly
before you rely on it.

**Notifications are recorded. They are barely delivered.**

- Nothing here stores a person's email address or a team's Slack channel.
  `domain.Contact` models one and **no store persists one**. There is **no SMTP
  transport and no Slack transport**.
- The only outbound destination is **one webhook endpoint an operator
  configures**, or the structured log. So a notification is delivered *about* a
  recipient rather than *to* them.
- **There is no reachability check on an address.** Configuring a destination
  does not prove anything can reach it.
- Delivery is **off unless configured**. With neither `NOTIFICATION_WEBHOOK_URL`
  nor `NOTIFICATION_DELIVERY_LOG` set, no transport is built, no dispatcher is
  constructed, no loop runs and no request leaves the process. Upgrading an
  existing deployment changes nothing.
- The console inbox is **the caller's own only**. There is no team inbox,
  because a team inbox needs a contact no store persists. There is **no unread
  state**: `InboxEntry.ReadAt` exists and no route writes it.

**Only two things emit at all today.** A reconciliation proposal that exhausts
its retry budget emits `PLAN_FAILURES`, and a recall coming into force emits
`PRODUCT_IN_OPEN_ENDED_RECALL` or `PRODUCT_RECALLS`. A grant changing, a Plan
failing and a Release becoming available are **emitted by nobody** — the code
that raises them is in a package at a line ceiling and each needs its own
extraction first.

Consequence, stated by the repository's own coverage note: *"A Plan fails, a
recall opens, a grant changes, and nobody learns unless they were already
looking."* Plan to be the one looking: poll `GET /v1/overview`, watch
`conduit_reconciliation_oldest_pending_seconds`, and read
`conduit changerequest list --status PENDING_APPROVALS`.

## Observability you do not get

- **No tracing at all.** A slow decision cannot be attributed to a stage.
- **No alerting rules ship.** The metrics exist; nothing wakes anybody.
- **Metrics only under `REPOSITORY_KIND=postgres`**, on their own listener, off
  unless `METRICS_ADDRESS` is set.

## Upgrades and data

Schema migrations are versioned, applied once per database, recorded in
`conduit_schema_migrations`, and each migration commits in the same transaction
as the row claiming it ran. Two replicas cannot race the DDL — a session
advisory lock is taken before the ledger table is even created. `pgmigrate.Pending`
reports what a start *would* apply without applying it, and without creating the
ledger table it reads, so you can ask a never-migrated database what it is in
for.

What you still own:

- **Nothing verifies a database's schema against the migrations that claim to
  have built it.** A ledger row asserts a migration ran; a column dropped by
  hand is not detected.
- **There is no down path, deliberately.** Roll forward with a new migration.
- **No upgrade rehearsal against a production-shaped dataset.** The tests
  upgrade a populated database with tens of rows, so a migration whose cost is
  proportional to table size is unmeasured before it runs on yours.

Backup and restore: the method is written down and rehearsed — a `pg_dump
--format=custom` taken *while decisions are committing* restores into an empty
database with the migration ledger intact and nothing torn from the transaction
that wrote decision, audit record and queued proposal together. But **the
database is bring-your-own**: the chart consumes a DSN from a Secret it does not
create, so **no backup schedule, retention policy, WAL archiving or PITR
configuration ships, and none can**. Choose the dump interval deliberately — it
*is* your RPO unless your PostgreSQL runs PITR.

Back up two things beside the database: the **Bundle signing key** and the
**Agent client CA**. `BUNDLE_SIGNING_KEY_FILE` is the one input whose absence
does not stop the Hub, so a restore that forgets it **comes up healthy and signs
with an ephemeral key**.

## Capacity and mode

- `REPOSITORY_KIND=file` supports **exactly one replica** — the chart enforces
  `Recreate` and `replicas: 1`. It is a development and migration profile and
  does not claim durable continuous reconciliation. Production is PostgreSQL,
  two replicas, with a PodDisruptionBudget protecting one.
- Behaviour **at depth is not characterised**. Concurrency is proven correct at
  small N; there is no load or soak evidence, and no failure-injection evidence
  (PostgreSQL failover mid-transaction, Agent disconnect mid-Plan, partial
  network).
- There is **no version-skew test**: an old Agent against a new Hub, or old data
  against new code, is untested. For a system whose job is upgrading other
  software, sequence your own upgrades conservatively.

## Things Conduit will not do, by design or by gap

| You might want | Reality |
| --- | --- |
| A shell into a workload | Refused by design. Conduit has no SSH, `kubectl exec`, host API or cluster-admin path, so it neither issues nor records terminal access. Use the Environment's own audited break-glass tooling; `conduit terminal --session-id` records an external session identifier and executes nothing |
| A due date per CVE | Not modelled. Conduit records grace-period findings — when a finding stops being tolerated — not when it is owed by. `conduit cve check` is the assessment it does produce |
| A registry of target Hubs | Does not exist. A target Hub is a name supplied per Export Pipeline; Conduit records versions against the (pipeline, target Hub) pair. Use `conduit environment version compare` |
| A recall to reach a disconnected target Hub | **Does not happen.** `Recalls` is a field of the desired state rather than of the Environment, and no bundle resource carries it. A Release recalled at the source keeps running on a disconnected target with no roll-off instruction, and `Source: SECURITY` provenance is lost |
| A refusal to roll a database service back past its schema | **Not representable.** Conduit models neither a service's readable schema set, its migration steps, nor a runtime-reported schema version, so an unsafe rollback is not refused — it cannot be expressed |
| Product-declared SLO monitors driving automatic recall | **Unmodelled.** The word appears only as two mute permissions in the generated role table and an unrelated internal prober |
| Email or Slack | No transport exists. See above |

## Boundaries of what this repository claims

Two claims are deliberately *not* made, and a tenant should not act as though
they were:

1. **The private Hub-to-Agent protocol is not claimed to be Apollo's.** The
   pinned public OpenAPI is authoritative for its 13 published operations and
   nothing else.
2. **A green build does not authorize a deployment.** Passing CI is not release,
   publication or deployment authorization, and deploying to any tenant requires
   its own explicit approval through the owning deployment repository.

And one that is easy to misread: this repository answers *"can Conduit run as a
delivery control plane in production?"*. It does not answer *"can Conduit
replace Apollo?"* — the Tier 2 gaps above are exactly the difference.
