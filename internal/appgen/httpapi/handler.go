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

// Generator runs one confirmed generation and reads stored results for one operator.
type Generator interface {
	Generate(ctx context.Context, input application.Input) (application.Result, error)
	Get(ctx context.Context, actor, id string) (application.Result, bool, error)
	List(ctx context.Context, actor string) ([]application.Result, error)
}

// Config is the route's dependencies.
type Config struct {
	Service   Generator
	Authorize func(*http.Request) error
	// Operator is the authenticated operator. It is resolved the same way a
	// planning session selects its owner, and it is not taken from the body.
	Operator func(*http.Request) (string, error)
}

// Register mounts the generation routes.
func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(CreateRoute, func(writer http.ResponseWriter, request *http.Request) {
		actor, ok := caller(cfg, writer, request)
		if !ok {
			return
		}
		var body struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Confirm     bool   `json:"confirm"`
			// AttachmentSHA256 binds stored intake files as read-only context.
			AttachmentSHA256 []string `json:"attachmentSha256"`
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
			Actor: actor, Name: body.Name, Description: body.Description, Confirm: body.Confirm,
			AttachmentSHA256: body.AttachmentSHA256,
		})
		if errors.Is(err, application.ErrBuildExecutorUnavailable) {
			// A deployment fact, not a failed upstream: this Hub has no
			// build executor, and a retry will get the same answer.
			writeError(writer, http.StatusServiceUnavailable, err.Error())
			return
		}
		if err != nil {
			writeError(writer, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	mux.HandleFunc(ListRoute, func(writer http.ResponseWriter, request *http.Request) {
		actor, ok := caller(cfg, writer, request)
		if !ok {
			return
		}
		if cfg.Service == nil {
			writeJSON(writer, http.StatusOK, map[string]any{"items": []application.Result{}})
			return
		}
		items, err := cfg.Service.List(request.Context(), actor)
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
		actor, ok := caller(cfg, writer, request)
		if !ok {
			return
		}
		if cfg.Service == nil {
			writeError(writer, http.StatusNotFound, "generation was not found")
			return
		}
		result, ok, err := cfg.Service.Get(request.Context(), actor, request.PathValue("id"))
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

func caller(cfg Config, writer http.ResponseWriter, request *http.Request) (string, bool) {
	if !authorized(cfg, writer, request) {
		return "", false
	}
	if cfg.Operator == nil {
		writeError(writer, http.StatusUnauthorized, "unauthorized")
		return "", false
	}
	name, err := cfg.Operator(request)
	if err != nil || strings.TrimSpace(name) == "" {
		writeError(writer, http.StatusUnauthorized, "unauthorized")
		return "", false
	}
	return strings.TrimSpace(name), true
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
