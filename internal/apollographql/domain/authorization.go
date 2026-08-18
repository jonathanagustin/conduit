package domain

// This file states which Governance coordinate each Apollo read is authorized
// against, once, so the GraphQL interface and the Apollo REST surface cannot
// drift apart.
//
// They had drifted. The GraphQL binder barred every query on platform/root
// while the REST surface authorizes per read -- platform/root for the
// Environment and namespace coordinates, global/product for the catalogue,
// product/<id> for one Product and its Releases. Because a grant on
// platform/root expands to itself alone and conveys no Product read, the two
// surfaces disagreed in both directions at once: a platform-scoped principal
// was refused Products over REST and served the whole catalogue, Release
// manifests included, over GraphQL, while a product-scoped principal was served
// over REST and refused over GraphQL. internal/apollographql/ports says a
// client cannot get a different answer by choosing a protocol; that is the
// sentence this file exists to keep true.
//
// The rule is stated here, in the domain, rather than at either transport. A
// copy at the GraphQL binder would be the second copy, and the second copy is
// how the first drift happened. Nothing here imports anything, so both surfaces
// and their tests can read the same answer.
//
// The three functions are one per shape the REST surface authorizes, not one
// per projection method, so adding a projection method forces a deliberate
// choice of shape rather than letting it inherit whatever the last one used.

// PlatformReadResource is the coordinate the Apollo reads that span the whole
// installation authorize against: Environments, installations and namespaces.
//
// Governance does not expand it -- a grant on platform/root reaches
// platform/root and nothing else -- which is exactly why barring the GraphQL
// interface on it alone could not stand in for the Product coordinates.
func PlatformReadResource() string { return "platform/root" }

// CatalogueReadResource is the coordinate the Product listing authorizes
// against, matching the Apollo REST ListProducts read. Governance expands
// product/<id> to include it, so a grant here reaches every Product, while a
// grant on one Product does not reach the listing.
func CatalogueReadResource() string { return "global/product" }

// ProductReadResource is the coordinate one Product and its Releases authorize
// against, matching the Apollo REST GetProduct, ListReleases and GetRelease
// reads. The identifier is the Maven group:name the Apollo surface calls a
// productId.
func ProductReadResource(productID string) string { return "product/" + productID }
