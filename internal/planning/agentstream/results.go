package agentstream

import (
	"context"
	"encoding/json"
)

type pendingTool struct {
	ID        string          `json:"id"`
	RunID     string          `json:"runId"`
	Eligible  bool            `json:"eligible"`
	Order     int             `json:"order"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type clientFeedback struct {
	Changed  bool
	Declined bool
}

func (service *Service) clientResults(ctx context.Context, state *thread, input RunInput) (clientFeedback, error) {
	feedback := clientFeedback{}
	for _, message := range input.Messages {
		if message["role"] != "tool" {
			continue
		}
		id, _ := message["toolCallId"].(string)
		service.mu.Lock()
		tool, pending := state.pending[id]
		oldHash, acknowledged := state.acknowledged[id]
		service.mu.Unlock()
		if !pending && !acknowledged {
			continue
		}
		text, ok := message["content"].(string)
		if !ok || len(text) > 4096 {
			return feedback, ErrInvalidToolArguments
		}
		hash := digest([]byte(text))
		if acknowledged {
			if hash != oldHash {
				return feedback, ErrInvalidToolArguments
			}
			continue
		}
		result, err := jsonObject([]byte(text))
		if err != nil {
			return feedback, ErrInvalidToolArguments
		}
		succeeded, ok := result["ok"].(bool)
		if !ok {
			return feedback, ErrInvalidToolArguments
		}
		for key := range result {
			if key != "ok" && key != "code" && key != "confirmed" {
				return feedback, ErrInvalidToolArguments
			}
		}
		if tool.Name == "request_confirmation" && succeeded {
			if _, ok := result["confirmed"].(bool); !ok {
				return feedback, ErrInvalidToolArguments
			}
		}
		if code, exists := result["code"]; exists {
			value, ok := code.(string)
			if !ok || len(value) > 64 {
				return feedback, ErrInvalidToolArguments
			}
		}
		if err := service.audit.Record(ctx, AuditRecord{ThreadID: input.ThreadID, RunID: input.RunID, Action: "agent.tool_result", PayloadSHA256: hash}); err != nil {
			return feedback, ErrAuditUnavailable
		}
		service.mu.Lock()
		delete(state.pending, id)
		state.acknowledged[id] = hash
		service.appendHistoryLocked(state, map[string]any{"id": "conduit:" + id + ":result", "role": "tool", "toolCallId": id, "content": text})
		for _, call := range state.toolCalls {
			if call["id"] == id {
				status := "failed"
				if succeeded {
					status = "succeeded"
				}
				call["status"] = status
			}
		}
		service.mu.Unlock()
		feedback.Changed = true
		if tool.Name == "request_confirmation" && succeeded && result["confirmed"] == false {
			feedback.Declined = true
		}
	}
	return feedback, nil
}
