// Package httpapi serves the planning-session HTTP contract. The routes
// start a session from a repository URL and ref, accept Needs Input
// answers, record Accept, and refuse compute before Accept.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

// TurnWriteWindow is how long a turn response may take to write. It covers
// several model phases at the configured model timeout.
const TurnWriteWindow = 30 * time.Minute

const (
	CancelTurnRoute = "POST /v1/planning-sessions/{sessionID}/turns/{runID}/cancel"
	ListRoute       = "GET /v1/planning-sessions"
	CreateRoute     = "POST /v1/planning-sessions"
	GetRoute        = "GET /v1/planning-sessions/{sessionID}"
	TurnRoute       = "POST /v1/planning-sessions/{sessionID}/turns"
	AnswerRoute     = "POST /v1/planning-sessions/{sessionID}/answers"
	AcceptRoute     = "POST /v1/planning-sessions/{sessionID}/accept"
	BindingRoute    = "GET /v1/planning-sessions/{sessionID}/binding"
	EventsRoute     = "GET /v1/planning-sessions/{sessionID}/events"
	ComputeRoute    = "POST /v1/planning-sessions/{sessionID}/compute"
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
	Service Sessions
	// ResolveService selects an isolated session store after authentication.
	ResolveService func(*http.Request) (Sessions, error)
	Authorize      func(*http.Request) error
}

func writeDomainError(writer http.ResponseWriter, err error) {
	var intake domain.IntakeError
	if errors.As(err, &intake) {
		writeError(writer, http.StatusBadRequest, domain.CodeInvalidIntake, intake.Reason)
		return
	}
	switch {
	case errors.Is(err, domain.ErrRunCancelled):
		writeError(writer, http.StatusConflict, "RUN_CANCELLED", err.Error())
	case errors.Is(err, domain.ErrRunConflict):
		writeError(writer, http.StatusConflict, "RUN_CONFLICT", err.Error())
	case errors.Is(err, domain.ErrRunCapacity):
		writeError(writer, http.StatusTooManyRequests, "RUN_CAPACITY", err.Error())
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
	case errors.Is(err, domain.ErrTurnInProgress):
		writeError(writer, http.StatusConflict, domain.CodeTurnInProgress, err.Error())
	case errors.Is(err, domain.ErrAlreadyAccepted):
		writeError(writer, http.StatusConflict, domain.CodeAlreadyAccepted, err.Error())
	case errors.Is(err, domain.ErrComputeBeforeAccept):
		writeError(writer, http.StatusConflict, domain.CodeComputeBeforeAccept, err.Error())
	case errors.Is(err, domain.ErrComputeInProgress):
		writeError(writer, http.StatusConflict, domain.CodeComputeInProgress, err.Error())
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
