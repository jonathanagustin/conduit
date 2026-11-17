// Package httpapi serves assistant tool calls, turns, and conversation
// threads. It does not apply an action.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jonathanagustin/conduit/internal/assistant/application"
	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

const (
	ToolRoute         = "POST /v1/assistant/tools"
	TurnRoute         = "POST /v1/assistant/turns"
	ThreadListRoute   = "GET /v1/assistant/threads"
	ThreadCreateRoute = "POST /v1/assistant/threads"
	ThreadReadRoute   = "GET /v1/assistant/threads/{threadID}"
	ThreadDeleteRoute = "DELETE /v1/assistant/threads/{threadID}"
)

// Tools runs one tool, one turn, or one thread operation for the caller.
type Tools interface {
	Invoke(ctx context.Context, actor application.Actor, mode, name string, arguments json.RawMessage) (application.Result, error)
	Converse(ctx context.Context, actor application.Actor, request application.TurnRequest, observer application.Observer) (application.Turn, error)
	CreateThread(ctx context.Context, actor application.Actor, title string) (domain.Thread, error)
	ListThreads(ctx context.Context, actor application.Actor) ([]domain.Thread, error)
	GetThread(ctx context.Context, actor application.Actor, id string) (domain.Thread, error)
	DeleteThread(ctx context.Context, actor application.Actor, id string) error
}

// Config is the route's dependencies.
type Config struct {
	Tools     Tools
	Authorize func(*http.Request) error
	Caller    func(*http.Request) (application.Actor, error)
}

// Register mounts the assistant routes.
func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(ToolRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		var body struct {
			Mode      string          `json:"mode"`
			Tool      string          `json:"tool"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := decode(request, &body); err != nil {
			writeError(writer, http.StatusBadRequest, codeInvalidRequest, "request body must be a JSON object")
			return
		}
		caller, ok := caller(cfg, writer, request)
		if !ok {
			return
		}
		result, err := cfg.Tools.Invoke(request.Context(), caller, body.Mode, body.Tool, body.Arguments)
		if err != nil {
			status := http.StatusBadRequest
			if !errors.Is(err, domain.ErrToolInvalid) {
				status = http.StatusBadGateway
			}
			if errors.Is(err, domain.ErrAuditUnavailable) {
				status = http.StatusServiceUnavailable
			}
			writeError(writer, status, application.ErrorCode(err), err.Error())
			return
		}
		writeJSON(writer, http.StatusOK, result)
	})
	mux.HandleFunc(TurnRoute, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		var body struct {
			Mode     string        `json:"mode"`
			Message  string        `json:"message"`
			Context  []domain.Chip `json:"context"`
			ThreadID string        `json:"threadId"`
		}
		if err := decode(request, &body); err != nil {
			writeError(writer, http.StatusBadRequest, codeInvalidRequest, "request body must be a JSON object")
			return
		}
		if strings.TrimSpace(body.Message) == "" {
			writeError(writer, http.StatusBadRequest, codeInvalidRequest, "message is required")
			return
		}
		caller, ok := caller(cfg, writer, request)
		if !ok {
			return
		}
		turn := application.TurnRequest{Mode: body.Mode, Message: body.Message, Chips: body.Context, ThreadID: strings.TrimSpace(body.ThreadID)}
		if wantsEventStream(request) {
			streamTurn(cfg.Tools, caller, turn, writer, request)
			return
		}
		answered, err := cfg.Tools.Converse(request.Context(), caller, turn, nil)
		if err != nil {
			writeTurnError(writer, answered, err)
			return
		}
		writeJSON(writer, http.StatusOK, answered)
	})
	mux.HandleFunc(ThreadListRoute, func(writer http.ResponseWriter, request *http.Request) {
		caller, ok := threadCaller(cfg, writer, request)
		if !ok {
			return
		}
		threads, err := cfg.Tools.ListThreads(request.Context(), caller)
		if err != nil {
			writeTurnError(writer, application.Turn{}, err)
			return
		}
		if threads == nil {
			threads = []domain.Thread{}
		}
		writeJSON(writer, http.StatusOK, map[string]any{"threads": threads})
	})
	mux.HandleFunc(ThreadCreateRoute, func(writer http.ResponseWriter, request *http.Request) {
		caller, ok := threadCaller(cfg, writer, request)
		if !ok {
			return
		}
		var body struct {
			Title string `json:"title"`
		}
		if err := decode(request, &body); err != nil && !errors.Is(err, io.EOF) {
			writeError(writer, http.StatusBadRequest, codeInvalidRequest, "request body must be a JSON object")
			return
		}
		thread, err := cfg.Tools.CreateThread(request.Context(), caller, body.Title)
		if err != nil {
			writeTurnError(writer, application.Turn{}, err)
			return
		}
		thread.Messages = []domain.Message{}
		writeJSON(writer, http.StatusCreated, threadDocument(thread))
	})
	mux.HandleFunc(ThreadReadRoute, func(writer http.ResponseWriter, request *http.Request) {
		caller, ok := threadCaller(cfg, writer, request)
		if !ok {
			return
		}
		thread, err := cfg.Tools.GetThread(request.Context(), caller, request.PathValue("threadID"))
		if err != nil {
			writeTurnError(writer, application.Turn{}, err)
			return
		}
		writeJSON(writer, http.StatusOK, threadDocument(thread))
	})
	mux.HandleFunc(ThreadDeleteRoute, func(writer http.ResponseWriter, request *http.Request) {
		caller, ok := threadCaller(cfg, writer, request)
		if !ok {
			return
		}
		if err := cfg.Tools.DeleteThread(request.Context(), caller, request.PathValue("threadID")); err != nil {
			writeTurnError(writer, application.Turn{}, err)
			return
		}
		writer.Header().Set("Cache-Control", "no-store")
		writer.WriteHeader(http.StatusNoContent)
	})
}

// threadDocument is a thread with its messages always present as an array.
func threadDocument(thread domain.Thread) map[string]any {
	messages := thread.Messages
	if messages == nil {
		messages = []domain.Message{}
	}
	return map[string]any{
		"id": thread.ID, "title": thread.Title, "createdAt": thread.CreatedAt, "updatedAt": thread.UpdatedAt,
		"messageCount": thread.MessageCount, "messages": messages,
	}
}

const (
	codeInvalidRequest = "INVALID_REQUEST"
	codeUnauthorized   = "UNAUTHORIZED"
	codeNotConfigured  = "ASSISTANT_NOT_CONFIGURED"
)

// turnStatus is the HTTP status for a failure code.
func turnStatus(code string) int {
	switch code {
	case application.CodeModelNotConfigured:
		return http.StatusUnprocessableEntity
	case application.CodeThreadNotFound:
		return http.StatusNotFound
	case application.CodeThreadLimit, application.CodeThreadFull:
		return http.StatusConflict
	case application.CodeThreadInvalid, application.CodeToolInvalid:
		return http.StatusBadRequest
	case application.CodeAuditUnavailable, application.CodeThreadsUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadGateway
	}
}

// writeTurnError answers a failed turn or thread call: {code, error}, plus the
// label on a refused model call, plus what already ran when anything did.
func writeTurnError(writer http.ResponseWriter, turn application.Turn, err error) {
	code := application.ErrorCode(err)
	body := map[string]any{"code": code, "error": err.Error()}
	if code == application.CodeModelNotConfigured {
		body["label"] = domain.LabelNoModel
	}
	if turn.Rounds > 0 {
		body["turn"] = turn
	}
	writeJSON(writer, turnStatus(code), body)
}

func authorized(cfg Config, writer http.ResponseWriter, request *http.Request) bool {
	if cfg.Authorize == nil {
		writeError(writer, http.StatusUnauthorized, codeUnauthorized, "unauthorized")
		return false
	}
	if err := cfg.Authorize(request); err != nil {
		writeError(writer, http.StatusUnauthorized, codeUnauthorized, "unauthorized")
		return false
	}
	return true
}

func caller(cfg Config, writer http.ResponseWriter, request *http.Request) (application.Actor, bool) {
	if cfg.Caller == nil || cfg.Tools == nil {
		writeError(writer, http.StatusBadGateway, codeNotConfigured, "assistant tools are not configured")
		return application.Actor{}, false
	}
	value, err := cfg.Caller(request)
	if err != nil || strings.TrimSpace(value.Username) == "" {
		writeError(writer, http.StatusUnauthorized, codeUnauthorized, "unauthorized")
		return application.Actor{}, false
	}
	return value, true
}

// threadCaller authenticates a thread route. A thread is private to its
// owner, so its responses are never cached.
func threadCaller(cfg Config, writer http.ResponseWriter, request *http.Request) (application.Actor, bool) {
	writer.Header().Set("Cache-Control", "no-store")
	if !authorized(cfg, writer, request) {
		return application.Actor{}, false
	}
	return caller(cfg, writer, request)
}

func decode(request *http.Request, dest any) error {
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dest)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]string{"code": code, "error": message})
}
