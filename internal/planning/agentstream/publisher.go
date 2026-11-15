package agentstream

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/jonathanagustin/conduit/api/agui"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
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
	state    map[string]json.RawMessage
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
	limit := (4 << 20) - 4096
	if terminal {
		limit = 4 << 20
	}
	if len(raw) > 1<<20 || p.bytes+len(raw) > limit {
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
	if errors.Is(cause, domain.ErrModelNotConfigured) {
		code, message = domain.CodeModelNotConfigured, "A planning model is not configured on this Hub."
	}
	if errors.Is(cause, domain.ErrAlreadyAccepted) {
		code, message = domain.CodeAlreadyAccepted, "This planning session is already accepted."
	}
	if errors.Is(cause, domain.ErrTurnInProgress) {
		code, message = domain.CodeTurnInProgress, "A planning turn is already running."
	}
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

func (p *publisher) stateUpdate(ctx context.Context, value map[string]any) error {
	next := map[string]json.RawMessage{}
	for key, child := range value {
		raw, err := json.Marshal(child)
		if err != nil {
			return err
		}
		next[key] = raw
	}
	if p.state == nil {
		if err := p.send(ctx, map[string]any{"type": "STATE_SNAPSHOT", "snapshot": value}); err != nil {
			return err
		}
	} else {
		keys := []string{}
		for key := range next {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		patch := []any{}
		for _, key := range keys {
			if bytes.Equal(p.state[key], next[key]) {
				continue
			}
			operation := "replace"
			if _, exists := p.state[key]; !exists {
				operation = "add"
			}
			path := strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
			patch = append(patch, map[string]any{"op": operation, "path": "/" + path, "value": json.RawMessage(next[key])})
		}
		if len(patch) > 0 {
			if err := p.send(ctx, map[string]any{"type": "STATE_DELTA", "delta": patch}); err != nil {
				return err
			}
		}
	}
	p.state = next
	return nil
}
