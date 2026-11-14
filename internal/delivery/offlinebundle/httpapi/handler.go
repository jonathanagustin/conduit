// Package httpapi serves offline bundle hand-off and install report-back.
package httpapi

import (
	"encoding/json"
	"net/http"
)

const Route = "POST /v1/offline-bundles"

type Config struct {
	Authorize func(*http.Request) error
	Write     func(*http.Request) (int, any, error)
}

func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(Route, func(w http.ResponseWriter, r *http.Request) {
		if cfg.Authorize != nil && cfg.Authorize(r) != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if cfg.Write == nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "offline bundles are unavailable"})
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

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
