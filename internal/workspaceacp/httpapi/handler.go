// Package httpapi serves the Hub-governed workspace ACP JSON-RPC, permission
// settlement, session listing, and mirrored console AG-UI stream routes.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/api/agui"
	"github.com/jonathanagustin/conduit/internal/workspaceacp/application"
	"github.com/jonathanagustin/conduit/internal/workspaceacp/domain"
)

// StatusError carries an HTTP status code from an authentication or standing check.
type StatusError struct {
	Status  int
	Message string
}

func (e StatusError) Error() string {
	if e.Message == "" {
		return "unauthorized"
	}
	return e.Message
}

// Config supplies the HTTP adapter's authentication, governance, and service callbacks.
type Config struct {
	Service    *application.Service
	Authorize  func(*http.Request) error
	Caller     func(*http.Request) (domain.Caller, error)
	Visible    func(*http.Request, string) bool
	Permit     func(*http.Request, string) error
	PermitRead func(*http.Request, string) error
}

// Register mounts the governed workspace ACP and mirrored AG-UI console routes.
func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc("POST /v1/dev-workspaces/{workspaceID}/acp", func(w http.ResponseWriter, r *http.Request) {
		handleACP(cfg, w, r)
	})
	mux.HandleFunc("POST /v1/dev-workspaces/{workspaceID}/acp/permissions/{requestID}", func(w http.ResponseWriter, r *http.Request) {
		handlePermission(cfg, w, r)
	})
	mux.HandleFunc("GET /v1/dev-workspaces/{workspaceID}/acp/sessions", func(w http.ResponseWriter, r *http.Request) {
		handleListSessions(cfg, w, r)
	})
	mux.HandleFunc("POST /v1/dev-workspaces/{workspaceID}/agent-runs", func(w http.ResponseWriter, r *http.Request) {
		handleWorkspaceAgentRuns(cfg, w, r)
	})
}

func handleACP(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok || !permitWrite(cfg, w, r, "") {
		return
	}
	if cfg.Service == nil {
		writeRPCError(w, http.StatusServiceUnavailable, nil, -32001, domain.ErrDisabled.Error())
		return
	}

	var envelope struct {
		JSONRPC string         `json:"jsonrpc"`
		ID      any            `json:"id"`
		Method  string         `json:"method"`
		Params  map[string]any `json:"params"`
	}
	if err := decodeJSON(w, r, &envelope); err != nil {
		writeRPCError(w, http.StatusBadRequest, nil, -32700, "request body must be valid JSON-RPC 2.0")
		return
	}
	if envelope.JSONRPC != "2.0" || strings.TrimSpace(envelope.Method) == "" {
		writeRPCError(w, http.StatusBadRequest, envelope.ID, -32600, "invalid ACP JSON-RPC 2.0 envelope")
		return
	}
	if envelope.Params == nil {
		envelope.Params = map[string]any{}
	}

	workspaceID := r.PathValue("workspaceID")
	branch := requestedBranch(r, envelope.Params)

	switch envelope.Method {
	case "initialize":
		proto, ok := intParam(envelope.Params["protocolVersion"])
		if !ok {
			writeRPCError(w, http.StatusBadRequest, envelope.ID, -32602, "protocolVersion integer is required")
			return
		}
		res, err := cfg.Service.Initialize(r.Context(), caller, workspaceID, branch, proto)
		if err != nil {
			writeDomainRPCError(w, envelope.ID, err)
			return
		}
		writeRPCResult(w, envelope.ID, res)

	case "session/new":
		threadID := metaString(envelope.Params, "threadId")
		sess, err := cfg.Service.NewSession(r.Context(), caller, workspaceID, branch, threadID)
		if err != nil {
			writeDomainRPCError(w, envelope.ID, err)
			return
		}
		writeRPCResult(w, envelope.ID, map[string]any{
			"sessionId":     sess.SessionID,
			"configOptions": sess.ConfigOptions,
			"_meta": map[string]any{
				"threadId":      sess.ThreadID,
				"workspaceId":   sess.WorkspaceID,
				"environmentId": sess.EnvironmentID,
				"owner":         sess.Owner,
				"branch":        sess.Branch,
				"valueTier":     sess.ValueTier,
			},
		})

	case "session/set_config_option":
		sessionID, _ := envelope.Params["sessionId"].(string)
		configID, _ := envelope.Params["configId"].(string)
		value, hasVal := envelope.Params["value"]
		if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(configID) == "" || !hasVal {
			writeRPCError(w, http.StatusBadRequest, envelope.ID, -32602, "sessionId, configId, and value are required")
			return
		}
		updated, err := cfg.Service.SetConfigOption(r.Context(), caller, workspaceID, branch, sessionID, configID, value)
		if err != nil {
			writeDomainRPCError(w, envelope.ID, err)
			return
		}
		writeRPCResult(w, envelope.ID, map[string]any{"configOptions": updated})

	case "session/cancel":
		sessionID, _ := envelope.Params["sessionId"].(string)
		if strings.TrimSpace(sessionID) == "" {
			writeRPCError(w, http.StatusBadRequest, envelope.ID, -32602, "sessionId is required")
			return
		}
		if err := cfg.Service.CancelSession(r.Context(), caller, workspaceID, branch, sessionID); err != nil {
			writeDomainRPCError(w, envelope.ID, err)
			return
		}
		writeRPCResult(w, envelope.ID, map[string]any{})

	case "session/close", "session/delete":
		sessionID, _ := envelope.Params["sessionId"].(string)
		if strings.TrimSpace(sessionID) == "" {
			writeRPCError(w, http.StatusBadRequest, envelope.ID, -32602, "sessionId is required")
			return
		}
		if err := cfg.Service.CloseSession(r.Context(), caller, workspaceID, branch, sessionID); err != nil {
			writeDomainRPCError(w, envelope.ID, err)
			return
		}
		writeRPCResult(w, envelope.ID, map[string]any{})

	case "session/prompt":
		sessionID, _ := envelope.Params["sessionId"].(string)
		promptBlocks := decodePromptBlocks(envelope.Params["prompt"])
		if strings.TrimSpace(sessionID) == "" || len(promptBlocks) == 0 {
			writeRPCError(w, http.StatusBadRequest, envelope.ID, -32602, "sessionId and non-empty prompt are required")
			return
		}
		runID := metaString(envelope.Params, "runId")

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
		defer cancel()

		var writeMu sync.Mutex
		started := false
		ctrl := http.NewResponseController(w)
		_ = ctrl.SetWriteDeadline(time.Now().Add(30 * time.Minute))

		err := cfg.Service.Prompt(ctx, caller, workspaceID, branch, sessionID, runID, envelope.ID, promptBlocks, func(ev map[string]any) error {
			raw, err := json.Marshal(ev)
			if err != nil {
				return err
			}
			writeMu.Lock()
			defer writeMu.Unlock()
			if !started {
				w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
				w.Header().Set("Cache-Control", "no-store")
				w.Header().Set("X-Content-Type-Options", "nosniff")
				w.Header().Set("X-Accel-Buffering", "no")
				w.WriteHeader(http.StatusOK)
				started = true
			}
			if _, err := w.Write(append(append([]byte("data: "), raw...), '\n', '\n')); err != nil {
				return err
			}
			if flushErr := ctrl.Flush(); flushErr != nil && !errors.Is(flushErr, http.ErrNotSupported) {
				return flushErr
			}
			return nil
		})
		if err != nil && !started {
			writeDomainRPCError(w, envelope.ID, err)
		}

	default:
		writeRPCError(w, http.StatusNotFound, envelope.ID, -32601, "unsupported ACP method: "+envelope.Method)
	}
}

func handlePermission(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok || !permitWrite(cfg, w, r, "") {
		return
	}
	if cfg.Service == nil {
		writeHTTPError(w, http.StatusServiceUnavailable, domain.ErrDisabled.Error())
		return
	}
	var body struct {
		SessionID string `json:"sessionId"`
		RunID     string `json:"runId"`
		Decision  string `json:"decision"`
		OptionID  string `json:"optionId"`
		Branch    string `json:"branch"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "request body is not valid JSON")
		return
	}
	branch := strings.TrimSpace(body.Branch)
	if branch == "" {
		branch = strings.TrimSpace(r.Header.Get("X-Conduit-Workspace-Branch"))
	}
	gate, err := cfg.Service.DecidePermission(
		r.Context(),
		caller,
		r.PathValue("workspaceID"),
		branch,
		r.PathValue("requestID"),
		body.SessionID,
		body.RunID,
		body.Decision,
		body.OptionID,
	)
	if err != nil {
		if errors.Is(err, domain.ErrPermissionPending) {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":      err.Error(),
				"permission": gate,
				"valueTier":  domain.ValueTier,
			})
			return
		}
		if errors.Is(err, domain.ErrSelfApproval) || errors.Is(err, domain.ErrPermissionDenied) {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"error":      err.Error(),
				"permission": gate,
				"valueTier":  domain.ValueTier,
			})
			return
		}
		writeDomainHTTPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"permission": gate,
		"valueTier":  domain.ValueTier,
	})
}

func handleListSessions(cfg Config, w http.ResponseWriter, r *http.Request) {
	caller, ok := authenticate(cfg, w, r)
	if !ok {
		return
	}
	if cfg.Service == nil {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []any{}, "valueTier": domain.ValueTier})
		return
	}
	sessions, err := cfg.Service.ListSessions(r.Context(), caller, r.PathValue("workspaceID"), func(env string) bool {
		return cfg.Visible != nil && cfg.Visible(r, env)
	})
	if err != nil {
		writeDomainHTTPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sessions":  sessions,
		"valueTier": domain.ValueTier,
	})
}

var runValidator, validatorErr = agui.NewValidator()

func handleWorkspaceAgentRuns(cfg Config, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	caller, ok := authenticate(cfg, w, r)
	if !ok || !permitRead(cfg, w, r, "") {
		return
	}
	if validatorErr != nil || cfg.Service == nil {
		writeHTTPError(w, http.StatusServiceUnavailable, domain.ErrDisabled.Error())
		return
	}
	if rawMedia := strings.TrimSpace(r.Header.Get("Content-Type")); rawMedia != "" {
		mediaType, _, err := mime.ParseMediaType(rawMedia)
		if err != nil || mediaType != "application/json" {
			writeHTTPError(w, http.StatusUnsupportedMediaType, "Use application/json.")
			return
		}
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeHTTPError(w, http.StatusRequestEntityTooLarge, "run input exceeds 1 MiB")
		return
	}
	if _, err := runValidator.Input(raw); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid AG-UI RunAgentInput")
		return
	}
	var parsed struct {
		ThreadID        string         `json:"threadId"`
		RunID           string         `json:"runId"`
		ProtocolVersion string         `json:"protocolVersion"`
		ForwardedProps  map[string]any `json:"forwardedProps"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil ||
		strings.TrimSpace(parsed.ThreadID) == "" ||
		strings.TrimSpace(parsed.RunID) == "" ||
		(parsed.ProtocolVersion != "" && parsed.ProtocolVersion != agui.Version) {
		writeHTTPError(w, http.StatusBadRequest, "threadId, runId, and supported protocolVersion are required")
		return
	}
	snapshotOnly := isSnapshotMode(parsed.ForwardedProps)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()

	var writeMu sync.Mutex
	started := false
	ctrl := http.NewResponseController(w)
	_ = ctrl.SetWriteDeadline(time.Now().Add(30 * time.Minute))

	streamErr := cfg.Service.StreamWorkspaceRun(
		ctx,
		caller,
		r.PathValue("workspaceID"),
		parsed.ThreadID,
		parsed.RunID,
		snapshotOnly,
		func(env string) bool { return cfg.Visible != nil && cfg.Visible(r, env) },
		func(frame []byte) error {
			writeMu.Lock()
			defer writeMu.Unlock()
			if !started {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("X-Accel-Buffering", "no")
				w.WriteHeader(http.StatusOK)
				started = true
			}
			if _, err := w.Write(append(append([]byte("data: "), frame...), '\n', '\n')); err != nil {
				return err
			}
			if flushErr := ctrl.Flush(); flushErr != nil && !errors.Is(flushErr, http.ErrNotSupported) {
				return flushErr
			}
			return nil
		},
	)
	if streamErr != nil && !started {
		writeDomainHTTPError(w, streamErr)
	}
}

func isSnapshotMode(forwarded map[string]any) bool {
	if forwarded == nil {
		return false
	}
	conduitMap, ok := forwarded["conduit"].(map[string]any)
	if !ok {
		return false
	}
	mode, _ := conduitMap["mode"].(string)
	return mode == "snapshot"
}

func authenticate(cfg Config, w http.ResponseWriter, r *http.Request) (domain.Caller, bool) {
	if cfg.Authorize == nil || cfg.Caller == nil {
		writeHTTPError(w, http.StatusUnauthorized, "unauthorized")
		return domain.Caller{}, false
	}
	if err := cfg.Authorize(r); err != nil {
		writeAuthError(w, err)
		return domain.Caller{}, false
	}
	caller, err := cfg.Caller(r)
	if err != nil {
		writeAuthError(w, err)
		return domain.Caller{}, false
	}
	if strings.TrimSpace(caller.Username) == "" {
		writeHTTPError(w, http.StatusUnauthorized, "unauthorized")
		return domain.Caller{}, false
	}
	return caller, true
}

func permitWrite(cfg Config, w http.ResponseWriter, r *http.Request, env string) bool {
	if cfg.Permit == nil {
		writeHTTPError(w, http.StatusForbidden, "forbidden")
		return false
	}
	if err := cfg.Permit(r, env); err != nil {
		writeAuthError(w, err)
		return false
	}
	return true
}

func permitRead(cfg Config, w http.ResponseWriter, r *http.Request, env string) bool {
	check := cfg.PermitRead
	if check == nil {
		check = cfg.Permit
	}
	if check == nil {
		writeHTTPError(w, http.StatusForbidden, "forbidden")
		return false
	}
	if err := check(r, env); err != nil {
		writeAuthError(w, err)
		return false
	}
	return true
}

func writeAuthError(w http.ResponseWriter, err error) {
	var statusErr StatusError
	if errors.As(err, &statusErr) && statusErr.Status != 0 {
		writeHTTPError(w, statusErr.Status, statusErr.Error())
		return
	}
	writeHTTPError(w, http.StatusForbidden, "forbidden")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	return dec.Decode(dst)
}

func requestedBranch(r *http.Request, params map[string]any) string {
	if metaBranch := metaString(params, "branch"); metaBranch != "" {
		return metaBranch
	}
	return strings.TrimSpace(r.Header.Get("X-Conduit-Workspace-Branch"))
}

func metaString(params map[string]any, key string) string {
	meta, ok := params["_meta"].(map[string]any)
	if !ok {
		return ""
	}
	val, _ := meta[key].(string)
	return strings.TrimSpace(val)
}

func intParam(v any) (int, bool) {
	switch typed := v.(type) {
	case float64:
		if typed != float64(int(typed)) {
			return 0, false
		}
		return int(typed), true
	case int:
		return typed, true
	default:
		return 0, false
	}
}

func decodePromptBlocks(raw any) []map[string]any {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if m["type"] == "text" {
			if txt, ok := m["text"].(string); ok && txt != "" {
				out = append(out, map[string]any{"type": "text", "text": txt})
			}
		} else if m["type"] == "image" {
			mimeVal, ok1 := m["mimeType"].(string)
			dataVal, ok2 := m["data"].(string)
			if ok1 && ok2 && mimeVal != "" && dataVal != "" {
				out = append(out, map[string]any{"type": "image", "mimeType": mimeVal, "data": dataVal})
			}
		}
	}
	return out
}

func writeDomainRPCError(w http.ResponseWriter, id any, err error) {
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		writeRPCError(w, http.StatusUnauthorized, id, -32001, err.Error())
	case errors.Is(err, domain.ErrForbidden), errors.Is(err, domain.ErrSelfApproval):
		writeRPCError(w, http.StatusForbidden, id, -32003, err.Error())
	case errors.Is(err, domain.ErrNotFound):
		writeRPCError(w, http.StatusNotFound, id, -32004, err.Error())
	case errors.Is(err, domain.ErrProtocolVersion), errors.Is(err, domain.ErrInvalid):
		writeRPCError(w, http.StatusBadRequest, id, -32602, err.Error())
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrPermissionPending):
		writeRPCError(w, http.StatusConflict, id, -32009, err.Error())
	case errors.Is(err, domain.ErrAuditUnavailable), errors.Is(err, domain.ErrDisabled):
		writeRPCError(w, http.StatusServiceUnavailable, id, -32010, err.Error())
	default:
		writeRPCError(w, http.StatusBadGateway, id, -32603, err.Error())
	}
}

func writeDomainHTTPError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		writeHTTPError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, domain.ErrForbidden):
		writeHTTPError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, domain.ErrNotFound):
		writeHTTPError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrInvalid), errors.Is(err, domain.ErrProtocolVersion):
		writeHTTPError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrConflict):
		writeHTTPError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrAuditUnavailable), errors.Is(err, domain.ErrDisabled):
		writeHTTPError(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeHTTPError(w, http.StatusBadGateway, err.Error())
	}
}

func writeRPCResult(w http.ResponseWriter, id any, result any) {
	writeJSON(w, http.StatusOK, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

func writeRPCError(w http.ResponseWriter, status int, id any, code int, message string) {
	writeJSON(w, status, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func writeHTTPError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error":     message,
		"valueTier": domain.ValueTier,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
