package controlplane

import "net/http"

// SpaceIndexMembership is private grouping data supplied by trusted composition
// after the selected Space is readable. It grants no access to any listed ID.
// In particular, a readable Plan or change request need not confer read access
// to its whole Environment, so index filtering must not reauthorize its anchor.
type SpaceIndexMembership struct {
	EnvironmentIDs []string
	ProductIDs     []string
	BundleIDs      []string
}

type indexSpace struct {
	id           string
	environments map[string]struct{}
	products     map[string]struct{}
	bundles      map[string]struct{}
}

func selectedIndexSpace(w http.ResponseWriter, r *http.Request, principal Principal, options HandlerOptions) (indexSpace, bool) {
	id, ok := selectedSpace(w, r, principal, options)
	if !ok || id == "" {
		return indexSpace{}, ok
	}
	if options.LoadSpaceIndexMembership == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "native Space index selection is not configured"})
		return indexSpace{}, false
	}
	membership, err := options.LoadSpaceIndexMembership(r.Context(), principal, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "native Space membership could not be read"})
		return indexSpace{}, false
	}
	return indexSpace{id: id, environments: stringSet(membership.EnvironmentIDs), products: stringSet(membership.ProductIDs), bundles: stringSet(membership.BundleIDs)}, true
}

func (space indexSpace) environment(id string) bool {
	_, found := space.environments[id]
	return space.id == "" || found
}
func (space indexSpace) product(id string) bool {
	_, found := space.products[id]
	return space.id == "" || found
}
func (space indexSpace) bundle(id string) bool {
	_, found := space.bundles[id]
	return space.id == "" || found
}

// Keep every existing unscoped cursor byte-compatible. A selected Space is an
// additional filter, never an authorization assertion or a frozen snapshot.
func fingerprintInSpace(filters any, spaceID string) string {
	if spaceID == "" {
		return fingerprintJSON(filters)
	}
	return fingerprintJSON(struct {
		Filters any    `json:"filters"`
		SpaceID string `json:"spaceId,omitempty"`
	}{filters, spaceID})
}

func requireIndexSpaceMatch(w http.ResponseWriter, match bool) bool {
	if !match {
		writeLifecycleError(w, ErrNotFound)
	}
	return match
}

// ResolveSpaceEnvironmentFilter validates optional Space selection and returns
// an Environment grouping predicate. Callers must still authorize every resource;
// membership alone grants no read authority. Errors are written to the response.
func ResolveSpaceEnvironmentFilter(w http.ResponseWriter, r *http.Request, principal Principal, options HandlerOptions) (func(string) bool, bool) {
	space, ok := selectedIndexSpace(w, r, principal, options)
	if !ok {
		return nil, false
	}
	return space.environment, true
}
