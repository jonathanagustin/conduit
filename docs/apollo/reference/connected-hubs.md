# Connected publisher boundary

Reviewed 2026-08-02 from the official Foundry **Connected hubs** page and the
public Apollo authorization documentation.

## Proven contract

A Foundry enrollment can authenticate to an Apollo Hub with a confidential
third-party application using the client-credentials grant. A Marketplace
store can be whitelisted for more than one Hub, and a Hub can receive releases
from more than one store. Publishing requires a Maven coordinate and strict
folder tracking at the source. The Hub-side application user is assigned:

- Artifact Creator and Viewer; and
- Product Creator, Release Creator, and Viewer.

Cutting a release in an eligible whitelisted store publishes it to every Hub
configured for that store. One ineligible Product does not block the remaining
Products. Installing a connected Product into an attached Apollo Environment
causes the Product to appear in a remote Marketplace store after the source
feature has been enabled.

## Platform adaptation

The public Product publication adapter resolves an authenticated principal
before parsing publication input and then requires three independent
operations:

1. `artifact:create` on `global/artifact`;
2. `product:create` on `global/product` when the coordinate is new; and
3. `product-release:create` on the exact Product, inherited from
   `global/product` when granted globally.

The native `artifact:creator` role and the observed `apollo-artifact:creator`
alias grant the first operation. `product:creator` and
`product:release-creator` remain distinct, so a Release Creator may publish a
new version of an existing Product but cannot create a new coordinate. Product
reads require viewer authority on the global or exact Product resource. Every
decision uses the normal authorization audit path. Product Release coordinates
are immutable: replaying equivalent decoded publication content is idempotent,
while different content at the same Maven coordinate returns a conflict.

This admission check does not weaken the outbound-only Spoke invariant:
publication enters the Hub's catalog plane; only an Environment-scoped Spoke
leases an approved executable Plan.

## Deliberate external ownership

Platform does not persist the publisher's client secret, network ingress CIDR,
Marketplace store inventory, strict-folder-tracking setting, or source-side
whitelist. The case-sensitive Apollo Space ID is also externally provisioned
connection/routing input: the recovered enrollment-admin interface names a
space-creation operation but exposes an opaque body and response, which is not
enough evidence to reconstruct a Space lifecycle. A recovered
`ApolloAgentEntityInfo` object independently confirms that Space ID is a
required Agent-to-Foundry installation identity alongside Environment and
installation Entity IDs; it still does not reveal provisioning behavior.
Those values belong to the external identity/front-door and Product publisher.
Product catalogs, charts, and installation configuration remain in Product
repositories.

The Platform adapter does not claim the private Connected Hubs protocol or the
remote Marketplace import protocol. Its claim is limited to the pinned public
Product publication routes and the tested Hub-side authorization semantics.

## Sources

- <https://www.palantir.com/docs/foundry/administration/connected-hubs/>
- <https://www.palantir.com/docs/apollo/core/authorization/>
- [`../ROLE-INTERFACE-INVENTORY.md`](../ROLE-INTERFACE-INVENTORY.md)
