// Package httpapi serves model usage and transcript export. Usage has an
// operator route that answers about the caller only and an administrative one
// for every owner. Transcripts are administrative only: a transcript can hold
// tenant source code, so no route here lets an owner read one, their own
// included.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
)

const (
	UsageRoute           = "GET /v1/ai/usage"
	AdminUsageRoute      = "GET /v1/admin/ai/usage"
	AdminTranscriptsList = "GET /v1/admin/ai/transcripts"
	AdminTranscriptRoute = "GET /v1/admin/ai/transcripts/{runID}"
)

const maxUsageDays = 92

// Config wires the routes. Every function is required.
type Config struct {
	Governor    *modelgovernor.Governor
	Transcripts modelgovernor.TranscriptStore
	// AuditEvents reads one run's model-call audit events.
	AuditEvents func(ctx context.Context, runID string) ([]modelgovernor.CallAudit, error)
	// Caller is the authenticated operator, or false.
	Caller func(*http.Request) (string, bool)
	// Administrator is the authenticated operator and 0 when they hold the
	// administrative authority, otherwise 401 or 403. The name is returned
	// beside a 403 so the refusal can be audited.
	Administrator func(*http.Request) (string, int)
	// RecordExport audits one export attempt. An error refuses the export.
	RecordExport func(ctx context.Context, actor, runID string, allowed bool, reason string, export *modelgovernor.Export) error
	Now          func() time.Time
}

// Register mounts the routes.
func Register(mux *http.ServeMux, cfg Config) {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	mux.HandleFunc(UsageRoute, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		owner, ok := cfg.Caller(request)
		if !ok || strings.TrimSpace(owner) == "" {
			writeError(writer, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
			return
		}
		days, ok := daysFrom(writer, request)
		if !ok {
			return
		}
		budget, err := cfg.Governor.BudgetFor(request.Context(), owner)
		if err != nil {
			writeError(writer, http.StatusServiceUnavailable, "USAGE_UNAVAILABLE", "model usage is unavailable")
			return
		}
		rows, err := cfg.Governor.Usage(request.Context(), owner, days)
		if err != nil {
			writeError(writer, http.StatusServiceUnavailable, "USAGE_UNAVAILABLE", "model usage is unavailable")
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"owner": owner, "days": days, "budget": budget, "usage": rows})
	})
	mux.HandleFunc(AdminUsageRoute, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		if _, ok := administrator(cfg, writer, request); !ok {
			return
		}
		days, ok := daysFrom(writer, request)
		if !ok {
			return
		}
		// An empty owner is every owner; ?owner= narrows to one.
		rows, err := cfg.Governor.Usage(request.Context(), strings.TrimSpace(request.URL.Query().Get("owner")), days)
		if err != nil {
			writeError(writer, http.StatusServiceUnavailable, "USAGE_UNAVAILABLE", "model usage is unavailable")
			return
		}
		config := cfg.Governor.Config()
		writeJSON(writer, http.StatusOK, map[string]any{
			"days": days, "usage": rows,
			"dailyTokenBudget": unlimited(config.DailyTokenBudget), "dailyCallBudget": unlimited(config.DailyCallBudget),
		})
	})
	mux.HandleFunc(AdminTranscriptsList, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		if _, ok := administrator(cfg, writer, request); !ok {
			return
		}
		runs := []modelgovernor.RunSummary{}
		if cfg.Transcripts != nil {
			query := request.URL.Query()
			found, err := cfg.Transcripts.Runs(request.Context(), modelgovernor.RunFilter{
				Owner: strings.TrimSpace(query.Get("owner")), SessionID: strings.TrimSpace(query.Get("session")), Limit: 200,
			}, now())
			if err != nil {
				writeError(writer, http.StatusServiceUnavailable, "TRANSCRIPTS_UNAVAILABLE", "the transcript store is unavailable")
				return
			}
			runs = found
		}
		config := cfg.Governor.Config()
		writeJSON(writer, http.StatusOK, map[string]any{
			"transcripts": config.Transcripts, "retention": config.TranscriptRetention.String(), "runs": runs,
		})
	})
	mux.HandleFunc(AdminTranscriptRoute, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		runID := request.PathValue("runID")
		actor, status := cfg.Administrator(request)
		if status != 0 {
			if status == http.StatusForbidden && modelgovernor.ValidRunID(runID) {
				// A refused attempt to read a transcript is worth a record.
				_ = cfg.RecordExport(request.Context(), actor, runID, false, "administrative authority is required", nil)
			}
			refuse(writer, status)
			return
		}
		if !modelgovernor.ValidRunID(runID) {
			writeError(writer, http.StatusBadRequest, "INVALID_REQUEST", "run id is not valid")
			return
		}
		events, err := cfg.AuditEvents(request.Context(), runID)
		if err != nil {
			writeError(writer, http.StatusServiceUnavailable, "AUDIT_UNAVAILABLE", "the audit log is unavailable")
			return
		}
		var records []modelgovernor.TranscriptRecord
		if cfg.Transcripts != nil {
			if records, err = cfg.Transcripts.Run(request.Context(), runID, now()); err != nil {
				writeError(writer, http.StatusServiceUnavailable, "TRANSCRIPTS_UNAVAILABLE", "the transcript store is unavailable")
				return
			}
		}
		export, err := modelgovernor.VerifyRun(runID, events, records)
		if errors.Is(err, modelgovernor.ErrRunNotFound) {
			writeError(writer, http.StatusNotFound, "RUN_NOT_FOUND", "no such AI run")
			return
		}
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "EXPORT_FAILED", "the export could not be built")
			return
		}
		// Audited before the body is written: an export nobody recorded is
		// not served.
		if err := cfg.RecordExport(request.Context(), actor, runID, true, "transcript exported", &export); err != nil {
			writeError(writer, http.StatusServiceUnavailable, "AUDIT_UNAVAILABLE", "the export could not be audited")
			return
		}
		writeJSON(writer, http.StatusOK, export)
	})
}

func administrator(cfg Config, writer http.ResponseWriter, request *http.Request) (string, bool) {
	actor, status := cfg.Administrator(request)
	if status != 0 {
		refuse(writer, status)
		return "", false
	}
	return actor, true
}

func refuse(writer http.ResponseWriter, status int) {
	if status == http.StatusForbidden {
		writeError(writer, status, "FORBIDDEN", "administrative authority is required")
		return
	}
	writeError(writer, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
}

func daysFrom(writer http.ResponseWriter, request *http.Request) (int, bool) {
	text := strings.TrimSpace(request.URL.Query().Get("days"))
	if text == "" {
		return 1, true
	}
	days, err := strconv.Atoi(text)
	if err != nil || days < 1 || days > maxUsageDays {
		writeError(writer, http.StatusBadRequest, "INVALID_REQUEST", "days must be between 1 and "+strconv.Itoa(maxUsageDays))
		return 0, false
	}
	return days, true
}

func unlimited(limit int64) *int64 {
	if limit <= 0 {
		return nil
	}
	return &limit
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]string{"code": code, "error": message})
}

// Stable codes for a model call refused by the governor.
const (
	CodeModelBusy           = "MODEL_BUSY"
	CodeModelBudgetExceeded = "MODEL_BUDGET_EXCEEDED"
)

// RefusalCode names a governor refusal, or returns false.
func RefusalCode(err error) (string, bool) {
	switch {
	case errors.Is(err, modelgovernor.ErrBudgetExceeded):
		return CodeModelBudgetExceeded, true
	case errors.Is(err, modelgovernor.ErrBusy):
		return CodeModelBusy, true
	}
	return "", false
}

// RetryAfterSeconds is the whole seconds a refusal asks the caller to wait,
// at least 1.
func RetryAfterSeconds(err error) int {
	var hint interface{ RetryAfterHint() time.Duration }
	seconds := 1
	if errors.As(err, &hint) {
		if whole := int((hint.RetryAfterHint() + time.Second - 1) / time.Second); whole > 1 {
			seconds = whole
		}
	}
	return seconds
}

// WriteRefusal answers 429 with Retry-After for a governor refusal.
func WriteRefusal(writer http.ResponseWriter, err error) {
	code, _ := RefusalCode(err)
	writer.Header().Set("Retry-After", strconv.Itoa(RetryAfterSeconds(err)))
	writeError(writer, http.StatusTooManyRequests, code, err.Error())
}

// RefusalAsTooManyRequests makes a POST whose model call the governor refused
// answer 429 with Retry-After, whatever the capability's own handler made of
// the error. One capability maps a model failure to 502 and another returns
// 200 with the failure inside a result document; this is the one place that
// knows the failure was a limit. The run in the request context is where the
// governor left the refusal. The capability's own body is not sent.
//
// A response that already started (a stream that sent its first event) is
// not rewritten: its status is on the wire.
func RefusalAsTooManyRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		run := modelgovernor.RunFrom(request.Context())
		if run == nil || request.Method != http.MethodPost {
			next.ServeHTTP(writer, request)
			return
		}
		next.ServeHTTP(&refusalWriter{ResponseWriter: writer, run: run}, request)
	})
}

type refusalWriter struct {
	http.ResponseWriter
	run       *modelgovernor.Run
	started   bool
	rewritten bool
}

func (writer *refusalWriter) WriteHeader(status int) {
	if writer.started {
		return
	}
	writer.started = true
	if refusal := writer.run.Refusal(); refusal != nil && status != http.StatusTooManyRequests {
		writer.rewritten = true
		header := writer.ResponseWriter.Header()
		header.Del("Content-Length")
		WriteRefusal(writer.ResponseWriter, refusal)
		return
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *refusalWriter) Write(data []byte) (int, error) {
	if !writer.started {
		writer.WriteHeader(http.StatusOK)
	}
	if writer.rewritten {
		// The capability's own error body is dropped; the 429 body is sent.
		return len(data), nil
	}
	return writer.ResponseWriter.Write(data)
}

func (writer *refusalWriter) Flush() {
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		if !writer.started {
			writer.WriteHeader(http.StatusOK)
		}
		flusher.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (writer *refusalWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }
