// Package httpapi serves Conduit-native Space identity and grouping operations.
// Published Apollo transports are not extended by these routes.
// CONDUIT-INVENTED: spaces.HTTPSurface
// Displaced-by: a published or sanitized Apollo Space management API establishing routes, body shapes, visibility and assignment authority.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/spaces/application"
	"github.com/jonathanagustin/conduit/internal/spaces/domain"
)

// BindService resolves the request's actual credential and binds its authority.
// Neither creation subject nor permissions may come from a JSON request body.
type BindService func(*http.Request) (*application.Service, error)
type StatusError struct {
	Status  int
	Message string
}

func (err StatusError) Error() string { return err.Message }

func Register(mux *http.ServeMux, bind BindService) error {
	if mux == nil || bind == nil {
		return errors.New("Space routes require a mux and caller-bound service")
	}
	mux.HandleFunc("POST /v1/spaces", func(w http.ResponseWriter, r *http.Request) {
		service, err := bind(r)
		if err != nil {
			fail(w, err)
			return
		}
		var body struct {
			Name        string `json:"name"`
			Description string `json:"description,omitempty"`
		}
		if !decode(w, r, &body) {
			return
		}
		value, err := service.Create(r.Context(), body.Name, body.Description)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, http.StatusCreated, value)
	})
	mux.HandleFunc("GET /v1/spaces", func(w http.ResponseWriter, r *http.Request) {
		service, err := bind(r)
		if err != nil {
			fail(w, err)
			return
		}
		values, err := service.List(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		write(w, http.StatusOK, map[string]any{"spaces": values})
	})
	mux.HandleFunc("GET /v1/spaces/{spaceID}", func(w http.ResponseWriter, r *http.Request) {
		service, err := bind(r)
		if err != nil {
			fail(w, err)
			return
		}
		value, err := service.Get(r.Context(), r.PathValue("spaceID"))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, http.StatusOK, value)
	})
	mux.HandleFunc("PUT /v1/spaces/{spaceID}/bindings", func(w http.ResponseWriter, r *http.Request) {
		service, err := bind(r)
		if err != nil {
			fail(w, err)
			return
		}
		var resource domain.Resource
		if !decode(w, r, &resource) {
			return
		}
		value, err := service.Bind(r.Context(), r.PathValue("spaceID"), resource)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, http.StatusOK, value)
	})
	mux.HandleFunc("GET /v1/spaces/{spaceID}/bindings", func(w http.ResponseWriter, r *http.Request) {
		service, err := bind(r)
		if err != nil {
			fail(w, err)
			return
		}
		values, err := service.ListBindings(r.Context(), r.PathValue("spaceID"))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, http.StatusOK, map[string]any{"bindings": values})
	})
	return nil
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid Space request body"})
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		write(w, http.StatusBadRequest, map[string]string{"error": "expected one Space request document"})
		return false
	}
	return true
}
func fail(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "Space operation failed"
	var transport StatusError
	switch {
	case errors.As(err, &transport):
		status = transport.Status
		message = transport.Message
	case errors.Is(err, domain.ErrInvalid):
		status = http.StatusBadRequest
		message = err.Error()
	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
		message = "Space or assignment not found"
	case errors.Is(err, domain.ErrConflict):
		status = http.StatusConflict
		message = "Space identity or assignment conflicts with an existing record"
	case errors.Is(err, domain.ErrForbidden):
		status = http.StatusForbidden
		message = "Space operation forbidden"
	}
	write(w, status, map[string]string{"error": message})
}
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
