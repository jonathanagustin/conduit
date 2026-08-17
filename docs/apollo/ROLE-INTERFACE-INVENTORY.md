# Recovered role-interface inventory

## Evidence grain

On 2026-08-02, Platform inspected the generated `Roles2Service/index.ts` in the
provided `@factory/foundry-system` package. Its SHA-256 is
`6ba1f924a621bcbe6659f2609f37e1c87c83d49bb593148aa93633fe5ad4abbd`.
The file says it was generated from runtime-derived system IR, a runtime
inventory, design-system reconstruction, and API-surface evidence.

This is **corroborating reconstruction evidence**, not an authenticated Apollo
response or authored server source. It proves that the recovered client
publishes the following interface and symbolic vocabulary. It does not prove
request behavior, authorization implications, or private protocol
compatibility; many bodies and response members are explicitly `unknown`.

The package contains 626 generated Conjure service interfaces. A mechanical
scan for `Apollo`, `apollo-`, or `ri.apollo` finds only `Roles2Service`,
`EnrollmentManagementService`, and `MonitoringServiceV2`. The latter two use
Apollo only in enrollment-space administration and monitoring receiver names.
There is no recovered Hub-to-Spoke control service in this interface corpus.

The supplied `EnrollmentManagementService` copy has SHA-256
`bb8f574daabaa8f1f64f91ae82e09130eacd7c7fcabe6a7326a3aef510dd53ed`
and 34 operations. Only two are Apollo-specific:
`createApolloSpaceForOrganization` and `restoreApolloPermissions`.

**Displaced 2026-08-17.** This paragraph read: "both accept an opaque
`ConjureBean` and return `unknown`... It does not reveal the Space record,
identifier derivation, role grant, or Hub API." The first half is no longer
true. A bytecode-exact projection of
`com.palantir.controlpanel:control-panel-api-objects:1.1588.0` carries both
operations in full, and a third, with their request and response shapes. They
are recorded in
[`../evidence/apollo-control-panel-seam.json`](../evidence/apollo-control-panel-seam.json)
and gated by `scripts/verify-apollo-control-panel-seam.mjs`.

What the shapes add: `CreateApolloSpaceRequest` carries **only**
`admins: set<PrincipalIdWithType>`, and `CreateApolloSpaceForOrganizationResponse`
returns a `spaceRid` the platform mints. So a caller states who administers a
Space and never states its identifier.

The second half of the old sentence still holds, and is the part that matters.
This reveals no Hub API, and it does not establish what an Apollo Space id is
for a resource Conduit holds -- which is the condition ADR 0005 records for
reopening the Space refusal, and it remains unmet. `spaceRid` is a Foundry-side
identifier; `ApolloSpaceId` is an opaque string in the pinned interop IR, linked
to Foundry by Maven group. Nothing here shows they are the same identifier, and
treating them as one would be the invented correspondence that record forbids.

The conclusion is therefore unchanged and better founded: the Connected Hubs
Space ID remains an externally provisioned, case-sensitive connection input
rather than a Platform-owned resource. It was previously held on the absence of
evidence; it is now held on evidence.

The supplied `MonitoringServiceV2` copy has SHA-256
`31545add36d3251d0cb316fc17453f0c170680597e07e9cd96fce595f473209f`
and 24 operations. Its only Apollo occurrence is the route
`/monitoring/v2/monitoring-view/apollo-event-receivers`, returning
`Array<unknown>`. Platform keeps notification delivery external; this route is
not evidence for a Product-delivery controller.

## Roles2Service operations

The complete recovered service interface has 16 operations:

1. `getAllRoles`
2. `getAllRolesIncludingDeleted`
3. `getRolesConfiguration`
4. `createRole`
5. `editRolesDeprecated`
6. `editRoles`
7. `previewActivityLogForEditsDeprecated`
8. `previewActivityLogForEdits`
9. `getMyRolesByResource`
10. `deleteRoleSet`
11. `editRoleSet`
12. `undeleteRoleSet`
13. `createRoleSet`
14. `getAllRoleSets`
15. `getRoleSets`
16. `getRoleSetsForRoles`

The structure supports several design conclusions already present in Platform:
role definitions and assignments are separate resources; edits have a preview
surface; deleted definitions remain discoverable; effective roles can be read
by resource; and role sets have their own lifecycle. Platform's native
resource grants, immutable audit events, and authorization checks preserve
those useful separations, but do not reproduce these private endpoints.

## Apollo-shaped role families

The generated return types contain 84 unique Apollo/product/release/security
role IDs across these families:

- artifact and artifact-registry administration;
- build factory;
- Bundle and imported-Bundle administration;
- CEL policies;
- egress policy;
- Environments and terminal access;
- Hub, transfer-target, and mesh administration;
- labels, Modules, namespaces, notifications, Pipelines, Teams, terminal
  profiles, and webhooks;
- Products and Release Channels; and
- vulnerability scans, SLA settings, and suppressions.

The exact artifact IDs include `apollo-artifact:creator` and
`apollo-artifact:viewer`, corroborating the official Connected Hubs permission
requirements. Platform admits those two aliases at its public publication
boundary while retaining provider-neutral native operation names.

This inventory also contains roles with no safe kernel implementation, such as
terminal-user, webhook, notification, build-factory, mesh-manager, and Hub
administration. They remain integration boundaries. A role-shaped string alone
is not sufficient evidence to create a controller, credential path, or private
API adapter.

### Complete recovered RoleContext taxonomy

The supplied bytecode inventory identifies
`com.palantir.gatekeeper.api.roles2.RoleContext` in
`multipass-api-objects` as an enum-shaped class with 43 values. Twenty-one are
Apollo-specific:

`APOLLO_ARTIFACTS`, `APOLLO_ARTIFACT_REGISTRIES`, `APOLLO_BUILD_FACTORY`,
`APOLLO_BUNDLES`, `APOLLO_CEL_POLICY`, `APOLLO_EGRESS_POLICY`,
`APOLLO_ENTITIES`, `APOLLO_ENVIRONMENTS`, `APOLLO_HUBS`,
`APOLLO_IMPORTED_BUNDLE`, `APOLLO_LABEL`, `APOLLO_MESH_MANAGER`,
`APOLLO_MODULE`, `APOLLO_TERMINAL_PROFILE`, `APOLLO_NAMESPACES`,
`APOLLO_NOTIFICATIONS`, `APOLLO_PERMISSIONS`, `APOLLO_PIPELINES`,
`APOLLO_TEAM`, `APOLLO_TRANSFER_TARGETS`, and `APOLLO_WEBHOOK`.

The other 22 contexts cover Branch, Enrollment, ontology, inbound/network
policy, Marketplace installation, Namespace, OAuth clients, Organization,
Product, Project, Release Channel, SBOM, Subdomain, Tables, Telemetry, unified
management, Use Case, and vulnerability surfaces.

This complete context list closes a discovery question but does not create 43
kernel capabilities. Platform has native, tested seams for the delivery-domain
contexts: artifacts/registries, Bundles/import, CEL approval, Entities,
Environments, labels, Modules, namespaces, permissions/Teams, Pipelines,
transfer evidence, Products, Release Channels, SBOM/provenance, and
vulnerability evidence. Build Factory, egress-policy administration,
mesh-manager, terminal access, notifications, webhook delivery, Foundry
enrollment/Marketplace, OAuth administration, and generic network policy remain
external integrations. Hub administration is implemented only through
Platform's own topology contract, never the private Multipass role API.

The class record is currently conversation-supplied and lacks a local
hash-bearing artifact receipt, so it is corroborating evidence. Its values are
consistent with the hash-pinned generated Roles2 response vocabulary above.

## Maven cross-check

The adjacent
`foundry-maven-dependency-graph-2026-07-17.json` is byte-identical to the
previously reviewed inventory (`SHA-256
c0b26c84bc64d406e1828f56880c6079f2014a9af556fb0d8bc45313f002840f`).
It contains 756 flattened coordinates from 70 fetched POMs and 15 names
containing `apollo`: 14 Palantir Apollo artifacts plus Alta Apollo telemetry.
The Apollo families cover artifact registry, entity/Maven identities, auth and
scoped tokens, bundling, catalog, deployment state, and labels.

That cross-check raises confidence that the vocabulary families were not
invented by the TypeScript generator. It does not raise confidence about
behavior: the flattened JSON retains no dependency edges or per-coordinate
artifact receipt, the later authenticated trace found the Apollo values only
under dependency management, and the exact Apollo POM/JAR probes returned 404.
See [`APOLLO-MAVEN-INVENTORY-EVIDENCE.md`](APOLLO-MAVEN-INVENTORY-EVIDENCE.md).
