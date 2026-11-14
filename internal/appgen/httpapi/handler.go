// Package httpapi serves app generation. A confirmed request runs the gates
// and does not approve a change.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jonathanagustin/conduit/internal/appgen/application"
)

const (
	CreateRoute = "POST /v1/app-generations"
	ListRoute   = "GET /v1/app-generations"
	GetRoute    = "GET /v1/app-generations/{id}"
)

// Generator runs one confirmed generation and reads stored results.
type Generator interface {
	Generate(ctx context.Context, input application.Input) (application.Result, error)
	Get(ctx context.Context, id string) (application.Result, bool, error)
	List(ctx context.Context) ([]application.Result, error)
}

// Config is the route's dependencies.
type Config struct {
	Service   Generator
	Authorize func(*http.Request) error
}

// Register mounts the generation routes.
func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(CreateRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		var body struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Confirm     bool   `json:"confirm"`
		}
		if err := json.NewDecoder(io.LimitReader(request.Body, 1<<16)).Decode(&body); err != nil {
			writeError(writer, http.StatusBadRequest, "request body must be a JSON object")
			return
		}
		if cfg.Service == nil {
			writeError(writer, http.StatusUnprocessableEntity, "app generation is not configured")
			return
		}
		result, err := cfg.Service.Generate(request.Context(), application.Input{
			Name: body.Name, Description: body.Description, Confirm: body.Confirm,
		})
		if err != nil {
			writeError(writer, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	mux.HandleFunc(ListRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		if cfg.Service == nil {
			writeJSON(writer, http.StatusOK, map[string]any{"items": []application.Result{}})
			return
		}
		items, err := cfg.Service.List(request.Context())
		if err != nil {
			writeError(writer, http.StatusBadGateway, err.Error())
			return
		}
		if items == nil {
			items = []application.Result{}
		}
		writeJSON(writer, http.StatusOK, map[string]any{"items": items})
	})
	mux.HandleFunc(GetRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		if cfg.Service == nil {
			writeError(writer, http.StatusNotFound, "generation was not found")
			return
		}
		result, ok, err := cfg.Service.Get(request.Context(), request.PathValue("id"))
		if err != nil {
			writeError(writer, http.StatusBadGateway, err.Error())
			return
		}
		if !ok {
			writeError(writer, http.StatusNotFound, "generation was not found")
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
}

func authorized(cfg Config, writer http.ResponseWriter, request *http.Request) bool {
	if cfg.Authorize == nil {
		writeError(writer, http.StatusUnauthorized, "unauthorized")
		return false
	}
	if err := cfg.Authorize(request); err != nil {
		writeError(writer, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": strings.TrimSpace(message)})
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("content-type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}

// ErrUnauthorized is returned by an authorize function that rejected the caller.
var ErrUnauthorized = errors.New("unauthorized")
