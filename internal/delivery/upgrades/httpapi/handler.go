package httpapi

import (
	"encoding/json"
	"net/http"
)

const (
	DependencyRoute = "GET /v1/products/{product}/dependencies/{version}"
	UpgradeRoute    = "GET /v1/fleet/upgrades"
)

type Config struct {
	Authorize    func(*http.Request) error
	Dependencies func(r *http.Request, product, version string) (any, error)
	Upgrades     func(r *http.Request) (any, error)
}

func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(DependencyRoute, func(w http.ResponseWriter, r *http.Request) {
		if !auth(cfg, w, r) {
			return
		}
		body, err := cfg.Dependencies(r, r.PathValue("product"), r.PathValue("version"))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"dependencies": body})
	})
	mux.HandleFunc(UpgradeRoute, func(w http.ResponseWriter, r *http.Request) {
		if !auth(cfg, w, r) {
			return
		}
		body, err := cfg.Upgrades(r)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"upgrades": body})
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
