# Apollo Agent installation boundary

Reviewed 2026-08-02 UTC. This note separates three similarly named public
surfaces so implementation work does not infer a private protocol from an API
type or from a scanner agent.

## Pinned public source

- `palantir/osdk-ts` commit
  [`fd00f48`](https://github.com/palantir/osdk-ts/commit/fd00f48adf2cf8cfc916d9ecb942bd3795ef7931).
- Generated `marketplace/apolloagent/api/__components.ts`: Git blob
  `83a9be6c38b51816160b89b01cdec4bbe43a1b96`, content SHA-256
  `07fb8dc2c629522c9356f2b345aea7dfe7fe8c82c9e0f7e3efbc82755f635968`.
- Generated `marketplace/installmanager/api/__components.ts`: content SHA-256
  `828c0773f398a3a0946103502efd0077f0ec631dfb683b188952674180ee80ab`.
- `palantir/tenablesc-client` `agentscan.go` at commit `35e868a`: content
  SHA-256 `30bf867ab49ca1cbdf6e58aa67e7920780c778536de4cf94955767b1963dcdc0`.
- Apollo Agents public page: source Markdown snapshot SHA-256
  `a5dacac4cbec3b2abc40080844a733c86424415f4db0f501c8eaee4ed80a3a9f`.

The generated files are Apache-2.0 licensed upstream. This repository keeps
the compact contract and hashes rather than mirroring generated bodies.

## Proven Agent-facing contract

The public Apollo Agents page defines the deployment topology independently of
the generated Foundry installation types:

- an Agent runs inside a managed Spoke, obtains Plans from the Hub, executes
  them, and reports Plan status plus Reported State;
- it must pull Helm charts and images from the Environment's declared artifact
  repositories;
- all Hub and repository traffic is encrypted and outbound from the
  Environment, preserving an operator-controlled network allowlist; and
- `HTTPS_PROXY` and `NO_PROXY` are the documented proxy controls.

Apollo registration generates a cryptographic key unique to an Environment and
uses a Hub-signed certificate to identify Agent traffic and prevent
impersonation. This proves an Environment-bound authenticated transport
requirement, but it does not publish the certificate profile, enrollment
exchange, routes, request bodies, lease protocol, or response encoding.

The public `apolloagent` namespace exports types, not a Hub polling service.
Its managed configuration adds `secretInputMapping` to the Install Manager
configuration and defines exactly three tagged secret-backed input variants:

- `edgePipelineMagritteSource`: source config, Apollo secret name, optional
  task config;
- `markings`: Apollo secret name plus `secretKeyName`;
- `namedCredential`: Apollo multi-key secret name. Upstream says the secret
  work operator creates a backing named credential and supplies its RID during
  resolution.

It also publishes Universal allow/deny lists and the seven exact namespace
ignore reasons implemented in `internal/controlplane/installation.go`.
Templates may occur recursively in all string values and resolve against the
target namespace enrollment.

Install Manager carries `ApolloAgentEntityInfo`: Apollo Environment ID, Apollo
Space ID, Foundry-space Maven group, Apollo Entity ID, and the secret input
mapping. Its public generated methods cover create/update/delete, lookup/list
by Apollo Entity ID, adoption of an existing installation, managed namespace
listing, and release-management settings. This is the Agent-to-Foundry
installation adapter surface. It is not evidence of the Hub-to-Agent lease,
heartbeat, plan, or report wire protocol.

## Local consequence

- Managed and Universal configuration use the published tagged union and exact
  ignore reason strings. The markings key is serialized as `secretKeyName`.
- Every non-templated secret input must reference an Environment secret; a
  markings key must be exposed by that assignment. Values remain external.
- Secret lifecycle mutation runs once per assignment/namespace before workload
  reconciliation. Helm no longer invokes a duplicate per-installation secret
  preparation path; the Worker rejects uncovered secret access.
- Kubernetes workload convergence remains the Flux/Helm adapter boundary.
  Foundry BlockSet installation is a distinct external executor integration;
  it must not be silently treated as Kubernetes reconciliation.
- The Worker already uses outbound-only HTTPS, standard Go proxy discovery,
  immutable repository pulls, Plan status, and Reported State. Its preferred
  production identity uses an Environment-attested SPIFFE X509-SVID rather than
  Apollo's undocumented Hub-signed certificate profile. This is semantic and
  security alignment, not PKI or wire compatibility.

## Distribution and Maven boundary

Maven Central contains no `com.palantir.apollo` group and no public Apollo
Hub/Agent executable. A private Factory dependency-graph capture does prove
published API-object libraries such as `com.palantir.apollo:*` and
`com.palantir.apollo.commons:*` inside an authenticated Palantir artifact
proxy. Those libraries, and Apollo's Maven-style Product coordinates, do not
prove that Hub or Agent executables are Maven-distributed. Public Apollo CLI
documentation continues to describe the CLI as a Hub download.

## Local corroboration (not portable public protocol evidence)

- Factory E2E recipes SHA-256
  `49a718c931bbfeac006034a5f4a7c3156aad3d03b67f9e6c62c2a28788add98b`
  expose Multipass role families such as `apollo-hubs`, `apollo-module`,
  `apollo-environments`, vulnerability scanning, and SBOM administration.
- The generated Factory `Roles2Service` SHA-256
  `6ba1f924a621bcbe6659f2609f37e1c87c83d49bb593148aa93633fe5ad4abbd`
  calls Multipass `/roles/v2/*`; Apollo names are role response data, not Hub
  service routes.
- A locally recovered Conjure Java object named `ApolloAgentEntityInfo`
  (SHA-256
  `678df02fef59e9e1966e24409d1b70ab6dd22dae62bd1ab9c72641ecf27de852`)
  independently carries the same five required fields as the pinned public
  OSDK type: Environment ID, Space ID, Foundry-space Maven group, installation
  Entity ID, and secret-input mapping. Its builder defaults only the mapping to
  empty and rejects every missing field. This corroborates the Install Manager
  boundary; it does not expose Hub polling or authorize Platform to reproduce a
  private service.
- The complete direct Java closure contains 42 Apollo-referencing files. Its
  field-by-field disposition—including the generic 107-variant BlockSet input
  catalog—is recorded in
  [`../JAVA-OBJECT-ALIGNMENT.md`](../JAVA-OBJECT-ALIGNMENT.md). No Platform-owned
  field gap was found; the unmatched objects are Foundry/Marketplace or private
  Agent enrollment boundaries.
- The private Maven graph SHA-256
  `c0b26c84bc64d406e1828f56880c6079f2014a9af556fb0d8bc45313f002840f`
  establishes internal API-library coordinates only.

The private Hub/Agent transport and native CLI bytes therefore remain an
explicit evidence gap, not an implementation claim.
