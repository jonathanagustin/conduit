// Package httpapi serves the developer workspace routes.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/jonathanagustin/conduit/internal/devworkspace/application"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

const (
	routeList     = "GET /v1/dev-workspaces"
	routeGet      = "GET /v1/dev-workspaces/{workspaceID}"
	routeCreate   = "POST /v1/dev-workspaces"
	routeStop     = "POST /v1/dev-workspaces/{workspaceID}/stop"
	routeDelete   = "DELETE /v1/dev-workspaces/{workspaceID}"
	routeSubmit   = "POST /v1/dev-workspaces/{workspaceID}/submit"
	routeTree     = "GET /v1/dev-workspaces/{workspaceID}/tree"
	routeFile     = "GET /v1/dev-workspaces/{workspaceID}/file"
	routeDiff     = "GET /v1/dev-workspaces/{workspaceID}/diff"
	routeHistory  = "GET /v1/dev-workspaces/{workspaceID}/history"
	routeRun      = "POST /v1/dev-workspaces/{workspaceID}/runtime"
	routeRunStop  = "POST /v1/dev-workspaces/{workspaceID}/runtime/stop"
	routeRunGet   = "GET /v1/dev-workspaces/{workspaceID}/runtime"
	routeLogs     = "GET /v1/dev-workspaces/{workspaceID}/logs"
	routeLogClear = "POST /v1/dev-workspaces/{workspaceID}/logs/clear"
	routeFork     = "POST /v1/dev-workspaces/{workspaceID}/fork"
	routeUpgrade  = "POST /v1/dev-workspaces/{workspaceID}/upgrade"
	routeDownload = "GET /v1/dev-workspaces/{workspaceID}/download"
)

// StatusError is an authorization failure with an HTTP status.
type StatusError struct {
	Status  int
	Message string
}

func (err StatusError) Error() string {
	if err.Message == "" {
		return "unauthorized"
	}
	return err.Message
}

// Config is the route's dependencies. A nil Authorize refuses everyone.
type Config struct {
	Service   *application.Service
	Authorize func(*http.Request) error
	Caller    func(*http.Request) (domain.Caller, error)
	Visible   func(*http.Request, string) bool
	Permit    func(*http.Request, string) error
}

// Register mounts the workspace routes.
func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(routeList, func(w http.ResponseWriter, r *http.Request) { list(cfg, w, r) })
	mux.HandleFunc(routeGet, func(w http.ResponseWriter, r *http.Request) { get(cfg, w, r) })
	mux.HandleFunc(routeCreate, func(w http.ResponseWriter, r *http.Request) { create(cfg, w, r) })
	mux.HandleFunc(routeStop, func(w http.ResponseWriter, r *http.Request) { stop(cfg, w, r) })
	mux.HandleFunc(routeDelete, func(w http.ResponseWriter, r *http.Request) { remove(cfg, w, r) })
	mux.HandleFunc(routeSubmit, func(w http.ResponseWriter, r *http.Request) { submit(cfg, w, r) })
	mux.HandleFunc(routeTree, func(w http.ResponseWriter, r *http.Request) { tree(cfg, w, r) })
	mux.HandleFunc(routeFile, func(w http.ResponseWriter, r *http.Request) { file(cfg, w, r) })
	mux.HandleFunc(routeDiff, func(w http.ResponseWriter, r *http.Request) { diff(cfg, w, r) })
	mux.HandleFunc(routeHistory, func(w http.ResponseWriter, r *http.Request) { history(cfg, w, r) })
	mux.HandleFunc(routeRun, func(w http.ResponseWriter, r *http.Request) { startRuntime(cfg, w, r) })
	mux.HandleFunc(routeRunStop, func(w http.ResponseWriter, r *http.Request) { stop(cfg, w, r) })
	mux.HandleFunc(routeRunGet, func(w http.ResponseWriter, r *http.Request) { runtimeStatus(cfg, w, r) })
	mux.HandleFunc(routeLogs, func(w http.ResponseWriter, r *http.Request) { logs(cfg, w, r) })
	mux.HandleFunc(routeLogClear, func(w http.ResponseWriter, r *http.Request) { clearLogs(cfg, w, r) })
	mux.HandleFunc(routeFork, func(w http.ResponseWriter, r *http.Request) { fork(cfg, w, r) })
	mux.HandleFunc(routeUpgrade, func(w http.ResponseWriter, r *http.Request) { upgrade(cfg, w, r) })
	mux.HandleFunc(routeDownload, func(w http.ResponseWriter, r *http.Request) { download(cfg, w, r) })
}

func list(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok {
		return
	}
	rows, err := cfg.Service.List(r.Context(), caller, func(environment string) bool {
		return cfg.Visible != nil && cfg.Visible(r, environment)
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	items := make([]workspaceJSON, 0, len(rows))
	for _, row := range rows {
		items = append(items, workspaceJSONFrom(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "valueTier": domain.ValueTier})
}

func get(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok {
		return
	}
	row, err := cfg.Service.Get(r.Context(), caller, r.PathValue("workspaceID"), visible(cfg, r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workspaceJSONFrom(row))
}

func create(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok || !permit(cfg, w, r, "") {
		return
	}
	var body struct {
		ProfileID     string `json:"profileId"`
		EnvironmentID string `json:"environmentId"`
		Title         string `json:"title"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "request body is not valid JSON")
		return
	}
	if !permit(cfg, w, r, body.EnvironmentID) {
		return
	}
	row, err := cfg.Service.Create(r.Context(), caller, body.ProfileID, body.EnvironmentID, body.Title)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, workspaceJSONFrom(row))
}

func stop(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok || !permit(cfg, w, r, "") {
		return
	}
	runtime, err := cfg.Service.StopRuntime(r.Context(), caller, r.PathValue("workspaceID"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runtime)
}

func remove(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok || !permit(cfg, w, r, "") {
		return
	}
	if err := cfg.Service.Delete(r.Context(), caller, r.PathValue("workspaceID"), r.URL.Query().Get("confirm")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func submit(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok || !permit(cfg, w, r, "") {
		return
	}
	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Approve     bool   `json:"approve"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "request body is not valid JSON")
		return
	}
	receipt, err := cfg.Service.Submit(r.Context(), caller, r.PathValue("workspaceID"), body.Title, body.Description, body.Approve)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"changeRequestId": receipt.ChangeRequestID, "status": receipt.Status, "valueTier": domain.ValueTier,
	})
}

func tree(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := cfg.Service.Tree(r.Context(), caller, r.PathValue("workspaceID"), r.URL.Query().Get("prefix"), r.URL.Query().Get("cursor"), limit, visible(cfg, r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func file(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok {
		return
	}
	view, err := cfg.Service.File(r.Context(), caller, r.PathValue("workspaceID"), r.URL.Query().Get("path"), r.URL.Query().Get("revision"), visible(cfg, r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func diff(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok {
		return
	}
	result, err := cfg.Service.Diff(r.Context(), caller, r.PathValue("workspaceID"), r.URL.Query().Get("revision"), visible(cfg, r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func startRuntime(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok || !permit(cfg, w, r, "") {
		return
	}
	runtime, err := cfg.Service.StartRuntime(r.Context(), caller, r.PathValue("workspaceID"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runtime)
}

func runtimeStatus(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok {
		return
	}
	runtime, err := cfg.Service.RuntimeStatus(r.Context(), caller, r.PathValue("workspaceID"), visible(cfg, r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runtime)
}

func logs(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok {
		return
	}
	id := r.PathValue("workspaceID")
	row, err := cfg.Service.Get(r.Context(), caller, id, func(string) bool { return true })
	inspect := err == nil && cfg.Visible != nil && cfg.Visible(r, row.EnvironmentID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		writeErr(w, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := cfg.Service.Logs(r.Context(), caller, id, r.URL.Query().Get("cursor"), limit, inspect || (err == nil && caller.Username == row.Owner))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func clearLogs(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok || !permit(cfg, w, r, "") {
		return
	}
	if err := cfg.Service.ClearLogs(r.Context(), caller, r.PathValue("workspaceID")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func fork(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok || !permit(cfg, w, r, "") {
		return
	}
	var body struct {
		Title string `json:"title"`
	}
	if r.ContentLength != 0 {
		if err := decode(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "request body is not valid JSON")
			return
		}
	}
	row, err := cfg.Service.Fork(r.Context(), caller, r.PathValue("workspaceID"), body.Title)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, workspaceJSONFrom(row))
}

func upgrade(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok || !permit(cfg, w, r, "") {
		return
	}
	var body struct {
		SourceDigest    string `json:"sourceDigest"`
		TemplateVersion string `json:"templateVersion"`
		ToolVersions    string `json:"toolVersions"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "request body is not valid JSON")
		return
	}
	row, err := cfg.Service.Upgrade(r.Context(), caller, r.PathValue("workspaceID"), body.SourceDigest, body.TemplateVersion, body.ToolVersions)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workspaceJSONFrom(row))
}

func download(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok {
		return
	}
	body, err := cfg.Service.Download(r.Context(), caller, r.PathValue("workspaceID"), visible(cfg, r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func authenticate(cfg Config, w http.ResponseWriter, r *http.Request) (domain.Caller, bool) {
	if cfg.Service == nil || cfg.Authorize == nil || cfg.Caller == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return domain.Caller{}, false
	}
	if err := cfg.Authorize(r); err != nil {
		writeErr(w, err)
		return domain.Caller{}, false
	}
	caller, err := cfg.Caller(r)
	if err != nil || strings.TrimSpace(caller.Username) == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return domain.Caller{}, false
	}
	return caller, true
}

func permit(cfg Config, w http.ResponseWriter, r *http.Request, environment string) bool {
	if cfg.Permit == nil {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	if err := cfg.Permit(r, environment); err != nil {
		writeErr(w, err)
		return false
	}
	return true
}

func visible(cfg Config, r *http.Request) func(string) bool {
	return func(environment string) bool {
		return cfg.Visible != nil && cfg.Visible(r, environment)
	}
}

type workspaceJSON struct {
	WorkspaceID       string `json:"workspaceId"`
	ProfileID         string `json:"profileId"`
	EnvironmentID     string `json:"environmentId"`
	Owner             string `json:"owner"`
	Title             string `json:"title"`
	Status            string `json:"status"`
	UpstreamRepo      string `json:"upstreamRepo,omitempty"`
	RepoURL           string `json:"repoUrl,omitempty"`
	Branch            string `json:"branch,omitempty"`
	BaseCommit        string `json:"baseCommit,omitempty"`
	HeadCommit        string `json:"headCommit,omitempty"`
	SourceDigest      string `json:"sourceDigest,omitempty"`
	ProposalRevision  string `json:"proposalRevision,omitempty"`
	VolumePreserved   bool   `json:"volumePreserved,omitempty"`
	PreservedUntil    string `json:"preservedUntil,omitempty"`
	ArtifactDigest    string `json:"artifactDigest,omitempty"`
	ParentID          string `json:"parentId,omitempty"`
	TemplateVersion   string `json:"templateVersion,omitempty"`
	ToolVersions      string `json:"toolVersions,omitempty"`
	ValidationOutcome string `json:"validationOutcome,omitempty"`
	ApprovalID        string `json:"approvalId,omitempty"`
	SignatureStatus   string `json:"signatureStatus"`
	SignatureValid    bool   `json:"signatureValid"`
	ValueTier         string `json:"valueTier"`
}

func workspaceJSONFrom(row domain.Workspace) workspaceJSON {
	preservedUntil := ""
	if !row.PreservedUntil.IsZero() {
		preservedUntil = row.PreservedUntil.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return workspaceJSON{
		WorkspaceID: row.ID, ProfileID: row.ProfileID, EnvironmentID: row.EnvironmentID,
		Owner: row.Owner, Title: row.Title, Status: row.Status,
		UpstreamRepo: row.UpstreamRepo, RepoURL: row.RepoURL,
		Branch: row.Branch, BaseCommit: row.BaseCommit, HeadCommit: row.HeadCommit,
		SourceDigest: row.SourceDigest, ProposalRevision: row.ProposalRevision,
		VolumePreserved: row.VolumePreserved, PreservedUntil: preservedUntil,
		ArtifactDigest: row.ArtifactDigest, ParentID: row.ParentID,
		TemplateVersion: row.TemplateVersion, ToolVersions: row.ToolVersions,
		ValidationOutcome: row.ValidationOutcome, ApprovalID: row.ApprovalID,
		SignatureStatus: row.SignatureStatus, SignatureValid: domain.SignatureValid(row.SignatureStatus),
		ValueTier: domain.ValueTier,
	}
}

func decode(r *http.Request, dest any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return err
	}
	return nil
}

func writeErr(w http.ResponseWriter, err error) {
	var status StatusError
	if errors.As(err, &status) {
		writeError(w, status.Status, status.Message)
		return
	}
	switch {
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrMissing):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, domain.ErrForbidden), errors.Is(err, domain.ErrSelfApproval):
		writeError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, domain.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "unauthorized")
	case errors.Is(err, domain.ErrStale):
		writeError(w, http.StatusConflict, "stale revision")
	case errors.Is(err, domain.ErrConflict):
		writeError(w, http.StatusConflict, "workspace is running")
	case errors.Is(err, domain.ErrDisabled):
		writeError(w, http.StatusServiceUnavailable, "workspace support is not configured")
	case errors.Is(err, domain.ErrPath), errors.Is(err, domain.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusBadGateway, "workspace request failed")
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// ApplyAction is the ontology executor for workspace_create, workspace_stop,
// workspace_delete, and workspace_submit.
func ApplyAction(ctx context.Context, service *application.Service, caller domain.Caller, action string, parameters map[string]string) error {
	switch action {
	case "workspace_create":
		_, err := service.Create(ctx, caller, parameters["profileId"], parameters["environmentId"], parameters["title"])
		return err
	case "workspace_stop":
		_, err := service.StopRuntime(ctx, caller, parameters["workspaceId"])
		return err
	case "workspace_delete":
		return service.Delete(ctx, caller, parameters["workspaceId"], parameters["confirmation"])
	case "workspace_submit":
		_, err := service.Submit(ctx, caller, parameters["workspaceId"], parameters["title"], parameters["description"], parameters["approve"] == "true")
		return err
	default:
		return domain.ErrForbidden
	}
}

func history(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok {
		return
	}
	result, err := cfg.Service.History(r.Context(), caller, r.PathValue("workspaceID"), r.URL.Query().Get("revision"), r.URL.Query().Get("head"), visible(cfg, r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
