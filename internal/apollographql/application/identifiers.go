package application

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Apollo resource identifiers for the resources Conduit holds and Apollo has
// never published an identifier for.
//
// ADR 0007 left one design question open here, and this file answers it. The
// grammar is not in question: the pinned OpenAPI publishes
// ri.<service>.main.<type>.<locator> and four instantiations of it, three under
// ri.apollo and one under ri.autopilot. What Apollo has not published is a type
// token for a Product, a Product Release, or a Bundle.
//
// The question was whether to guess the token inside ri.apollo, or to take a
// namespace that cannot collide. The answer here is the second, and the reason
// is not squeamishness about guessing -- ADR 0007 permits guessing, provided the
// guess is disclosed and registered. It is that the two failure modes are not
// symmetric. A guessed token inside ri.apollo that turns out to be Apollo's real
// token for something else is a silent collision: two different objects, one
// identifier, and nothing anywhere raises a hand. A Conduit-namespaced
// identifier can only ever be wrong in the direction of being obviously ours.
//
// It is also the more literal reading of the published grammar. The service
// segment names the service that owns the resource, which is why Autopilot's
// teams are ri.autopilot rather than ri.apollo. A Product that exists only in
// Conduit is owned by Conduit, so ri.conduit is what the grammar says to write.
//
// Where Apollo HAS published the token -- environment, installation, package --
// Conduit follows ri.apollo, in internal/apollo/application. That is
// compatibility with a published example and is not revisited here; changing it
// would break a documented shape to signal something valueTier already carries.
//
// Every coordinate serving one of these discloses valueTier: CONDUIT_NATIVE and
// is registered in docs/evidence/conduit-inventions.json.
//
// CONDUIT-INVENTED: apollographql.ConduitNamespacedRID
// Displaced-by: a published statement giving Apollo's own type tokens for
// Product, Product Release or Bundle resource identifiers
const conduitRIDPrefix = "ri.conduit.main."

// productRID identifies a Product Conduit holds.
func productRID(productID string) string {
	return conduitRIDPrefix + "product." + ridLocator(productID)
}

// productReleaseRID identifies one Release.
//
// Keyed on the Maven coordinate rather than on product plus version, because the
// coordinate is what Apollo already treats as the release's identity -- it is
// group:artifact:version, so it names exactly one release on its own, and
// ApolloProductRelease.mavenCoordinate serves it today. Composing an identity
// out of two other fields would invent a second key for a resource that already
// has one.
func productReleaseRID(mavenCoordinate string) string {
	return conduitRIDPrefix + "product-release." + ridLocator(mavenCoordinate)
}

// ridLocator is the locator half, and is the same construction the Apollo
// projection uses for the published types: a truncated SHA-256, so the same
// resource yields the same identifier across restarts and replicas without
// anything being stored.
//
// Apollo's published examples use 123, so no format is established. The one
// Apollo locator whose format is documented -- a Plan's -- is a UUID, which this
// deliberately is not: a UUID would have to be persisted to stay stable, and a
// stored identifier is a migration and a failure mode where a pure function is
// neither.
func ridLocator(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:12])
}

// isConduitRID reports whether a string was minted here, so a lookup can refuse
// an Apollo-shaped identifier outright instead of scanning the catalogue for
// something that could not be in it.
func isConduitRID(value string) bool {
	return strings.HasPrefix(value, conduitRIDPrefix)
}
