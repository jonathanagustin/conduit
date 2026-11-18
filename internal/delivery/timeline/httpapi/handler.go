// Package httpapi serves the per-environment timeline.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
)

const Route = "GET /v1/environments/{environmentID}/timeline"

// StatusError is a composition-owned public authorization refusal.
type StatusError struct {
	Status  int
	Message string
}

func (err StatusError) Error() string { return err.Message }

type Config struct {
	Authorize func(*http.Request) error
	Build     func(r *http.Request, environment string) (any, error)
}

func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(Route, func(w http.ResponseWriter, r *http.Request) {
		if cfg.Authorize == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if err := cfg.Authorize(r); err != nil {
			status, message := http.StatusUnauthorized, "unauthorized"
			var public StatusError
			if errors.As(err, &public) && public.Status >= 400 && public.Status <= 599 {
				status, message = public.Status, public.Message
			}
			writeJSON(w, status, map[string]string{"error": message})
			return
		}
		body, err := cfg.Build(r, r.PathValue("environmentID"))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "timeline evidence is unavailable"})
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
