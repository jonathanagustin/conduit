# Recovered Apollo Java object alignment

Reviewed 2026-08-02 from the recovered Conjure Java object corpus under
`com.palantir.foundry.recovered`, then joined to Palantir's official,
provenance-pinned generated OSDK TypeScript. These files are generated/recovered
contract objects, not authored server source. The source-derived Go pipeline is
documented in [`OSDK-CONJURE-GO-GENERATION.md`](OSDK-CONJURE-GO-GENERATION.md).

## Exhaustive scope

A mechanical content scan finds exactly 42 Java files that reference an
`Apollo*` type:

- 30 files whose own name begins with `Apollo`;
- three secret-input sibling shapes;
- publishing status and batch status objects;
- create, update, adopt, failed-create, and stored managed-installation
  records;
- a Foundry Products downloader-store manifest; and
- the Apollo publication arm of the release-creation error union.

Hashing each file, sorting by relative filename, and hashing that manifest
produces closure SHA-256
`35ce45d7280d157d2fd9955dda2c194c85081abd7760274c92c7efe56017e207`.
The 30 name-prefixed files alone produce
`7ac2a58caa8bad8003b9839799b9a992a399f2952f7b5fa9c04bdafe8d2955b6`.
The exact 42 filenames are retained in
[`reference-java-object-closure-2026-08-02.json`](reference-java-object-closure-2026-08-02.json).

This direct-reference closure is distinct from the generic BlockSet input
catalog. `ResolvedInputShape` has 107 tagged variants plus its root wrapper,
and `InputShapeResolver` has six variants plus its root. Those shapes describe
Foundry Products across ontology, data, compute, Workshop, credentials,
network policy, and other domains. Product repositories and the external
Foundry installer own their meaning; Platform intentionally transports their
resolved values as bounded JSON rather than cloning 107 product-specific
types into the delivery kernel.

## Alignment matrix

| Recovered object family | Proven fields/variants | Platform disposition |
| --- | --- | --- |
| Agent installation identity | Environment ID, Space ID, Foundry-space Maven group, installation Entity ID, secret-input mapping; update changes the secret mapping | External Agent-to-Foundry Install Manager identity. Documented and hash-corroborated; not placed on the private Hub wire. |
| Managed configuration V0 | CBAC markings plus resolved, preset-resolver, and secret-input maps | Exact field-level adapter in `ManagedInstallationConfiguration`; arbitrary resolved/preset shapes remain JSON because the executor is external. |
| Universal configuration V0 | Enrollment allowlist/denylist, required managed config, namespace denylist | Exact field-level adapter and seven public ignore outcomes, with target-namespace template resolution. |
| Secret-resolved inputs | Edge Magritte source (`config`, secret name, optional task config), markings (secret + key), named credential (secret name) | Exact tagged variants. Platform validates assignment/key coverage and sends only opaque provider coordinates; secret values never enter Hub state. |
| Agent bootstrap secret type | `TOFU_SECRET`, `PRIVATE_KEY_PASSPHRASE`, forward-compatible unknown | Deliberate non-adapter. Platform uses Environment-attested SPIFFE SVIDs and does not invent Apollo's unpublished TOFU/passphrase enrollment exchange. |
| ID/URI wrappers | Environment, Space, Entity, Plan, Plan RID, Plan Task RID, host URI, artifact URI, secret name | Native strongly validated string coordinates where Platform owns the resource; external identifiers remain opaque inputs. No private RID derivation is claimed. |
| Apollo Environment | Environment ID and `privileged` boolean | The boolean's semantics are not present in public delivery documentation, so Platform does not turn it into an authorization bypass or workload privilege. |
| Space publication status | Map from BlockSet version to Space status; tagged in-progress/failure/success; success carries artifact URI | Source-side Connected Hubs/Marketplace status boundary. Platform's public publication route returns only its pinned public API envelope and does not claim this private status wire. |
| Cross-stack/store management | Store name plus granular or stack-level config; granular enrollment/organization/group include/exclude lists; stack defaults include enablement, access, and force-default | Foundry Marketplace administration boundary. Platform owns neither store inventories nor enrollment/org/group policy. |
| Store access | `INSTALL_EVERYTHING`, `VIEW_EVERYTHING`, `NONE`, forward-compatible unknown | External remote-store permission policy; not mapped onto Spoke execution authority. |
| Downloader store | Cross-stack manifest, optional Space ID, optional deprecate-unlisted flag, Product coordinates | External source catalog policy. Platform catalog retirement remains an explicit governed tombstone and never infers deletion from an unlisted private store entry. |
| Managed installation lifecycle | Create, update, adopt existing installation, failed creation, and stored installation records carry BlockSet/Marketplace/namespace/config/version/status/RBAC metadata | External Foundry executor seam. Platform's analogous Entity Plan lifecycle remains Kubernetes/Helm-native and does not reuse private BlockSet requests. |
| Publication error | Apollo publication reason as one arm of a broader release-creation error union | Preserve actionable local authorization/validation errors; do not claim byte or enum compatibility with the private union. |

## Concrete implementation result

The generated `internal/foundryinterop/wire` package now carries a one-to-one
inventory of all 42 direct-reference files. Its stable layout is generated
from a 59-type bounded Conjure IR derived from the exact official OSDK source
commit. Scalar aliases, records, open enums, and known tagged variants have
typed Go representations. The three admitted discriminated unions are retained
as unions rather than flattened to `any`. The 107
Product-specific resolved-input variants, private status payloads, and
Marketplace/BlockSet payloads use `json.RawMessage`: their bytes and future
fields round-trip without giving them invented control-plane semantics.

Managed and universal Apollo configuration is operational, not a passive DTO:
the adapter maps it onto a native `Entity`, derives explicit secret
requirements from caller-supplied key scopes, and then uses the ordinary
desired-state validation and Plan lifecycle. Missing key scopes and unknown
secret variants fail closed. Consequently the existing approval, immutable
configuration digest, Environment-owned external-secret metadata, namespace
eligibility, and outbound-only Spoke execution rules apply. Recovered Agent
IDs never become authentication credentials; production Agent identity
remains an Environment-attested SPIFFE SVID.

The production server registers this adapter as `foundry-apollo-v0` on the
generic `POST /v1/adapted-plans/{adapter}` ingress. Its request contains a
complete native desired-state document plus Entity-named configuration items
with mode `managed-v0` or `universal-v0`, the recovered JSON payload, and an
explicit map of required keys per secret input. The ingress authenticates and
authorizes the proposer for the target Environment before it creates the same
governed, approval-gated `PlanRecord` as `POST /v1/plans`. It cannot add a
Product or Release, infer a namespace, approve a Plan, or execute work.

The executable gates are:

```text
go test ./internal/foundryinterop -run 'TestRecoveredApollo'
go test ./internal/controlplane -run 'TestAdaptedPlan|TestManagedInstallationConfiguration|TestUniversalInstallation|TestConnectedPublisher|TestApolloPublicAPI'
./scripts/verify-foundry-interop.sh
```

These tests pin the exact 42-file inventory, JSON field names, aliases and
tagged envelopes, opaque-shape preservation, all three known secret variants,
native round trips, unsupported-variant rejection, and a translated
configuration through native validation and Plan construction. This is a
first-class import adapter and still not a private-protocol compatibility
claim.
