// Package httpapi exposes native reviewed temporary maintenance admission.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/maintenance"
)

type AgentController interface {
	Lease(context.Context, time.Duration) (any, error)
	Start(context.Context, string, string) (any, error)
}
type StatusError struct {
	Status  int
	Message string
}

func (e StatusError) Error() string { return e.Message }

type Config struct {
	BindOperator func(*http.Request) (maintenance.OverrideController, error)
	BindAgent    func(*http.Request) (AgentController, error)
}

func Register(mux *http.ServeMux, config Config) error {
	if mux == nil || config.BindOperator == nil || config.BindAgent == nil {
		return errors.New("temporary maintenance HTTP requires bound operator and Agent controllers")
	}
	mux.HandleFunc("POST /v1/temporary-maintenance-overrides", func(w http.ResponseWriter, r *http.Request) {
		controller, err := config.BindOperator(r)
		if err != nil {
			failure(w, err)
			return
		}
		var draft maintenance.OverrideDraft
		if !decode(w, r, &draft) {
			return
		}
		if _, err := maintenance.ValidateDraft(draft); err != nil {
			failure(w, StatusError{400, "invalid temporary maintenance proposal"})
			return
		}
		result, err := controller.CreateMaintenanceOverrides(r.Context(), draft)
		if err != nil {
			failure(w, err)
			return
		}
		status := http.StatusCreated
		if draft.DryRun {
			status = http.StatusOK
		}
		if result.Failure != "" {
			status = http.StatusMultiStatus
		}
		write(w, status, result)
	})
	mux.HandleFunc("POST /v1/environments/{environmentID}/maintenance-admission/leases", func(w http.ResponseWriter, r *http.Request) {
		controller, err := config.BindAgent(r)
		if err != nil {
			failure(w, err)
			return
		}
		var body struct {
			DurationSeconds int `json:"durationSeconds"`
		}
		if !decode(w, r, &body) {
			return
		}
		if body.DurationSeconds == 0 {
			body.DurationSeconds = 60
		}
		result, err := controller.Lease(r.Context(), time.Duration(body.DurationSeconds)*time.Second)
		if err != nil {
			failure(w, err)
			return
		}
		write(w, 200, result)
	})
	mux.HandleFunc("POST /v1/plans/{planID}/maintenance-admission/start", func(w http.ResponseWriter, r *http.Request) {
		controller, err := config.BindAgent(r)
		if err != nil {
			failure(w, err)
			return
		}
		if r.URL.RawQuery != "" {
			failure(w, StatusError{400, "query parameters are unsupported"})
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
		if err != nil || len(data) != 0 {
			failure(w, StatusError{400, "start requires an empty body"})
			return
		}
		result, err := controller.Start(r.Context(), r.PathValue("planID"), r.Header.Get("X-Platform-Lease-Token"))
		if err != nil {
			failure(w, err)
			return
		}
		write(w, 200, result)
	})
	return nil
}
func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	if r.URL.RawQuery != "" {
		failure(w, StatusError{400, "query parameters are unsupported"})
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			failure(w, StatusError{413, "request exceeds payload limit"})
		} else {
			failure(w, StatusError{400, "invalid temporary maintenance request"})
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
	status, message := 500, "temporary maintenance operation unavailable"
	var typed StatusError
	if errors.As(err, &typed) {
		status, message = typed.Status, typed.Message
	}
	if status == 204 {
		w.WriteHeader(status)
		return
	}
	write(w, status, map[string]string{"error": message})
}
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
