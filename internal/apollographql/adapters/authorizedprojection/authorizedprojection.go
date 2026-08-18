// Package authorizedprojection puts the Apollo REST surface's authorization
// rule in front of the Apollo projection the GraphQL interface reads.
//
// It exists because the resolver's contract says the ports it is handed are
// already authorized, and for the Apollo half that was not true. The binder
// authorized the request once, against platform/root, and then handed over the
// whole projection; the REST surface authorizes each read against its own
// coordinate. Two surfaces over one set of records disagreed about who may see
// them, in both directions -- see internal/apollographql/domain/authorization.go
// for the measured table.
//
// The fix is a decorator rather than a check inside the resolver. Putting it in
// the resolver would give the resolver an identity to filter on, which is the
// second thing its package comment says it deliberately does not do, and would
// put a drifting copy of Governance's rules in a read path. A decorator keeps
// the resolver ignorant and keeps the rule beside the port it guards.
//
// The decorator refuses; it does not filter. A filtered listing cannot be told
// from an empty one, and the reader would record an absence Conduit never
// observed. ports.ErrNotAuthorized propagates to the client as a field error,
// which is the same answer the REST surface's 403 gives, in the shape GraphQL
// has for it.
package authorizedprojection

import (
	apollo "github.com/jonathanagustin/conduit/internal/apollo"
	"github.com/jonathanagustin/conduit/internal/apollographql/domain"
	"github.com/jonathanagustin/conduit/internal/apollographql/ports"
)

// ReadsResource reports whether the reader this projection is bound for may
// perform the Apollo read operation on a Governance coordinate.
//
// It takes a resource identifier and nothing else, so this package never learns
// who is asking. Governance answers that question and this package only asks
// it, which is why no principal, team or grant type appears below.
type ReadsResource func(resourceRID string) bool

// New wraps a projection so every read is authorized against the coordinate
// domain.PlatformReadResource, CatalogueReadResource or ProductReadResource
// names for it -- the same coordinate the Apollo REST surface authorizes that
// read against.
//
// A nil inner projection stays nil rather than becoming a guard over nothing.
// The binder uses nil to mean "this half of the interface is unavailable", and
// wrapping it would convert an unavailability into a refusal, which says
// something different and is not this package's to say.
func New(inner ports.ApolloProjection, reads ReadsResource) ports.ApolloProjection {
	if inner == nil {
		return nil
	}
	return guarded{inner: inner, reads: reads}
}

// AnyReadable reports whether the reader may see any coordinate this projection
// serves at all.
//
// The binder uses it for the one decision that is about the request rather than
// about a field: whether to refuse the whole query with Governance's status, or
// to bind the projection and let each read answer for itself. A reader who may
// see nothing gets the former, because a document full of refusals is a worse
// answer to "you have no access here" than the refusal itself.
//
// It enumerates the catalogue when the two broad coordinates do not settle it,
// because a grant on one Product is a real grant and the Apollo REST surface
// honours it. Refusing such a reader at the door would reintroduce, in
// miniature, the asymmetry this package exists to remove. The enumeration is
// the same read the resolver would make for a products query and is skipped
// whenever a broad grant already answers.
//
// A nil projection is not a refusal. The binder uses nil for "this half is
// unavailable", and a reader holding a broad grant still reaches the other
// half; only the per-Product enumeration needs records, so only it stops.
func AnyReadable(inner ports.ApolloProjection, reads ReadsResource) bool {
	if reads == nil {
		return false
	}
	if reads(domain.PlatformReadResource()) || reads(domain.CatalogueReadResource()) {
		return true
	}
	if inner == nil {
		return false
	}
	listed, err := inner.ListProducts()
	if err != nil || listed.Products == nil {
		return false
	}
	for _, product := range *listed.Products {
		if reads(domain.ProductReadResource(string(product.Id))) {
			return true
		}
	}
	return false
}

type guarded struct {
	inner ports.ApolloProjection
	reads ReadsResource
}

// allows is the single place the authorization question is asked. A nil
// ReadsResource denies rather than permits: a guard constructed without one is
// a wiring mistake, and a wiring mistake that opens the projection is the
// failure this package was written to prevent.
func (projection guarded) allows(resourceRID string) bool {
	return projection.reads != nil && projection.reads(resourceRID)
}

func (projection guarded) ListEnvironments() (apollo.ListEnvironmentsResponse, error) {
	if !projection.allows(domain.PlatformReadResource()) {
		return apollo.ListEnvironmentsResponse{}, ports.ErrNotAuthorized
	}
	return projection.inner.ListEnvironments()
}

func (projection guarded) GetEnvironment(rid string) (apollo.Environment, error) {
	if !projection.allows(domain.PlatformReadResource()) {
		return apollo.Environment{}, ports.ErrNotAuthorized
	}
	return projection.inner.GetEnvironment(rid)
}

func (projection guarded) ListInstallations(environmentRID string) (apollo.ListInstallationsResponse, error) {
	if !projection.allows(domain.PlatformReadResource()) {
		return apollo.ListInstallationsResponse{}, ports.ErrNotAuthorized
	}
	return projection.inner.ListInstallations(environmentRID)
}

func (projection guarded) ListProducts() (apollo.ListProductsResponse, error) {
	if !projection.allows(domain.CatalogueReadResource()) {
		return apollo.ListProductsResponse{}, ports.ErrNotAuthorized
	}
	return projection.inner.ListProducts()
}

func (projection guarded) GetProduct(product string) (apollo.Product, error) {
	if !projection.allows(domain.ProductReadResource(product)) {
		return apollo.Product{}, ports.ErrNotAuthorized
	}
	return projection.inner.GetProduct(product)
}

func (projection guarded) ListReleases(product string) (apollo.ListReleasesResponse, error) {
	if !projection.allows(domain.ProductReadResource(product)) {
		return apollo.ListReleasesResponse{}, ports.ErrNotAuthorized
	}
	return projection.inner.ListReleases(product)
}

func (projection guarded) GetRelease(product, version string) (apollo.Release, error) {
	if !projection.allows(domain.ProductReadResource(product)) {
		return apollo.Release{}, ports.ErrNotAuthorized
	}
	return projection.inner.GetRelease(product, version)
}
