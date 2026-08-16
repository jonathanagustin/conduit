// Package ports declares what the Apollo GraphQL interface needs in order to
// answer a query, and nothing more.
//
// The split follows provenance, because the two halves of the interface have
// genuinely different authorities behind them. ApolloProjection answers the
// coordinates an Apollo Hub established, from Conduit's Apollo compatibility
// projection. OverviewSource answers the coordinates Conduit invented, from
// Reporting's already-authorized fleet snapshot. Merging them into one port
// would hide which authority answered a field, which is the one thing this
// interface exists to keep visible.
//
// Both ports are read-only. The recovered surface establishes no mutation, and
// inventing one here would be an Apollo compatibility claim nothing supports.
package ports

import (
	"context"
	"errors"

	apollo "github.com/jonathanagustin/conduit/internal/apollo"
	reportingports "github.com/jonathanagustin/conduit/internal/reporting/ports"
)

// ErrRecordNotFound is the only error a projection may use to mean "the
// contract declares this coordinate and Conduit holds no record for it".
//
// The distinction is the whole point of this interface. A null answers "there
// is no such record"; anything else -- a store that is down, a timeout, a
// permission failure -- is a failure to answer, and reporting it as null would
// let a client record an absence Conduit never observed. That is the
// unknown-reads-as-none conflation the provenance model exists to prevent.
//
// A projection that returns a bare error gets its error propagated, which is
// the safe default: the interface refuses to answer rather than inventing an
// absence. Only this sentinel buys a null.
var ErrRecordNotFound = errors.New("apollo projection: no record")

// ApolloProjection is Conduit's Apollo compatibility projection, narrowed to
// the reads this interface makes. It is the same data the Apollo REST surface
// serves, so a client cannot get a different answer by choosing a protocol.
type ApolloProjection interface {
	ListEnvironments() apollo.ListEnvironmentsResponse
	GetEnvironment(rid string) (apollo.Environment, error)
	ListInstallations(environmentRID string) apollo.ListInstallationsResponse
	ListProducts() apollo.ListProductsResponse
	GetProduct(product string) (apollo.Product, error)
	ListReleases(product string) apollo.ListReleasesResponse
	GetRelease(product, version string) (apollo.Release, error)
}

// OverviewSource is Reporting's port, reused rather than restated.
//
// Aliasing it is deliberate. The Conduit-native coordinates -- plans, change
// requests, promotions, health -- are exactly the panels Reporting already
// answers for one authorized reader, and declaring a parallel interface here
// would invite a second, differently-authorized path to the same records.
type OverviewSource = reportingports.OverviewSource

// Sources is everything the resolver reads. A nil member is a refusal to answer
// that half of the interface rather than a panic: an interface that dies
// because one store is absent is worse than one that says which half is
// unavailable.
type Sources struct {
	Apollo   ApolloProjection
	Overview OverviewSource
}

// Reader is the caller a query is answered for. It exists so the resolver takes
// the request's context explicitly rather than reaching for an ambient one;
// authorization stays where Governance and Reporting already enforce it.
type Reader = context.Context
