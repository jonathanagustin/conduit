package agentstream

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/jonathanagustin/conduit/api/agui"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

type runObserver struct {
	service  *Service
	state    *thread
	input    RunInput
	policy   *ToolPolicy
	output   *publisher
	calls    map[string]string
	next     int
	session  *domain.Session
	frontend []pendingTool
}

func (observer *runObserver) Started(ctx context.Context, name string, arguments json.RawMessage) (string, error) {
	if name != "inspect_repo" && name != "read_file" {
		if err := observer.service.audit.Record(ctx, AuditRecord{ThreadID: observer.input.ThreadID, RunID: observer.input.RunID, Action: "agent.tool_denied", PayloadSHA256: digest(arguments)}); err != nil {
			return "", ErrAuditUnavailable
		}
		return "", ErrToolNotOffered
	}
	return observer.start(ctx, name, arguments, "hub")
}

func (observer *runObserver) start(ctx context.Context, name string, arguments json.RawMessage, execution string) (string, error) {
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	if len(arguments) > 8<<10 {
		return "", ErrInvalidToolArguments
	}
	if _, err := jsonObject(arguments); err != nil {
		return "", ErrInvalidToolArguments
	}
	observer.next++
	id := observer.input.RunID + ":tool:" + strconv.Itoa(observer.next)
	parentID := "conduit:" + id + ":request"
	for _, event := range []map[string]any{
		{"type": "TEXT_MESSAGE_START", "messageId": parentID, "role": "assistant"},
		{"type": "TOOL_CALL_START", "toolCallId": id, "toolCallName": name, "parentMessageId": parentID},
		{"type": "TOOL_CALL_ARGS", "toolCallId": id, "delta": string(arguments)},
		{"type": "TOOL_CALL_END", "toolCallId": id},
		{"type": "TEXT_MESSAGE_END", "messageId": parentID},
	} {
		if err := observer.output.send(ctx, event); err != nil {
			return "", err
		}
	}
	observer.calls[id] = name
	observer.service.mu.Lock()
	observer.state.toolCalls = append(observer.state.toolCalls, map[string]any{"id": id, "name": name, "status": "running", "execution": execution})
	observer.service.appendHistoryLocked(observer.state, map[string]any{"id": parentID, "role": "assistant", "toolCalls": []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": name, "arguments": string(arguments)}}}})
	observer.service.mu.Unlock()
	return id, nil
}

func (observer *runObserver) Finished(ctx context.Context, id string, succeeded bool) error {
	if observer.calls[id] == "" {
		return ErrInvalidToolArguments
	}
	content, _ := json.Marshal(map[string]any{"ok": succeeded})
	if err := observer.output.send(ctx, map[string]any{"type": "TOOL_CALL_RESULT", "toolCallId": id, "messageId": "conduit:" + id + ":result", "content": string(content)}); err != nil {
		return err
	}
	observer.service.mu.Lock()
	observer.service.appendHistoryLocked(observer.state, map[string]any{"id": "conduit:" + id + ":result", "role": "tool", "toolCallId": id, "content": string(content)})
	for _, call := range observer.state.toolCalls {
		if call["id"] == id {
			status := "failed"
			if succeeded {
				status = "succeeded"
			}
			call["status"] = status
		}
	}
	observer.service.mu.Unlock()
	delete(observer.calls, id)
	return nil
}

func jsonObject(raw []byte) (map[string]any, error) {
	value, err := agui.DecodeJSON(raw)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, ErrInvalidToolArguments
	}
	return object, nil
}

func (observer *runObserver) QuestionsObserved(_ context.Context, questions []domain.Question) error {
	view := domain.CloneSession(observer.session)
	view.NeedsInput = questions
	policy, err := NewToolPolicy(observer.input.Tools, fieldsFor(view))
	if err != nil {
		return err
	}
	observer.policy = policy
	return nil
}

func (observer *runObserver) Call(ctx context.Context, name string, arguments json.RawMessage) error {
	if _, err := observer.policy.Validate(name, arguments); err != nil {
		payload, _ := json.Marshal(map[string]any{"name": name, "arguments": arguments})
		if auditErr := observer.service.audit.Record(ctx, AuditRecord{ThreadID: observer.input.ThreadID, RunID: observer.input.RunID, Action: "agent.tool_denied", PayloadSHA256: digest(payload)}); auditErr != nil {
			return ErrAuditUnavailable
		}
		return err
	}
	id, err := observer.start(ctx, name, arguments, "client")
	if err != nil {
		return err
	}
	observer.frontend = append(observer.frontend, pendingTool{ID: id, RunID: observer.input.RunID, Name: name, Arguments: append(json.RawMessage(nil), arguments...)})
	return nil
}

func (observer *runObserver) interrupted() {
	observer.service.mu.Lock()
	defer observer.service.mu.Unlock()
	for id := range observer.calls {
		if pending, exists := observer.state.pending[id]; exists {
			if !pending.Eligible {
				for _, call := range observer.state.toolCalls {
					if call["id"] == id {
						call["status"] = "interrupted"
					}
				}
			}
			continue
		}
		content := `{"ok":false,"code":"RUN_INTERRUPTED"}`
		observer.service.appendHistoryLocked(observer.state, map[string]any{"id": "conduit:" + id + ":result", "role": "tool", "toolCallId": id, "content": content})
		for _, call := range observer.state.toolCalls {
			if call["id"] == id {
				call["status"] = "interrupted"
			}
		}
	}
}
