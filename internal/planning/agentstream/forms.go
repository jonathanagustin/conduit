package agentstream

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/api/a2ui"
	"github.com/jonathanagustin/conduit/api/agui"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

func pointerPart(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
func formSurface(session *domain.Session, form *inputForm, surfaceID string) (map[string]any, error) {
	if len(session.NeedsInput) > 60 {
		return nil, ErrRunCapacity
	}
	values := map[string]any{}
	answers := map[string]string{}
	for _, answer := range session.Answers {
		answers[answer.ID] = answer.Value
	}
	components := []any{}
	children := []string{}
	actionContext := map[string]any{}
	for _, question := range session.NeedsInput {
		if question.ID == "__proto__" || question.ID == "prototype" || question.ID == "constructor" {
			return nil, ErrInvalidToolArguments
		}
		id := "question:" + question.ID
		path := "/answers/" + pointerPart(question.ID)
		binding := map[string]any{"@path": path}
		kind := "TextField"
		value := question.Value
		if answer, ok := answers[question.ID]; ok {
			value = answer
		}
		values[question.ID] = value
		if question.Type == domain.QuestionBoolean {
			kind = "CheckBox"
			values[question.ID] = value == "true"
		}
		if question.Type == domain.QuestionSelect && len(question.Options) > 0 {
			kind = "Select"
		}
		label := question.Label
		if label == "" {
			label = question.Name
		}
		if label == "" {
			label = question.ID
		}
		component := map[string]any{"id": id, "component": kind, "label": label, "value": binding}
		checks := []any{}
		if question.Required {
			checks = append(checks, map[string]any{"condition": map[string]any{"@call": "required", "args": map[string]any{"value": binding}}, "message": "Enter a value."})
		}
		if kind == "Select" {
			component["options"] = question.Options
			checks = append(checks, map[string]any{"condition": map[string]any{"@call": "enum", "args": map[string]any{"value": binding, "options": question.Options}}, "message": "Choose one of the offered values."})
		}
		component["@checks"] = checks
		components = append(components, component)
		children = append(children, id)
		actionContext[id] = binding
	}
	children = append(children, "submit")
	components = append([]any{map[string]any{"id": "root", "component": "Column", "children": children}}, components...)
	components = append(components, map[string]any{"id": "submit", "component": "Button", "label": "Submit answers", "action": map[string]any{"event": map[string]any{"name": "submit_needs_input", "context": actionContext}}})
	return map[string]any{"version": "1.0", "createSurface": map[string]any{"surfaceId": surfaceID, "catalogId": a2ui.CatalogID, "components": components, "dataModel": map[string]any{"answers": values}}}, nil
}

func (service *Service) formSnapshot(ctx context.Context, output *publisher, state *thread, session *domain.Session) error {
	form := currentForm(session)
	if form != nil && output.surfaceFormID == form.id {
		return nil
	}
	if output.surfaceID != "" {
		if err := output.send(ctx, map[string]any{"type": "CUSTOM", "name": "a2ui", "value": map[string]any{"version": "1.0", "deleteSurface": map[string]any{"surfaceId": output.surfaceID}}}); err != nil {
			return err
		}
		output.surfaceID = ""
		output.surfaceFormID = ""
	}
	if form == nil {
		return nil
	}
	surfaceID := form.id + ":" + output.runID
	message, err := formSurface(session, form, surfaceID)
	if err != nil {
		return err
	}
	validator, err := a2ui.NewNativeValidator()
	if err != nil {
		return err
	}
	update := map[string]any{"version": "1.0", "updateDataModel": map[string]any{"surfaceId": surfaceID, "path": "/", "value": message["createSurface"].(map[string]any)["dataModel"]}}
	for _, prefill := range []map[string]any{message, update} {
		raw, err := json.Marshal(prefill)
		if err != nil {
			return err
		}
		if _, err := validator.Agent(raw); err != nil {
			return ErrInvalidToolArguments
		}
		if err := service.audit.Record(ctx, AuditRecord{ThreadID: output.threadID, RunID: output.runID, Action: "agent.a2ui_prefill", PayloadSHA256: digest(raw)}); err != nil {
			return ErrAuditUnavailable
		}
		if err := output.send(ctx, map[string]any{"type": "CUSTOM", "name": "a2ui", "value": prefill}); err != nil {
			return err
		}
	}
	output.surfaceID = surfaceID
	output.surfaceFormID = form.id
	service.mu.Lock()
	state.surfaceID = surfaceID
	state.surfaceFormID = form.id
	service.mu.Unlock()
	return nil
}
func inputAction(input RunInput) json.RawMessage {
	var options struct {
		Action json.RawMessage `json:"a2uiAction"`
	}
	if json.Unmarshal(input.ForwardedProps["conduit"], &options) != nil {
		return nil
	}
	return options.Action
}
func (service *Service) validateFormAction(input RunInput, state *thread, form *inputForm) error {
	raw := inputAction(input)
	if len(raw) == 0 {
		return nil
	}
	if len(raw) > 65536 || form == nil || len(input.Resume) != 1 || input.Resume[0].Status != "resolved" {
		return ErrInvalidResume
	}
	validator, err := a2ui.NewNativeValidator()
	if err != nil {
		return err
	}
	message, err := validator.Renderer(raw)
	if err != nil {
		return ErrInvalidResume
	}
	action, ok := message["action"].(map[string]any)
	if !ok {
		return ErrInvalidResume
	}
	service.mu.Lock()
	surfaceID, surfaceFormID := state.surfaceID, state.surfaceFormID
	service.mu.Unlock()
	if surfaceID == "" || surfaceFormID != form.id || action["surfaceId"] != surfaceID || action["sourceComponentId"] != "submit" || action["name"] != "submit_needs_input" {
		return ErrInvalidResume
	}
	timestamp, ok := action["timestamp"].(string)
	if !ok {
		return ErrInvalidResume
	}
	if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
		return ErrInvalidResume
	}
	values, ok := action["context"].(map[string]any)
	if !ok {
		return ErrInvalidResume
	}
	normalized := map[string]any{}
	for key, value := range values {
		question, ok := form.questions[key]
		if !ok {
			return ErrInvalidResume
		}
		if boolean, ok := value.(bool); ok && question.Type == domain.QuestionBoolean {
			normalized[key] = strconv.FormatBool(boolean)
			continue
		}
		text, ok := value.(string)
		if !ok {
			return ErrInvalidResume
		}
		normalized[key] = text
	}
	payload, err := agui.DecodeJSON(input.Resume[0].Payload)
	if err != nil || !reflect.DeepEqual(normalized, payload) {
		return ErrInvalidResume
	}
	return nil
}
