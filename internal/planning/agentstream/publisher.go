package agentstream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"

	"github.com/jonathanagustin/conduit/api/agui"
)

// AuditRecord contains correlation and content digests, never raw model/tool
// bodies. The Hub audit adapter binds the authenticated owner independently.
type AuditRecord struct {
	ThreadID      string
	RunID         string
	Action        string
	PayloadSHA256 string
}

// Auditor commits one event receipt before publication or tool execution.
type Auditor interface {
	Record(context.Context, AuditRecord) error
}

func digest(raw []byte) string { value := sha256.Sum256(raw); return hex.EncodeToString(value[:]) }

type publisher struct {
	mu       sync.Mutex
	threadID string
	runID    string
	sequence *agui.Sequence
	audit    Auditor
	write    func([]byte) error
	bytes    int
	started  bool
	broken   bool
}

func (p *publisher) send(ctx context.Context, event map[string]any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	terminal := event["type"] == "RUN_ERROR" || event["type"] == "RUN_FINISHED"
	limit := (1 << 20) - 4096
	if terminal {
		limit = 1 << 20
	}
	if len(raw) > 64<<10 || p.bytes+len(raw) > limit {
		return ErrRunCapacity
	}
	next, err := p.sequence.Next(raw)
	if err != nil {
		return err
	}
	if err := p.audit.Record(ctx, AuditRecord{ThreadID: p.threadID, RunID: p.runID, Action: "agent.event." + event["type"].(string), PayloadSHA256: digest(raw)}); err != nil {
		return ErrAuditUnavailable
	}
	if err := p.write(raw); err != nil {
		p.broken = true
		return err
	}
	p.sequence = next
	p.bytes += len(raw)
	p.started = true
	return nil
}

func (p *publisher) fail(ctx context.Context, cause error) {
	code, message := "RUN_FAILED", "The planning run could not finish."
	if errors.Is(cause, ErrAuditUnavailable) {
		code, message = "AUDIT_UNAVAILABLE", "The run stopped because its audit record could not be written."
	}
	if errors.Is(cause, ErrToolNotOffered) {
		code, message = "TOOL_NOT_OFFERED", ErrToolNotOffered.Error()
	}
	if errors.Is(cause, ErrInvalidToolArguments) || errors.Is(cause, ErrFieldUnavailable) {
		code, message = "TOOL_ARGUMENTS_REJECTED", "The suggested form operation was rejected."
	}
	event := map[string]any{"type": "RUN_ERROR", "code": code, "message": message}
	p.mu.Lock()
	broken := p.broken
	p.mu.Unlock()
	if broken {
		return
	}
	if err := p.send(ctx, event); err == nil {
		return
	} else if !errors.Is(err, ErrAuditUnavailable) {
		return
	}
	// An audit outage must still be reported as failure, never as a successful
	// run. No tool/state event takes this fallback path.
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.started || p.sequence.Complete() {
		return
	}
	raw, _ := json.Marshal(map[string]any{"type": "RUN_ERROR", "code": "AUDIT_UNAVAILABLE", "message": "The run stopped; its failure receipt could not be persisted."})
	next, err := p.sequence.Next(raw)
	if err != nil {
		return
	}
	if err := p.write(raw); err == nil {
		p.sequence = next
	}
}
