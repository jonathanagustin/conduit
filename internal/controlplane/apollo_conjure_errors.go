package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/apollo"
)

// Wiring only. The envelope shape, the pinned codes and what is grounded about
// each field live in internal/apollo, which owns Apollo vocabulary; what is
// here is which Conduit refusal becomes which published error.

func writeApolloConjureError(
	w http.ResponseWriter, status int, code, name, message string, parameters map[string]string,
) {
	writeJSON(w, status, apollo.ErrorEnvelope(code, name, message, parameters))
}

// apolloConjureError is the same envelope as an error, for the generated strict
// handler's return path, which carries a captured response rather than writing
// one directly.
func apolloConjureError(status int, code, name, message string, parameters map[string]string) error {
	return apolloCapturedError(status, apollo.ErrorEnvelope(code, name, message, parameters))
}

func apolloCapturedError(status int, body map[string]any) error {
	capture := newApolloResponseCapture()
	writeJSON(capture, status, body)
	return &apolloHTTPResponseError{
		status: capture.status, header: capture.header.Clone(), body: bytes.Clone(capture.body.Bytes()),
	}
}

// apolloAuthorizationRefusal converts a refusal the shared authorization
// helpers wrote into the published ApiUsageDenied envelope, preserving the
// status they chose and the message they wrote.
//
// It returns the pieces rather than a response because the refusal happens in
// two places that cannot share a return type: the identity middleware, which
// holds the real ResponseWriter, and the strict handler's adapter, which must
// hand back an error. Both must produce the same body -- converting only one
// was the shape of the original defect, where the middleware's 401 never
// reached the adapter's funnel at all.
func apolloAuthorizationRefusal(capture *apolloResponseCapture) (int, map[string]any) {
	message := "forbidden"
	status := http.StatusForbidden
	if capture.status != 0 {
		status = capture.status
		var written struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(capture.body.Bytes(), &written); err == nil && written.Error != "" {
			message = written.Error
		}
	}
	return status, apollo.ErrorEnvelope(
		apollo.ErrorCodePermissionDenied, apollo.ErrorNameAPIUsageDenied, message, nil)
}
