// Package httpapi serves the planning-session HTTP contract. The routes
// start a session from a repository URL and ref, accept Needs Input
// answers, record Accept, and refuse compute before Accept.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

const (
	CreateRoute  = "POST /v1/planning-sessions"
	GetRoute     = "GET /v1/planning-sessions/{sessionID}"
	TurnRoute    = "POST /v1/planning-sessions/{sessionID}/turns"
	AnswerRoute  = "POST /v1/planning-sessions/{sessionID}/answers"
	AcceptRoute  = "POST /v1/planning-sessions/{sessionID}/accept"
	BindingRoute = "GET /v1/planning-sessions/{sessionID}/binding"
	EventsRoute  = "GET /v1/planning-sessions/{sessionID}/events"
	ComputeRoute = "POST /v1/planning-sessions/{sessionID}/compute"
)

const maxBody = 1 << 20

// Sessions is the planning-session application port.
type Sessions interface {
	Create(ctx context.Context, repoURL, ref string) (*domain.Session, error)
	Get(ctx context.Context, id string) (*domain.Session, error)
	Turn(ctx context.Context, id, message string) (*domain.Session, error)
	Answer(ctx context.Context, id string, answers []domain.Answer) (*domain.Session, error)
	Accept(ctx context.Context, id string) (*domain.Session, error)
	Compute(ctx context.Context, id string) (*domain.Session, error)
	Events(ctx context.Context, id string) ([]domain.Event, error)
}

// Config is the route's dependencies. A nil Authorize refuses everyone.
type Config struct {
	Service   Sessions
	Authorize func(*http.Request) error
}

// Register mounts the planning-session routes.
func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(CreateRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		var body struct {
			RepoURL string `json:"repoUrl"`
			Ref     string `json:"ref"`
		}
		if !decodeBody(writer, request, &body) {
			return
		}
		session, err := cfg.Service.Create(request.Context(), body.RepoURL, body.Ref)
		if err != nil {
			writeDomainError(writer, err)
			return
		}
		writeJSON(writer, http.StatusCreated, session)
	})
	mux.HandleFunc(GetRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		session, err := cfg.Service.Get(request.Context(), request.PathValue("sessionID"))
		if err != nil {
			writeDomainError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, session)
	})
	mux.HandleFunc(TurnRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		var body struct {
			Message string `json:"message"`
		}
		if request.Body != nil && request.ContentLength != 0 {
			if !decodeBodyAllowEmpty(writer, request, &body) {
				return
			}
		}
		id := request.PathValue("sessionID")
		prior := 0
		if existing, err := cfg.Service.Get(request.Context(), id); err == nil {
			prior = len(existing.Events)
		}
		session, err := cfg.Service.Turn(request.Context(), id, body.Message)
		if err != nil {
			writeDomainError(writer, err)
			return
		}
		if wantsStream(request) {
			if prior > len(session.Events) {
				prior = 0
			}
			writeTurnStream(writer, session.Events[prior:])
			return
		}
		writeJSON(writer, http.StatusOK, session)
	})
	mux.HandleFunc(AnswerRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		var body struct {
			Answers []domain.Answer `json:"answers"`
		}
		if !decodeBody(writer, request, &body) {
			return
		}
		session, err := cfg.Service.Answer(request.Context(), request.PathValue("sessionID"), body.Answers)
		if err != nil {
			writeDomainError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, session)
	})
	mux.HandleFunc(AcceptRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		session, err := cfg.Service.Accept(request.Context(), request.PathValue("sessionID"))
		if err != nil {
			writeDomainError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, session)
	})
	mux.HandleFunc(BindingRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		session, err := cfg.Service.Get(request.Context(), request.PathValue("sessionID"))
		if err != nil {
			writeDomainError(writer, err)
			return
		}
		if session.Binding == nil {
			writeError(writer, http.StatusConflict, domain.CodeNotAccepted, "the planning session has not been accepted")
			return
		}
		writeJSON(writer, http.StatusOK, session.Binding)
	})
	mux.HandleFunc(EventsRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		events, err := cfg.Service.Events(request.Context(), request.PathValue("sessionID"))
		if err != nil {
			writeDomainError(writer, err)
			return
		}
		if events == nil {
			events = []domain.Event{}
		}
		writeJSON(writer, http.StatusOK, map[string]any{"items": events})
	})
	mux.HandleFunc(ComputeRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		session, err := cfg.Service.Compute(request.Context(), request.PathValue("sessionID"))
		if err != nil {
			writeDomainError(writer, err)
			return
		}
		writeJSON(writer, http.StatusAccepted, session)
	})
}

func wantsStream(request *http.Request) bool {
	return strings.Contains(request.Header.Get("Accept"), "text/event-stream")
}

func writeTurnStream(writer http.ResponseWriter, events []domain.Event) {
	writer.Header().Set("content-type", "text/event-stream")
	writer.Header().Set("cache-control", "no-cache")
	writer.Header().Set("x-content-type-options", "nosniff")
	writer.WriteHeader(http.StatusOK)
	flusher, _ := writer.(http.Flusher)
	for _, event := range events {
		payload, err := json.Marshal(event)
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintf(writer, "id: %s\nevent: %s\ndata: %s\n\n", event.ID, event.Type, payload)
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func authorized(cfg Config, writer http.ResponseWriter, request *http.Request) bool {
	if cfg.Authorize == nil || cfg.Authorize(request) != nil {
		writeError(writer, http.StatusUnauthorized, "", "unauthorized")
		return false
	}
	return true
}

func decodeBody(writer http.ResponseWriter, request *http.Request, dest any) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, maxBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(writer, http.StatusRequestEntityTooLarge, "", "request body is larger than the limit")
			return false
		}
		writeError(writer, http.StatusBadRequest, "", "request body must be a JSON object")
		return false
	}
	return true
}

func decodeBodyAllowEmpty(writer http.ResponseWriter, request *http.Request, dest any) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, maxBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		if errors.Is(err, io.EOF) {
			return true
		}
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(writer, http.StatusRequestEntityTooLarge, "", "request body is larger than the limit")
			return false
		}
		writeError(writer, http.StatusBadRequest, "", "request body must be a JSON object")
		return false
	}
	return true
}

func writeDomainError(writer http.ResponseWriter, err error) {
	var intake domain.IntakeError
	if errors.As(err, &intake) {
		writeError(writer, http.StatusBadRequest, domain.CodeInvalidIntake, intake.Reason)
		return
	}
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(writer, http.StatusNotFound, domain.CodeSessionNotFound, err.Error())
	case errors.Is(err, domain.ErrModelNotConfigured):
		writeError(writer, http.StatusServiceUnavailable, domain.CodeModelNotConfigured, err.Error())
	case errors.Is(err, domain.ErrPlanRequired):
		writeError(writer, http.StatusConflict, domain.CodePlanRequired, err.Error())
	case errors.Is(err, domain.ErrCommitNotPinned):
		writeError(writer, http.StatusConflict, domain.CodeCommitNotPinned, err.Error())
	case errors.Is(err, domain.ErrNeedsInputPending):
		writeError(writer, http.StatusConflict, domain.CodeNeedsInputPending, err.Error())
	case errors.Is(err, domain.ErrAlreadyAccepted):
		writeError(writer, http.StatusConflict, domain.CodeAlreadyAccepted, err.Error())
	case errors.Is(err, domain.ErrComputeBeforeAccept):
		writeError(writer, http.StatusConflict, domain.CodeComputeBeforeAccept, err.Error())
	case errors.Is(err, domain.ErrComputeNotConfigured):
		writeError(writer, http.StatusNotImplemented, domain.CodeComputeNotConfigured, err.Error())
	case errors.Is(err, domain.ErrNoPendingQuestions):
		writeError(writer, http.StatusConflict, domain.CodeNoPendingQuestions, err.Error())
	case errors.Is(err, domain.ErrUnknownQuestion):
		writeError(writer, http.StatusBadRequest, domain.CodeUnknownQuestion, err.Error())
	case errors.Is(err, domain.ErrInvalidAnswer):
		writeError(writer, http.StatusBadRequest, domain.CodeInvalidAnswer, err.Error())
	default:
		slog.Error("planning session request failed", "error", err)
		writeError(writer, http.StatusBadGateway, "", "planning session is unavailable")
	}
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	body := map[string]string{"error": strings.TrimSpace(message)}
	if code != "" {
		body["code"] = code
	}
	writeJSON(writer, status, body)
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("content-type", "application/json")
	writer.Header().Set("x-content-type-options", "nosniff")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}
