package controlplane

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
)

// These errors preserve native input/configuration outcomes for typed adapters.
var (
	ErrInvalidSpaceSelection     = errors.New("spaceId must name one native Space identity")
	ErrSpaceSelectionUnavailable = errors.New("native Space selection is not configured")
)

func authorizeIndexSpaceSelection(ctx context.Context, principal Principal, options HandlerOptions, id string) error {
	if id == "" {
		return nil
	}
	if !nativeSpaceSelectionID.MatchString(id) {
		return ErrInvalidSpaceSelection
	}
	if options.RequireSpaceSelection == nil || options.SpaceIDLookup == nil {
		return ErrSpaceSelectionUnavailable
	}
	return options.RequireSpaceSelection(ctx, principal, id)
}

// CONDUIT-INVENTED: spaces.SelectionPublication
// Displaced-by: a published or sanitized Apollo Space selection and publication contract establishing transport, caller access, grouping reservation and failure behavior.
var nativeSpaceSelectionID = regexp.MustCompile(`^space_[a-f0-9]{32}$`)

// Space selection narrows existing resource authority. The trusted composition
// callback must establish a readable persisted Space, even for an empty page.
func selectedSpace(w http.ResponseWriter, r *http.Request, principal Principal, options HandlerOptions) (string, bool) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid query encoding"})
		return "", false
	}
	values, present := query["spaceId"]
	if !present {
		return "", true
	}
	if len(values) != 1 || !nativeSpaceSelectionID.MatchString(values[0]) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "spaceId must name one native Space identity"})
		return "", false
	}
	if err := authorizeIndexSpaceSelection(r.Context(), principal, options, values[0]); err != nil {
		if errors.Is(err, ErrSpaceSelectionUnavailable) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return "", false
		}
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
