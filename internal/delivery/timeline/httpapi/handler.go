// Package httpapi serves the per-environment timeline.
package httpapi

import (
	"encoding/json"
	"net/http"
)

const Route = "GET /v1/environments/{environmentID}/timeline"

type Config struct {
	Authorize func(*http.Request) error
	Build     func(r *http.Request, environment string) (any, error)
}

func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(Route, func(w http.ResponseWriter, r *http.Request) {
		if cfg.Authorize != nil && cfg.Authorize(r) != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		body, err := cfg.Build(r, r.PathValue("environmentID"))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"timeline": body})
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
