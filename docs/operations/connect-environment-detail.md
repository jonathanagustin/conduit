# Full native Environment detail contract

Status: the full detail adapter is explicitly mounted beside REST (#392).
Current callers remain REST; #394 owns opt-in caller migration. No deprecation
or automatic fallback is activated.

`conduit.environments.v1.EnvironmentDetailService/GetEnvironment` corresponds to
`GET /v1/environments/{environmentID}` in main
`2767df8b00bf4fa04b2af768eb78b142c03c9e82`. Its response is the existing
`EnvironmentCatalogRecord`: full Environment configuration, an optional report,
and Spoke liveness. It contains no desired Entity list. This is a Conduit
extension derived from the native route and model, not a private Apollo schema.

## Native field ownership

| Contract shape                                                                            | Native source                                                                  |
| ----------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| Environment and MaintenancePolicy                                                         | `internal/controlplane/model.go`                                               |
| Namespace, organization, installed target, registry/secret assignment and policy messages | `internal/environments/domain/configuration.go`                                |
| WeeklyMaintenanceWindow                                                                   | `internal/catalog/domain/release.go`                                           |
| MaintenanceOverride                                                                       | `internal/delivery/plan/plan.go` (`Override`)                                  |
| EnvironmentRecord and reused SpokeLiveness                                                | `internal/controlplane/catalog_projection.go`                                  |
| Reused ReportedEnvironmentState                                                           | Existing `environment_observation.proto` and its native reported-state mapping |

All 18 native Environment fields and their 13 nested configuration types are
represented. `scripts/verify-environment-detail-contract.py` compares the built
descriptor against native JSON-tagged fields, scalar widths, collection shape,
message types and scalar pointer presence. The existing proto gate runs it.
The check also constrains the request to identity/Space selection and the
response to the native record; it does not establish runtime authorization.

Configuration remains raw declared state. Zero policy values are retained;
the read must not resolve effective defaults or invent readiness. String
vocabularies remain open. Registry credentials and external secrets remain
references, and the read introduces no provider dereference.

## Transport representation

Native `int` fields use signed int64, and report revision retains the existing
unsigned representation. ProtoJSON represents 64-bit integers as strings.
`NamespaceTarget.deletion_protected` is optional, preserving absent versus
explicit false. Message presence retains nil policies and missing reports; a
present image-pre-pull policy with false remains an explicit opt-out.

Only the arbitrary `Environment.Config` map uses JSON bytes (`config_json`), so
its numbers need not pass through protobuf Struct's double representation.
ProtoJSON encodes those bytes as base64. Closed Environment fields remain typed.
Repeated fields and maps do not preserve Go nil-versus-empty distinctions.
Timestamp messages retain instants; reported zero/unknown time follows the
existing reported-state contract, with no invented current timestamp. These
wire representations are not byte-identical REST JSON.

## Serving requirements

The adapter must authenticate before decoding, preserve the original
carrier and represented identity, and reuse native resource authorization.
Resource authority precedes optional Space validation and membership reads;
Space membership only narrows access. Omitted Space selection differs from a
present empty/invalid value. Forbidden, missing, malformed and unconfigured
selection outcomes must retain their native categories and store-access order.
No request field supplies a principal, grant, approval or execution credential.
Recorded `approved_by` in a maintenance override is response metadata.

Compare seeded REST/RPC values and errors for authorized,
anonymous, delegated, outsider and administrator callers; selected/mismatched
Spaces; missing reports; explicit false and absent policies; large integers;
JSON configuration and timestamps. Verify authority and Space refusal before
private reads. The adapter tests cover those boundaries; each promoted tree
still requires its actual validation results. Other remaining Hub methods stay tracked in #392.

## Native read and transport boundary

`ReadAuthorizedEnvironmentDetail` is shared with the REST route. It checks the
original carrier's Environment ReadResource authority once, then decodes and
authorizes optional Space selection, loads membership, and rejects a mismatch
before reading the catalog. The catalog port exposes only existing Plan/report
reads. Liveness samples the clock after reading the matching record.

The Connect route authenticates before decoding, accepts only the contracted
POST, and is explicitly registered in the Hub authorization matrix. Input,
permission, missing-resource and unconfigured-Space outcomes map to typed codes;
internal diagnostics are redacted. The adapter projects raw native fields and
reuses the reported-state mapper. No secret provider is called, no default is
resolved, and recorded override reviewer metadata grants no authority.
