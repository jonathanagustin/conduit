# Reference: the tenant-facing surfaces

Read this when you need the exact command, the exact flag, or the exact route.
Every command and flag below was read out of `conduit <command> --help` on the
built binary; every `/api/v0` operation was read out of the pinned OpenAPI
snapshot; every `/v1` route was read out of the Hub's own route registrations.

---

## §1: The desired-state document

One document per Environment. It is the unit of change: you edit it, propose
it, and the Hub computes a Plan from the difference.

```yaml
apiVersion: platform.delivery/v1alpha1     # checked literally
kind: EnvironmentDeployment                # checked literally
environment:
  id: prod-use1                            # DNS label
  generation: "9f2c1ab"                    # must not be blank
  # optional: labels, defaultReleaseChannel, namespaces, config, helmExecution,
  #           vulnerabilityUpgrades, artifactRegistries, secrets, maintenance,
  #           artifactSecurityPolicy, artifactAvailabilityPolicy,
  #           autoSuppressionThreshold
releases:                                  # the immutable catalog this state may draw on
  - product: com.example:checkout          # group:name
    version: 1.4.2
    chart: { repository: …, tag: …, digest: sha256:… }
    images:
      - { repository: …, tag: …, digest: sha256:… }
    # optional: dependencies, incompatibilities, channels, labels,
    #           maintenanceWindow, vulnerabilityAssessment, malwareAssessment,
    #           artifactAvailability, rolloutStrategy,
    #           resourceReadinessTimeoutSeconds, imagePrePullTimeoutSeconds
entities:                                  # what should exist
  - name: checkout                         # becomes the installationId
    product: com.example:checkout
    version: 1.4.2
    namespace: checkout
    # optional: labels, releaseChannel, helmReleaseName, stack, dependencyGroups,
    #           ignoredDependencies, requiresDowntime, maintenance, helmValues,
    #           configOverrides, configuration, universal, secretRequirements,
    #           healthQuorum
# optional top level: promotions, recalls, releaseChannels, productSettings,
#                     ownershipHandoffs
```

Rejections you will actually hit, quoted from `Validate`:

| Field | Rule |
| --- | --- |
| `apiVersion` | `must be platform.delivery/v1alpha1` |
| `kind` | `must be EnvironmentDeployment` |
| `environment.id` | `must be a DNS label` |
| `environment.generation` | `must pin a Git commit or other immutable generation` |
| `environment.labels` | `label names and values must not be empty` |
| `environment.artifactSecurityPolicy.requiredOutcome` | `must be PASS or ANY` |
| `environment.artifactAvailabilityPolicy.requiredOutcome` | `must be AVAILABLE` |
| `environment.autoSuppressionThreshold` | `must be positive when configured` |
| `environment.namespaces[n].rid` / `.name` | must be non-empty, unique, and a DNS label |

The response to `POST /v1/plans` (which is what `conduit plan propose` calls)
sets `X-Change-Request-ID` and returns the Plan record. `X-Change-Title` and
`X-Change-Description` request headers name the Change Request; the CLI derives
a default title of the form `Reconcile <env> generation <generation>`.

---

## §2: The complete CLI surface

`conduit --help` describes itself as *"Operate the Conduit Hub/Spoke delivery
control plane"*. Global flags, on every command:

```
--bearer-token-file string     mode-0600 Multipass bearer token file
--ca-file string               Hub CA PEM file (CONDUIT_CA_FILE)
--client-cert-file string      operator client certificate PEM file
--client-key-file string       operator client private key PEM file
--development-groups strings   groups for the loopback-only development identity
--development-subject string   explicit loopback-only insecure development identity
--hub-spiffe-id string         exact Conduit Hub SPIFFE ID
-o, --output string            output format: json or yaml (default "json")
--profile string               named local connection profile
--server string                Conduit Hub base URL (CONDUIT_SERVER)
--spiffe-endpoint string       SPIFFE Workload API address
--timeout duration             request timeout (default 30s)
```

Every leaf command in the binary, and what a tenant uses it for:

### Connection

| Command | Notable flags |
| --- | --- |
| `configure` | (uses the global connection flags; writes `--profile` or `default` and makes it current) |
| `profile create NAME` · `use NAME` · `show NAME` · `list` · `rename` · `delete` · `export` · `import` · `schema` | |
| `version` | prints `version`, `commit`, `buildDate` |

### Catalog: authoring and publishing

| Command | Flags |
| --- | --- |
| `product-release helm-chart init` | `--chart-path` `--name` `--version` `--repository-url` `--maven-coordinate` `--artifact` (repeatable, `@sha256` required) `--values` `--config-schema` `--output-dir` `--as-tgz` |
| `product-release terraform init` | |
| `product-release init` | `--manifest-file` `--output-dir` |
| `product-release verify` | `--manifest` `--print-manifest` |
| `product-release create` | `--manifest` `--product-tgz` `--default-config` / `--values` `--config-schema` / `--values-schema` |
| `product-release asset publish` | `--asset-path` `--maven-coordinate` `--manifest-file` |
| `product-release list` | `-p, --product-id` (**required**) `--release-channel` `--page-size` |
| `product-release get MAVEN_COORDINATE…` | |
| `product-release definition schema` · `definition verify` | |
| `helm-chart validate` | `--chart-path` `--name` `--version` `--values` `--repository-url` |
| `product list` | |

### Environments

| Command | Flags |
| --- | --- |
| `environment create NAME` | `--contact-team` (authors a descriptor locally) |
| `environment list` · `get ENV` · `reported-state ENV` | |
| `environment version compare ENV` | `-r, --release-channel` |
| `environment manifest generate ENV` | `--namespace` `--image` (`@sha256` required) `--control-plane-url` (https) `--client-tls-secret` `--external-ca-configmap` `--agent-id` `--k8s-storage-class` |
| `environment setting get` · `setting edit` | `-f, --file` `--settings-file` / `--content` |
| `environment config get` · `config apply` | `--environment-config` |
| `environment release-channel set ENV` | `-r, --release-channel` `-f, --file` |
| `environment artifact-registry list` · `assign ENV REG…` · `unassign` | `-f, --file` |
| `environment delete ENV` | `-f, --file` (proposes uninstalling **all** Entities) |
| `artifact-registry list` | |

### Change: propose, review, approve

| Command | Flags |
| --- | --- |
| `plan propose` | `-f, --file` (`-` for stdin) |
| `plan get PLAN_ID` | |
| `plan approve PLAN_ID` | `--rationale` |
| `plan cancel PLAN_ID` | `--reason` |
| `changerequest list` (alias `change-request`) | `--environment` `--status` `--author` `--title` `--limit` |
| `changerequest get CR_ID` | |
| `changerequest approve` · `reject` | `--rationale` `--dry-run` |
| `changerequest cancel` | `--reason` `--dry-run` |
| `changerequest reopen` | `--dry-run` |
| `entity install` | `-e, --environment` `-p, --product-id` `-f, --file` (**required**) |
| `entity list` | `-e, --environment` |
| `entity get AEID` | |
| `entity enforce INSTALLATION_ID` | `-v, --version` `-f, --file` `--reason` |
| `entity aggregate` | `-r, --release-channel` `--generate-versions` `--generate-lowest` `--output-dir` |

### Governance

| Command | Flags |
| --- | --- |
| `approval-policy apply POLICY_ID` | `-f, --file` (`-` for stdin) |
| `team apply TEAM_NAME` | `-f, --file` |
| `grant create` | `-f, --file` |

### Desired-state editing (writes a document, not the Hub)

Each takes the current document and emits the next generation. All support
`--dry-run` except where noted.

| Command | Flags |
| --- | --- |
| `label list TYPE ID` · `get TYPE ID` | `-f, --file` `--label-id` |
| `label set TYPE ID` · `delete TYPE ID` | `-f, --file` `--label-id` `--label-value` `--dry-run` |
| `release-channel create NAME` | `--description` `--label-requirement` `--label-requirement-value` `--state-file` `--dry-run` |
| `release-channel update` · `add` · `import` · `export` · `get` · `list` · `diff` | `--state-file`, `-r/--release-channel`, `-f/--maven-coordinates-file`, `-y/--yes` |
| `product promotion get PRODUCT_ID` | `--state-file` |
| `product promotion set` · `append` | `-f, --file` `--state-file` `--override` `--dry-run` |
| `product promotion enable` · `disable` | |

### Modules

| Command | Flags |
| --- | --- |
| `module lock` | `-f, --module-definition-file` `--module-directory` `--verify` |
| `module create` | `-f, --module-definition-file` `--module-directory` `--dry-run` |
| `module update` · `list` · `get MODULE_PRODUCT_ID` · `diff` · `evaluate` · `flatten` | |
| `module install MODULE_PRODUCT_ID_OR_COORDINATE` | `-e, --environment` `--version` `-r, --release-channel` `--variable key=value` (repeatable) |

### Operations

| Command | Flags |
| --- | --- |
| `suppression list` | `-e, --environment` |
| `suppression remove SUPPRESSION_ID` | |
| `cve check` | `-f, --maven-coordinates-file` `-p, --product-id` `-e, --environment` `-r, --release-channel` `--details` |

### Deliberately unavailable

`cve list` · `target-hub get|list|pipeline list` · `terminal grant|policy`.
Each exits non-zero with a written reason and a pointer to what to use instead.
`terminal` also carries `--session-id` for an *external* audited break-glass
session identifier, which Conduit records and never executes.

---

## §3: The two HTTP surfaces

### `/api/v0` — the pinned public Apollo contract, 13 operations

Regenerated from `docs/reference-platform/snapshots/reference-openapi-…/api-definition.yml` and
diffed against the committed router by `scripts/verify-apollo-routes.sh`, so the
list cannot drift silently.

```
POST /api/v0/products/helmChart/{mavenCoordinate}/publish
POST /api/v0/products/service/{mavenCoordinate}/publish
GET  /api/v0/products
GET  /api/v0/products/{productId}
GET  /api/v0/products/{productId}/releases
GET  /api/v0/products/{productId}/releases/{productVersion}
GET  /api/v0/environments
GET  /api/v0/environments/{environmentRid}
GET  /api/v0/environments/{environmentRid}/installations
GET  /api/v0/environments/{environmentRid}/installations/{installationRid}
POST /api/v0/status/environments/{environmentId}/entities/info
POST /api/v0/status/environments/{environmentId}/entities/health
GET  /api/v0/namespaces
```

Refusal envelope, observed:

```json
{"error":"forbidden","errorCode":"PERMISSION_DENIED",
 "errorInstanceId":"…","errorName":"ApiUsageDenied","parameters":{}}
```

The contract declares no status codes at all — every path lists only `200` — so
it constrains the body, not the status. A `401` is preserved as a `401` rather
than being rewritten to `403`. Resource identifiers are Apollo-shaped, e.g.
`ri.apollo.main.environment.ee85a5def38213d10b5eb97e`.

**Two caveats a tenant must carry.** The RID *locator* — the segment after the
type — is a registered Conduit invention (`apollo.RIDLocator`: the first 12
bytes of a SHA-256 over a stable input, hex encoded), because Apollo's published
examples use the placeholder `123`. Identifiers for resources Apollo publishes
no type token for are minted under `ri.conduit.main.…` instead
(`apollographql.ConduitNamespacedRID`). Both are opaque and self-consistent
inside Conduit; **do not round-trip one into a real Apollo API**.

The `/api/v0` operations serve a bounded projection, not the whole Hub. On a
live Hub holding one Product, `/v1/products` returned it and
`/api/v0/products` returned `{"products":[]}` — different projections, not a
bug and not a version skew.

### `/v1` — Conduit's own operator surface

This is what the CLI and the console call. Paths do not overlap `/api/v0` at
all. The routes a tenant reaches for, by area:

| Area | Routes |
| --- | --- |
| Session and dashboard | `GET /v1/session`, `GET /v1/overview` |
| Environments | `GET /v1/environments`, `/{id}`, `/{id}/entities`, `/{id}/reported-state`, `/{id}/commands`, `/{id}/profile` (`GET`/`PUT`), `/{id}/profile/revisions`, `/{id}/suppressions` (`GET`/`POST`), `/{id}/leases`, `/{id}/health-targets`, `/{id}/health-observations`, `/{id}/target-hub-versions`, `/{id}/module-installations` |
| Catalog | `GET /v1/products`, `/{product}/releases`, `/{product}/releases/{version}`, `/{product}/publications`, `GET /v1/catalog-retirements`, `GET /v1/labels`, `/{labelID}`, `/{labelID}/revisions` |
| Publication | `POST /v1/product-publications/helm-chart`, `POST /v1/product-publications/terraform`, `POST|PUT|GET|HEAD /v1/artifacts/blobs[/{digest}]` |
| Plans | `POST /v1/plans`, `GET /v1/plans`, `GET /v1/plans/{id}`, `POST /v1/plans/{id}/approvals`, `/cancel`, `/start`, `/heartbeat`, `/finish`, `/undo-unmanage`, `POST /v1/adapted-plans/{adapter}` |
| Review | `GET /v1/change-requests`, `/{id}`, `POST /{id}/approve`, `/reject`, `/reopen`, `/cancel`, `PUT /{id}/labels` |
| Governance | `GET /v1/approval-policies`, `PUT /v1/approval-policies/{policyID}`, `GET /v1/teams`, `GET /v1/teams/{teamName}`, `PUT /v1/teams/{teamName}`, `GET /v1/resource-grants`, `POST /v1/resource-grants`, `GET /v1/authorization-audit` |
| Promotion | `GET /v1/products/{product}/promotion-stages/{from}/{to}`, `POST /v1/promotions/evaluations`, `GET /v1/promotions`, `GET /v1/promotions/{id}` |
| Suppression | `GET /v1/environments/{id}/suppressions`, `POST` same, `DELETE /v1/suppressions/{id}` |
| Modules | `GET /v1/modules`, `POST /v1/modules/releases`, `POST /v1/modules/lock`, `GET /v1/modules/{name}/releases`, `POST /v1/modules/{name}/releases/{version}/promote`, `/recall`, `GET|PUT /v1/modules/{module}/auto-update-policy`, `POST /v1/environments/{id}/module-installations`, `POST /v1/module-installations/{id}/pause`, `/resume`, `/update`, `/update/automatic`, `/unlink` |
| Bundles and export | `PUT|GET /v1/export-pipelines[/{id}]`, `POST /{id}/preview`, `/build`, `GET /{id}/schedule-runs`, `PUT /{id}/target-hub-versions/{targetHub}`, `POST /v1/bundles/preview`, `/import`, `GET /v1/bundles/{id}`, `GET /v1/bundle-imports`, `POST /v1/bundles/{id}/transfer-confirmations`, `POST /v1/bundle-transfer-confirmations/{id}/rescind`, `GET /v1/bundles/signing-key` |
| Commands and fleet | `POST /v1/commands`, `GET /v1/fleet/installations`, `POST /v1/fleet/actions` |
| Validation | `POST /v1/validate`, `POST /v1/apollo/product-manifests/validate`, `POST /v1/apollo/environment-settings/validate` |
| Scanning | `POST /v1/artifact-scans`, `GET /v1/artifact-registries` |
| Apollo GraphQL | `GET /v1/apollo/graphql/schema`, `POST /v1/apollo/graphql` |
| Notifications (**Conduit extension**) | `GET /v1/notifications/inbox`, `GET /v1/notifications/events`, `GET|PUT /v1/notifications/preferences` |
| Health and identity | `GET /health/live`, `GET /health/ready`, `GET /version` |

Notification routes are **mounted only when the deployment has a durable
notification store** — that is, PostgreSQL. Under `REPOSITORY_KIND=file` they
are absent rather than answering `500`. Their shapes are the registered
invention `notifications.HTTPSurface`; the pinned Apollo OpenAPI publishes no
notification operation, so nothing here is claimed to be Apollo's.

`GET /v1/overview` returns `generatedAt`, `staleAfterSeconds`, and six sections
— `awaitingApprovals`, `failedPlans`, `runningPlans`, `unhealthyEntities`,
`staleReports`, `blockedPromotions` — each with `status`, `count` and `items`.
A section that could not be computed reports `status: UNAVAILABLE` rather than
`count: 0`, so an unavailable section never reads as a healthy one.

`GET /v1/authorization-audit` returns `audit.3` events with
`categories: ["authorizationCheck"]`, `subject`, `action`, `resourceRid`,
`decision` (`ALLOW` or `DENY`), `reason`, and request/result fields naming the
operations and targets checked. **Allows are recorded, not only denies** — an
audit trail holding only refusals cannot answer who was let in.

This administrator-only read uses Conduit's native pagination: `limit` defaults
to 100 and is capped at 200; `cursor`, inclusive `since`, exclusive `until` and
`resourceRid` bound the read. Omitted `order` or `order=asc` preserves ascending
recorded order; `order=desc` selects the most recently recorded events first.
`view=chain` returns a reconstructed projection folded in the requested order,
not the stored ledger's sequences or digests. The console requests descending
pages and keeps the returned projection order for proof checks. See
`docs/ui/contracts/pagination.md` for cursor binding and restart behavior during
legacy audit adoption.

For verified JSONL archival use
the Hub binary's `verify-authorization-audit --export-file=...` with an
independently retained `--checkpoint-file`; `--archive-file=...` checks that
archive offline. See `docs/operations/authorization-audit-integrity.md` for the
explicit bootstrap mode, guarantees and remaining live-retention decision.

---

## §4: The operator console

Served at `/console/` when the Hub is built with a console handler. The
navigation is:

- Rail: **Home** (`/overview`), **Environments**, **Software Catalog** (`/products`), **Notifications**
- Applications: **Change Requests**, **Fleet**, **Plans**, **Promotions**, **Export**, **Import**, **Risk Management**, **Teams**

Search and Settings are deliberately absent: there is no `/v1/search` contract
and no settings surface, and a rail button opening an empty omnibar would
promise a capability the Hub does not have.

The notification screen shows the **calling principal's own** inbox on a chosen
channel and edits the preference for one resource. It names no owner on any
request, so it cannot ask for somebody else's inbox. It distinguishes empty from
loading from failed — an empty inbox arrives as `{"entries":null}`, because Go
encodes an empty slice as null, and a failed request reads as a failed request
rather than as an empty inbox.

There is **no unread state**: `InboxEntry.ReadAt` exists and no route writes it.
There is **no team inbox**: a team inbox needs a contact no store persists.

---

## §5: The profile document

`$XDG_CONFIG_HOME/conduit/config.yaml` (override with `CONDUIT_CONFIG_FILE`),
written mode `0600`. `conduit profile schema` prints the authoritative JSON
Schema.

```yaml
version: 1                       # const 1
current: staging                 # must name an existing profile
profiles:
  staging:
    name: staging                # must equal its map key
    server: https://conduit.example.com
    timeout: 30s
    output: json                 # json or yaml
    # exactly one identity family:
    bearerTokenFile: /etc/conduit/token
    # clientCertFile / clientKeyFile
    # spiffeEndpoint / hubSpiffeId
    # caFile
```

---

## §6: Environment variables

### The CLI reads

`CONDUIT_SERVER` · `CONDUIT_CA_FILE` · `CONDUIT_CLIENT_CERT_FILE` ·
`CONDUIT_CLIENT_KEY_FILE` · `CONDUIT_BEARER_TOKEN_FILE` ·
`CONDUIT_SPIFFE_ENDPOINT` · `CONDUIT_HUB_SPIFFE_ID` · `CONDUIT_PROFILE` ·
`CONDUIT_CONFIG_FILE`

### The Spoke agent reads

`CONTROL_PLANE_URL` · `CONTROL_PLANE_CA_FILE` · `CONTROL_PLANE_CLIENT_CERT_FILE`
· `CONTROL_PLANE_CLIENT_KEY_FILE` · `CONTROL_PLANE_SPIFFE_ID` ·
`ENVIRONMENT_ID` · `AGENT_ID` · `EXTERNAL_MATERIAL_PREPARER` ·
`HEALTH_PROBE_CA_FILE`

### The Hub reads (the ones a tenant sets)

| Variable | Effect |
| --- | --- |
| `LISTEN_ADDRESS` | Where the Hub serves |
| `REPOSITORY_KIND` | `file` or `postgres`; anything else is a startup error. `file` is one replica only |
| `TLS_CERT_FILE` / `TLS_KEY_FILE` / `SPIFFE_ENDPOINT_SOCKET` | Server identity |
| `AGENT_CLIENT_CA_FILE` | The CA that signs Spoke client certificates |
| `PLATFORM_BOOTSTRAP_ADMINISTRATORS` | The initial administrator list |
| `BUNDLE_SIGNING_KEY_FILE` | Bundle signing. **Absence does not stop the Hub** — it warns and signs with an ephemeral development key |
| `BUNDLE_TRUSTED_KEYS_FILE` | Which signers imported Bundles may carry |
| `METRICS_ADDRESS` | The metrics listener, off when unset |
| `NOTIFICATION_WEBHOOK_URL` / `NOTIFICATION_WEBHOOK_TIMEOUT` / `NOTIFICATION_DELIVERY_LOG` | Notification delivery, off when unset; setting both destinations is a startup error |
| `EXTERNAL_ARTIFACT_SCANNER` / `_TIMEOUT` | The scanner `conduit cve check` drives |
| `CONSOLE_BOOTSTRAP_FILE` | Tenant runtime overrides for the console bundle |
| `GITLESS_*` | See `docs/operations/gitless-gitops.md`; every binding is parsed and every bound Environment's publisher policy looked up **before** the process serves, so a misconfiguration is a startup failure rather than a first-tick surprise |
| `ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS` / `ALLOW_INSECURE_AGENT_IDENTITY_HEADERS` | Development only. The binary exits rather than combine either with a secure mechanism, and the chart aborts if either is set |

---

## §7: Where the ground truth lives

| Question | Read |
| --- | --- |
| Does this command exist? | `conduit <command> --help` on your build |
| What does `/api/v0` publish? | `docs/reference-platform/snapshots/reference-openapi-…/api-definition.yml` |
| Which shapes are guesses? | `docs/evidence/conduit-inventions.json` |
| Where does Conduit deliberately differ from Apollo? | `docs/operations/TENANT-FIDELITY.md`, "Declared divergences" |
| What is not production-ready? | `docs/operations/PRODUCTION-READINESS.md`, blocking gaps and residual items |
| How do schema upgrades work? | `docs/operations/schema-migrations.md` |
| How do I back this up? | `docs/operations/backup-and-restore.md` |
| How do I run without a Git credential in the cluster? | `docs/operations/gitless-gitops.md` |
