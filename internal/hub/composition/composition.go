// Package composition is the Hub composition root.
//
// It is deliberately thin: concrete adapters are assembled here, while domain
// decisions remain in their owning bounded contexts. During the incremental
// migration it delegates to the legacy controlplane facade so routes and
// persistence remain byte-compatible.
//
// New Hub surfaces are mounted here rather than added to internal/controlplane,
// which is on a ratcheted line ceiling. A surface assembled here composes over
// the facade: the facade keeps "/", and a bounded context's own adapter
// registers the exact patterns it owns.
package composition

import (
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// NewHubHandler assembles the Hub HTTP surface from explicitly supplied ports.
// The controlplane dependency is a temporary compatibility boundary, not a
// license for new domain behavior to enter the legacy package.
//
// The Hub options are variadic so that every existing caller keeps compiling
// and keeps getting exactly the surface it had. Supplying none is not a
// degraded mode; it is a deployment with nothing extra to serve.
func NewHubHandler(
	repository controlplane.Repository, options controlplane.HandlerOptions, hub ...Options,
) http.Handler {
	handler, err := NewHub(repository, options, hub...)
	if err != nil {
		// A Hub that came up serving a surface it could not assemble would be
		// discovered by an operator reading 500s, so this refuses to start for
		// the same reason the facade panics on an unmountable GraphQL schema.
		panic("assemble the Hub HTTP surface: " + err.Error())
	}
	return handler
}

// NewHub is NewHubHandler with the failure returned rather than raised, for
// callers that would rather decide themselves.
//
// The assembled surface is wrapped in guardUnregisteredRoutes, so that the Hub
// serves exactly the routes HubRouteAuthorization names a decision for and
// nothing else. That wrap is the outermost thing here on purpose: a route added
// anywhere beneath it -- the facade, the generated Apollo transport, a surface
// mounted in this package -- reaches a caller only once somebody has said what
// it requires.
func NewHub(
	repository controlplane.Repository, options controlplane.HandlerOptions, hub ...Options,
) (http.Handler, error) {
	hubOptions := Options{}
	if len(hub) > 0 {
		hubOptions = hub[0]
	}
	// Share the same configured signing identity with native review handoffs.
	if len(options.BundlePrivateKey) == 0 {
		key, err := controlplane.GenerateBundleSigningKey()
		if err != nil {
			return nil, err
		}
		options.BundlePrivateKey = key
	}
	options = withSpaceLookup(repository, options, hubOptions.Spaces)
	handler, err := mountNotifications(controlplane.NewHandler(repository, options), repository, options, hubOptions)
	if err != nil {
		return nil, err
	}
	handler, err = mountSpaces(handler, repository, options, hubOptions.Spaces)
	if err != nil {
		return nil, err
	}
	handler, err = mountProductAssets(handler, repository, options, hubOptions.ProductAssets)
	if err != nil {
		return nil, err
	}
	handler, err = mountChangeActivity(handler, repository, options, hubOptions.ChangeDiscussion)
	if err != nil {
		return nil, err
	}
	handler, err = mountModuleOverrides(handler, repository, options)
	if err != nil {
		return nil, err
	}
	return guardUnregisteredRoutes(mountOntology(mountPackaging(handler, repository, options, hubOptions), repository, options)), nil
}
