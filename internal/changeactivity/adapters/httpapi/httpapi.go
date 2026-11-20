// Package httpapi serves native discussion and atomic governed label operations.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jonathanagustin/conduit/internal/changeactivity/application"
	"github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"io"
	"net/http"
	"strings"
)

type LabelMutator interface {
	MutateLabel(context.Context, string, string, string, bool) (domain.LabelMutation, error)
}
type Config struct {
	BindDiscussion func(*http.Request) (*application.Service, error)
	BindLabels     func(*http.Request) (LabelMutator, error)
}
type StatusError struct {
	Status  int
	Message string
}

func (e StatusError) Error() string { return e.Message }
func Register(mux *http.ServeMux, config Config) error {
	if mux == nil || config.BindLabels == nil {
		return errors.New("request activity requires a mux and governed label authority")
	}
	mux.HandleFunc("POST /v1/change-requests/{requestID}/label-mutations", func(w http.ResponseWriter, r *http.Request) {
		service, err := config.BindLabels(r)
		if err != nil {
			fail(w, err)
			return
		}
		var body struct {
			LabelName string `json:"labelName"`
			Operation string `json:"operation"`
			DryRun    bool   `json:"dryRun"`
		}
		if !decode(w, r, &body, 4096) {
			return
		}
		id := r.PathValue("requestID")
		if !domain.ValidRequestID(id) || body.LabelName == "" || body.LabelName != strings.TrimSpace(body.LabelName) || len(body.LabelName) > 128 || (body.Operation != "ADD" && body.Operation != "REMOVE") {
			fail(w, domain.ErrInvalid)
			return
		}
		result, err := service.MutateLabel(r.Context(), id, body.LabelName, body.Operation, body.DryRun)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, http.StatusOK, result)
	})
	if config.BindDiscussion == nil {
		return nil
	}
	RegisterDiscussionReadConnect(mux, config.BindDiscussion)
	mux.HandleFunc("POST /v1/change-requests/{requestID}/comments", func(w http.ResponseWriter, r *http.Request) {
		service, err := config.BindDiscussion(r)
		if err != nil {
			fail(w, err)
			return
		}
		var body struct {
			Text string `json:"text"`
		}
		if !decode(w, r, &body, domain.MaxDocumentBytes) {
			return
		}
		result, err := service.Append(r.Context(), r.PathValue("requestID"), body.Text)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, http.StatusCreated, result)
	})
	mux.HandleFunc("GET /v1/change-requests/{requestID}/comments", func(w http.ResponseWriter, r *http.Request) {
		service, err := config.BindDiscussion(r)
		if err != nil {
			fail(w, err)
			return
		}
		for key, values := range r.URL.Query() {
			if key != "cursor" || len(values) != 1 {
				fail(w, domain.ErrInvalid)
				return
			}
		}
		page, err := service.ReadPage(r.Context(), r.PathValue("requestID"), r.URL.Query().Get("cursor"))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, http.StatusOK, page)
	})
	return nil
}

func decode(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		fail(w, domain.ErrInvalid)
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		fail(w, domain.ErrInvalid)
		return false
	}
	return true
}
func fail(w http.ResponseWriter, err error) {
	status, message := http.StatusInternalServerError, "Change Request activity failed"
	var transport StatusError
	switch {
	case errors.As(err, &transport):
		status, message = transport.Status, transport.Message
	case errors.Is(err, domain.ErrInvalid):
		status, message = http.StatusBadRequest, "invalid Change Request activity input"
	case errors.Is(err, domain.ErrNotFound):
		status, message = http.StatusNotFound, "Change Request or label not found"
	case errors.Is(err, domain.ErrForbidden):
		status, message = http.StatusForbidden, "Change Request activity forbidden"
	case errors.Is(err, domain.ErrConflict):
		status, message = http.StatusConflict, "Change Request activity conflicts with current state"
	}
	write(w, status, map[string]string{"error": message})
}
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
