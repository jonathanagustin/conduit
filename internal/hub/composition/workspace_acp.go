package composition

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/api/agui"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	devapp "github.com/jonathanagustin/conduit/internal/devworkspace/application"
	devdomain "github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/workspaceacp/adapters/nodebridge"
	acpapp "github.com/jonathanagustin/conduit/internal/workspaceacp/application"
	acpdomain "github.com/jonathanagustin/conduit/internal/workspaceacp/domain"
	acphttp "github.com/jonathanagustin/conduit/internal/workspaceacp/httpapi"
	acpports "github.com/jonathanagustin/conduit/internal/workspaceacp/ports"
)

func assembleWorkspaceACP(
	repository controlplane.Repository,
	spaces *devapp.Service,
	hubOptions Options,
	getenv func(string) string,
) *acpapp.Service {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	bridges := hubOptions.WorkspaceACPBridges
	if bridges == nil {
		offline := hubOptions.WorkspaceACPOfflineBundle || getenv("CONDUIT_OFFLINE_BUNDLE") == "1"
		if getenv("DEVWORKSPACE_ACP_BRIDGE") == "node" {
			webDir := strings.TrimSpace(getenv("CONDUIT_WEB_DIR"))
			if webDir == "" {
				webDir = filepath.Clean("web")
			}
			bridges = nodebridge.NewPool(nodebridge.Config{
				WebDir:        webDir,
				OfflineBundle: offline,
			})
		}
	}
	govService := controlplane.NewGovernanceService(repository)
	return acpapp.New(acpapp.Dependencies{
		Workspaces: workspaceACPSpacesAdapter{spaces: spaces},
		Bridges:    bridges,
		Governance: workspaceACPChangeAdapter{governance: govService, repository: repository},
		Auditor:    workspaceACPAuditor{repository: repository},
	})
}

func mountWorkspaceACP(
	legacy http.Handler,
	repository controlplane.Repository,
	options controlplane.HandlerOptions,
	acpService *acpapp.Service,
) http.Handler {
	govService := controlplane.NewGovernanceService(repository)
	routeMux := http.NewServeMux()
	authorize := func(request *http.Request) error {
		if _, err := operatorClaims(request, options); err != nil {
			return acphttp.StatusError{Status: statusForIdentityError(err), Message: "unauthorized"}
		}
		return nil
	}
	resolveCaller := func(request *http.Request) (acpdomain.Caller, error) {
		principal, ok := packagingPrincipal(request, govService, options)
		if !ok {
			return acpdomain.Caller{}, acphttp.StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
		}
		return toWorkspaceACPCaller(principal), nil
	}
	visible := func(request *http.Request, environmentID string) bool {
		principal, ok := packagingPrincipal(request, govService, options)
		if !ok || environmentID == "" {
			return false
		}
		return govService.AuthorizeResourceOperation(
			request.Context(), principal, options, "environment/"+environmentID, controlplane.OperationReadResource,
		)
	}
	permit := func(request *http.Request, environmentID string) error {
		principal, ok := packagingPrincipal(request, govService, options)
		if !ok {
			return acphttp.StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
		}
		if strings.TrimSpace(environmentID) == "" {
			if !devStanding(request.Context(), govService, options, principal) {
				return acphttp.StatusError{Status: http.StatusForbidden, Message: "forbidden"}
			}
			return nil
		}
		if govService.AuthorizeResourceOperation(request.Context(), principal, options, "environment/"+environmentID, controlplane.OperationProposeChange) ||
			govService.AuthorizeResourceOperation(request.Context(), principal, options, "environment/"+environmentID, controlplane.OperationOperate) {
			return nil
		}
		return acphttp.StatusError{Status: http.StatusForbidden, Message: "forbidden"}
	}
	permitRead := func(request *http.Request, environmentID string) error {
		principal, ok := packagingPrincipal(request, govService, options)
		if !ok {
			return acphttp.StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
		}
		if strings.TrimSpace(environmentID) == "" {
			if !devStanding(request.Context(), govService, options, principal) {
				return acphttp.StatusError{Status: http.StatusForbidden, Message: "forbidden"}
			}
			return nil
		}
		if govService.AuthorizeResourceOperation(request.Context(), principal, options, "environment/"+environmentID, controlplane.OperationReadResource) {
			return nil
		}
		return acphttp.StatusError{Status: http.StatusForbidden, Message: "forbidden"}
	}

	acphttp.Register(routeMux, acphttp.Config{
		Service:    acpService,
		Authorize:  authorize,
		Caller:     resolveCaller,
		Visible:    visible,
		Permit:     permit,
		PermitRead: permitRead,
	})
	routeMux.Handle("/", legacy)

	validator, _ := agui.NewValidator()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/agent-runs" && acpService != nil {
			if _, err := operatorClaims(r, options); err != nil {
				routeMux.ServeHTTP(w, r)
				return
			}
			mediaType, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if mediaErr != nil || mediaType != "application/json" {
				routeMux.ServeHTTP(w, r)
				return
			}
			raw, readErr := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
			if readErr != nil || len(raw) > 1<<20 {
				r.Body = io.NopCloser(bytes.NewReader(raw))
				routeMux.ServeHTTP(w, r)
				return
			}
			var peek struct {
				ThreadID        string         `json:"threadId"`
				RunID           string         `json:"runId"`
				ProtocolVersion string         `json:"protocolVersion"`
				ForwardedProps  map[string]any `json:"forwardedProps"`
			}
			if json.Unmarshal(raw, &peek) != nil || !strings.HasPrefix(peek.ThreadID, "wsp-acp-") {
				r.Body = io.NopCloser(bytes.NewReader(raw))
				routeMux.ServeHTTP(w, r)
				return
			}
			serveMirroredAgentRun(w, r, validator, acpService, resolveCaller, visible, raw, peek.ThreadID, peek.RunID, peek.ProtocolVersion, peek.ForwardedProps)
			return
		}
		routeMux.ServeHTTP(w, r)
	})
}

func serveMirroredAgentRun(
	w http.ResponseWriter,
	r *http.Request,
	validator *agui.Validator,
	acpService *acpapp.Service,
	resolveCaller func(*http.Request) (acpdomain.Caller, error),
	visible func(*http.Request, string) bool,
	raw []byte,
	threadID, runID, protocolVersion string,
	forwarded map[string]any,
) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	caller, err := resolveCaller(r)
	if err != nil || strings.TrimSpace(caller.Username) == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "UNAUTHORIZED", "error": "Operator authentication is required."})
		return
	}
	if validator == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "RUN_UNAVAILABLE", "error": "AG-UI validator unavailable."})
		return
	}
	if _, err := validator.Input(raw); err != nil || strings.TrimSpace(runID) == "" || (protocolVersion != "" && protocolVersion != agui.Version) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "INVALID_RUN_INPUT", "error": "the agent run input was rejected"})
		return
	}
	snapshotOnly := false
	if forwarded != nil {
		if conduitMap, ok := forwarded["conduit"].(map[string]any); ok {
			if mode, _ := conduitMap["mode"].(string); mode == "snapshot" {
				snapshotOnly = true
			}
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	var writeMu sync.Mutex
	started := false
	ctrl := http.NewResponseController(w)
	_ = ctrl.SetWriteDeadline(time.Now().Add(30 * time.Minute))

	streamErr := acpService.StreamThreadRun(
		ctx,
		caller,
		threadID,
		runID,
		snapshotOnly,
		func(env string) bool { return visible(r, env) },
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
		status := http.StatusBadGateway
		code := "RUN_UNAVAILABLE"
		switch {
		case errors.Is(streamErr, acpdomain.ErrNotFound), errors.Is(streamErr, acpdomain.ErrForbidden):
			status = http.StatusNotFound
			code = "SESSION_NOT_FOUND"
		case errors.Is(streamErr, acpdomain.ErrAuditUnavailable):
			status = http.StatusServiceUnavailable
			code = "AUDIT_UNAVAILABLE"
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "error": streamErr.Error()})
	}
}

func toWorkspaceACPCaller(principal controlplane.Principal) acpdomain.Caller {
	caller := acpdomain.Caller{
		Username:       principal.Username,
		Teams:          principal.Teams,
		Groups:         principal.Groups,
		ServiceAccount: principal.ServiceAccount,
	}
	if represented := principal.OnBehalfOf; represented != nil {
		caller.OnBehalfOf = &acpdomain.Caller{
			Username:       represented.Username,
			Teams:          represented.Teams,
			Groups:         represented.Groups,
			ServiceAccount: represented.ServiceAccount,
		}
	}
	return caller
}

func toDevCaller(caller acpdomain.Caller) devdomain.Caller {
	dc := devdomain.Caller{
		Username:       caller.Username,
		Teams:          caller.Teams,
		Groups:         caller.Groups,
		ServiceAccount: caller.ServiceAccount,
	}
	if represented := caller.OnBehalfOf; represented != nil {
		dc.OnBehalfOf = &devdomain.Caller{
			Username:       represented.Username,
			Teams:          represented.Teams,
			Groups:         represented.Groups,
			ServiceAccount: represented.ServiceAccount,
		}
	}
	return dc
}

type workspaceACPSpacesAdapter struct {
	spaces *devapp.Service
}

func (a workspaceACPSpacesAdapter) Observe(ctx context.Context, workspaceID string) (acpdomain.WorkspaceSnapshot, error) {
	if a.spaces == nil {
		return acpdomain.WorkspaceSnapshot{}, acpdomain.ErrDisabled
	}
	rows, err := a.spaces.Records(ctx)
	if err != nil {
		return acpdomain.WorkspaceSnapshot{}, err
	}
	for _, row := range rows {
		if row.ID == workspaceID {
			return toWorkspaceSnapshot(row), nil
		}
	}
	return acpdomain.WorkspaceSnapshot{}, acpdomain.ErrNotFound
}

func (a workspaceACPSpacesAdapter) CanInspect(ctx context.Context, caller acpdomain.Caller, workspaceID string, visible func(string) bool) (acpdomain.WorkspaceSnapshot, error) {
	if a.spaces == nil {
		return acpdomain.WorkspaceSnapshot{}, acpdomain.ErrDisabled
	}
	row, err := a.spaces.Get(ctx, toDevCaller(caller), workspaceID, visible)
	if err != nil {
		return acpdomain.WorkspaceSnapshot{}, mapDevErr(err)
	}
	return toWorkspaceSnapshot(row), nil
}

func (a workspaceACPSpacesAdapter) SnapshotFiles(ctx context.Context, caller acpdomain.Caller, workspaceID string) (map[string][]byte, error) {
	if a.spaces == nil {
		return nil, acpdomain.ErrDisabled
	}
	files, err := a.spaces.SnapshotFiles(ctx, toDevCaller(caller), workspaceID)
	if err != nil {
		return nil, mapDevErr(err)
	}
	return files, nil
}

func (a workspaceACPSpacesAdapter) ApplyEdit(ctx context.Context, caller acpdomain.Caller, workspaceID, path string, content []byte) (string, error) {
	if a.spaces == nil {
		return "", acpdomain.ErrDisabled
	}
	if _, err := a.spaces.ApplyGovernedEdit(ctx, toDevCaller(caller), workspaceID, path, content); err != nil {
		return "", mapDevErr(err)
	}
	return acpdomain.DigestBytes(content), nil
}

func toWorkspaceSnapshot(row devdomain.Workspace) acpdomain.WorkspaceSnapshot {
	return acpdomain.WorkspaceSnapshot{
		ID:               row.ID,
		Owner:            row.Owner,
		EnvironmentID:    row.EnvironmentID,
		ProposalRevision: row.ProposalRevision,
		SourceDigest:     row.SourceDigest,
		Status:           row.Status,
	}
}

func mapDevErr(err error) error {
	switch {
	case errors.Is(err, devdomain.ErrUnauthorized):
		return acpdomain.ErrUnauthorized
	case errors.Is(err, devdomain.ErrForbidden):
		return acpdomain.ErrForbidden
	case errors.Is(err, devdomain.ErrNotFound):
		return acpdomain.ErrNotFound
	case errors.Is(err, devdomain.ErrInvalid):
		return acpdomain.ErrInvalid
	case errors.Is(err, devdomain.ErrConflict):
		return acpdomain.ErrConflict
	default:
		return err
	}
}

type workspaceACPChangeAdapter struct {
	governance *controlplane.GovernanceService
	repository controlplane.Repository
}

func (g workspaceACPChangeAdapter) OpenHighRiskChange(
	ctx context.Context,
	caller acpdomain.Caller,
	workspace acpdomain.WorkspaceSnapshot,
	gate acpdomain.PermissionGate,
) (acpports.ChangeTicket, error) {
	if g.governance == nil {
		return acpports.ChangeTicket{}, acpdomain.ErrDisabled
	}
	author := controlplane.Principal{
		Username:       caller.Username,
		Teams:          caller.Teams,
		Groups:         caller.Groups,
		ServiceAccount: caller.ServiceAccount,
	}
	rawSeed, _ := json.Marshal(struct {
		Workspace, Branch, Session, Run, Tool, Title string
	}{workspace.ID, gate.Branch, gate.SessionID, gate.RunID, gate.ToolCallID, gate.Title})
	sum := sha256.Sum256(rawSeed)
	targetID := workspace.ID + ":acp:" + hex.EncodeToString(sum[:8])

	change := controlplane.EntityChange{
		ID:   "chg_acp_" + gate.RequestID,
		Type: controlplane.ChangeTypeUpdateEnvironmentConfig,
		Request: map[string]any{
			"config": map[string]any{
				"workspaceId": workspace.ID,
				"branch":      gate.Branch,
				"sessionId":   gate.SessionID,
				"runId":       gate.RunID,
				"threadId":    gate.ThreadID,
				"toolCallId":  gate.ToolCallID,
				"actionKind":  string(gate.ActionKind),
				"path":        gate.Path,
				"command":     gate.Command,
			},
		},
		ReferencedResources: map[string]controlplane.ReferencedResource{
			"environment": {RID: "environment/" + workspace.EnvironmentID, Name: workspace.EnvironmentID},
		},
	}
	title := fmt.Sprintf("High-risk workspace ACP action in %s: %s", workspace.ID, gate.Title)
	desc := fmt.Sprintf("Session %s / run %s / thread %s on branch %s requested high-risk %s.", gate.SessionID, gate.RunID, gate.ThreadID, gate.Branch, gate.ActionKind)
	cr, err := g.governance.ProposeHumanChange(ctx, "WORKSPACE_ACP", targetID, workspace.EnvironmentID, []controlplane.EntityChange{change}, author, title, desc)
	if err != nil {
		return acpports.ChangeTicket{}, err
	}
	return acpports.ChangeTicket{
		ChangeRequestID: cr.ID,
		Status:          string(cr.Status),
		ApprovedByOther: false,
	}, nil
}

func (g workspaceACPChangeAdapter) CheckChange(ctx context.Context, changeRequestID, author string) (acpports.ChangeTicket, error) {
	if g.repository == nil {
		return acpports.ChangeTicket{}, acpdomain.ErrDisabled
	}
	cr, err := g.repository.GetChangeRequest(ctx, changeRequestID)
	if err != nil {
		if errors.Is(err, controlplane.ErrNotFound) {
			return acpports.ChangeTicket{}, acpdomain.ErrNotFound
		}
		return acpports.ChangeTicket{}, err
	}
	approvedByOther := false
	if cr.Status == controlplane.ChangeApproved {
		for _, rev := range cr.Reviews {
			if strings.EqualFold(rev.Decision, "APPROVE") &&
				rev.Subject != "" &&
				rev.Subject != author &&
				rev.Subject != cr.Author.Username {
				approvedByOther = true
				break
			}
		}
	}
	return acpports.ChangeTicket{
		ChangeRequestID: cr.ID,
		Status:          string(cr.Status),
		ApprovedByOther: approvedByOther,
	}, nil
}

type workspaceACPAuditor struct {
	repository controlplane.Repository
}

func (a workspaceACPAuditor) Record(ctx context.Context, entry acpdomain.AuditEntry) error {
	if a.repository == nil {
		return acpdomain.ErrAuditUnavailable
	}
	decision := governance.DecisionAllow
	if strings.EqualFold(entry.Decision, "DENY") {
		decision = governance.DecisionDeny
	}
	attrs := map[string]any{
		"sessionId":   entry.SessionID,
		"runId":       entry.RunID,
		"threadId":    entry.ThreadID,
		"workspaceId": entry.WorkspaceID,
		"branch":      entry.Branch,
		"valueTier":   acpdomain.ValueTier,
	}
	if entry.ToolCallID != "" {
		attrs["toolCallId"] = entry.ToolCallID
	}
	if entry.Kind != "" {
		attrs["kind"] = entry.Kind
	}
	if entry.Path != "" {
		attrs["path"] = entry.Path
	}
	if entry.Command != "" {
		attrs["command"] = entry.Command
	}
	if entry.RiskTier != "" {
		attrs["riskTier"] = entry.RiskTier
	}
	if entry.ChangeRequestID != "" {
		attrs["changeRequestId"] = entry.ChangeRequestID
	}
	if entry.Digest != "" {
		attrs["digest"] = entry.Digest
	}
	at := entry.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	ev := governance.NewAuthorizationCheckEvent(at, entry.Subject, entry.Action, entry.Resource, decision, entry.Reason, attrs)
	_, err := a.repository.AppendAuthorizationAudit(ctx, ev)
	return err
}

var _ = os.Getenv
