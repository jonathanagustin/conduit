// Package httpapi serves the channel subscription view.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/delivery/channels/domain"
)

// Route is the read.
const Route = "GET /v1/channel-subscriptions"

// Board reads the view the caller may see.
type Board interface {
	Board(context.Context, func(environment string) bool) (domain.View, error)
}

// Config is the route's dependencies.
type Config struct {
	Board     Board
	Authorize func(*http.Request) error
	Allow     func(*http.Request, string) bool
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
		if cfg.Board == nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "channel subscriptions are unavailable"})
			return
		}
		view, err := cfg.Board.Board(r.Context(), func(environment string) bool {
			if cfg.Allow == nil {
				return true
			}
			return cfg.Allow(r, environment)
		})
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "channel subscriptions are unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, view)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
