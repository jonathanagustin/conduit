// Package httpapi serves intake attachments to an authorized operator. An upload
// is one JSON object, not a multipart stream. Nothing here approves a change.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jonathanagustin/conduit/internal/attachments/domain"
)

const (
	CreateRoute = "POST /v1/intake-attachments"
	ListRoute   = "GET /v1/intake-attachments"
	GetRoute    = "GET /v1/intake-attachments/{digest}"
)

// maxBody bounds the JSON envelope. Escaping can make a file larger than its bytes.
const maxBody = 4*domain.MaxFileBytes + 4096

// Attachments stores and reads one operator's attachments.
type Attachments interface {
	Add(ctx context.Context, actor, name string, content []byte) (domain.Attachment, error)
	Get(ctx context.Context, actor, digest string) (domain.Stored, error)
	List(ctx context.Context, actor string) ([]domain.Attachment, error)
}

// Config is the route's dependencies. A nil Authorize or Operator refuses everyone.
type Config struct {
	Service   Attachments
	Authorize func(*http.Request) error
	// Operator is the authenticated operator. It is resolved the same way a
	// planning session selects its owner, and it is not taken from the body.
	Operator func(*http.Request) (string, error)
}

// Register mounts the attachment routes.
func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(CreateRoute, func(writer http.ResponseWriter, request *http.Request) {
		actor, ok := caller(cfg, writer, request)
		if !ok {
			return
		}
		if cfg.Service == nil {
			writeError(writer, http.StatusUnprocessableEntity, "attachments are not configured")
			return
		}
		var body struct {
			Filename string `json:"filename"`
			Content  string `json:"content"`
		}
		request.Body = http.MaxBytesReader(writer, request.Body, maxBody)
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				writeError(writer, http.StatusRequestEntityTooLarge, "upload is larger than the limit")
				return
			}
			writeError(writer, http.StatusBadRequest, "request body must be a JSON object with filename and content")
			return
		}
		item, err := cfg.Service.Add(request.Context(), actor, body.Filename, []byte(body.Content))
		if err != nil {
			var rejection domain.Rejection
			if errors.As(err, &rejection) {
				status := http.StatusUnprocessableEntity
				if rejection.Large {
					status = http.StatusRequestEntityTooLarge
				}
				writeError(writer, status, rejection.Reason)
				return
			}
			writeError(writer, http.StatusBadGateway, "attachment could not be stored")
			return
		}
		writeJSON(writer, http.StatusCreated, item)
	})
	mux.HandleFunc(ListRoute, func(writer http.ResponseWriter, request *http.Request) {
		actor, ok := caller(cfg, writer, request)
		if !ok {
			return
		}
		items := []domain.Attachment{}
		if cfg.Service != nil {
			rows, err := cfg.Service.List(request.Context(), actor)
			if err != nil {
				writeError(writer, http.StatusBadGateway, "attachments are unavailable")
				return
			}
			if rows != nil {
				items = rows
			}
		}
		writeJSON(writer, http.StatusOK, map[string]any{
			"items": items,
			"limits": map[string]int{
				"maxFileBytes": domain.MaxFileBytes, "maxSpecBytes": domain.MaxSpecBytes,
				"maxTotalBytes": domain.MaxTotalBytes, "maxCount": domain.MaxCount,
			},
		})
	})
	mux.HandleFunc(GetRoute, func(writer http.ResponseWriter, request *http.Request) {
		actor, ok := caller(cfg, writer, request)
		if !ok {
			return
		}
		if cfg.Service == nil {
			writeError(writer, http.StatusNotFound, "attachment was not found")
			return
		}
		item, err := cfg.Service.Get(request.Context(), actor, request.PathValue("digest"))
		if errors.Is(err, domain.ErrNotFound) {
			writeError(writer, http.StatusNotFound, "attachment was not found")
			return
		}
		if err != nil {
			writeError(writer, http.StatusBadGateway, "attachment is unavailable")
			return
		}
		writeJSON(writer, http.StatusOK, item)
	})
}

func authorized(cfg Config, writer http.ResponseWriter, request *http.Request) bool {
	if cfg.Authorize == nil || cfg.Authorize(request) != nil {
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
	writer.Header().Set("x-content-type-options", "nosniff")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}
