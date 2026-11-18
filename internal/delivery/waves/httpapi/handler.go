// Package httpapi serves staged rollout reads and mutations.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/delivery/waves/domain"
)

// StatusError carries a composition-owned public refusal.
type StatusError struct {
	Status  int
	Message string
}

func (err StatusError) Error() string { return err.Message }

const (
	ListRoute  = "GET /v1/rollouts"
	WriteRoute = "POST /v1/rollouts"
)

// Config is the route's dependencies.
type Config struct {
	Authorize func(*http.Request) error
	List      func(*http.Request) ([]domain.Rollout, error)
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
			var err error
			rows, err = cfg.List(r)
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": "rollout evidence is unavailable"})
				return
			}
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
	if cfg.Authorize == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return false
	}
	if cfg.Authorize != nil {
		if err := cfg.Authorize(r); err != nil {
			status, message := http.StatusUnauthorized, "unauthorized"
			var public StatusError
			if errors.As(err, &public) && public.Status >= 400 && public.Status <= 599 {
				status, message = public.Status, public.Message
			}
			writeJSON(w, status, map[string]string{"error": message})
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
