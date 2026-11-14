// Package httpapi serves native publisher-declared download metadata.
// CONDUIT-INVENTED: productassets.HTTPSurface
// Displaced-by: a published or owner-approved download-asset publication API establishing routes, payloads, authority and failure semantics.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/productassets/application"
	"github.com/jonathanagustin/conduit/internal/productassets/domain"
)

// BindService establishes the original request carrier and its narrow ports.
// Caller identity and publication provenance never come from the document.
type BindService func(*http.Request) (*application.Service, error)
type StatusError struct {
	Status  int
	Message string
}

func (e StatusError) Error() string { return e.Message }
func Register(mux *http.ServeMux, bind BindService) error {
	if mux == nil || bind == nil {
		return errors.New("download-asset routes require caller-bound services")
	}
	mux.HandleFunc("POST /v1/product-publications/download-assets", func(w http.ResponseWriter, r *http.Request) {
		service, err := bind(r)
		if err != nil {
			fail(w, err)
			return
		}
		var manifest domain.Manifest
		r.Body = http.MaxBytesReader(w, r.Body, domain.MaxDocumentBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&manifest); err != nil {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid download-asset declaration"})
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			write(w, http.StatusBadRequest, map[string]string{"error": "expected one download-asset declaration"})
			return
		}
		result, err := service.Publish(r.Context(), manifest)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, http.StatusCreated, result)
	})
	mux.HandleFunc("GET /v1/products/{productID}/releases/{version}/download-assets", func(w http.ResponseWriter, r *http.Request) {
		service, err := bind(r)
		if err != nil {
			fail(w, err)
			return
		}
		result, err := service.Get(r.Context(), r.PathValue("productID"), r.PathValue("version"))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /v1/products/{productID}/download-releases", func(w http.ResponseWriter, r *http.Request) {
		service, err := bind(r)
		if err != nil {
			fail(w, err)
			return
		}
		result, err := service.List(r.Context(), r.PathValue("productID"))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, http.StatusOK, map[string]any{"releases": result})
	})
	return nil
}
func fail(w http.ResponseWriter, err error) {
	status, message := http.StatusInternalServerError, "download-asset operation failed"
	var transport StatusError
	switch {
	case errors.As(err, &transport):
		status, message = transport.Status, transport.Message
	case errors.Is(err, domain.ErrInvalid):
		status, message = http.StatusBadRequest, "invalid download-asset declaration or coordinate"
	case errors.Is(err, domain.ErrForbidden):
		status, message = http.StatusForbidden, "download-asset operation forbidden"
	case errors.Is(err, domain.ErrNotFound):
		status, message = http.StatusNotFound, "download-asset release not found"
	case errors.Is(err, domain.ErrConflict):
		status, message = http.StatusConflict, "immutable download-asset release conflicts with an existing coordinate"
	}
	write(w, status, map[string]string{"error": message})
}
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
