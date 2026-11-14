// Package httpapi exposes native reviewed Module metadata admission.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/installoverride/ports"
)

type StatusError struct {
	Status  int
	Message string
}

func (e StatusError) Error() string { return e.Message }

type Config struct {
	Bind func(*http.Request) (ports.ModuleController, error)
}

func Register(mux *http.ServeMux, config Config) error {
	if mux == nil || config.Bind == nil {
		return errors.New("Module override HTTP requires a caller-bound controller")
	}
	mux.HandleFunc("POST /v1/module-override-proposals", func(w http.ResponseWriter, r *http.Request) {
		controller, err := config.Bind(r)
		if err != nil {
			failure(w, err)
			return
		}
		if r.URL.RawQuery != "" {
			failure(w, StatusError{400, "query parameters are not supported"})
			return
		}
		var body struct {
			Draft  ports.ModuleDraft `json:"draft"`
			DryRun bool              `json:"dryRun"`
		}
		if !decode(w, r, &body) {
			return
		}
		result, err := controller.ProposeModuleOverride(r.Context(), body.Draft, body.DryRun)
		if err != nil {
			failure(w, err)
			return
		}
		status := http.StatusCreated
		if body.DryRun {
			status = http.StatusOK
		}
		write(w, status, result)
	})
	mux.HandleFunc("POST /v1/module-override-proposals/{requestID}/apply", func(w http.ResponseWriter, r *http.Request) {
		controller, err := config.Bind(r)
		if err != nil {
			failure(w, err)
			return
		}
		if r.URL.RawQuery != "" {
			failure(w, StatusError{400, "query parameters are not supported"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1)
		data, err := io.ReadAll(r.Body)
		if err != nil || len(data) != 0 {
			failure(w, StatusError{400, "apply requires an empty body"})
			return
		}
		result, err := controller.ApplyModuleOverride(r.Context(), r.PathValue("requestID"))
		if err != nil {
			failure(w, err)
			return
		}
		write(w, http.StatusOK, result)
	})
	return nil
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			failure(w, StatusError{413, "request exceeds payload limit"})
		} else {
			failure(w, StatusError{400, "invalid Module override request"})
		}
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		failure(w, StatusError{400, "expected one request document"})
		return false
	}
	return true
}

func failure(w http.ResponseWriter, err error) {
	status, message := 500, "Module override operation unavailable"
	var typed StatusError
	if errors.As(err, &typed) {
		status, message = typed.Status, typed.Message
	}
	write(w, status, map[string]string{"error": message})
}
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
