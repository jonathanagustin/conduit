# Conduit CLI contract and parity plan

`conduit` is Platform's native operator CLI. Its source entrypoint is
`cmd/conduit`, its installed executable is `conduit`, and it calls only
Platform's public, semantic operator APIs. It is not a renamed Apollo CLI and
does not claim compatibility with a private Apollo Hub, profile file, archive
byte stream, authentication service, or control protocol.

## Ground-truth denominator

The primary command-shape source is Palantir's official
[Apollo CLI specification](https://www.palantir.com/docs/apollo/apollo-cli/).
On 2026-08-03, the committed public-documentation manifest identified 91 Apollo
CLI reference pages. `scripts/capture-apollo-cli-contract.mjs` fetched exactly
those URLs, extracted only command names, synopses, flags, support status, and
the SHA-256 of each source Markdown document, and wrote
`docs/apollo/cli-command-contract.json`. The fixture contains 69 executable
synopses and is checked offline against the already committed source hashes by
`scripts/verify-conduit-cli-contract.mjs`.

The user-supplied
[`park-kyungchan/palantir-mini-marketplace`](https://github.com/park-kyungchan/palantir-mini-marketplace/tree/e0e850a6914b9459b25e042d5626765e6146894b/plugins/palantir-mini/runtime-overlay/research-library/palantir-official/apollo)
snapshot is corroborating evidence only. Its front matter says it was extracted
from public Next.js Markdown on 2026-05-12. It preserves a larger deprecated
command set, but predates current `artifact-registry` and `label` commands and
the current SSO token-provider text. The current official corpus wins every
conflict.

Palantir's public `foundry-platform-python` 1.101.0 CLI at commit
`168d51910570b6966afa2320333ab36f6a3f7ccf` was also reviewed. Its generated
API-group/resource/operation tree is useful implementation guidance, but it is
not the requested product contract and establishes no Apollo behavior.

## Meaning of parity

Parity means that an operator familiar with the documented Apollo CLI can find
the corresponding Conduit workflow with the same resource-first command shape,
typed flags, file/stdin support, shell completion, and deterministic
machine-readable output. It does not mean matching undocumented output bytes or
calling Apollo endpoints.

Conduit deliberately diverges where Platform's trust model is stronger:

- Conduit supports mounted static mTLS, rotating SPIFFE identity, and a
  short-lived Multipass-compatible bearer token read from a referenced file.
  The token file must be a bounded regular mode-0600 file on Unix; the token is
  never accepted as a positional argument, written to a profile, logged, or
  added to a redirected request.
- `--insecure-skip-verify` has no production equivalent. The only insecure mode
  is an explicit development subject accepted for loopback HTTP.
- mutating operations retain explicit rationales, RBAC checks, Change Requests,
  Plan approval, exact coordinates, and immutable execution evidence;
- Environment inventories, Product catalogs, charts, credentials, and install
  configuration remain in their owning product repositories. CLI authoring
  commands may emit those files but do not move their ownership into Platform;
- JSON and YAML are stable automation formats. `pretty` is currently a
  human-readable YAML projection; an optional future Charmbracelet TUI would
  remain a presentation adapter over the same semantic service, not an
  alternate execution path.

## First workflow gate

The official
[experimental end-to-end workflow](https://www.palantir.com/docs/apollo/apollo-references/experimental-end-to-end-workflow/)
is the first vertical-slice acceptance test.

| Official step | Conduit consequence | Closure gate |
| --- | --- | --- |
| `environment create` | Author a validated, Git-owned Environment descriptor and return its stable ID. Do not create an implicit mutable server inventory. | Golden JSON/YAML plus invalid-ID/contact-team tests |
| `module install` | Resolve an exact Module release or explicit Release Channel and create/update the Environment installation through the existing semantic Module API. | HTTP contract and idempotent install/update tests |
| `environment manifest generate` | Render the reusable Spoke chart inputs/manifests without owning Namespace, credentials, storage class, or cluster-wide RBAC; stdout must remain pipeable to the operator's deployment tool. | Helm 3/4 render plus restricted/Rubix profile gates |
| `product-release helm-chart init` | Produce the documented `helm-chart.v1` manifest/archive shape while requiring the exact OCI digests Conduit needs for execution. | Reconstructed 0.814.0 semantic fixture plus deterministic archive tests; native byte equivalence remains H17 |
| `product-release create` | Validate the manifest, publish immutable Product/Release metadata through the connected-publisher boundary, and reject coordinate overwrite. | Publication replay/overwrite and authorization tests |
| `entity install` | Author or propose an exact-digest desired-state change and return its Change Request/Plan; never bypass explicit approval. | End-to-end approved Plan, Helm install, and exact reported-state test |

The workflow is complete only when one checked-in script runs those six Conduit
commands against an ephemeral Hub, Spoke, Kubernetes cluster, and OCI registry.
The existing `scripts/test-spoke-e2e.sh` remains the runtime gate and should be
extended rather than replaced.

## Current implementation

The Cobra adapter contains every command path in the pinned 91-page reference
tree. `internal/conduitcli/contract_test.go` compares that tree mechanically;
the offline source verifier separately confirms the captured pages and hashes.
Commands are implemented through typed semantic ports or local authoring
operations. `terminal` is the sole deliberate capability refusal: interactive
remote shells would violate the no-SSH/no-`kubectl exec` trust boundary, so it
returns an explicit policy error instead of pretending to work.

Implemented families include Artifact Registries, Change Requests, profiles,
CVE checks, Entity operations, Environment configuration/catalog/manifests,
Helm validation, labels, Modules, Products and promotion pipelines, Product
Releases and assets, Release Channels, Plans, suppressions, Teams, grants, and
approval policies. The adapted six workflow command paths include:

- `environment create` emits a validated Git-owned descriptor;
- `module install` accepts the documented `module:<name>:<version>` coordinate
  (or `module:<name>` with an explicit Release Channel) and calls the typed
  Module installation port; the unprefixed `name[:version]` form remains a
  Conduit convenience rather than a native-byte compatibility claim;
- `environment manifest generate` emits a pipeable restricted Kubernetes List,
  requires an exact Agent image digest, and references externally supplied
  Namespace identity material rather than creating it;
- `product-release helm-chart init` authors a validated `helm-chart.v1`
  manifest and refuses to overwrite existing files;
- `product-release create` publishes through the authenticated, immutable
  connected-publisher boundary. Native Conduit publication additionally
  requires explicit chart and image tags plus SHA-256 digests; the durable
  receipt survives Hub restart and only exact publications enter the
  executable Release catalog; and
- `entity install` validates a product-bearing exact desired-state file and
  proposes a governed Plan.

All remote commands use a narrow `internal/operator` service rather than an
arbitrary HTTP escape hatch. JSON/YAML input rejects unknown fields and multiple
documents; responses are bounded by the existing operator transport.

Named profile configuration stores only endpoint and credential-file
references—including an optional bearer-token file path—in an atomic mode-0600
document. Static bearer, mTLS, SPIFFE, and loopback-development identities are
mutually exclusive. The embedded Draft 2020-12 schema
is available through `conduit profile schema`; strict runtime validation also
enforces the cross-field and map-reference constraints that JSON Schema cannot
express. Semantic tags build deterministic
Linux/macOS/Windows archives for amd64/arm64, generate SPDX SBOMs and checksums,
create keyless Sigstore signature bundles, and create and immediately verify a
keyless SLSA v1 provenance bundle for every archive. These portable bundles do
not depend on GitHub's unavailable repository-attestation API for user-owned
private repositories.

`scripts/test-spoke-e2e.sh` now invokes the six steps through `conduit`, asserts
their machine-readable outputs, and then drives the resulting approved Plan
through the Spoke. A fresh 2026-08-03 local k3d run passed OCI pull, approved
Plan, Helm install/upgrade, and exact reported state with the `0.13.6` working
tree. H61 remains `OPEN` until the immutable release is reconciled and the same
version passes the external deployment smoke test. Exact private Apollo wire,
profile, terminal, and archive-byte compatibility remain unclaimed.

## AWS deployment smoke test

On 2026-08-03, the current working-tree `conduit` binary was exercised from a
short-lived Restricted Pod in the `dev-use2` EKS deployment. The Pod matched
the deployment-owned `platform-operator` SPIRE selector and obtained
`spiffe://platform.delivery/operator/aws-deployer`; the CLI pinned the Hub peer
to `spiffe://platform.delivery/hub`. No identity headers, bearer credentials,
Secret reads, approvals, or desired-state mutations were used.

Against Hub and Spoke `0.13.1`, exact Change Request and Plan reads,
Environment reported state, suppression listing, and Environment version
comparison succeeded over the internal HTTPS Service. Catalog-oriented reads
(`environment list/get`, `product list`, `module list`, and Artifact Registry
listing) returned HTTP 404. This is a release skew, not an authentication
failure: the local `v0.13.1` route inventory predates the corresponding Hub
routes implemented in this working tree. A release containing both CLI and Hub
route additions must be reconciled before those commands can pass live.

The test Pod was deleted after execution. Flux remained Ready at runtime
revision `sha256:6474302e4716356e613528913260e21daaf408cc5fe90d37ffa9948bf26c696c`,
and the Hub returned to one Ready and Available replica. During an active
image-pre-pull Plan, the `0.13.1` Spoke then exposed a separate typed-nil
composition defect: its absent external material preparer crossed an interface
boundary as non-nil and panicked in `PrepareImages`. The working tree now
assigns those interfaces only for a concrete provider and makes a nil receiver
fail closed; command-level and Agent regression tests cover both boundaries.
The deployed Spoke continues to require a fixed release and GitOps digest
update. This operational observation is not a committed raw tenant capture and
therefore does not elevate unrelated tenant-topology claims.
