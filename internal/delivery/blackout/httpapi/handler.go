// Package httpapi serves blackout reads and mutations.
package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/blackout/application"
)

const (
	ReadRoute  = "GET /v1/blackouts"
	WriteRoute = "POST /v1/blackouts"
)

// Config is the route's dependencies.
type Config struct {
	Snapshot  func(environment string, at time.Time) application.Snapshot
	Authorize func(*http.Request) error
	Active    func(environment string, at time.Time) string
	Write     func(r *http.Request) (int, any, error)
}

// Register mounts the routes.
func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(ReadRoute, func(w http.ResponseWriter, r *http.Request) {
		if !auth(cfg, w, r) {
			return
		}
		at := time.Now().UTC()
		if raw := r.URL.Query().Get("at"); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at must be RFC3339"})
				return
			}
			at = parsed
		}
		environment := r.URL.Query().Get("environment")
		if raw := r.URL.Query().Get("includeIntervals"); raw != "" {
			if raw != "true" && raw != "false" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "includeIntervals must be true or false"})
				return
			}
			if raw == "true" {
				if cfg.Snapshot == nil {
					writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "blackout intervals are unavailable"})
					return
				}
				writeJSON(w, http.StatusOK, cfg.Snapshot(environment, at))
				return
			}
		}
		reason := ""
		if cfg.Active != nil {
			reason = cfg.Active(environment, at)
		}
		writeJSON(w, http.StatusOK, map[string]any{"environment": environment, "reason": reason, "blocked": reason != ""})
	})
	mux.HandleFunc(WriteRoute, func(w http.ResponseWriter, r *http.Request) {
		if !auth(cfg, w, r) {
			return
		}
		if cfg.Write == nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "blackouts are unavailable"})
			return
		}
		status, payload, err := cfg.Write(r)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, status, payload)
	})
}

func auth(cfg Config, w http.ResponseWriter, r *http.Request) bool {
	if cfg.Authorize != nil && cfg.Authorize(r) != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
