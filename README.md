# Conduit

Conduit is an independently versioned, product-agnostic Hub-and-Spoke
delivery control plane. Its vocabulary and observable contracts align with
Palantir Apollo's public documentation, pinned OpenAPI, Product manifests, and
CLI fixtures. It is not named Apollo and does not claim compatibility with
Palantir's private Hub-to-Agent protocol.

Development is trunk-based: `main` is the only long-lived branch, changes land
through small squash-merged pull requests, and the required Trunk gate is
manually dispatched to control Actions cost. Apollo-facing changes also follow
the [tenant fidelity contract](docs/operations/TENANT-FIDELITY.md), which keeps
public contracts, sanitized tenant observations, Conduit extensions, and
unknowns distinct. See [CONTRIBUTING.md](CONTRIBUTING.md).

The repository owns the reusable platform implementation. Application Product
catalogs, Helm charts, environment inventories, credentials, and Module
installations belong in their respective product repositories. The files under
`examples/` are non-production conformance fixtures only.

Reusable Flux runtime manifests live under [`deploy/flux`](deploy/flux). A
deployment repository selects their immutable source revision and supplies only
the Environment identity, registry mirrors, credentials, and cluster overlay.
Flux reconciles the Hub/Spoke runtime; it does not replace the platform's Plan
and approval model or run beside the Spoke as a second Product Helm executor.

Tagged releases also publish signed, architecture-specific Zarf packages for
disconnected deployment. The package carries only the Hub/Spoke runtime;
Product artifacts, credentials, application namespaces, and installation
configuration retain their existing owners. See [`deploy/zarf`](deploy/zarf).

Production-fail-closed Helm charts for Kubernetes application platforms,
including a bounded public-evidence Rubix profile, live under
[`deploy/helm`](deploy/helm). Hub and Spoke are separate releases because they
have different trust, persistence, lifecycle, and cluster-placement boundaries.
They install only Platform runtime resources into existing namespaces and never
own cluster operators, CRDs, application namespaces, or Product charts.

The initial independent version is recorded in [`VERSION`](VERSION). Releases
use semantic `vMAJOR.MINOR.PATCH` Git tags; Product and Module versions remain
independent from the platform binary version.

Tagged releases publish private, multi-architecture Hub and Spoke Agent images
to GHCR with BuildKit SBOM/provenance referrers, Syft native/SPDX SBOMs, Grype
reports, and Sigstore keyless signatures and attestations bound to the release
workflow identity. Deployments verify and pin the two
immutable references from `platform-oci-manifest.json`; see
[`docs/architecture/OCI-RELEASES.md`](docs/architecture/OCI-RELEASES.md).
OCI build definitions are intentionally named [`Containerfile`](Containerfile)
and [`Containerfile.agent`](Containerfile.agent). Podman recognizes the former
natively; Docker BuildKit and all Agent builds pass the selected file
explicitly. `.dockerignore` and `.containerignore` are kept byte-identical by
the trusted-supply-chain gate. CI and local verification run integrity-pinned
Hadolint at the `info` failure threshold against both files with no ignored
rules:

```bash
HADOLINT_BIN=/path/to/hadolint ./scripts/verify-containerfiles.sh
```

That gate, and every other one CI runs, is also runnable in a single command.
It fetches the pinned Hadolint, Helm, and Zarf at the checksums recorded in
[`platform-ci.yml`](.github/workflows/platform-ci.yml) rather than restating them, and fails if
CI grows a step it does not cover:

```bash
./scripts/verify-local.sh                 # everything
./scripts/verify-local.sh 'Apollo'        # gates whose name contains Apollo
```

Set `PLATFORM_VERIFY_CACHE` to keep the downloaded tools somewhere other than
`~/.cache/platform-verify`.

The Hub image also contains `/platform-module-publisher` for narrowly scoped,
Flux-reconciled publication jobs and `/platform-operator` as the production
operator adapter. The publisher only imports Module definitions into the Hub.
The operator obtains a distinct rotating SPIFFE identity and exposes semantic
governance, Plan, approval, and reported-state operations; neither binary
bypasses Plans and approvals.

Renovate configuration is committed in [`renovate.json`](renovate.json). Once
the Renovate GitHub App is granted access to this private repository, it updates
Go modules, Docker base tags/digests, and digest-pinned GitHub Actions. Major
updates require explicit Dependency Dashboard approval and no dependency PR is
auto-merged.

An Environment can impose an `artifactSecurityPolicy`. When present, each
selected Release must include a provider-neutral, completed vulnerability
assessment that covers the exact image digests and satisfies the
Environment's required `PASS`/`ANY` scan policy. This is an
Apollo-aligned install/promotion precondition; scanner credentials and raw
vendor responses remain outside desired state. The Hub can invoke a separately
configured scanner executable and accepts only bounded, normalized evidence.

An Environment can also pin revisioned `artifactRegistries` and require an
`artifactAvailabilityPolicy`. Registry assignments constrain every selected
chart and image to an explicitly assigned OCI host. Availability evidence is
provider-neutral, must be complete, and must cover the exact immutable digests.
Mirror names are validated for deterministic ownership, but runtime repository
rewriting preserves path, tag, and digest. Plans carry only opaque credential
references; the Spoke invokes an Environment-owned preparer before Helm.

Environment `secrets` are likewise write-only external-provider assignments.
Entities bind named requirements to an assignment and required keys. The Hub
stores no value: an immutable assignment revision changes the configuration
coordinate and enters the governed Plan. The Agent applies one explicit
create/verify/replace/delete mutation per assignment and namespace before
workload reconciliation. The external system remains responsible for provider
authentication and materialization; mutations are rolled back after workload
rollback.
Helm charts receive only the documented
`apollo.secrets.<assignment>.k8sSecretName` metadata, and configuration
preprocessing fails closed when that assignment is absent.

Set `EXTERNAL_ARTIFACT_SCANNER` on the Hub to a clean absolute executable path.
The process receives an `ArtifactScan` JSON document on stdin containing only
Product/version and immutable chart/image coordinates. It must return at most
1 MiB of strict JSON containing normalized vulnerability and/or malware
assessments with exact digest coverage. `EXTERNAL_ARTIFACT_SCANNER_TIMEOUT`
defaults to `2m`. Plan creation invokes the scanner only when an Environment
security policy requires missing vulnerability evidence; scanner absence,
timeout, malformed output, inconsistent findings, or digest mismatch fails
closed. `POST /v1/artifact-scans` exposes the same authenticated operator seam.

The Hub image includes `/platform-grype-scanner` as one concrete normalizer.
Configure it as the external scanner and provide `GRYPE_BIN` plus an explicit
absolute YAML `GRYPE_CONFIG_FILE` only when the deployment supplies the Grype executable, registry identity,
database, and writable cache. It scans exact image digests and rejects Grype
ignored matches because those records lack Platform's required suppression
approval provenance. This is a local integration, not a claim that Grype
implements Palantir's scanner protocol.

The implemented kernel owns these contracts:

1. Validate immutable Helm and image release coordinates.
2. Validate the exact desired Entities for one Environment.
3. Turn Product dependencies into a deterministic, reviewable plan.
4. Expose liveness, readiness, validation, and planning over HTTP.
5. Keep literal, preset, and secret-backed Product inputs distinct and prevent
   secret material from entering desired state.
6. Expand Universal installations over authoritative namespace/enrollment
   inventory while retaining explicit reasons for every ignored namespace.
7. Persist immutable plans, explicit approvals, expiring agent leases, attempts,
   terminal outcomes, and observed Entity state.
8. Reject a successful outcome unless every planned installation reports the
   exact Product, version, Product Release digest, namespace, configuration
   digest, and healthy state.
9. Let an outbound-only Go agent lease approved Plans, renew its lease during
   execution, pull and verify the registry-reported Helm chart digest, perform
   atomic dependency-ordered upgrades, observe release metadata, and roll back
   completed steps in reverse order when a later step fails.
10. Persist manual and automatic Environment/Entity suppression windows, block
    Plan issuance while they are active, escalate automatically when failed
    Entities exceed the Environment threshold, and remove that escalation when
    the count recovers.
11. Cancel active Plans cooperatively: the Agent observes cancellation through
    its heartbeat and rolls completed work back before stopping.
12. Resolve channel-following Entities to the newest eligible Release and
    exclude Releases covered by single, closed, or open-ended recall ranges,
    including explicit exclusions and bounded downgrade floors.
13. Constrain Releases to revisioned Environment registry assignments and
    exclude channel candidates that lack exact-digest availability or
    vulnerability-policy evidence.
14. Build and admit Ed25519-signed, platform-native disconnected Bundles with
    previewable snapshot/incremental/checkpoint lineage and deletion
    tombstones. This is an Apollo-aligned format, not a Palantir wire-format
    compatibility claim.
15. Preserve health, rollout, configuration, lifecycle, and freshness as
    independent reported-state dimensions and project latest desired/observed
    installations through a filterable fleet endpoint.

Git remains the reviewed desired-state ledger. The service persists execution
state because Git alone cannot safely coordinate leases, attempts, or live
reported state. OCI registries store charts and images. The pull-based Spoke
process in `cmd/platform-spoke-agent` executes approved plans through Helm.

The Hub includes a small content-addressable blob boundary for immutable
manifests, bundles, attestations, and other control-plane content. OCI
registries remain authoritative for Helm charts and container images; this
service does not replace them or implement another secret store, identity
provider, or Helm renderer.

Artifact bytes can be uploaded with a server-computed digest using
`POST /v1/artifacts/blobs`, or verified against an expected digest using
`PUT /v1/artifacts/blobs/{sha256:...}`. `GET` and `HEAD` return immutable bytes
with OCI-compatible `Docker-Content-Digest` metadata. The filesystem backend
stores raw bytes under `blobs/sha256/<prefix>/<digest>/data` and never replaces
an existing digest address. See
[`docs/architecture/CONTENT-ADDRESSABLE-ARTIFACTS.md`](docs/architecture/CONTENT-ADDRESSABLE-ARTIFACTS.md).

The default process backend writes crash-safe JSON records below
`DATA_DIRECTORY` (local default `.platform-delivery`; container default
`/var/lib/platform-delivery`). It fsyncs each replacement and its parent
directory. This backend is intentionally limited to one control-plane replica
on one persistent volume. Multiple replicas require the planned transactional
PostgreSQL repository; a shared filesystem does not make the in-process lock
distributed.

Disconnected Bundle signing keys remain externally owned. Set
`BUNDLE_SIGNING_KEY_FILE` to a mounted PEM PKCS#8 Ed25519 private key. Set
`BUNDLE_TRUSTED_KEYS_FILE` to a mounted JSON object mapping the advertised
`ed25519:<id>` to its base64 public key. If the signing-key file is absent, the
Hub logs a warning and generates an ephemeral development key; Bundles signed
before a restart then cannot be reproduced under that key.

## Run and exercise

```bash
go test ./...
./scripts/test-gitops.sh
# Requires Docker, k3d, kubectl, and Helm:
./scripts/test-spoke-e2e.sh
ALLOW_INSECURE_AGENT_IDENTITY_HEADERS=true \
ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS=true \
PLATFORM_BOOTSTRAP_ADMINISTRATORS=local-developer \
  go run ./cmd/platform-delivery

curl --fail --request PUT \
  --header 'content-type: application/json' \
  --data '{"displayName":"Local reviewers","adminUsers":["local-developer"],"memberUsers":["local-developer"]}' \
  http://127.0.0.1:8081/v1/teams/local-reviewers

curl --fail --request PUT \
  --header 'content-type: application/json' \
  --data '{"displayName":"Local review","expression":"require_team(\"local-reviewers\", \"local review\")","enabled":true}' \
  http://127.0.0.1:8081/v1/approval-policies/local-review

curl --fail --silent --show-error \
  --header 'content-type: application/json' \
  --data-binary @examples/example-foundation.json \
  http://127.0.0.1:8081/v1/plans | jq
```

`test-gitops.sh` verifies Git-directory publication, immutable Module locks,
Release Channel gating, installation updates, pause/resume, and durable Hub
restart. `test-spoke-e2e.sh` creates an ephemeral Kubernetes cluster and OCI
registry, starts the real Hub and Spoke images, and verifies approved Plan
leasing, digest-pinned chart pull, Helm install/upgrade, and exact reported
state. Both are CI gates; the cluster and registry are deleted on exit.

Run an agent after approving a Plan:

```bash
export CONTROL_PLANE_URL=https://delivery.example.internal
export ENVIRONMENT_ID=dev-use2
export AGENT_ID=spoke-dev-use2
export CONTROL_PLANE_CA_FILE=/var/run/platform-identity/hub-ca.pem
export CONTROL_PLANE_CLIENT_CERT_FILE=/var/run/platform-identity/tls.crt
export CONTROL_PLANE_CLIENT_KEY_FILE=/var/run/platform-identity/tls.key
export LEASE_DURATION=2m
export POLL_INTERVAL=15s
# Managed-rollout fallback when no scoped readiness timeout is configured:
export HELM_TIMEOUT=5m
export HEALTH_POLL_INTERVAL=5s
# Optional private CA for annotated Pod HTTPS endpoints:
# export HEALTH_PROBE_CA_FILE=/var/run/platform-health/ca.pem
# Optional absolute path to an Environment-owned executable that consumes
# registry access, secret lifecycle, and optional image pre-pull requests as JSON
# on stdin and emits no output:
# export EXTERNAL_MATERIAL_PREPARER=/opt/platform/bin/material-preparer
# Local, explicitly insecure registries only:
# export HELM_PLAIN_HTTP=true
go run ./cmd/platform-spoke-agent
```

The client certificate must contain the URI SAN
`spiffe://platform.delivery/environment/<environment>/agent/<agent>` and be
signed by the CA configured on the Hub as `AGENT_CLIENT_CA_FILE`. The Hub also
requires `TLS_CERT_FILE` and `TLS_KEY_FILE`. The Agent transport honors the
standard `HTTPS_PROXY` and `NO_PROXY` variables. Plain HTTP and identity headers
are accepted only when the Hub explicitly sets
`ALLOW_INSECURE_AGENT_IDENTITY_HEADERS=true`, as the local k3d manifest does.

The agent expects `helm` and a namespace-scoped kubeconfig. It never creates a
Namespace. Authenticate Helm to each OCI registry before starting the agent;
standard OCI registries use the Docker credential flow (`helm registry login`).
The tenant Foundry Artifacts proxy investigated during reconstruction also
required `Authorization`, Docker/OCI `Accept`, and `palantir-asset-track`
headers, but those tenant-specific headers are not a portable Helm registry
contract and are not hard-coded here.

When `EXTERNAL_MATERIAL_PREPARER` is set, it must be a clean absolute executable
path. The Agent passes a versioned `RegistryAccess`, `SecretMutation`, or
`ImagePrePull` envelope on stdin containing only immutable metadata and opaque
provider references. The executable must idempotently prepare registry access,
apply/rollback the requested secret lifecycle action, or pre-stage every exact
image digest in the requested namespace and exit successfully; stdout/stderr
are discarded to prevent provider output from entering Agent logs. A Plan that
needs preparation fails before Helm when the executable is absent or fails.

After a successful Plan, the Agent polls the Hub for authoritative health
targets every five seconds. It discovers Pods using the conventional
`app.kubernetes.io/instance=<Entity>` label and the public
`apollo.palantir.com/health.probe` annotation, then queries only verified HTTPS
`<context>/status/health` endpoints. Charts that opt into custom health must
emit both the Helm instance label and annotation. The Agent never disables TLS
verification; mount `HEALTH_PROBE_CA_FILE` when Pod certificates use a private
CA.

For each step, the agent stages the chart with `helm pull`, rejects the plan if
Helm does not report the expected OCI manifest digest, then installs the local
package. `manageRollout` is the default. It uses `--atomic --wait` on Helm 3
and `--rollback-on-failure --wait=watcher` on Helm 4; Helm 4 explicitly retains
client-side apply semantics. The shipped runtime source-builds hash-pinned Helm
4.2.3 with fixed `oras-go` 2.6.2 and Kubernetes 1.36.3 `kubectl` with fixed
network modules into an exact-digest Chainguard Static image; the scratch Hub
imports only the CA bundle from that same reviewed image. The E2E gate also runs pinned Helm
3.21.1. `applyChangesNoWait` omits readiness waiting, rolls back an apply failure, and
reports health as unknown/in-grace until active health observation catches up.
The Helm release description records the chart and resolved-config digests. A
success report is emitted only after `helm status` observes `deployed`, the
expected chart version, and those exact digest markers. Before mutation, the
agent also renders the chart and rejects every workload image not present as an
immutable tag-plus-digest in the Release. Environment policy can require a
bounded external pre-pull of those exact images before Helm mutation.

The example follows the `RELEASE` channel and uses placeholder digests and
registry names. A promoted desired state must use registry-reported chart and
image digests and must pin `environment.generation` to the reviewed Git commit.
Exact `version` selection remains available for format adapters and controlled
recovery; normal Entities specify `releaseChannel` instead.

Creating a plan also creates a governed Change Request. CEL policies are
evaluated for every Entity change; only an `APPROVED` Change Request makes its
Plan eligible for lease. See the
[governance contract](docs/apollo/GOVERNANCE-CONTRACT.md). The minimum Plan
lifecycle is:

```text
AWAITING_APPROVAL -> APPROVED -> LEASED -> RUNNING -> SUCCEEDED | FAILED
                         |          |
                         +----------+---------------------------> CANCELLED
                                      ^
                                      +--- expired leases are reissued as a new attempt
```

Create an immediate manual Entity suppression, or cancel a Plan and create the
recommended 30-minute suppression in one operator workflow:

```bash
curl --fail --request POST \
  --header 'content-type: application/json' \
  --header 'X-Platform-Subject: operator@example.test' \
  --data '{"installationId":"example-api","reason":"incident containment"}' \
  https://delivery.example.internal/v1/environments/dev-use2/suppressions

curl --fail --request POST \
  --header 'content-type: application/json' \
  --header 'X-Platform-Subject: operator@example.test' \
  --data '{"reason":"stop rollout","createSuppression":true,"installationId":"example-api"}' \
  https://delivery.example.internal/v1/plans/pln_example/cancel
```

Automatic Plan-failure suppressions are Entity-scoped. When the number of
distinct automatically suppressed Entities exceeds
`environment.autoSuppressionThreshold` (default 30), the control plane creates
an Environment suppression and removes it automatically after the count
recovers.

The production `platform-operator` exposes the recovery path without a generic
HTTP escape hatch. It obtains its operator identity through SPIFFE and the Hub
rechecks Environment-scoped authorization:

```bash
platform-operator list-suppressions dev-use2
platform-operator cancel-plan pln_example 'superseded by a corrected deployment'
platform-operator remove-suppression sup_example
```

The first-class human and automation client is built as `conduit`:

```bash
go build -o conduit ./cmd/conduit

export CONDUIT_SERVER=https://delivery.example.internal
export CONDUIT_CA_FILE=/var/run/platform-identity/hub-ca.pem
export CONDUIT_CLIENT_CERT_FILE=/var/run/platform-identity/tls.crt
export CONDUIT_CLIENT_KEY_FILE=/var/run/platform-identity/tls.key

conduit changerequest list --environment dev-use2 --output json
conduit changerequest approve cr_example --rationale 'reviewed exact release digests'
conduit plan get pln_example --output yaml
conduit completion bash > conduit.bash
```

The first adapted delivery slice keeps authored state in its owning Git
repository and sends only semantic mutations to the Hub:

```bash
conduit environment create dev-use2 --contact-team release-team -o yaml
conduit module install com.example:foundation:1.2.3 -e dev-use2 --variable environmentId=dev-use2
conduit environment manifest generate dev-use2 --namespace platform-spoke \
  --image 'registry.example/platform/agent:1.2.3@sha256:<digest>' \
  --control-plane-url https://hub.example --client-tls-secret spoke-client \
  --external-ca-configmap platform-ca -o yaml
conduit product-release helm-chart init \
  --repository-url 'oci://registry.example/charts/application:1.2.3@sha256:<chart-digest>' \
  --maven-coordinate com.example:application:1.2.3 --name application --version 1.2.3 \
  --values values.yaml --output-dir release
conduit product-release create --manifest release/manifest.yml
conduit entity install -e dev-use2 -p com.example:application -f desired-state.yaml
```

The manifest generator references an existing Namespace, mTLS Secret, and CA
ConfigMap. It does not create credentials, cluster-wide RBAC, or product-owned
installation configuration.

SPIFFE clients may instead set `CONDUIT_SPIFFE_ENDPOINT` and
`CONDUIT_HUB_SPIFFE_ID`. Plain HTTP identity headers are available only through
the explicit `--development-subject` flag against a loopback server. The
hash-pinned Apollo CLI UX denominator, current coverage, and six-step parity
gate are documented in
[`docs/architecture/CONDUIT-CLI.md`](docs/architecture/CONDUIT-CLI.md).
Semantic release tags publish deterministic Linux, macOS, and Windows
`conduit` archives for amd64 and arm64, plus SHA-256 checksums, SPDX SBOMs,
keyless Sigstore signature bundles, and archive-bound, keyless SLSA provenance
bundles that are verified before publication.

Emergency Entity operations use `POST /v1/commands`; their durable history is
available at `GET /v1/environments/{environmentID}/commands`. Commands are
audited single-Entity Plans that take priority over overlapping routine work,
bypass ordinary eligibility constraints, and retain the same outbound lease,
cancellation, rollback, and exact-reporting protocol. See
[`docs/apollo/reference/commands.md`](docs/apollo/reference/commands.md).

Production operator calls use an injected trusted identity resolver or a
verified client certificate with URI SAN
`spiffe://platform.delivery/operator/<subject>`. `X-Platform-Subject` and
`X-Platform-Groups` are accepted only when the explicit operator-development
escape hatch is enabled. Agent identities continue to use their distinct,
Environment-scoped SPIFFE URI.

## Boundary with GitOps

GitOps supplies desired state, review history, and reconciliation. The delivery
control plane adds the concepts Git alone lacks: Products, immutable Releases,
Environments, Entities, dependency-ordered Plans, promotion constraints,
reported state, and recall. Flux distributes the reviewed runtime but cannot
bypass Change Request approval or Plan eligibility. Planning, typed input/targeting, lifecycle
persistence, leases, reported state, and the first Helm execution loop are
implemented here. Per-Environment Agent identity and outbound proxy support are
also implemented. Release Channel selection and recall-range exclusion are
implemented. Module installations track promoted Module Releases by channel,
exclude recalled targets without automatically rolling back, and support
pause/resume. Immutable channel-promotion evidence and the pinned public Apollo
API v0 adapter are also implemented. Authenticated Teams, resource-scoped
grants, per-change CEL policy evaluation, governed reviewer requirements, and
authorization audit, promotion suppression, external secret/registry material
preparation, governed Bundle imports and bulk operations, active health,
ramped rollout, observed-state-aware recall strategies, and prioritized
emergency Entity commands are implemented.
Flux remains responsible for reconciling the released runtime; provider-native
secret values, scanner engines, credentials, raw findings, and scanner-native
scheduling remain outside this repository.

## Apollo alignment and compatibility

The domain vocabulary and behavior intentionally align with Apollo's public
documentation: Maven-coordinate Products and immutable Releases, Environments,
Entities, dependency-ordered Plans, constraints, outbound Spoke execution,
reported state, Release Channels, promotion, and recall. Product packaging is
validated against the public Apollo OpenAPI and product-spec evidence where a
public contract exists.

Plan constraints include required and optional dependencies, incompatibility
ranges, default/custom dependency groups, per-Product group overrides, ignored
dependencies, suppression windows, and resolved maintenance windows. Weekly
maintenance intervals use IANA time zones and Apollo's
`DAY/HH:MM-DAY/HH:MM` syntax. The Agent can lease a reviewed Plan only while
every step is inside the intersection of its Product and resolved Entity
window, unless an active approved temporary override applies.

“Apollo-compatible” is reserved for a tested import/export boundary. The public
API adapter serves all 13 operations in the pinned API v0 OpenAPI contract. The
core does not emulate undocumented Hub endpoints, accept Apollo credentials,
or claim that an Apollo Agent can connect to it. The strict
`POST /v1/apollo/product-manifests/validate` boundary accepts and validates the
reconstructed CLI 0.814.0 `helm-chart.v1` manifest format. Native CLI artifact
compatibility remains provisional until a tenant-generated fixture confirms
that reconstruction; all other behavior is described as Apollo-aligned.

The public Apollo OpenAPI definition is vendored at its pinned upstream commit,
validated by [`../../docs/apollo/validate-public-api.rb`](../../docs/apollo/validate-public-api.rb),
and checked against the adapter's exact route set by
[`../../docs/apollo/verify-public-api-adapter.rb`](../../docs/apollo/verify-public-api-adapter.rb).
