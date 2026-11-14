// Package httpapi serves staged rollout reads and mutations.
package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/delivery/waves/domain"
)

const (
	ListRoute  = "GET /v1/rollouts"
	WriteRoute = "POST /v1/rollouts"
)

// Config is the route's dependencies.
type Config struct {
	Authorize func(*http.Request) error
	List      func() []domain.Rollout
	Write     func(r *http.Request, body Body) (int, any, error)
}

// Body is one rollout mutation.
type Body struct {
	Action          string        `json:"action"`
	ID              string        `json:"id"`
	Product         string        `json:"product"`
	Version         string        `json:"version"`
	Waves           []domain.Wave `json:"waves"`
	Confirm         bool          `json:"confirm"`
	ChangeRequestID string        `json:"changeRequestId"`
	Rationale       string        `json:"rationale"`
	WindowOpen      bool          `json:"windowOpen"`
	Approved        bool          `json:"approved"`
	Healthy         []string      `json:"healthy"`
}

// Register mounts the routes.
func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(ListRoute, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(cfg, w, r) {
			return
		}
		rows := []domain.Rollout{}
		if cfg.List != nil {
			rows = cfg.List()
		}
		writeJSON(w, http.StatusOK, map[string]any{"rollouts": rows})
	})
	mux.HandleFunc(WriteRoute, func(w http.ResponseWriter, r *http.Request) {
		if !authorized(cfg, w, r) {
			return
		}
		var body Body
		decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body is not valid JSON"})
			return
		}
		if cfg.Write == nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "rollouts are unavailable"})
			return
		}
		status, payload, err := cfg.Write(r, body)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, status, payload)
	})
}

func authorized(cfg Config, w http.ResponseWriter, r *http.Request) bool {
	if cfg.Authorize != nil {
		if err := cfg.Authorize(r); err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return false
		}
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
