// Package httpapi serves explicitly native non-executing request metadata.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jonathanagustin/conduit/internal/debugsessions/application"
	"github.com/jonathanagustin/conduit/internal/debugsessions/domain"
)

type Bind func(*http.Request) (*application.Service, error)
type StatusError struct {
	Status  int
	Message string
}

func (e StatusError) Error() string { return e.Message }
func Register(mux *http.ServeMux, bind Bind) error {
	if mux == nil || bind == nil {
		return errors.New("debug request routes require a mux and authenticated service binding")
	}
	mux.HandleFunc("POST /v1/debug-session-requests", func(w http.ResponseWriter, r *http.Request) {
		s, err := bind(r)
		if err != nil {
			fail(w, err)
			return
		}
		if _, err := query(r, map[string]bool{}); err != nil {
			fail(w, err)
			return
		}
		var body struct {
			SessionID       string        `json:"sessionId"`
			Target          domain.Target `json:"target"`
			LifetimeSeconds int64         `json:"lifetimeSeconds"`
		}
		if err := decode(r, &body, false, true); err != nil {
			fail(w, err)
			return
		}
		record, err := s.Create(r.Context(), application.Draft{SessionID: body.SessionID, Target: body.Target, LifetimeSeconds: body.LifetimeSeconds})
		if err != nil {
			fail(w, err)
			return
		}
		write(w, record)
	})
	mux.HandleFunc("GET /v1/debug-session-requests", func(w http.ResponseWriter, r *http.Request) {
		s, err := bind(r)
		if err != nil {
			fail(w, err)
			return
		}
		values, err := query(r, map[string]bool{"environment": true, "namespace": true, "after": true, "limit": true})
		if err != nil {
			fail(w, err)
			return
		}
		limit := 50
		if raw := values.Get("limit"); raw != "" {
			limit, err = strconv.Atoi(raw)
			if err != nil {
				fail(w, domain.ErrInvalid)
				return
			}
		}
		records, next, err := s.List(r.Context(), domain.Scope{Environment: values.Get("environment"), Namespace: values.Get("namespace")}, values.Get("after"), limit)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, struct {
			Requests   []domain.Event `json:"requests"`
			NextCursor string         `json:"nextCursor,omitempty"`
			ValueTier  string         `json:"valueTier"`
		}{records, next, domain.ValueTier})
	})
	mux.HandleFunc("GET /v1/debug-session-requests/{requestID}", func(w http.ResponseWriter, r *http.Request) {
		s, err := bind(r)
		if err != nil {
			fail(w, err)
			return
		}
		if _, err := query(r, map[string]bool{}); err != nil {
			fail(w, err)
			return
		}
		record, err := s.Get(r.Context(), r.PathValue("requestID"))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, record)
	})
	mux.HandleFunc("GET /v1/debug-session-requests/{requestID}/history", func(w http.ResponseWriter, r *http.Request) {
		s, err := bind(r)
		if err != nil {
			fail(w, err)
			return
		}
		if _, err := query(r, map[string]bool{}); err != nil {
			fail(w, err)
			return
		}
		events, err := s.History(r.Context(), r.PathValue("requestID"))
		if err != nil {
			fail(w, err)
			return
		}
		write(w, struct {
			Events    []domain.Event `json:"events"`
			ValueTier string         `json:"valueTier"`
		}{events, domain.ValueTier})
	})
	mux.HandleFunc("POST /v1/debug-session-requests/{requestID}/revoke", func(w http.ResponseWriter, r *http.Request) {
		s, err := bind(r)
		if err != nil {
			fail(w, err)
			return
		}
		if _, err := query(r, map[string]bool{}); err != nil {
			fail(w, err)
			return
		}
		if err := decode(r, &struct{}{}, true, true); err != nil {
			fail(w, err)
			return
		}
		record, err := s.Mutate(r.Context(), r.PathValue("requestID"), domain.Revoked)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, record)
	})
	mux.HandleFunc("DELETE /v1/debug-session-requests/{requestID}", func(w http.ResponseWriter, r *http.Request) {
		s, err := bind(r)
		if err != nil {
			fail(w, err)
			return
		}
		if _, err := query(r, map[string]bool{}); err != nil {
			fail(w, err)
			return
		}
		if err := decode(r, &struct{}{}, true, false); err != nil {
			fail(w, err)
			return
		}
		record, err := s.Mutate(r.Context(), r.PathValue("requestID"), domain.Deleted)
		if err != nil {
			fail(w, err)
			return
		}
		write(w, record)
	})
	return nil
}
func query(r *http.Request, allowed map[string]bool) (url.Values, error) {
	if len(r.URL.RawQuery) > 4096 {
		return nil, domain.ErrInvalid
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, domain.ErrInvalid
	}
	for key, list := range values {
		if !allowed[key] || len(list) != 1 {
			return nil, domain.ErrInvalid
		}
	}
	return values, nil
}
func decode(r *http.Request, out any, empty, requireJSON bool) error {
	var document []byte
	var err error
	if r.Body != nil {
		document, err = io.ReadAll(io.LimitReader(r.Body, 4097))
	}
	if err != nil || len(document) > 4096 {
		return domain.ErrInvalid
	}
	if requireJSON || len(bytes.TrimSpace(document)) > 0 {
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			return StatusError{http.StatusUnsupportedMediaType, "JSON content type required"}
		}
	}
	if len(bytes.TrimSpace(document)) == 0 {
		if empty {
			return nil
		}
		return domain.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(document))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return domain.ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(document))
	if err := unique(d, 0, empty); err != nil {
		return domain.ErrInvalid
	}
	if _, err := d.Token(); err != io.EOF {
		return domain.ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(document))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return domain.ErrInvalid
	}
	return nil
}
func unique(d *json.Decoder, depth int, empty bool) error {
	if depth > 4 {
		return domain.ErrInvalid
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			token, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok || seen[key] || !allowedKey(depth, key, empty) {
				return domain.ErrInvalid
			}
			seen[key] = true
			if err := unique(d, depth+1, empty); err != nil {
				return err
			}
		}
		token, err = d.Token()
		if err != nil || token != json.Delim('}') {
			return domain.ErrInvalid
		}
	case '[':
		for d.More() {
			if err := unique(d, depth+1, empty); err != nil {
				return err
			}
		}
		token, err = d.Token()
		if err != nil || token != json.Delim(']') {
			return domain.ErrInvalid
		}
	default:
		return domain.ErrInvalid
	}
	return nil
}
func allowedKey(depth int, key string, empty bool) bool {
	if empty {
		return false
	}
	if depth == 0 {
		switch key {
		case "sessionId", "target", "lifetimeSeconds":
			return true
		}
	}
	if depth == 1 {
		switch key {
		case "environment", "namespace", "profile":
			return true
		}
	}
	return false
}
func write(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, err error) {
	status, message := http.StatusInternalServerError, "request operation unavailable"
	var failure StatusError
	switch {
	case errors.As(err, &failure):
		status, message = failure.Status, failure.Message
	case errors.Is(err, domain.ErrInvalid):
		status, message = 400, "invalid debug-session request"
	case errors.Is(err, domain.ErrForbidden):
		status, message = 403, "debug-session request forbidden"
	case errors.Is(err, domain.ErrNotFound):
		status, message = 404, "debug-session request not found"
	case errors.Is(err, domain.ErrConflict):
		status, message = 409, "debug-session request conflict"
	case errors.Is(err, application.ErrUnavailable):
		status, message = 503, "debug-session request unavailable"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, message = 408, "debug-session request cancelled"
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
