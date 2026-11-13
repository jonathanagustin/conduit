package controlplane

import (
	"net/http"
	"regexp"
)

// CONDUIT-INVENTED: spaces.SelectionPublication
// Displaced-by: a published or sanitized Apollo Space selection and publication contract establishing transport, caller access, grouping reservation and failure behavior.
var nativeSpaceSelectionID = regexp.MustCompile(`^space_[a-f0-9]{32}$`)

// Space selection narrows existing resource authority. The trusted composition
// callback must establish a readable persisted Space, even for an empty page.
func selectedSpace(w http.ResponseWriter, r *http.Request, principal Principal, options HandlerOptions) (string, bool) {
	values, present := r.URL.Query()["spaceId"]
	if !present {
		return "", true
	}
	if len(values) != 1 || !nativeSpaceSelectionID.MatchString(values[0]) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "spaceId must name one native Space identity"})
		return "", false
	}
	if options.RequireSpaceSelection == nil || options.SpaceIDLookup == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "native Space selection is not configured"})
		return "", false
	}
	if err := options.RequireSpaceSelection(r.Context(), principal, values[0]); err != nil {
		writeLifecycleError(w, err)
		return "", false
	}
	return values[0], true
}
func reservePublicationSpace(w http.ResponseWriter, r *http.Request, principal Principal, options HandlerOptions, product string) bool {
	id, ok := selectedSpace(w, r, principal, options)
	if !ok {
		return false
	}
	if id == "" {
		return true
	}
	if options.ReserveProductSpace == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "native Space publication is not configured"})
		return false
	}
	if err := options.ReserveProductSpace(r.Context(), principal, id, product); err != nil {
		writeLifecycleError(w, err)
		return false
	}
	return true
}
