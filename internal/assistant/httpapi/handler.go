// Package httpapi serves assistant tool calls. It does not apply an action.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jonathanagustin/conduit/internal/assistant/application"
	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

const (
	ToolRoute = "POST /v1/assistant/tools"
	TurnRoute = "POST /v1/assistant/turns"
)

// Tools runs one tool or one model turn.
type Tools interface {
	Invoke(ctx context.Context, actor application.Actor, mode, name string, arguments json.RawMessage) (application.Result, error)
	Turn(ctx context.Context, actor application.Actor, mode, message string, chips []domain.Chip) (application.Turn, error)
}

// Config is the route's dependencies.
type Config struct {
	Tools     Tools
	Authorize func(*http.Request) error
	Caller    func(*http.Request) (application.Actor, error)
}

// Register mounts the assistant routes.
func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(ToolRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		var body struct {
			Mode      string          `json:"mode"`
			Tool      string          `json:"tool"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := decode(request, &body); err != nil {
			writeError(writer, http.StatusBadRequest, "request body must be a JSON object")
			return
		}
		caller, ok := caller(cfg, writer, request)
		if !ok {
			return
		}
		result, err := cfg.Tools.Invoke(request.Context(), caller, body.Mode, body.Tool, body.Arguments)
		if err != nil {
			status := http.StatusBadRequest
			if !errors.Is(err, domain.ErrToolInvalid) {
				status = http.StatusBadGateway
			}
			writeError(writer, status, err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	mux.HandleFunc(TurnRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		var body struct {
			Mode    string        `json:"mode"`
			Message string        `json:"message"`
			Context []domain.Chip `json:"context"`
		}
		if err := decode(request, &body); err != nil {
			writeError(writer, http.StatusBadRequest, "request body must be a JSON object")
			return
		}
		if strings.TrimSpace(body.Message) == "" {
			writeError(writer, http.StatusBadRequest, "message is required")
			return
		}
		caller, ok := caller(cfg, writer, request)
		if !ok {
			return
		}
		turn, err := cfg.Tools.Turn(request.Context(), caller, body.Mode, body.Message, body.Context)
		if errors.Is(err, domain.ErrModelUnavailable) {
			writeJSON(writer, http.StatusUnprocessableEntity, map[string]string{
				"error": err.Error(), "label": domain.LabelNoModel,
			})
			return
		}
		if err != nil {
			writeError(writer, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, turn)
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

func caller(cfg Config, writer http.ResponseWriter, request *http.Request) (application.Actor, bool) {
	if cfg.Caller == nil || cfg.Tools == nil {
		writeError(writer, http.StatusBadGateway, "assistant tools are not configured")
		return application.Actor{}, false
	}
	value, err := cfg.Caller(request)
	if err != nil || strings.TrimSpace(value.Username) == "" {
		writeError(writer, http.StatusUnauthorized, "unauthorized")
		return application.Actor{}, false
	}
	return value, true
}

func decode(request *http.Request, dest any) error {
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dest)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}
