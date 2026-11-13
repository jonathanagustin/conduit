package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

const (
	CancelRoute = "POST /v1/packaging-runs/{runID}/cancel"
	RerunRoute  = "POST /v1/packaging-runs/{runID}/rerun"

	actionCancel = "packaging-run.cancel"
	actionRerun  = "packaging-run.rerun"
	verdictAllow = "ALLOW"
	verdictDeny  = "DENY"
)

// Controls cancels a packaging run or starts a new one. The Hub calls it only
// after authorization and the audit record. A nil controls value means no
// worker is configured. Cancel does not cancel a Plan.
type Controls interface {
	Reader
	Configured() bool
	Cancel(ctx context.Context, runID string) error
	Start(ctx context.Context, id domain.RunIdentity) (domain.RunStatus, error)
}

// Operator is the authenticated caller a governance decision allowed.
type Operator struct {
	Username string
}

// Permit is the governance decision for a write. It refuses a caller who
// may not operate the environment. A nil function refuses everyone.
type Permit func(*http.Request, string) (Operator, error)

// DecisionRecord is one audited cancel or rerun. The binding is the run
// identity the operator named, not a Plan approval.
type DecisionRecord struct {
	Subject          string
	Action           string
	RunID            string
	Verdict          string
	Reason           string
	DecisionID       string
	SourceDigest     string
	ProposalRevision string
	TargetPlanRef    string
	PlanID           string
	NewRunID         string
	At               time.Time
}

// DecisionLog remembers decisions so a replay, a stale binding, or another
// operator's copy of the same decision id fails closed.
type DecisionLog interface {
	Lookup(ctx context.Context, runID, decisionID string) (DecisionRecord, bool, error)
	Append(ctx context.Context, record DecisionRecord) error
}

type controlBody struct {
	DecisionID       string `json:"decisionId"`
	SourceDigest     string `json:"sourceDigest"`
	ProposalRevision string `json:"proposalRevision"`
	TargetPlanRef    string `json:"targetPlanRef"`
	TemplateVersion  string `json:"templateVersion"`
	ToolVersions     string `json:"toolVersions"`
}

type controlResult struct {
	domain.RunStatus
	PlanCancelled   bool   `json:"planCancelled"`
	PlanCancelRoute string `json:"planCancelRoute,omitempty"`
	PriorRunID      string `json:"priorRunId,omitempty"`
}

func mountControls(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(CancelRoute, func(writer http.ResponseWriter, request *http.Request) {
		serveCancel(cfg, writer, request)
	})
	mux.HandleFunc(RerunRoute, func(writer http.ResponseWriter, request *http.Request) {
		serveRerun(cfg, writer, request)
	})
}

func serveCancel(cfg Config, writer http.ResponseWriter, request *http.Request) {
	body, operator, ok := admitControl(cfg, writer, request, false)
	if !ok {
		return
	}
	runID := request.PathValue("runID")
	if seen(cfg, writer, request, operator, actionCancel, runID, body) {
		return
	}
	if refuseUnconfigured(cfg, writer, request, operator, actionCancel, runID, body, "") {
		return
	}
	status, ok := loadRun(cfg, writer, request, runID)
	if !ok {
		return
	}
	if !bindingMatches(status, body) {
		refuse(cfg, writer, request, operator, actionCancel, runID, body, status.PlanID, "",
			http.StatusConflict, "decision does not match the current run")
		return
	}
	if status.PlanID != "" || status.Phase == domain.PhaseComplete {
		refuse(cfg, writer, request, operator, actionCancel, runID, body, status.PlanID, "",
			http.StatusConflict, "packaging run already handed off a plan")
		return
	}
	if status.Phase == domain.PhaseBlocked || status.Outcome == string(domain.OutcomeCancelled) {
		refuse(cfg, writer, request, operator, actionCancel, runID, body, status.PlanID, "",
			http.StatusConflict, "packaging run is already stopped")
		return
	}
	if !record(cfg, writer, request, operator, actionCancel, runID, body, status.PlanID, "", verdictAllow, "packaging run cancel authorized") {
		return
	}
	if err := cfg.Controls.Cancel(request.Context(), runID); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(writer, http.StatusNotFound, ErrNotFound.Error())
			return
		}
		writeError(writer, http.StatusBadGateway, "packaging run cancel is unavailable")
		return
	}
	updated, ok := loadRun(cfg, writer, request, runID)
	if !ok {
		return
	}
	writeJSON(writer, http.StatusOK, controlResult{
		RunStatus: redactRun(updated), PlanCancelled: false, PlanCancelRoute: planRoute(status.PlanID),
	})
}

func serveRerun(cfg Config, writer http.ResponseWriter, request *http.Request) {
	body, operator, ok := admitControl(cfg, writer, request, true)
	if !ok {
		return
	}
	runID := request.PathValue("runID")
	if seen(cfg, writer, request, operator, actionRerun, runID, body) {
		return
	}
	newID := newRunID(body.DecisionID, runID)
	if refuseUnconfigured(cfg, writer, request, operator, actionRerun, runID, body, newID) {
		return
	}
	status, ok := loadRun(cfg, writer, request, runID)
	if !ok {
		return
	}
	if !bindingMatches(status, body) {
		refuse(cfg, writer, request, operator, actionRerun, runID, body, status.PlanID, newID,
			http.StatusConflict, "decision does not match the current run")
		return
	}
	identity := domain.RunIdentity{
		RunID: newID, SourceDigest: body.SourceDigest,
		TemplateVersion: body.TemplateVersion, ToolVersions: body.ToolVersions,
		TargetPlanRef: body.TargetPlanRef,
	}
	if err := identity.Validate(); err != nil {
		refuse(cfg, writer, request, operator, actionRerun, runID, body, status.PlanID, "",
			http.StatusConflict, "rerun binding is not a packaging run identity")
		return
	}
	if !record(cfg, writer, request, operator, actionRerun, runID, body, status.PlanID, newID, verdictAllow, "packaging run rerun authorized") {
		return
	}
	started, err := cfg.Controls.Start(request.Context(), identity)
	if err != nil {
		writeError(writer, http.StatusBadGateway, "packaging run rerun is unavailable")
		return
	}
	if started.RunID == "" {
		started.RunID = newID
	}
	started.ProposalRevision = ""
	started.SourceDigest = identity.SourceDigest
	started.TargetPlanRef = identity.TargetPlanRef
	started.Phase = domain.PhaseAssess
	result := controlResult{RunStatus: redactRun(started), PlanCancelled: false, PriorRunID: runID}
	writeJSON(writer, http.StatusCreated, result)
}

func admitControl(cfg Config, writer http.ResponseWriter, request *http.Request, rerun bool) (controlBody, Operator, bool) {
	if !authorized(cfg, writer, request) {
		return controlBody{}, Operator{}, false
	}
	body, err := decodeControl(request)
	if writeCoded(writer, err) {
		return controlBody{}, Operator{}, false
	}
	if cfg.Permit == nil {
		writeError(writer, http.StatusUnauthorized, "unauthorized")
		return controlBody{}, Operator{}, false
	}
	operator, err := cfg.Permit(request, domain.EnvironmentOf(body.TargetPlanRef))
	if err != nil {
		status := http.StatusForbidden
		var coded StatusError
		if errors.As(err, &coded) && coded.Status != 0 {
			status = coded.Status
		}
		writeError(writer, status, "forbidden")
		return controlBody{}, Operator{}, false
	}
	if !bindingComplete(body, rerun) || domain.RejectSensitive(body.DecisionID) != nil || domain.RejectSensitive(body.SourceDigest) != nil {
		refuse(cfg, writer, request, operator, actionOf(rerun), request.PathValue("runID"), body, "", "",
			http.StatusConflict, "decision binding is incomplete")
		return controlBody{}, Operator{}, false
	}
	return body, operator, true
}

func seen(cfg Config, writer http.ResponseWriter, request *http.Request, operator Operator, action, runID string, body controlBody) bool {
	if cfg.Decisions == nil {
		refuse(cfg, writer, request, operator, action, runID, body, "", "", http.StatusConflict, "packaging decision log is not configured")
		return true
	}
	prior, found, err := cfg.Decisions.Lookup(request.Context(), runID, body.DecisionID)
	if err != nil {
		writeError(writer, http.StatusBadGateway, "packaging decision log is unavailable")
		return true
	}
	if !found {
		return false
	}
	if prior.Subject != operator.Username {
		if !record(cfg, writer, request, operator, action, runID, body, "", "", verdictDeny, "decision belongs to another operator") {
			return true
		}
		writeError(writer, http.StatusForbidden, "forbidden")
		return true
	}
	writeError(writer, http.StatusConflict, "duplicate decision")
	return true
}

func refuseUnconfigured(cfg Config, writer http.ResponseWriter, request *http.Request, operator Operator, action, runID string, body controlBody, newID string) bool {
	if cfg.Controls != nil && cfg.Controls.Configured() {
		return false
	}
	refuse(cfg, writer, request, operator, action, runID, body, "", newID, http.StatusConflict, "packaging worker is not configured")
	return true
}

func loadRun(cfg Config, writer http.ResponseWriter, request *http.Request, runID string) (domain.RunStatus, bool) {
	if cfg.Reader == nil {
		writeError(writer, http.StatusConflict, "packaging worker is not configured")
		return domain.RunStatus{}, false
	}
	status, err := cfg.Reader.Read(request.Context(), runID)
	if errors.Is(err, ErrNotFound) {
		writeError(writer, http.StatusNotFound, ErrNotFound.Error())
		return domain.RunStatus{}, false
	}
	if err != nil {
		writeError(writer, http.StatusBadGateway, "packaging run status is unavailable")
		return domain.RunStatus{}, false
	}
	return status, true
}

func bindingMatches(status domain.RunStatus, body controlBody) bool {
	return status.SourceDigest == body.SourceDigest &&
		status.ProposalRevision == body.ProposalRevision &&
		status.TargetPlanRef == body.TargetPlanRef
}

func bindingComplete(body controlBody, rerun bool) bool {
	if strings.TrimSpace(body.DecisionID) == "" || strings.Contains(body.DecisionID, "/") || len(body.DecisionID) > domain.MaxIdentityBytes {
		return false
	}
	if body.SourceDigest == "" || body.TargetPlanRef == "" || domain.EnvironmentOf(body.TargetPlanRef) == "" {
		return false
	}
	if rerun && (body.TemplateVersion == "" || body.ToolVersions == "") {
		return false
	}
	return true
}

func refuse(cfg Config, writer http.ResponseWriter, request *http.Request, operator Operator, action, runID string, body controlBody, planID, newID string, status int, reason string) {
	if !record(cfg, writer, request, operator, action, runID, body, planID, newID, verdictDeny, reason) {
		return
	}
	writeConflict(writer, status, reason, planID)
}

func record(cfg Config, writer http.ResponseWriter, request *http.Request, operator Operator, action, runID string, body controlBody, planID, newID, verdict, reason string) bool {
	if cfg.Decisions == nil {
		writeError(writer, http.StatusConflict, "packaging decision log is not configured")
		return false
	}
	err := cfg.Decisions.Append(request.Context(), DecisionRecord{
		Subject: operator.Username, Action: action, RunID: runID, Verdict: verdict, Reason: reason,
		DecisionID: body.DecisionID, SourceDigest: body.SourceDigest, ProposalRevision: body.ProposalRevision,
		TargetPlanRef: body.TargetPlanRef, PlanID: planID, NewRunID: newID, At: time.Now().UTC(),
	})
	if err != nil {
		writeError(writer, http.StatusBadGateway, "packaging decision log is unavailable")
		return false
	}
	return true
}

func decodeControl(request *http.Request) (controlBody, error) {
	var body controlBody
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<16))
	if err := decoder.Decode(&body); err != nil {
		return controlBody{}, StatusError{Status: http.StatusBadRequest, Message: "request body must be a JSON object"}
	}
	return body, nil
}

func actionOf(rerun bool) string {
	if rerun {
		return actionRerun
	}
	return actionCancel
}

func newRunID(decisionID, priorRunID string) string {
	sum := sha256.Sum256([]byte(decisionID + "\x00" + priorRunID))
	return "run_" + hex.EncodeToString(sum[:8])
}

func planRoute(planID string) string {
	if planID == "" {
		return ""
	}
	return "/v1/plans/" + planID + "/cancel"
}

func writeConflict(writer http.ResponseWriter, status int, reason, planID string) {
	payload := map[string]any{"error": reason, "status": itoa(status), "planCancelled": false}
	if route := planRoute(planID); route != "" {
		payload["planId"] = planID
		payload["planCancelRoute"] = route
	}
	writeJSON(writer, status, payload)
}
