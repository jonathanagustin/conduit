// Package httpapi serves site connectivity.
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/delivery/checkin/application"
)

const Route = "POST /v1/sites/check-in"

// Config is the route's dependencies.
type Config struct {
	Authorize func(*http.Request) error
	Observe   func(r *http.Request, site, environment string, reportOld bool) (application.Status, error)
	Submit    func(site, change string) (application.Status, error)
}

// Register mounts the route.
func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(Route, func(w http.ResponseWriter, r *http.Request) {
		if cfg.Authorize != nil && cfg.Authorize(r) != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		var body struct {
			Site        string `json:"site"`
			Environment string `json:"environment"`
			ReportOld   bool   `json:"reportOld"`
			Change      string `json:"change"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body is not valid JSON"})
			return
		}
		if body.Change != "" {
			if cfg.Submit == nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": "check-in is unavailable"})
				return
			}
			status, err := cfg.Submit(body.Site, body.Change)
			if err != nil {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusAccepted, status)
			return
		}
		status, err := cfg.Observe(r, body.Site, body.Environment, body.ReportOld)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
