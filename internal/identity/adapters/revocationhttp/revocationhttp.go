// Package revocationhttp is the operator surface over the Agent deny-list:
// list, add and remove revocations for one Environment, and read the last
// certificate each of its Agents presented.
//
// It decides nothing about who may call it. Authorize is supplied by the Hub
// composition root, which asks Governance; this adapter only refuses to run
// without one. Whether a credential is refused is the domain's rule and its
// enforcement on Agent routes is the Hub's -- neither is here.
package revocationhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/identity/ports"
)

// StatusError is a refusal Authorize wants answered with a specific status.
type StatusError struct {
	Status  int
	Message string
}

func (err StatusError) Error() string { return err.Message }

// Audit actions, as they appear in the authorization audit trail.
const (
	ActionAdd    = "AGENT_REVOCATION_ADD"
	ActionRemove = "AGENT_REVOCATION_REMOVE"
)

type Config struct {
	Store ports.AgentCredentialStore
	Now   func() time.Time
	// Authorize establishes the operator and whether they may read, or when
	// manage is true change, the Environment's Agent credentials. It returns
	// the acting username. Any error refuses the request; a StatusError
	// chooses the status and anything else is a 403.
	Authorize func(request *http.Request, environment string, manage bool) (string, error)
	// Audit records the content of a change after it has landed. before is
	// nil for a first revocation and after is nil for a removal.
	Audit func(ctx context.Context, actor, action string, before, after *domain.AgentRevocation) error
}

// CredentialView is the per-Agent expiry the metrics endpoint must not carry:
// a gauge labelled by Agent would publish the fleet's topology to whoever can
// scrape. Here it is behind the Environment's read authority.
type CredentialView struct {
	domain.AgentCredentialSighting
	SecondsUntilExpiry int64 `json:"secondsUntilExpiry"`
	// RevokedBy names the revocation that would refuse this certificate if it
	// were presented now. Empty means none would.
	RevokedBy string `json:"revokedBy,omitempty"`
}

const unaudited = "the change could not be written to the authorization audit trail"

// CONDUIT-INVENTED: identity.AgentRevocationHTTPSurface
// Displaced-by: a published or sanitized Apollo agent credential management API establishing revocation routes, body shapes and authority.
func Register(mux *http.ServeMux, config Config) error {
	if mux == nil || config.Store == nil || config.Now == nil || config.Authorize == nil || config.Audit == nil {
		return errors.New("Agent revocation routes require a store, a clock, an authorizer and an audit sink")
	}
	store, now := config.Store, config.Now
	authorize := func(writer http.ResponseWriter, request *http.Request, manage bool) (string, string, bool) {
		environment := request.PathValue("environmentID")
		if !domain.DNSLabelPattern.MatchString(environment) {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "environment must be a DNS label"})
			return "", "", false
		}
		actor, err := config.Authorize(request, environment, manage)
		if err != nil {
			status, message := http.StatusForbidden, "forbidden"
			var refusal StatusError
			if errors.As(err, &refusal) {
				status, message = refusal.Status, refusal.Message
			}
			writeJSON(writer, status, map[string]string{"error": message})
			return "", "", false
		}
		return actor, environment, true
	}
	unavailable := func(writer http.ResponseWriter) {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "Agent revocation state is unavailable"})
	}
	mux.HandleFunc("GET /v1/environments/{environmentID}/agent-revocations", func(writer http.ResponseWriter, request *http.Request) {
		_, environment, ok := authorize(writer, request, false)
		if !ok {
			return
		}
		revocations, err := store.Revocations(request.Context(), environment)
		if err != nil {
			unavailable(writer)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"revocations": revocations})
	})
	mux.HandleFunc("POST /v1/environments/{environmentID}/agent-revocations", func(writer http.ResponseWriter, request *http.Request) {
		actor, environment, ok := authorize(writer, request, true)
		if !ok {
			return
		}
		var draft domain.AgentRevocation
		decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<16))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&draft); err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "request body is not a valid Agent revocation"})
			return
		}
		if draft.Environment != "" && draft.Environment != environment {
			writeJSON(writer, http.StatusUnprocessableEntity, map[string]string{"error": "environment must match the path"})
			return
		}
		draft.Environment = environment
		revocation, err := domain.NewAgentRevocation(draft, actor, now())
		if err != nil {
			writeJSON(writer, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		previous, err := store.PutRevocation(request.Context(), revocation)
		switch {
		case errors.Is(err, domain.ErrRevocationLimit):
			writeJSON(writer, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		case err != nil:
			unavailable(writer)
			return
		}
		// Recorded after the write, so the trail cannot claim a revocation that
		// did not land. A revocation that landed and could not be recorded is
		// left in force -- the safe direction -- and reported as a failure, so
		// the operator retries and the retry, being the same record, files it.
		if err := config.Audit(request.Context(), actor, ActionAdd, previous, &revocation); err != nil {
			writeJSON(writer, http.StatusInternalServerError,
				map[string]string{"error": unaudited + "; the revocation is in force, repeat the request to record it"})
			return
		}
		writeJSON(writer, http.StatusOK, revocation)
	})
	mux.HandleFunc("DELETE /v1/environments/{environmentID}/agent-revocations/{revocationID}", func(writer http.ResponseWriter, request *http.Request) {
		actor, environment, ok := authorize(writer, request, true)
		if !ok {
			return
		}
		removed, err := store.DeleteRevocation(request.Context(), environment, request.PathValue("revocationID"))
		switch {
		case errors.Is(err, domain.ErrRevocationNotFound):
			writeJSON(writer, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		case err != nil:
			unavailable(writer)
			return
		}
		// Lifting a revocation readmits a credential, so an unrecorded removal
		// is the unsafe direction. It is put back rather than left standing.
		if err := config.Audit(request.Context(), actor, ActionRemove, &removed, nil); err != nil {
			message := unaudited + "; the revocation was restored"
			if _, restoreErr := store.PutRevocation(request.Context(), removed); restoreErr != nil {
				message = unaudited + " and the revocation could not be restored; it is no longer in force"
			}
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": message})
			return
		}
		writeJSON(writer, http.StatusOK, removed)
	})
	mux.HandleFunc("GET /v1/environments/{environmentID}/agent-credentials", func(writer http.ResponseWriter, request *http.Request) {
		_, environment, ok := authorize(writer, request, false)
		if !ok {
			return
		}
		sightings, err := store.Sightings(request.Context(), environment)
		if err != nil {
			unavailable(writer)
			return
		}
		revocations, err := store.Revocations(request.Context(), environment)
		if err != nil {
			unavailable(writer)
			return
		}
		instant := now()
		credentials := make([]CredentialView, 0, len(sightings))
		for _, sighting := range sightings {
			view := CredentialView{
				AgentCredentialSighting: sighting,
				SecondsUntilExpiry:      int64(sighting.NotAfter.Sub(instant) / time.Second),
			}
			if revocation, revoked := domain.RefusingRevocation(revocations, domain.AgentCredential{
				Identity: domain.AgentIdentity{Environment: sighting.Environment, AgentID: sighting.AgentID},
				Serial:   sighting.Serial, NotBefore: sighting.NotBefore, NotAfter: sighting.NotAfter,
			}); revoked {
				view.RevokedBy = revocation.ID
			}
			credentials = append(credentials, view)
		}
		writeJSON(writer, http.StatusOK, map[string]any{"agentCredentials": credentials})
	})
	return nil
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}
