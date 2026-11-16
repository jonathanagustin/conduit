// Package httpapi serves the native release rollout read contract.
package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/jonathanagustin/conduit/internal/delivery/releasestatus/domain"
	"net/http"
	"regexp"
)

const Route = "GET /v1/products/{product}/releases/{version}/rollout"

var coordinate = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:+-]{0,199}$`)

type Config struct {
	Authorize func(*http.Request) int
	Read      func(*http.Request, string, string) (domain.View, error)
}

func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(Route, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		failure := func(status int, message string) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
		}
		if cfg.Authorize == nil {
			failure(http.StatusUnauthorized, "unauthorized")
			return
		}
		if status := cfg.Authorize(r); status != 0 {
			if status == http.StatusForbidden {
				failure(status, "forbidden")
			} else {
				failure(http.StatusUnauthorized, "unauthorized")
			}
			return
		}
		product, version := r.PathValue("product"), r.PathValue("version")
		if !coordinate.MatchString(product) || !coordinate.MatchString(version) || r.URL.RawQuery != "" || r.ContentLength != 0 || len(r.TransferEncoding) > 0 {
			failure(http.StatusBadRequest, "invalid release rollout read")
			return
		}
		if cfg.Read == nil {
			failure(http.StatusServiceUnavailable, "release rollout read unavailable")
			return
		}
		view, err := cfg.Read(r, product, version)
		if errors.Is(err, domain.ErrNotFound) {
			failure(http.StatusNotFound, "release not found")
			return
		}
		if err != nil {
			failure(http.StatusServiceUnavailable, "release rollout read unavailable")
			return
		}
		_ = json.NewEncoder(w).Encode(view)
	})
}
