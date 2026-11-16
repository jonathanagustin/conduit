package application_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jonathanagustin/conduit/api/agui"
	"github.com/jonathanagustin/conduit/internal/workspaceacp/adapters/memorybridge"
	"github.com/jonathanagustin/conduit/internal/workspaceacp/application"
	"github.com/jonathanagustin/conduit/internal/workspaceacp/domain"
	"github.com/jonathanagustin/conduit/internal/workspaceacp/ports"
)

type memWorkspaces struct {
	mu    sync.Mutex
	ws    domain.WorkspaceSnapshot
	files map[string][]byte
}

func (m *memWorkspaces) Observe(_ context.Context, id string) (domain.WorkspaceSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ws.ID != id {
		return domain.WorkspaceSnapshot{}, domain.ErrNotFound
	}
	return m.ws, nil
}

func (m *memWorkspaces) CanInspect(_ context.Context, caller domain.Caller, id string, visible func(string) bool) (domain.WorkspaceSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ws.ID != id {
		return domain.WorkspaceSnapshot{}, domain.ErrNotFound
	}
	if caller.Username != m.ws.Owner && (visible == nil || !visible(m.ws.EnvironmentID)) {
		return domain.WorkspaceSnapshot{}, domain.ErrNotFound
	}
	return m.ws, nil
}

func (m *memWorkspaces) SnapshotFiles(_ context.Context, _ domain.Caller, _ string) (map[string][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string][]byte, len(m.files))
	for k, v := range m.files {
		out[k] = append([]byte(nil), v...)
	}
	return out, nil
}

func (m *memWorkspaces) ApplyEdit(_ context.Context, caller domain.Caller, id, path string, content []byte) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ws.ID != id || m.ws.Owner != caller.Username {
		return "", domain.ErrForbidden
	}
	m.files[path] = append([]byte(nil), content...)
	return domain.DigestBytes(content), nil
}

type memGovernance struct {
	mu      sync.Mutex
	tickets map[string]ports.ChangeTicket
}

func (g *memGovernance) OpenHighRiskChange(_ context.Context, _ domain.Caller, ws domain.WorkspaceSnapshot, gate domain.PermissionGate) (ports.ChangeTicket, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	id := "crq_" + ws.ID + "_" + gate.RequestID
	t := ports.ChangeTicket{
		ChangeRequestID: id,
		Status:          "PENDING_APPROVALS",
		ApprovedByOther: false,
	}
	g.tickets[id] = t
	return t, nil
}

func (g *memGovernance) CheckChange(_ context.Context, id, _ string) (ports.ChangeTicket, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, ok := g.tickets[id]
	if !ok {
		return ports.ChangeTicket{}, domain.ErrNotFound
	}
	return t, nil
}

type memAuditor struct {
	mu      sync.Mutex
	fail    bool
	entries []domain.AuditEntry
}

func (a *memAuditor) Record(_ context.Context, e domain.AuditEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fail {
		return errors.New("audit outage")
	}
	a.entries = append(a.entries, e)
	return nil
}

func newHarness() (*application.Service, *memWorkspaces, *memGovernance, *memAuditor) {
	ws := &memWorkspaces{
		ws: domain.WorkspaceSnapshot{
			ID:            "wsp_unit",
			Owner:         "alice",
			EnvironmentID: "dev",
			Status:        "READY",
		},
		files: map[string][]byte{"README.md": []byte("initial\n")},
	}
	gov := &memGovernance{tickets: map[string]ports.ChangeTicket{}}
	aud := &memAuditor{}
	svc := application.New(application.Dependencies{
		Workspaces: ws,
		Bridges:    memorybridge.NewPool(),
		Governance: gov,
		Auditor:    aud,
	})
	return svc, ws, gov, aud
}

func TestProtocolVersionPinAndBranchConfinement(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := newHarness()
	alice := domain.Caller{Username: "alice"}

	if _, err := svc.Initialize(t.Context(), alice, "wsp_unit", "", 2); !errors.Is(err, domain.ErrProtocolVersion) {
		t.Fatalf("Initialize(v2) = %v, want ErrProtocolVersion", err)
	}
	res, err := svc.Initialize(t.Context(), alice, "wsp_unit", "", domain.ProtocolVersion)
	if err != nil {
		t.Fatalf("Initialize(v1): %v", err)
	}
	if res["protocolVersion"] != domain.ProtocolVersion {
		t.Fatalf("protocolVersion = %v, want %d", res["protocolVersion"], domain.ProtocolVersion)
	}

	if _, err := svc.NewSession(t.Context(), domain.Caller{Username: "bob"}, "wsp_unit", "", ""); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("cross-user NewSession = %v, want ErrForbidden", err)
	}
	if _, err := svc.NewSession(t.Context(), domain.Caller{Username: "alice", ServiceAccount: true}, "wsp_unit", "", ""); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("service-account NewSession = %v, want ErrForbidden", err)
	}
	if _, err := svc.NewSession(t.Context(), alice, "wsp_unit", "main", ""); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("branch escape NewSession = %v, want ErrForbidden", err)
	}

	sess, err := svc.NewSession(t.Context(), alice, "wsp_unit", "workspace/alice/wsp_unit/feature-1", "")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if sess.Branch != "workspace/alice/wsp_unit/feature-1" {
		t.Fatalf("bound branch = %q", sess.Branch)
	}
	if err := svc.CloseSession(t.Context(), alice, "wsp_unit", "workspace/alice/wsp_unit/other", sess.SessionID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("cross-branch CloseSession = %v, want ErrForbidden", err)
	}
}

func TestHighRiskPermissionBlocksPendingDeniedAndSelfApproval(t *testing.T) {
	t.Parallel()
	svc, ws, gov, aud := newHarness()
	alice := domain.Caller{Username: "alice"}

	sess, err := svc.NewSession(t.Context(), alice, "wsp_unit", "", "")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	gateCh := make(chan domain.PermissionGate, 1)
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- svc.Prompt(
			t.Context(),
			alice,
			"wsp_unit",
			sess.Branch,
			sess.SessionID,
			"run-high-1",
			1,
			[]map[string]any{{"type": "text", "text": "edit deploy/values.yaml: replicas: 5"}},
			func(ev map[string]any) error {
				if ev["type"] == "permission_request" {
					if g, ok := ev["request"].(domain.PermissionGate); ok {
						gateCh <- g
					}
				}
				return nil
			},
		)
	}()

	gate := <-gateCh
	if gate.RiskTier != domain.RiskHighChangeRequest || gate.Status != domain.StatusPendingChangeRequest || gate.ChangeRequestID == "" {
		t.Fatalf("unexpected high-risk gate: %+v", gate)
	}
	for _, opt := range gate.Options {
		if opt.Kind == "allow_always" {
			t.Fatalf("allow_always survived sanitization: %+v", gate.Options)
		}
	}

	if _, err := svc.DecidePermission(t.Context(), alice, "wsp_unit", sess.Branch, gate.RequestID, sess.SessionID, "run-high-1", "confirm", ""); !errors.Is(err, domain.ErrPermissionPending) {
		t.Fatalf("pending ChangeRequest confirm = %v, want ErrPermissionPending", err)
	}
	ws.mu.Lock()
	if _, exists := ws.files["deploy/values.yaml"]; exists {
		ws.mu.Unlock()
		t.Fatal("file written while ChangeRequest was still pending")
	}
	ws.mu.Unlock()

	gov.mu.Lock()
	gov.tickets[gate.ChangeRequestID] = ports.ChangeTicket{
		ChangeRequestID: gate.ChangeRequestID,
		Status:          "APPROVED",
		ApprovedByOther: false,
	}
	gov.mu.Unlock()
	if _, err := svc.DecidePermission(t.Context(), alice, "wsp_unit", sess.Branch, gate.RequestID, sess.SessionID, "run-high-1", "confirm", ""); !errors.Is(err, domain.ErrSelfApproval) {
		t.Fatalf("self-approved ChangeRequest confirm = %v, want ErrSelfApproval", err)
	}

	gov.mu.Lock()
	gov.tickets[gate.ChangeRequestID] = ports.ChangeTicket{
		ChangeRequestID: gate.ChangeRequestID,
		Status:          "APPROVED",
		ApprovedByOther: true,
	}
	gov.mu.Unlock()
	settled, err := svc.DecidePermission(t.Context(), alice, "wsp_unit", sess.Branch, gate.RequestID, sess.SessionID, "run-high-1", "confirm", "")
	if err != nil || settled.Status != domain.StatusApproved {
		t.Fatalf("separate-human-approved confirm = %+v, %v", settled, err)
	}
	if err := <-doneCh; err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	ws.mu.Lock()
	got := string(ws.files["deploy/values.yaml"])
	ws.mu.Unlock()
	if got != "replicas: 5" {
		t.Fatalf("workspace file = %q, want %q", got, "replicas: 5")
	}

	seq := agui.NewSequence(sess.ThreadID, "console-replay-1")
	if err := svc.StreamWorkspaceRun(t.Context(), alice, "wsp_unit", sess.ThreadID, "console-replay-1", false, nil, func(frame []byte) error {
		return seq.Accept(frame)
	}); err != nil {
		t.Fatalf("StreamWorkspaceRun: %v", err)
	}
	if !seq.Complete() {
		t.Fatal("mirrored AG-UI stream did not complete")
	}

	aud.mu.Lock()
	defer aud.mu.Unlock()
	for _, entry := range aud.entries {
		if entry.SessionID == "" || entry.RunID == "" || entry.ThreadID == "" {
			t.Fatalf("audit entry missing session/run/thread correlation: %+v", entry)
		}
	}
}

func TestAuditOutageFailsClosed(t *testing.T) {
	t.Parallel()
	svc, _, _, aud := newHarness()
	aud.fail = true
	if _, err := svc.NewSession(t.Context(), domain.Caller{Username: "alice"}, "wsp_unit", "", ""); !errors.Is(err, domain.ErrAuditUnavailable) {
		t.Fatalf("NewSession during audit outage = %v, want ErrAuditUnavailable", err)
	}
}
