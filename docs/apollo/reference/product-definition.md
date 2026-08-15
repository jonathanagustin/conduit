# Product Definition contracts

A Product Definition is a pair of documents: `manifest.yml` says what the
Product is and what it needs from the platform, `configuration.yml` says how one
installation of it is wired. These are reconstructed public semantics, not a
private format claim.

## Provenance

This note was written from the Apollo Product Specification traits supplied to
the reconstruction task, before any snapshot of the pages that publish them
existed. It therefore had **no `review-ledger.tsv` row**: a ledger entry asserts
a reviewed URL and content hash, and inventing one would corrupt the drift index.

**Updated 2026-08-15.** Those pages are now captured. The specification
extensions behind the contracts below are published on seven Beta pages that the
documentation crawler could not previously see — it read the rendered sidebar,
and these are excluded from it:

| Page | What it publishes |
| --- | --- |
| [`hidden_apollo-product-spec-definition-full`](https://www.palantir.com/docs/apollo/apollo-product-specification/hidden_apollo-product-spec-definition-full/) | the full manifest/configuration definition |
| [`hidden_endpoints`](https://www.palantir.com/docs/apollo/apollo-product-specification/hidden_endpoints/) | `endpoints`, `sls-status-endpoint`, `desired-port`, `path`, `external-port` |
| [`hidden_liveness-and-readiness`](https://www.palantir.com/docs/apollo/apollo-product-specification/hidden_liveness-and-readiness/) | `/status/liveness`, `/status/readiness`, the 200–399 band, the 5-second poll, `allow-traffic-when-not-ready` |
| [`hidden_volumes`](https://www.palantir.com/docs/apollo/apollo-product-specification/hidden_volumes/) | volume declaration and sizing |
| [`hidden_resources`](https://www.palantir.com/docs/apollo/apollo-product-specification/hidden_resources/) | CPU and memory requirements |
| [`hidden_replication`](https://www.palantir.com/docs/apollo/apollo-product-specification/hidden_replication/) | replica counts |
| [`hidden_substitution-reference`](https://www.palantir.com/docs/apollo/apollo-product-specification/hidden_substitution-reference/) | the substitution language |

All seven now carry a URL and content hash in
[`public-documentation-manifest.tsv`](../public-documentation-manifest.tsv) and
are watched by `scripts/verify-documentation-manifest.mjs`, so a change to any of
them fails a re-crawl rather than passing unnoticed.

This closes a **provenance gap, not an implementation gap**: the contracts were
already implemented and tested from the CLI fixtures. What was missing was any
citable source for them — which also meant a `documentedBy` citation naming one
of these pages would have been rejected as an unknown URL. They are Beta
upstream, which bounds the claim rather than weakening it.

## manifest.yml

- `product-group`, `product-name`, `product-version`, and `product-type` are the
  four required fields. Known types are `helm-chart.v1`, `terraform.v1`,
  `service.v1`, `service.v2`, and `daemon-set.v1`.
- `manifest-version` is **optional** in this validator and must be `1.0` when
  present. The Product Manifest page lists exactly four required fields and its
  minimal example carries no `manifest-version`. `apollo-cli` emits one, so the
  packaging validator that gates execution still requires it; rejecting a
  hand-written manifest for omitting a field the specification never asked for
  would be a conformance bug, not strictness.
- `extensions` is open. Unknown extensions are preserved through a round trip
  rather than rejected, for the same reason the Environment settings document
  preserves unmodelled fields: a validator that drops what it does not yet
  understand silently rewrites a Product author's document.
- `volumes-v2` is `Map<name, {volume-type: {durable-volume: {}}}>`. It is a
  tagged union whose only member is `durable-volume`, and that member is empty:
  the presence of the key is the whole signal, so an absent member is an error
  and any other member is a decode failure. Names are alphanumerics and hyphens
  only. The declaration carries no size — sizing is `configuration.yml`'s job,
  which is what lets one installation ask for more disk than another while the
  manifest stays immutable for the Release.
- `public-proxy-endpoints`, `public-tcp-endpoints`, and `public-udp-endpoints`
  are lists of `endpoints.definitions` names rather than repeated ports, so the
  two documents cannot disagree about which port is exposed. A name may not be
  both proxied and raw: the proxy terminates TLS and routes on a path, while the
  raw paths forward an allocated external port. TCP and UDP together are
  allowed, and are how a Product asks for both protocols on one external port.
- `require-stable-hostname` and `allow-traffic-when-not-ready` both default to
  `false`. They are modelled as pointers so an explicit `false` survives a round
  trip as an explicit `false`: an author who writes it is recording a decision,
  and rewriting their document to drop it loses that.
- `quorum-requirement` is `all`, `all-but-one`, `any`, or `simple-majority`,
  defaulting to `all-but-one`. `upgrade-strategy` is `rolling` or `shutdown`.
  They belong together because a rolling upgrade is only safe while the quorum
  requirement stays satisfied.
- `product-dependencies` and `product-incompatibilities` keep their existing
  range semantics.

## configuration.yml

- `resources.requests` and `resources.limits` carry `cpu` and `memory`, and the
  two use different grammars. `cpu` is a unit-less core count, so `500m` is
  rejected: it is a Kubernetes spelling the Resources extension does not define,
  and reading it as half a core would be this validator inventing a unit.
  `memory` takes `Ki`, `K`, `Mi`, `M`, `Gi`, `G`, or `T`, or a bare byte count.
  Requests may not exceed limits. Absent values default to `0.25` CPU and `1G`
  memory, and a lone limit becomes its own request.
- `replication.desired` must be **greater than zero**, and an absent block means
  two replicas. Zero is not a stop switch: the Replication extension requires a
  positive value, and an installation is stopped through its Entity rather than
  by publishing a Product that asks for nothing. The field stays a pointer so an
  absent block is distinguishable from an explicit value, not so zero can be
  written.
- `volumes` is `Map<name, {durable-storage-configuration: {desired-size}}>`.
  The storage grammar is narrower than the compute one: a positive whole number
  followed by a required `M`, `G`, or `T`, with a 10T ceiling. `1Gi`, `1.5G`,
  `1K`, and a bare `1024` are all rejected, because each is something Kubernetes
  would take and the platform would then silently reinterpret.
- The two unions must pair exactly. `durable-storage-configuration` is the
  configuration *for* a `durable-volume`, every configured name must be declared
  in `volumes-v2`, and every declared name must be sized here — an empty
  `durable-volume` carries no default, so an unsized declaration has no size at
  all. Writing `desired-size` directly under the volume name is the
  specification's worked-example spelling; the normative definition does not
  allow it and it is rejected rather than silently ignored.
- Volume overrides are upsize-only. A smaller volume cannot hold what the
  current one does, so the platform would have to discard data to satisfy the
  request; an unparseable size on either side fails closed rather than comparing
  against zero, which would permit every downsize.
- `endpoints.definitions.<name>` gives `desired-port`, optional `external-port`,
  and optional `path` (defaulting to `/`). Names are kebab-cased over `[a-z0-9]`
  because they become DNS and Service object names downstream, where case is not
  preserved. `desired-port` is bounded to 1024–19999: below it are the
  privileged ports a container should not bind, above it the ephemeral range the
  node allocates from. Two endpoints may share a port only when their paths
  differ.
- `sls-status-endpoint` names the definition serving the status resources the
  platform polls, and may be omitted only when there is exactly one definition —
  with more, an absent selection would leave the platform guessing which port
  decides whether the Product is alive. `/status/liveness`, `/status/readiness`,
  and `/status/health` are derived relative to that endpoint's own path, so a
  Product serving `/my-app` is probed at `/my-app/status/liveness`. Liveness and
  readiness answer with a status code in [200, 399]; health always answers 200
  and carries its state in the body.
- `conf` is the Product's own configuration tree. Its shape belongs to the
  Product; the only parts Platform interprets are the substitution language
  inside its strings and the reserved `conf.tags` list, which is the documented
  source of the tags that appear on produced discovery entries.
- `managed_files.<relative path>` has `type` (`json`, `yaml`, `yml`, `tmpl`),
  `content`, and `reload-type` (`install`, `runtime`, defaulting to `install`).
  Absolute or parent-traversing paths are rejected. `content` names a key of the
  `conf` block, and a name that matches nothing is rejected because it renders an
  empty file. The shorthand `some-conf.yml: some-config` takes its type from the
  key's extension and its content from the value, and fixes the reload type to
  `install`. A `tmpl` entry instead needs `deployment/templates/<name>.tmpl`
  shipped alongside; substitutions are **not** validated inside it, because those
  braces are Go template syntax fed the substituted `conf` tree via `{{ .conf }}`.
  Rendered files land in `/opt/palantir/services/<product-name>/var/conf`.
- `generated_secrets.<name>.secret-type` is one of `random_string`,
  `random_bytes`, `rsa_pkcs8`, `ec_pkcs8`, or `stig_password`. `length` carries
  the size for every type that has one — a string length, a byte count, or a key
  size — because that is how the specification spells all three. `allow-chars`
  and `disallow-chars` belong to `random_string` alone.
- `discovery.produces` and `discovery.consumes` are maps keyed by the name a
  substitution refers to, which is why they are maps rather than lists with a
  name field. A produced entry's payload is open — its contents are the
  Product's contract with its consumers. A consumed entry's `select` picks one
  field out of that payload, which is what makes a consumed reference usable
  directly as a client's `uris` list.
- `produce-to-additional-stacks` widens every produced entry, not one of them;
  the specification is explicit that it has no per-entry form.
- URI augmentation is producer-side and automatic. Any produced entry whose
  `port` is a valid port number gains `uri` and `uris`, with `scheme` defaulting
  to `https` and `path` to `/`. The platform derives the address from the
  Kubernetes Service it created, so a consumer asking for `uris` gets what was
  actually built rather than an address the author typed and must keep in step.

## Configuration overrides

An installation may merge overrides onto the Product's configuration. Maps merge
recursively; every other kind of value, lists included, is replaced wholesale,
because merging two lists has no meaning the Product author could have
predicted.

`generated_secrets` is refused rather than merged. Overwriting a generated
secret's declaration rotates the value under a running installation, which
presents as an outage or as data encrypted under a key nobody holds. Volume
overrides are upsize-only for the same class of reason.

## Specification errata

The worked example in "Tying It Together" contradicts the normative extension
pages in two places. Both are rejected, and
`TestSpecificationWorkedExampleErrata` pins them so that a future revision
repairing either one fails the build rather than passing silently:

- it writes `desired-size` directly under the volume name, where the Volumes
  extension defines `VolumeConfiguration` as a union whose only member is
  `durable-storage-configuration`;
- its bcrypt reference is `{{"bar-client-shared-secret", part: "bcrypt_hash"}}`,
  omitting the `secret` helper and leaving a bare quoted string in the braces.

Two further inconsistencies are accommodated rather than rejected, because in
each case one page's spelling is a document an author could reasonably ship:

- the port is `endpoints.definitions.<name>.desired-port` in the Product
  Specification and `.port` in the Endpoints extension; both resolve to the
  container port, so both parse;
- the `generated_secrets` example writes `ec_pkcs8` with `length: 2048`, an RSA
  key size rather than one of the curves the same page enumerates. The
  enumerated values win.

## Substitution language

Double-curly substitutions are the only place a definition can name something it
does not itself declare, which is why they are validated rather than passed
through:

| Form | Resolves against |
| --- | --- |
| `{{secret("name")}}` | a `generated_secrets` entry |
| `{{secret("name", part: bcrypt_hash)}}` | a derived form of that secret |
| `{{discovered.<name>.<field>}}` | a `discovery.consumes` entry |
| `{{self_discovered.<name>.<field>}}` | a `discovery.produces` entry |
| `{{conf.<path>}}` | this document's own `conf` tree |
| `{{ssl.<field>}}` | the certificate mount |
| `{{volumes.<name>.PathOnDisk}}` | a `volumes-v2` declaration |
| `{{endpoints.definitions.<name>.<field>}}` | an endpoint definition |

The `ssl` fields are `pem_path`, `cert_path`, `ca_path`, `keystore_path`, and
`truststore_path`; the endpoint fields are `desired-port`, `external-port`, and
`path`. A secret's parts resolve under distinct keys, so a `bcrypt_hash` cannot
be served where the raw value was asked for — which is the point of having the
part at all, since it lets a Product publish a verifier for its own shared
secret through discovery without publishing the secret.

Resolution fails closed. An unresolved reference is an error, never an empty
string: an empty credential or peer URI is the failure mode that reaches
production looking like a working configuration. An unrecognized token is also
an error — a Product that starts with a literal `{{ secret(...) }}` in its
configuration has silently lost its credential.

## Cross-document rules

Each document can be individually well-formed while the pair is uninstallable.
These checks exist only when both are read together, and publication is the last
point at which they are cheap to find:

- a public proxy, TCP, or UDP endpoint naming an absent endpoint definition, or
  one name appearing as both proxied and raw;
- `sls-status-endpoint` naming an absent endpoint definition;
- a configured volume the manifest never declared, or a declared volume nothing
  sizes;
- a `durable-storage-configuration` paired with a volume not declared as a
  `durable-volume`;
- a `desired-size` outside the storage grammar or above the 10T ceiling,
  including a ceiling breach written in a smaller unit such as `10001G`;
- any substitution naming a secret, consumer, producer, volume, or endpoint the
  definition does not declare.

## Local evidence

- `internal/controlplane/apollo_product_definition.go` models
  `configuration.yml` and validates both documents as a pair.
  `ValidateApolloVolumeOverride` is the upsize-only gate for an installation's
  replacement size.
- `internal/controlplane/apollo_substitution.go` parses and resolves the
  substitution language.
- `internal/controlplane/apollo_manifest.go` carries the workload extensions
  (`volumes-v2`, public endpoints, hostname and traffic traits).
- `internal/conduitcli/apollo-product-definition-v1.schema.json` is the embedded
  document schema, printed by `conduit product-release definition schema` and
  validated by `conduit product-release definition verify`.
- `apollo_product_definition_test.go` and `product_definition_test.go` cover a
  conformant service definition, the cross-document rejections above, per-type
  secret parameters, tri-state decoding, substitution parse/resolve/fail-closed
  behavior, URI augmentation, and schema/validator enum agreement.

## Uncertainties

- `pem_path`, `cert_path`, and `ca_path` appear in the specification's worked
  examples. `keystore_path` and `truststore_path` are derived: the same section
  documents `keystore.jks` and `truststore.jks` as part of the mount, and a Java
  Product that could not name them would have to hardcode the mount layout. A
  name outside the set is rejected rather than passed through.
- The specification has one endpoint example that omits the name
  (`endpoints.definitions.port`). Read literally that names an endpoint called
  `port`, so the name is required here; accepting the short form would let a
  typo resolve to nothing at install time.
- The volume size ceiling is enforced in Go across all units. The JSON Schema
  pattern bounds terabyte values only, because a regular expression cannot
  compare magnitudes across units; the schema documents where it stops and a
  test pins the split.
- Kubernetes pod controller selection (Deployment versus StatefulSet versus
  DaemonSet) is implied by `product-type` and durable volumes but is not modeled
  here; it stays with the execution plane.
- The specification's Annotations page is outside *this document pair* but not
  outside the platform. Its `apollo.palantir.com/*` label and annotations are
  applied by the Agent at install time rather than written by a Product author,
  so there is nothing in the pair to validate against them. They are defined in
  `internal/controlplane/apollo_annotations.go` and stamped onto rendered objects
  by a Helm post-renderer. The flag is spelled differently per Helm major
  version — an executable path on 3, a `postrenderer/v1` plugin name on 4 — and
  the plugin manifest's schema was established against a real Helm 4 binary,
  since the published documentation does not yet specify that plugin type.
  Because the liveness/readiness label lands on the Pod template, the first
  upgrade after this ships changes every workload's template hash and therefore
  rolls its replicas once; that is inherent to applying a Pod-template label and
  is not repeated on later upgrades. The health-probe annotation's payload
  contract is covered by the
  [reporting contract](../contracts/reporting.md).
- The Endpoints extension calls the status endpoint `sls-status-endpoint`, while
  the Health page calls the same thing `apollo-spec-status-endpoint`. This note
  and the model use `sls-status-endpoint`, which is the spelling that appears in
  every YAML example.
