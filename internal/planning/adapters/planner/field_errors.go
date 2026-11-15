package planner

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

func parseFieldErrors(raw []byte, questions []domain.Question) ([]domain.Question, bool, error) {
	for _, candidate := range replyObjects(raw) {
		var object map[string]json.RawMessage
		if json.Unmarshal(candidate, &object) != nil {
			continue
		}
		data, present := object["fieldErrors"]
		if !present {
			continue
		}
		invalid := errors.New("planning fieldErrors must identify existing questions with bounded nonempty messages, without a plan")
		for key := range object {
			if key != "fieldErrors" && key != "clientToolCalls" {
				return nil, true, invalid
			}
		}
		var fields []struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&fields) != nil || len(fields) == 0 || len(fields) > maxQuestions {
			return nil, true, invalid
		}
		known := map[string]int{}
		out := append([]domain.Question(nil), questions...)
		for index, question := range questions {
			known[question.ID] = index
		}
		seen := map[string]bool{}
		for _, field := range fields {
			index, ok := known[field.ID]
			message := strings.TrimSpace(field.Message)
			if !ok || seen[field.ID] || message == "" || len(message) > 4096 {
				return nil, true, invalid
			}
			seen[field.ID] = true
			out[index].ValidationError = message
		}
		return out, true, nil
	}
	return nil, false, nil
}
