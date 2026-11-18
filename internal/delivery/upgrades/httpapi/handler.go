// Package httpapi serves release dependency and upgrade views.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
)

// StatusError carries only a composition-owned public refusal.
type StatusError struct {
	Status  int
	Message string
}

func (err StatusError) Error() string { return err.Message }

func writeReadError(w http.ResponseWriter, err error) {
	status, message := http.StatusBadGateway, "upgrade evidence is unavailable"
	var public StatusError
	if errors.As(err, &public) && public.Status >= 400 && public.Status <= 599 {
		status, message = public.Status, public.Message
	}
	writeJSON(w, status, map[string]string{"error": message})
}

const (
	DependencyRoute = "GET /v1/products/{product}/dependencies/{version}"
	UpgradeRoute    = "GET /v1/fleet/upgrades"
)

type Config struct {
	Authorize func(*http.Request) error
	// Select narrows Environment grouping after authentication; it grants no authority.
	Select       func(http.ResponseWriter, *http.Request) (func(string) bool, bool)
	Dependencies func(r *http.Request, product, version string) (any, error)
	Upgrades     func(r *http.Request, selected func(string) bool) (any, error)
}

func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(DependencyRoute, func(w http.ResponseWriter, r *http.Request) {
		if !auth(cfg, w, r) {
			return
		}
		body, err := cfg.Dependencies(r, r.PathValue("product"), r.PathValue("version"))
		if err != nil {
			writeReadError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"dependencies": body})
	})
	mux.HandleFunc(UpgradeRoute, func(w http.ResponseWriter, r *http.Request) {
		if !auth(cfg, w, r) {
			return
		}
		var selected func(string) bool
		if cfg.Select != nil {
			var ok bool
			selected, ok = cfg.Select(w, r)
			if !ok {
				return
			}
		}
		body, err := cfg.Upgrades(r, selected)
		if err != nil {
			writeReadError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"upgrades": body})
	})
}

func auth(cfg Config, w http.ResponseWriter, r *http.Request) bool {
	if cfg.Authorize == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return false
	}
	if err := cfg.Authorize(r); err != nil {
		var public StatusError
		if errors.As(err, &public) {
			writeReadError(w, err)
		} else {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
