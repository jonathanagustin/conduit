package agentstream

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/jonathanagustin/conduit/api/agui"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

type inputForm struct {
	id        string
	schema    map[string]any
	questions map[string]domain.Question
	order     []string
}

func currentForm(session *domain.Session) *inputForm {
	if !domain.PendingRequired(session.NeedsInput, session.Answers) {
		return nil
	}
	bytes, _ := json.Marshal(struct {
		ID, Commit string
		Questions  []domain.Question
		Answers    []domain.Answer
	}{session.ID, session.ResolvedCommit, session.NeedsInput, session.Answers})
	digest := sha256.Sum256(bytes)
	form := &inputForm{id: "needs-input:" + hex.EncodeToString(digest[:16]), questions: map[string]domain.Question{}}
	properties := map[string]any{}
	required := []string{}
	for _, question := range session.NeedsInput {
		key := "question:" + question.ID
		item := map[string]any{"type": "string", "maxLength": 4096}
		if question.Type == domain.QuestionBoolean {
			item["enum"] = []string{"true", "false"}
		}
		if len(question.Options) > 0 {
			item["enum"] = question.Options
		}
		properties[key] = item
		form.questions[key] = question
		form.order = append(form.order, key)
		if question.Required || question.ValidationError != "" {
			required = append(required, key)
			item["minLength"] = 1
		}
	}
	form.schema = map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	return form
}

func (form *inputForm) answers(entries []ResumeEntry) ([]domain.Answer, bool, error) {
	if len(entries) != 1 || entries[0].InterruptID != form.id {
		return nil, false, ErrInvalidResume
	}
	entry := entries[0]
	if entry.Status == "cancelled" {
		return nil, true, nil
	}
	if entry.Status != "resolved" {
		return nil, false, ErrInvalidResume
	}
	value, err := agui.DecodeJSON(entry.Payload)
	if err != nil {
		return nil, false, ErrInvalidResume
	}
	schema, err := compileArguments("resume", form.schema)
	if err != nil {
		return nil, false, ErrInvalidResume
	}
	if err := schema.Validate(value); err != nil {
		return nil, false, ErrInvalidResume
	}
	object := value.(map[string]any)
	answers := []domain.Answer{}
	for _, key := range form.order {
		question := form.questions[key]
		if text, exists := object[key]; exists {
			answers = append(answers, domain.Answer{ID: question.ID, Value: text.(string)})
		}
	}
	return answers, false, nil
}

func fieldsFor(session *domain.Session) map[string]Field {
	fields := map[string]Field{"repoUrl": {Type: "string", ReadOnly: true}, "ref": {Type: "string", ReadOnly: true}}
	pending := domain.PendingRequired(session.NeedsInput, session.Answers)
	for _, question := range session.NeedsInput {
		field := Field{Type: "string", Choices: question.Options, ReadOnly: !pending}
		if question.Type == domain.QuestionBoolean {
			field.Type = "boolean"
			field.Choices = nil
		}
		fields["question:"+question.ID] = field
	}
	return fields
}
