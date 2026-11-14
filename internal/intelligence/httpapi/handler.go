// Package httpapi serves proposal logic, evals, and course options.
// It does not apply a change.
package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/jonathanagustin/conduit/internal/intelligence/application"
)

const (
	LogicRoute   = "GET /v1/intelligence/logic"
	EvalsRoute   = "GET /v1/intelligence/evals"
	RunEvalRoute = "POST /v1/intelligence/evals/run"
	CaseRoute    = "POST /v1/intelligence/evals/cases"
	CourseRoute  = "POST /v1/intelligence/courses"
	SelectRoute  = "POST /v1/intelligence/courses/select"
	EventsRoute  = "GET /v1/intelligence/events"
)

// API is the application service the composition root wires.
type API interface {
	Logic() application.Config
	Cases() []application.Case
	AddCase(scenario, bound, value, expected string) (application.Case, error)
	Run(ctx context.Context, actor string) ([]application.CaseResult, string, error)
	Courses() []application.Option
	Select(ctx context.Context, actor, value, environment, installation, product string) (string, string, error)
}

// Events lists observed packaging runs beside rules that are not executed.
type Events interface {
	Observed(ctx context.Context) []map[string]string
}

// Config is the route's dependencies.
type Config struct {
	API       API
	Events    Events
	Authorize func(*http.Request) error
	Caller    func(*http.Request) string
}

// Register mounts the intelligence routes.
func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(LogicRoute, func(w http.ResponseWriter, r *http.Request) {
		if !allowed(cfg, w, r) {
			return
		}
		writeJSON(w, http.StatusOK, cfg.API.Logic())
	})
	mux.HandleFunc(EvalsRoute, func(w http.ResponseWriter, r *http.Request) {
		if !allowed(cfg, w, r) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"cases": cfg.API.Cases()})
	})
	mux.HandleFunc(RunEvalRoute, func(w http.ResponseWriter, r *http.Request) {
		if !allowed(cfg, w, r) || cfg.API == nil {
			return
		}
		results, auditID, err := cfg.API.Run(r.Context(), caller(cfg, r))
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": results, "auditEventId": auditID, "label": "No model call"})
	})
	mux.HandleFunc(CaseRoute, func(w http.ResponseWriter, r *http.Request) {
		if !allowed(cfg, w, r) {
			return
		}
		var body struct {
			Scenario, BoundObject, Value, ExpectedOutcome string
		}
		if err := decode(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "request body must be a JSON object")
			return
		}
		added, err := cfg.API.AddCase(body.Scenario, body.BoundObject, body.Value, body.ExpectedOutcome)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, added)
	})
	mux.HandleFunc(CourseRoute, func(w http.ResponseWriter, r *http.Request) {
		if !allowed(cfg, w, r) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"label": "No model call", "options": cfg.API.Courses()})
	})
	mux.HandleFunc(SelectRoute, func(w http.ResponseWriter, r *http.Request) {
		if !allowed(cfg, w, r) {
			return
		}
		var body struct {
			Value, Environment, InstallationID, ProductID string
		}
		if err := decode(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "request body must be a JSON object")
			return
		}
		id, status, err := cfg.API.Select(r.Context(), caller(cfg, r), body.Value, body.Environment, body.InstallationID, body.ProductID)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"changeRequestId": id, "status": status, "label": "No model call"})
	})
	mux.HandleFunc(EventsRoute, func(w http.ResponseWriter, r *http.Request) {
		if !allowed(cfg, w, r) {
			return
		}
		observed := []map[string]string{}
		if cfg.Events != nil {
			observed = cfg.Events.Observed(r.Context())
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"label":    "Example data",
			"note":     "Automation rules on this screen are not executed.",
			"observed": observed,
			"rules": []map[string]any{{
				"name": "When a packaging run is blocked, open a proposal", "executes": false, "label": "Example data",
			}},
		})
	})
}

func allowed(cfg Config, w http.ResponseWriter, r *http.Request) bool {
	if cfg.Authorize == nil || cfg.API == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	if err := cfg.Authorize(r); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

func caller(cfg Config, r *http.Request) string {
	if cfg.Caller == nil {
		return "operator"
	}
	if name := strings.TrimSpace(cfg.Caller(r)); name != "" {
		return name
	}
	return "operator"
}

func decode(r *http.Request, dest any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	decoder.DisallowUnknownFields()
	return decoder.Decode(dest)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
