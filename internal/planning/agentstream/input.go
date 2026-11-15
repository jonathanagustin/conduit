package agentstream

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/jonathanagustin/conduit/api/agui"
)

var (
	ErrInvalidInput     = errors.New("the agent run input was rejected")
	ErrRunExists        = errors.New("the run ID has already been used")
	ErrRunBusy          = errors.New("another run is active for this thread")
	ErrAuditUnavailable = errors.New("the run audit could not be written")
	ErrRunCapacity      = errors.New("the agent run capacity was reached")
	ErrInvalidResume    = errors.New("resume must answer the current input form")
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

// RunInput is the Hub projection of the pinned public input. State and history
// are grounding data, never authority to change a planning session or approval.
type RunInput struct {
	ThreadID        string                     `json:"threadId"`
	RunID           string                     `json:"runId"`
	ProtocolVersion string                     `json:"protocolVersion,omitempty"`
	Messages        []map[string]any           `json:"messages"`
	Tools           []ToolOffer                `json:"tools,omitempty"`
	Resume          []ResumeEntry              `json:"resume,omitempty"`
	Context         []map[string]any           `json:"context,omitempty"`
	State           json.RawMessage            `json:"state,omitempty"`
	ForwardedProps  map[string]json.RawMessage `json:"forwardedProps,omitempty"`
}

// ResumeEntry answers the one current form interrupt, using the public values.
type ResumeEntry struct {
	InterruptID string          `json:"interruptId"`
	Status      string          `json:"status"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

// DecodeRunInput applies public shape validation before native resource bounds.
func DecodeRunInput(raw []byte) (RunInput, error) {
	validator, err := agui.NewValidator()
	if err != nil {
		return RunInput{}, err
	}
	if _, err := validator.Input(raw); err != nil {
		return RunInput{}, ErrInvalidInput
	}
	var input RunInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&input); err != nil {
		return input, ErrInvalidInput
	}
	if !identifier.MatchString(input.ThreadID) || !identifier.MatchString(input.RunID) || (input.ProtocolVersion != "" && input.ProtocolVersion != agui.Version) || len(input.Messages) > 256 || len(input.Context) > 32 || len(input.Resume) > 1 {
		return input, ErrInvalidInput
	}
	for _, item := range input.Context {
		encoded, _ := json.Marshal(item)
		if len(encoded) > 8<<10 {
			return input, ErrInvalidInput
		}
	}
	return input, nil
}

func (input RunInput) snapshotOnly() bool {
	raw := input.ForwardedProps["conduit"]
	if len(raw) == 0 {
		return false
	}
	var options struct {
		Mode string `json:"mode"`
	}
	return json.Unmarshal(raw, &options) == nil && options.Mode == "snapshot"
}

// lastUserText keeps text parts and ignores unsupported media without fetching
// URLs or resolving opaque provider handles.
func (input RunInput) lastUserText() (id, text string) {
	for index := len(input.Messages) - 1; index >= 0; index-- {
		message := input.Messages[index]
		if message["role"] != "user" {
			continue
		}
		id, _ = message["id"].(string)
		switch content := message["content"].(type) {
		case string:
			return id, strings.TrimSpace(content)
		case []any:
			parts := []string{}
			for _, part := range content {
				if object, ok := part.(map[string]any); ok && object["type"] == "text" {
					if value, ok := object["text"].(string); ok {
						parts = append(parts, value)
					}
				}
			}
			return id, strings.TrimSpace(strings.Join(parts, "\n"))
		}
		return id, ""
	}
	return "", ""
}

func (input RunInput) grounding(fields map[string]Field) string {
	messages := []map[string]any{}
	for _, original := range input.Messages {
		message := map[string]any{}
		for key, value := range original {
			message[key] = value
		}
		if parts, ok := message["content"].([]any); ok {
			text := []string{}
			for _, part := range parts {
				if object, ok := part.(map[string]any); ok && object["type"] == "text" {
					if value, ok := object["text"].(string); ok {
						text = append(text, value)
					}
				}
			}
			message["content"] = strings.Join(text, "\n")
		}
		messages = append(messages, message)
	}
	raw, _ := json.Marshal(map[string]any{"messages": messages, "context": input.Context, "clientState": input.State, "frontendTools": input.Tools, "fields": fields})
	return string(raw)
}
