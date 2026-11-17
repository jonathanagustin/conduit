// Package httpapi serves the propose-fix action.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/driftfix/application"
)

// Route opens a change request from one drifted row.
const Route = "POST /v1/fleet/propose-fix"

// Config is the route's dependencies.
type Config struct {
	Propose   func(r *http.Request, environment, installation string) (application.Result, error)
	Authorize func(*http.Request) error
}

// Register mounts the route.
func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(Route, func(w http.ResponseWriter, r *http.Request) {
		if cfg.Authorize != nil {
			if err := cfg.Authorize(r); err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		var body struct {
			Environment    string `json:"environment"`
			InstallationID string `json:"installationId"`
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body is not valid JSON"})
			return
		}
		if strings.TrimSpace(body.Environment) == "" || strings.TrimSpace(body.InstallationID) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "environment and installationId are required"})
			return
		}
		if cfg.Propose == nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "propose-fix is unavailable"})
			return
		}
		result, err := cfg.Propose(r, body.Environment, body.InstallationID)
		if errors.Is(err, controlplane.ErrForbidden) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden: change:propose is required on the environment"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "propose-fix is unavailable"})
			return
		}
		if !result.Allowed {
			status := http.StatusConflict
			if result.Reason == "installation was not found" {
				status = http.StatusNotFound
			}
			writeJSON(w, status, map[string]any{"allowed": false, "reason": result.Reason, "auditEventId": result.AuditID})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"allowed": true, "reason": result.Reason, "changeRequestId": result.ChangeRequestID,
			"changeRequestStatus": result.Status, "auditEventId": result.AuditID,
		})
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
