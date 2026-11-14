// Package httpapi serves native discussion and atomic governed label operations.
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/jonathanagustin/conduit/internal/changeactivity/application"
	"github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"io"
	"net/http"
	"strings"
	"time"
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
		id := r.PathValue("requestID")
		cursor, err := readCursor(r, id)
		if err != nil {
			fail(w, err)
			return
		}
		comments, err := service.List(r.Context(), id)
		if err != nil {
			fail(w, err)
			return
		}
		page := make([]domain.Comment, 0, domain.MaxPageSize)
		more := false
		for _, comment := range comments {
			if !cursor.At.IsZero() && (comment.CreatedAt.Before(cursor.At) || (comment.CreatedAt.Equal(cursor.At) && comment.ID <= cursor.ID)) {
				continue
			}
			if len(page) == domain.MaxPageSize {
				more = true
				break
			}
			page = append(page, comment)
		}
		next := ""
		if more {
			last := page[len(page)-1]
			document, _ := json.Marshal(pageCursor{RequestID: id, ID: last.ID, At: last.CreatedAt})
			next = base64.RawURLEncoding.EncodeToString(document)
		}
		write(w, http.StatusOK, map[string]any{"comments": page, "nextCursor": next, "valueTier": domain.ValueTier})
	})
	return nil
}

type pageCursor struct {
	RequestID string    `json:"request"`
	ID        string    `json:"id"`
	At        time.Time `json:"at"`
}

func readCursor(r *http.Request, id string) (pageCursor, error) {
	for key, values := range r.URL.Query() {
		if key != "cursor" || len(values) != 1 {
			return pageCursor{}, domain.ErrInvalid
		}
	}
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return pageCursor{}, nil
	}
	if len(raw) > 1024 {
		return pageCursor{}, domain.ErrInvalid
	}
	document, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return pageCursor{}, domain.ErrInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(string(document)))
	decoder.DisallowUnknownFields()
	var c pageCursor
	if err := decoder.Decode(&c); err != nil {
		return pageCursor{}, domain.ErrInvalid
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return pageCursor{}, domain.ErrInvalid
	}
	if c.RequestID != id || !domain.ValidRequestID(c.RequestID) || !domain.ValidCommentID(c.ID) || c.At.IsZero() {
		return pageCursor{}, domain.ErrInvalid
	}
	return c, nil
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
