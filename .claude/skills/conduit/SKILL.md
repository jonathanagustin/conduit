---
name: conduit
description: Ship your software through a Conduit deployment as a tenant — model a Product and Release, get one into an Environment through a reviewed Plan, read back what the control plane decided, and recover when it refuses. Covers the `conduit` CLI, the Hub's two distinct HTTP surfaces, and what a tenant has to configure themselves.
when_to_use: Use when someone is operating a Conduit Hub to deploy their own software and asks how to publish a Product Release, install or upgrade an Entity in an Environment, propose or approve a Plan, drive a Change Request, promote between Release Channels, deal with a recall or a suppression, install a Spoke agent, or read the audit trail; when a `conduit` command is refused and the reason is unclear ("no enabled approval policies", "the author of a change request cannot approve it", "--product-id is required"); when a Plan fails or a proposal exhausts its retries and nobody was told; when someone asks which Conduit HTTP surface to call (`/api/v0` versus `/v1`); or when someone asks whether Conduit can send them email or Slack, page them, or give them a shell — those have definite negative answers here.
---

# Using Conduit as a tenant

Conduit is a self-hostable delivery control plane: a **Hub** holds reviewed
intent, **Spoke** agents apply it in clusters, and the `conduit` CLI, an HTTP
API and an operator console are how you drive it. This skill is about getting
your software deployed through one — not about building Conduit.

Everything below was run against the real binary at this tree. Where a claim
comes from a document rather than an execution, it says so. Where Conduit does
something Apollo does not, or where a shape is a registered guess, it says that
too.

## The model, in a tenant's words

| Term | What it is for you |
| --- | --- |
| **Product** | A thing you ship, named by a Maven-style `group:name` coordinate (`com.example:checkout`). |
| **Product Release** | One immutable version of it, pinning a chart and image artifacts by `@sha256` digest. Published once, never edited. |
| **Environment** | A deployment target with a DNS-label ID (`prod-use1`). Holds a `generation` you pin to an immutable source revision. |
| **Entity** | One installed thing in an Environment — a Product at a version in a namespace. Its identity in a Plan is an `installationId`. |
| **Desired state** | One YAML/JSON document (`kind: EnvironmentDeployment`) that names the Environment, the Releases it may draw on, and the Entities that should exist. **This document is the unit of change**; you do not mutate the Hub field by field. |
| **Plan** | What the Hub computes from a submitted desired state: ordered steps with digests, plus its execution evidence. It is the durable record of what was decided and what happened. |
| **Change Request** | The review wrapper around a Plan. Policy decides what review it needs; a Plan does not execute until it is approved. |
| **Release Channel** | A named track (`STABLE`, `DEV`) a Release can be assigned to and an Entity can follow. |
| **Promotion** | Moving one immutable Release from a source channel to a distinct target channel once evidence satisfies the stage. |
| **Recall** | A statement that a Release must not be selected. Roll-off is computed from what an installation *reports*, not only from the catalog. |
| **Suppression** | A block on deploying into an Environment — created automatically after failures, or by a human. Rechecked when a Plan is leased. |
| **Module** | A reusable bundle of Entities with variables, versioned and locked independently of Products. |

## Connect

The CLI takes connection settings from flags, from `CONDUIT_SERVER` /
`CONDUIT_CA_FILE` / `CONDUIT_BEARER_TOKEN_FILE` / `CONDUIT_CLIENT_CERT_FILE` /
`CONDUIT_CLIENT_KEY_FILE` / `CONDUIT_SPIFFE_ENDPOINT` / `CONDUIT_HUB_SPIFFE_ID`,
or from a named profile. Profiles live in `$XDG_CONFIG_HOME/conduit/config.yaml`
(override with `CONDUIT_CONFIG_FILE`) and are written mode `0600`.

```bash
conduit profile create prod \
  --server https://conduit.example.com \
  --bearer-token-file /etc/conduit/token      # the file must itself be mode 0600
conduit profile use prod
conduit profile list
conduit profile schema        # the versioned JSON Schema for the profile document
```

Three things the CLI enforces, each reproduced:

- **A profile with no identity is refused.** `conduit profile create staging
  --server https://…` with no credential exits 1 with `configure static mTLS,
  SPIFFE, or bearer-token identity`. Pick mTLS (`--client-cert-file` +
  `--client-key-file`), SPIFFE (`--spiffe-endpoint`, `--hub-spiffe-id`), or a
  bearer token file.
- **A bearer token file must not be group- or world-readable.** The check is at
  use time, not at profile creation, and reads `bearer token file permissions
  must not grant group or other access`.
- **An authenticated connection must be `https`.** Pointing a credentialled CLI
  at `http://` fails with `authenticated CLI connections must use https`.

`conduit configure` is **not interactive**: it writes the profile named by
`--profile` (or `default`) from whatever connection flags you passed, and makes
it current. It is `profile create` plus `profile use` in one step.

`--development-subject` / `--development-groups` exist for a **loopback-only
insecure development identity** and only work against a Hub started with
`ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS=true`. Never use them against a real
Hub; `cmd/platform-delivery` refuses to combine that opt-in with a secure
identity mechanism, and the Helm chart aborts if any `ALLOW_INSECURE_` variable
is set.

Output is `-o json` (default) or `-o yaml`, globally.

## Get a Release into an Environment, end to end

### 1. Author and publish the Product Release

```bash
conduit product-release helm-chart init \
  --chart-path ./charts/checkout --name checkout --version 1.4.2 \
  --repository-url https://charts.example.com \
  --maven-coordinate com.example:checkout:1.4.2 \
  --artifact registry.example.com/checkout:1.4.2@sha256:<64 hex> \
  --output-dir ./release
conduit product-release verify --manifest ./release/manifest.yml --print-manifest
conduit product-release create --manifest ./release/manifest.yml \
  --values ./release/values.yaml --values-schema ./release/values.schema.json
```

`--artifact` is repeatable and each reference **must** carry an `@sha256:`
digest. `--values` / `--values-schema` are aliases for `--default-config` /
`--config-schema`; both spellings are in the binary. `--as-tgz` on
`helm-chart init` writes one deterministic Product TGZ instead of loose files,
which `product-release create --product-tgz` then consumes. There is also
`conduit product-release terraform init` for a `terraform.v1` manifest and
`conduit product-release asset publish` for content-addressed assets.

Validate a chart before you commit to it:

```bash
conduit helm-chart validate --chart-path ./charts/checkout \
  --name checkout --version 1.4.2 --values ./values.yaml \
  --repository-url https://charts.example.com
```

### 2. Write the desired state

One document per Environment. Minimum accepted shape (this exact document was
accepted by a running Hub):

```yaml
apiVersion: platform.delivery/v1alpha1
kind: EnvironmentDeployment
environment:
  id: prod-use1              # DNS label
  generation: "9f2c1ab"      # pin a commit or other immutable generation; required
releases:
  - product: com.example:checkout
    version: 1.4.2
    chart:
      repository: registry.example.com/charts/checkout
      tag: 1.4.2
      digest: sha256:<64 hex>
    images:
      - repository: registry.example.com/checkout
        tag: 1.4.2
        digest: sha256:<64 hex>
entities:
  - name: checkout
    product: com.example:checkout
    version: 1.4.2
    namespace: checkout
```

`apiVersion` and `kind` are checked literally, and an empty `generation` is
rejected with `must pin a Git commit or other immutable generation`.

`conduit environment create NAME [--contact-team TEAM]` *authors a descriptor*
locally — it prints an `EnvironmentDescriptor` and does not create anything on
the Hub. The Environment comes into existence when a desired state naming it is
accepted.

Several commands **edit this document rather than the Hub**, and take
`--state-file` or `-f` plus `--dry-run`: `conduit label set|delete`,
`conduit release-channel create|update|add|import`,
`conduit product promotion set|append|enable|disable`,
`conduit environment release-channel set`,
`conduit environment artifact-registry assign|unassign`,
`conduit environment setting edit`, `conduit environment delete`. Each emits the
next-generation desired state; you then propose it. That is the Git-owned half of
the model — the file is the source of truth and the Hub is fed from it.

### 3. Propose the Plan

```bash
conduit plan propose -f desired.yaml       # use - for stdin
```

The response carries `id` (`pln_…`), `desiredStateDigest`, the normalized
`desiredState`, the computed `plan.steps`, `changeRequestId` (`crq_…`) and
`status`. Each step names `installationId`, `product`, `version`,
`releaseDigest`, `namespace`, `configDigest` and the chart — that is what will
be applied, computed before anyone approves it.

### 4. Get it reviewed, then approved

```bash
conduit changerequest list --environment prod-use1 --status PENDING_APPROVALS
conduit changerequest get crq_…
conduit changerequest approve crq_… --rationale "reviewed the diff"   # --dry-run available
conduit plan get pln_…
```

### 5. Install the Spoke that will apply it

```bash
conduit environment manifest generate prod-use1 \
  --namespace conduit-spoke \
  --image ghcr.io/example/conduit-agent@sha256:<64 hex> \
  --control-plane-url https://conduit.example.com \
  --client-tls-secret spoke-mtls \
  --external-ca-configmap conduit-hub-ca \
  -o yaml | kubectl apply -f -
```

Verified: the image **must** be an exact `@sha256:` reference, the Hub URL must
be `https://`, and both the existing client-TLS Secret and the Hub CA ConfigMap
names are required. The generated Deployment runs one replica as non-root
(65532) with `seccompProfile: RuntimeDefault`, and carries the Spoke's
`CONTROL_PLANE_URL` / `CONTROL_PLANE_CA_FILE` / `CONTROL_PLANE_CLIENT_CERT_FILE`
/ `CONTROL_PLANE_CLIENT_KEY_FILE` / `ENVIRONMENT_ID` / `AGENT_ID` environment.
`--k8s-storage-class` is recorded for compatibility only — the Spoke is
stateless and claims no storage.

## How approval actually gates a change

Three things about review, all reproduced against a running Hub:

**A Hub with no approval policy cannot approve anything.** Propose a Plan on a
fresh Hub and the Change Request lands in `status: ERROR` with
`all enabled policies returned no_op for change chg_…; no enabled approval
policies`. Approval policies are not optional infrastructure — they are the
thing that decides review, and with none enabled every change errors.

```bash
conduit approval-policy apply entity-review -f policy.yaml
```

```yaml
id: entity-review
displayName: Reviewed entity changes
enabled: true
expression: 'type == "CREATE_ENTITY" ? require_team("release-managers", "release review") : approve("no review required")'
```

The expression is CEL. It is given `type`, `author`, `onBehalfOf`, `editors`,
`metadata`, `request`, `diff`, `referencedResources`, `createdAt`, `now` and
`policyVariable`, and it must return one of:

`approve(reason)` · `reject(reason)` · `no_op()` ·
`require_team(team, reason)` · `require_role(resourceRid, role, reason)` ·
`require_operation(resourceRid, operation, reason)` ·
`require_any([…], reason)`

With that policy in place, the same proposal returns
`status: AWAITING_APPROVAL` and the Change Request sits at
`PENDING_APPROVALS` carrying a typed requirement:

```json
{"kind": "TEAM", "team": "release-managers", "reason": "release review", "satisfiable": true}
```

`satisfiable` is computed at read time: `false` means **no current grant could
satisfy this requirement at all**, so the change will not move until somebody
makes a grant. That is the field to read when a Change Request is stuck and
nothing says why.

**An author cannot approve their own change.** Verified:

```
conduit: operator control plane returned 403: forbidden: the author of a change request cannot approve it
```

**Teams and grants are what satisfy requirements.**

```bash
conduit team apply release-managers -f team.yaml     # displayName, adminGroups, memberGroups, memberUsers
conduit grant create -f grant.yaml                   # resourceRid + team + roles/operations
```

Approving as a second principal in the team flips the Change Request to
`APPROVED` / `requirementsSatisfied: true`, and the Plan to `status: APPROVED`
with `requiredApprovals` and an `approvals` array recording subject, rationale
and time.

`conduit plan approve PLAN_ID --rationale …` approves a Plan directly. Note the
asymmetry, which is in the Hub's own comment: a Plan **with** a Change Request is
approved through governance; a Plan **without** one — the reconciliation planner
and the command path both create these — is approved only if the caller holds
the `Operate` operation on that Environment.

## Telling what happened, and why

| Question | How to ask it |
| --- | --- |
| What will this change do? | `conduit plan propose -f …` then read `plan.steps`; or `--dry-run` on the desired-state editing commands |
| What did the Hub decide about a Plan? | `conduit plan get PLAN_ID` — status, `requiredApprovals`, `approvals[]`, `desiredStateDigest`, execution evidence |
| Why is this change not moving? | `conduit changerequest get CR_ID` — `status`, `error`, `requirements[]` with `satisfiable` / `unsatisfiable` |
| What is installed and at what version? | `conduit entity list -e ENV`, `conduit entity get AEID`, `conduit environment version compare ENV [-r CHANNEL]` |
| What did the cluster actually report? | `conduit environment reported-state ENV` (404 until a Spoke has reported) |
| Who was allowed to do what? | `GET /v1/authorization-audit` — `audit.3` records with `subject`, `action`, `resourceRid`, `decision`, and the reason, for **ALLOW as well as DENY** |
| What needs attention right now? | `GET /v1/overview` — `awaitingApprovals`, `failedPlans`, `runningPlans`, `unhealthyEntities`, `staleReports`, `blockedPromotions`, each with a `status` and `count`, plus `staleAfterSeconds` |
| Is the Hub keeping up? | the metrics listener, **off by default** — see below |
| What was I told? | `GET /v1/notifications/inbox`, or the `/console/notifications` screen |

### The reconciliation lag metric is the one that matters

With `METRICS_ADDRESS` set **and** `REPOSITORY_KIND=postgres`, the Hub exports
on a separate listener:

- `conduit_reconciliation_pending`, `_leased`, `_exhausted` — each labelled by queue
- `conduit_reconciliation_oldest_pending_seconds`
- `conduit_build_info`

Read the lag gauge, not the depth. Depth alone cannot separate a busy Hub from a
stuck one — both sit at some number — while how long the oldest due item has
waited can. No tenant identifier appears in any label, and that is enforced on
the publishing path.

If `METRICS_ADDRESS` is set under `REPOSITORY_KIND=file` the Hub logs a warning
and serves nothing: the file repository has none of the queues these metrics
describe, and exporting zeroes would say the fleet is idle.

## Recovery

**A Plan fails.** `conduit plan get PLAN_ID` carries the structured execution
record: per-attempt tasks with phase/status, timestamps, installation and
namespace, rollback outcomes and typed Kubernetes/Helm diagnostics. Per
`docs/apollo/reference/plans.md`, that output is **bounded and redacted at the
Hub boundary** — per task, per attempt and in total — so a long provider log
will be truncated; that is the contract, not a bug. There is no separate
unaudited log channel: the authenticated Plan `GET` projects the same durable
record. A failed attempt is rolled back *before* its automatic suppression is
created.

**A Plan is wrong or wedged.** `conduit plan cancel PLAN_ID --reason "…"` —
before or during execution. Leased or running work is never silently replaced by
a newer generation; only non-executing work is superseded.

**A suppression is blocking deployment.**

```bash
conduit suppression list -e prod-use1
conduit suppression remove SUPPRESSION_ID
```

Suppression is re-evaluated when a Plan is leased, so removing one does not
retroactively unblock a Plan that already failed the check — propose again. A
suppression a human created is never ignored by a later ordinary Plan.

**A reconciliation proposal exhausts its retries.** The worker escalates
*before* it marks the proposal exhausted, deliberately: an exhausted proposal is
never leased again, so a crash between marking and telling would lose the signal
permanently, and duplicate-over-lost is the right trade. The escalation is a
`PLAN_FAILURES` notification event. Whether anyone learns of it depends entirely
on the delivery configuration below. The retry budget is ten attempts from a
two-second base doubling to an hour's ceiling — the same curve the notification
dispatcher uses, so you only need one model of when the next attempt lands.

**A recall opens.** A recall in an accepted desired state is announced as
`PRODUCT_IN_OPEN_ENDED_RECALL` (no upper bound) or `PRODUCT_RECALLS` (bounded).
Roll-off is computed from the version an installation actually **reports**: roll
forward prefers the newest non-recalled Release on the tracked channel, and if
none exists the Entity stays on its recalled Release rather than receiving an
unsafe Plan. Recall roll-offs bypass ramp pacing but still obey approvals,
maintenance, artifact policies and suppression. Subscription grain for a recall
is the Environment.

Two things about recalls a tenant must know: the split between those two event
types is a **registered Conduit invention**
(`catalog.RecallEventTypeSelection`), and **a recall does not reach a
disconnected Target Hub** — `Recalls` is a field of the desired state rather than
of the Environment, so a Release recalled at the source keeps running on a
disconnected target with no roll-off instruction. That is a known Tier 2 gap.

**Rolling a stateful service back is not refused, because it cannot be
expressed.** Conduit models neither a service's readable schema set, its
migration steps, nor a runtime-reported schema version. Do not rely on the
control plane to stop an unsafe rollback of a storage service.

## What you must configure yourself

Everything here is **off unless you turn it on**, and an upgrade never turns any
of it on for you.

| Capability | Switch | If unset |
| --- | --- | --- |
| Metrics | `METRICS_ADDRESS`, and `REPOSITORY_KIND=postgres` | No listener at all |
| Notification delivery | `NOTIFICATION_WEBHOOK_URL` (plus optional `NOTIFICATION_WEBHOOK_TIMEOUT`) **or** `NOTIFICATION_DELIVERY_LOG=true` | Nothing leaves the process. Events are still recorded and readable |
| Notification routes and console screen | a durable notification store, i.e. PostgreSQL | The routes are not mounted at all rather than answering 500 |
| Gitless GitOps | `GITLESS_BINDINGS`, `GITLESS_PUBLISHER_POLICY_FILE`, `GITLESS_CONVERGENCE` (no default) | The Hub starts exactly as if the feature did not exist |
| Approval policy | `conduit approval-policy apply` | Every Change Request errors, see above |
| Bundle signing key | `BUNDLE_SIGNING_KEY_FILE` | **The Hub still starts** and signs with an ephemeral development key. This is the one input whose absence does not stop it |
| Production identity | OIDC for operators, mTLS or SPIFFE for Agents; PostgreSQL DSN, TLS cert/key, Agent client CA, bootstrap administrators | The chart refuses to render a production Hub |
| Backup schedule, retention, PITR, secrets copies | yours | Nothing ships. The chart consumes a DSN from a Secret it does not create, so it cannot |

Setting both notification transports is a **startup error**, not a
precedence decision: a Hub that quietly picked one would deliver everything to
the loser's operator exactly never. Gitless misconfiguration is likewise a
startup failure rather than a first-tick surprise.

Notification delivery is the one to think hardest about, because of what it
cannot do — see `troubleshooting.md`.

## Two HTTP surfaces, same binary, no overlap

This trips people up, so be explicit about which one you mean.

| Surface | What it is | Shape |
| --- | --- | --- |
| **`/api/v0`** | The pinned public Apollo contract. **13 operations**, generated from the committed OpenAPI snapshot and gated against it. Products, Releases, Environments, Installations, entity status, namespaces | Apollo's DTOs, Apollo's error envelope (`errorName`, `errorCode`, `errorInstanceId`), `ri.apollo.main.…` resource identifiers |
| **`/v1`** | Conduit's own operator surface, and what the CLI and console call. Plans, Change Requests, approval policies, teams, grants, labels, suppressions, promotions, modules, bundles, export pipelines, the audit trail, the overview, notifications | Conduit's own shapes |

**Their paths do not overlap at all.** They also do not answer the same question:
on the same live Hub, `/v1/products` returned the full catalog while
`/api/v0/products` returned `{"products":[]}`, because the two are backed by
different projections. Do not read one as a version of the other.

Notification routes (`GET /v1/notifications/inbox`, `/events`, `GET` and
`PUT /v1/notifications/preferences`) are a **Conduit extension**: the pinned
Apollo OpenAPI publishes no notification operation, and their shapes are
registered as the invention `notifications.HTTPSurface`. Treat them as Conduit's,
not as Apollo's.

Full operation lists, the desired-state anatomy and the complete command index
are in `reference.md`.

## Commands that exist to refuse

Three command groups are wired up only to explain why Conduit will not do
something. They exit non-zero with the reason (each message quoted in
`troubleshooting.md`):

- `conduit cve list` — Conduit records grace-period findings, not per-CVE due dates. Use `conduit cve check`.
- `conduit target-hub get|list|pipeline list` — target Hubs are named per Export Pipeline; there is no registry to enumerate. Use `conduit environment version compare`.
- `conduit terminal grant|policy` — Conduit has no SSH, `kubectl exec`, host API or cluster-admin execution path, so it neither issues nor records terminal access. Use the Environment's own audited break-glass tooling.

`conduit entity enforce` **is** available — it is the break-glass path — but a
command whose target Release carries a `SECURITY` recall is refused. That is a
deliberate Conduit divergence from the published behaviour that commands bypass
recall eligibility, recorded as such in `docs/operations/TENANT-FIDELITY.md`.

## Files

| File | Contents |
| --- | --- |
| `reference.md` | Complete verified command index, the desired-state document, both API surfaces operation by operation, the profile document |
| `troubleshooting.md` | Refusals and what they mean, and the honest list of what a tenant cannot do yet |

> Claims here are bound to this tree. The command tree is held against the
> pinned CLI inventory by `TestCommandTreeCoversPinnedOfficialInventory` in
> `internal/conduitcli/contract_test.go` (with
> `scripts/verify-conduit-cli-contract.mjs` holding that inventory to its
> captured sources); the `/api/v0` surface is regenerated from the pinned
> snapshot and diffed by `scripts/verify-apollo-routes.sh`. If the binary
> disagrees with this text, the binary is right — re-run
> `conduit <command> --help`.
