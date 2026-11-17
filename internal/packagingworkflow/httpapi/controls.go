package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	app "github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

const (
	CancelRoute  = "POST /v1/packaging-runs/{runID}/cancel"
	RerunRoute   = "POST /v1/packaging-runs/{runID}/rerun"
	actionCancel = app.ControlCancel
	actionRerun  = app.ControlRerun
	verdictAllow = "ALLOW"
	verdictDeny  = "DENY"
)

// Controls retains the HTTP reader surface for existing composition callers while the
// shared command service needs only a single-run read.
type Controls interface {
	app.Controls
	Reader
}
type DecisionRecord = app.DecisionRecord
type DecisionLog = app.DecisionLog
type Operator struct{ Username string }
type Permit func(*http.Request, string) (Operator, error)
type controlBody = app.ControlBinding

type controlResult struct {
	domain.RunStatus
	PlanCancelled   bool   `json:"planCancelled"`
	PlanCancelRoute string `json:"planCancelRoute,omitempty"`
	PriorRunID      string `json:"priorRunId,omitempty"`
	CommandAccepted bool   `json:"commandAccepted"`
	CommandOutcome  string `json:"commandOutcome"`
}

func mountControls(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(CancelRoute, func(w http.ResponseWriter, r *http.Request) { serveControl(cfg, w, r, actionCancel) })
	mux.HandleFunc(RerunRoute, func(w http.ResponseWriter, r *http.Request) { serveControl(cfg, w, r, actionRerun) })
}

func serveControl(cfg Config, w http.ResponseWriter, r *http.Request, action string) {
	if !authorized(cfg, w, r) {
		return
	}
	body, err := decodeControl(r)
	if writeCoded(w, err) {
		return
	}
	var permitErr error
	service := app.ControlService{Reader: cfg.Reader, Worker: cfg.Controls, Decisions: cfg.Decisions,
		Permit: func(_ context.Context, environment string) (string, error) {
			if cfg.Permit == nil {
				permitErr = StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
				return "", permitErr
			}
			operator, err := cfg.Permit(r, environment)
			permitErr = err
			return operator.Username, err
		},
	}
	result, err := service.Execute(r.Context(), r.PathValue("runID"), action, body, false)
	if err != nil {
		if permitErr != nil {
			status := http.StatusForbidden
			var coded StatusError
			if errors.As(permitErr, &coded) && coded.Status != 0 {
				status = coded.Status
			}
			writeError(w, status, "forbidden")
			return
		}
		var failure app.ControlFailure
		if !errors.As(err, &failure) {
			writeError(w, http.StatusBadGateway, "packaging control is unavailable")
			return
		}
		status := http.StatusConflict
		switch failure.Kind {
		case "forbidden":
			status = http.StatusForbidden
		case "missing":
			status = http.StatusNotFound
		case "unavailable":
			status = http.StatusBadGateway
		}
		if status == http.StatusConflict {
			writeConflict(w, status, failure.Reason, failure.PlanID)
		} else {
			writeError(w, status, failure.Reason)
		}
		return
	}
	status := http.StatusOK
	if action == actionRerun {
		status = http.StatusCreated
	}
	writeJSON(w, status, controlResult{RunStatus: redactRun(result.RunStatus), PlanCancelled: false, PlanCancelRoute: planRoute(result.PlanID), PriorRunID: result.PriorRunID, CommandAccepted: result.Accepted, CommandOutcome: result.Outcome})
}

func decodeControl(request *http.Request) (controlBody, error) {
	var body controlBody
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<16))
	if err := decoder.Decode(&body); err != nil {
		return controlBody{}, StatusError{Status: http.StatusBadRequest, Message: "request body must be a JSON object"}
	}
	return body, nil
}
func planRoute(planID string) string {
	if planID == "" {
		return ""
	}
	return "/v1/plans/" + planID + "/cancel"
}
func writeConflict(w http.ResponseWriter, status int, reason, planID string) {
	payload := map[string]any{"error": reason, "status": itoa(status), "planCancelled": false}
	if route := planRoute(planID); route != "" {
		payload["planId"], payload["planCancelRoute"] = planID, route
	}
	writeJSON(w, status, payload)
}
