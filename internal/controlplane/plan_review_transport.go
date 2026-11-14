package controlplane

import (
	"mime"
	"net/http"

	review "github.com/jonathanagustin/conduit/internal/planreview/domain"
)

func decodePlanProposal(w http.ResponseWriter, request *http.Request) (DesiredState, string, string, *review.Review, bool) {
	mediaType, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if mediaType != review.MediaType {
		state, ok := decodeJSON[DesiredState](w, request)
		return state, request.Header.Get("X-Change-Title"), request.Header.Get("X-Change-Description"), nil, ok
	}
	body, ok := decodeJSON[struct {
		DesiredState DesiredState  `json:"desiredState"`
		Review       review.Review `json:"review"`
	}](w, request)
	if !ok {
		return DesiredState{}, "", "", nil, false
	}
	if body.Review.Validate() != nil || request.Header.Get("X-Change-Title") != "" || request.Header.Get("X-Change-Description") != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid or ambiguous Conduit-native Plan review context"})
		return DesiredState{}, "", "", nil, false
	}
	return body.DesiredState, body.Review.Title, body.Review.Rationale, &body.Review, true
}
