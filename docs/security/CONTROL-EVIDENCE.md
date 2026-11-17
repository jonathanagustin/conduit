# Selected control evidence and deployment responsibilities

Version 1, 2026-11-17. Conduit source baseline:
[`de657c0266ece0e567b460d3d37aba1212658727`](https://github.com/jonathanagustin/conduit/tree/de657c0266ece0e567b460d3d37aba1212658727).
This readable component mapping accompanies the [threat model](THREAT-MODEL.md).
It provides reusable implementation evidence for a deployment's own assessment.
It is not an OSCAL component-definition file, a system baseline, whole-control
compliance, inherited accreditation, or a FedRAMP/CMMC/STIG/FIPS/ATO claim.

## Primary catalog and interpretation

Control identifiers and statement parts below use NIST SP 800-53 Revision 5,
**release 5.2.0**, from NIST's [official publication
record](https://csrc.nist.gov/pubs/sp/800/53/r5/upd1/final). The exact primary
[OSCAL catalog](https://github.com/usnistgov/oscal-content/blob/5006f18c8047643eaa70e35dd0d20a58bcc41856/nist.gov/SP800-53/rev5/json/NIST_SP-800-53_rev5_catalog.json)
was read at commit `5006f18c8047643eaa70e35dd0d20a58bcc41856` on 2026-11-17.
Its metadata version is `5.2.0`, OSCAL version `1.2.3`, last-modified
`2026-10-22T11:30:00.00000-00:00`, and file SHA-256 is
`64ca6b4161de51ab6af098a7d101d326f14c95e1abf721396cd8a049e8115eeb`.
No upstream catalog or manual is copied into this repository.

The selected statements are summarized, not reproduced or tailored. Unlisted
controls/enhancements and organization-defined parameters have not been
assessed. A deployer must select its own applicable profile, parameter values,
assessment procedures and authorization boundary. An example Environment named
`FEDRAMP_HIGH` supplies none of those decisions. NIST's [component-definition
guidance](https://pages.nist.gov/OSCAL/documentation/schema/implementation-layer/component/)
explains how component evidence can inform a system security plan; this document
does not make that plan or its authorization decision.

Status applies only to the **bounded component contribution described**:

- **Supported:** code and named tests support that bounded mechanism.
- **Partial:** useful mechanisms exist, but the selected statement has material
  gaps or additional deployment dependencies.
- **Deployment-owned:** organization/cluster/issuer configuration establishes
  the result; this repository supplies integration evidence only.
- **Not established:** the required assurance or decision is absent here.

Test references are evidence locations, not fresh execution receipts. For each
deployed version, retain exact configuration and negative-case results outside
this public repository without exposing credentials or tenant payloads.

## Access and identity

| Catalog statement / status | Repository evidence and enabling configuration | Deployer validation, responsibility and limits |
| --- | --- | --- |
| **AC-3, Access Enforcement** (`ac-3_smt`): enforce authorized logical access. **Supported** for the registered Hub route/grant and Environment-bound Agent checks. | [Route register](../../internal/hub/composition/authorization.go) and [route/tenant tests](../../internal/hub/composition/authorization_test.go); [Agent identity test](../../internal/controlplane/identity_test.go); [Plan authorization](../../internal/controlplane/http.go). Enable verified operator identity and Agent TLS/SPIFFE; leave `ALLOW_INSECURE_*_IDENTITY_HEADERS` disabled and configure native grants. See [route policy](../operations/route-authorization.md). | As A, attempt B's by-name/index/approval requests and Agent lease/Plan operations with real credentials. Check response and audit against an authorized positive case. Deployment owns teams/grants/delegation and issuer policy. This contribution is not proof of every product integration or an independent protocol review (TM-01–04). |
| **AC-4, Information Flow Enforcement** (`ac-4_smt`): constrain flows under chosen policy. **Partial.** | Operator row filtering and Bundle import/export policy are in [route tests](../../internal/hub/composition/authorization_test.go) and [Bundle tests](../../internal/controlplane/bundles_test.go). [HTTP model rules](../operations/model-http.md) and [experimental ACP limits](../architecture/workspace-acp-connections.md) describe separate egress boundaries. | Choose data classifications/destinations, network policies and approved model/provider access. Validate actual rendered UDS/cluster egress and offline transfers. Outbound-only Agent control traffic, prompt framing and loopback filtering do not enforce all process/network information flows; see TM-06/11–13 and #887. |
| **AC-5, Separation of Duties** (`ac-5_smt.a`, `.b`): define incompatible duties and their authorizations. **Partial.** | [Governance/approval contract](../architecture/identity-and-governance.md#approval), [approval authorization tests](../../internal/controlplane/plan_approval_authorization_test.go), and [ACP independent-review mapping](../architecture/workspace-acp-connections.md). Native change policy governs leaseability; experimental ACP high-risk callbacks require a separate reviewer. | Define incompatible roles, configure required reviewers and test self-/cross-scope refusal under the chosen policy. The repository does not define the organization's duties, personnel independence or a universal two-person rule for every route (TM-03/10/13). |
| **AC-6, Least Privilege** (`ac-6_smt`): limit access to assigned tasks. **Partial.** | Native resource operations/grants, [Agent execution](../../internal/agent/helm.go) and the Spoke RBAC floor checked by [Helm verification](../../scripts/verify-helm-runtime.rb). [Trusted baseline](TRUSTED-TECHNOLOGY-BASELINE.md) assigns providers/cluster operators to deployments. | Inspect effective Kubernetes bindings, allowed chart kinds, admission policy, provider commands and operator grants. Additional tenant grants can exceed the rendered floor; #264 is not a privilege-ceiling attestation. At this source baseline, model CLI same-UID access remains #809 (TM-07/11). |
| **IA-2, Identification and Authentication (Organizational Users)** (`ia-2_smt`): identify users and associate acting processes. **Partial.** | [OIDC claim policy/tests](../../internal/identity/domain/oidc_test.go), [HTTP bearer/certificate resolver tests](../../internal/identity/adapters/httpidentity/resolver_test.go), [identity contract](../architecture/identity-and-governance.md). Configure `OPERATOR_OIDC_ISSUER`, audience/client and claim policy or verified operator certificates through the [Hub composition root](../../cmd/platform-delivery/main.go). | Prove issuer/audience/purpose/freshness refusal and intended user/delegated-service identity. Deployment owns account lifecycle, MFA, issuer availability, token custody and proxy behavior. No MFA enhancement or organizational identity program is asserted (TM-01/03/09). |
| **IA-3, Device Identification and Authentication** (`ia-3_smt`): authenticate selected devices/connections. **Partial** for Agent workload identity; device identity is **deployment-owned**. | [TLS configuration](../../internal/controlplane/server_tls.go), [rotation fixtures](../../internal/controlplane/server_tls_test.go), [Agent URI parser](../../internal/identity/domain/identity.go). Configure `AGENT_CLIENT_CA_FILE` with Hub TLS key/certificate or `SPIFFE_ENDPOINT_SOCKET`; Agent transport uses the matching deployment-issued identity. | Validate real issuer/trust roots, intended Environment/Agent SAN, wrong-scope/expired/untrusted certificates and rotation. A workload certificate is not hardware/device attestation. Enrollment, per-Agent revocation and expiry operations remain separate #847 work; test fixtures are not a deployment lifecycle rehearsal (TM-01/02). |

## Audit, transport and integrity

| Catalog statement / status | Repository evidence and enabling configuration | Deployer validation, responsibility and limits |
| --- | --- | --- |
| **AU-3, Content of Audit Records** (`au-3_smt.a`–`.f`): record event, time, location, source, outcome and associated identities. **Partial.** | [Audit event model/tests](../../internal/governance/domain/audit_test.go), [resource-decision audit tests](../../internal/governance/application/resource_decision_audit_test.go), [identity audit contract](../architecture/identity-and-governance.md#audit-contract). Use the production PostgreSQL audit adapter and configured audit sinks. | Sample allow, deny, approval, lease and optional ACP paths; establish event coverage, clock provenance and all required fields. The shared resource-decision mutation gate fails closed, while reads and separate direct predicates have documented limits. A policy ALLOW is not a completion receipt or proof every path emitted (TM-08/13/14). |
| **AU-9, Protection of Audit Information** (`au-9_smt.a`, `.b`): protect logs/tools and alert on unauthorized change. **Partial.** | [PostgreSQL append/guard tests](../../internal/governance/adapters/auditpostgres/store_test.go), [chain verification](../../internal/governance/adapters/auditpostgres/verify.go), [verified export tests](../../internal/governance/adapters/auditpostgres/export_test.go) and [checkpoint procedure](../operations/authorization-audit-integrity.md). | Run read-only verification against an independently trusted checkpoint, test tamper/truncation refusal, and route failures to named responders. Protect archive/receipt credentials separately from database administration. An uncheckpointed suffix can be rewritten by the database owner; alert delivery and retention capacity are deployment-owned. No destructive prune is established (#865; TM-08). |
| **SC-8, Transmission Confidentiality and Integrity** (`sc-8_smt`): protect the selected transmission properties. **Partial.** | [Agent TLS](../../internal/agent/transport.go), [Hub TLS](../../internal/controlplane/server_tls.go) and [HTTP model negative cases](../../internal/modelhttp/client_test.go). Secure Agent transport and default HTTPS model calls use TLS; model off-origin redirects are refused. See [model transport configuration](../operations/model-http.md). | Inspect every real hop: browser/ingress, Hub/Agent, database, registry, provider and tenant API. Protect proxy roots and keys. Explicit `CONDUIT_MODEL_ALLOW_INSECURE_HTTP=true` permits a weaker non-loopback hop; any mesh protection must be proven in that deployment. No blanket encryption claim covers external systems or local model/ACP processes (TM-01/12/13). |
| **SC-13, Cryptographic Protection** (`sc-13_smt.a`, `.b`): select cryptographic uses/types and implement them. **Not established** as a system/FIPS posture. | Maintained Go TLS/signature mechanisms and [Bundle envelope implementation](../../internal/artifacts/domain/envelope.go) exist. [Hub](../../Containerfile) and [Agent](../../Containerfile.agent) build ordinary pinned Go binaries without a selected `GOFIPS140` module or a FIPS gate. [Parent #846](https://github.com/jonathanagustin/conduit/issues/846) owns the decision. | Decide system boundary, uses, module/security-policy versions, supported operating environments and evidence for every shipped/external crypto component. Go's [FIPS guidance](https://go.dev/doc/security/fips140) distinguishes module selection from application compliance; its `fips140=only` assessment/debug mode is not a production recommendation. No algorithm replacement or flag change follows from this mapping. |
| **SI-7, Software, Firmware, and Information Integrity** (`si-7_smt.a`, `.b`): detect unauthorized changes and take selected actions. **Partial.** | [Envelope tamper/chunk tests](../../internal/artifacts/domain/envelope_test.go), [Bundle verification/admission tests](../../internal/controlplane/bundles_test.go), [release workflow policy](../../scripts/verify-release-workflow.rb) and [audit integrity procedure](../operations/authorization-audit-integrity.md). Configure trusted Bundle keys and digest/signature policy for each distribution path. | Check rejection of wrong bytes, signer and target under actual artifact policy and trusted release coordinates. Define incident response and repair/rollback actions; test backup and restore. Digest/signature verification does not authenticate caller-authored assessment provenance (#887), establish firmware integrity or protect a compromised verifier/host (TM-05/06/08). |
| **CA-2, Control Assessments** (`ca-2_smt.a`–`.f`): select assessors, approve scope, assess and report results. **Not established** for the commissioned independent review. | [Review commissioning brief](EXTERNAL-REVIEW-SCOPE.md) and historical [August audit limitations](../operations/EXTERNAL-AUDIT-2026-08-18.md). The brief's status is **NOT COMMISSIONED**; repository tests and this mapping are preparatory evidence. | The accountable owner selects an independent reviewer and authorizes access/scope, schedule, deliverables, disclosure handling and findings tracking under #846/#845. Do not fill assessor, authorization or passed-assessment fields from self-review. |

## Reuse by a deployment

For every selected contribution, a system owner should preserve this document's
source SHA, catalog SHA/hash and threat ID, then add the deployed artifact digest,
configuration/parameter values, local evidence, responsible owner and remaining
exceptions to its own control implementation record. Do not mark an entire
control inherited from this table. Issuers, trust roots, credentials, scanners,
cluster operators and independent audit custody remain deployment facts.

Refresh the mapping when the source baseline or selected catalog changes. A
future machine-readable OSCAL component definition should retain these limits,
reference the same pinned catalog and describe unsupported portions explicitly;
format conversion alone establishes no additional assurance.
