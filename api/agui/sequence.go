package agui

import (
	"fmt"
)

// Sequence checks the Hub's explicit start/content/end producer profile. The
// public protocol also permits an initial RUN_ERROR and late failures after a
// finished run; this Hub profile uses HTTP errors before start and one terminal
// event once started. It does not resume a broken SSE exchange.
// One serialized publisher must own a Sequence.
type Sequence struct {
	threadID string
	runID    string
	started  bool
	terminal bool
	messages map[string]bool
	tools    map[string]int
	steps    map[string]bool
}

// NewSequence binds one publisher to the requested thread and run.
func NewSequence(threadID, runID string) *Sequence {
	return &Sequence{threadID: threadID, runID: runID, messages: map[string]bool{}, tools: map[string]int{}, steps: map[string]bool{}}
}

// Accept validates one event and advances its ordering state only on success.
func (s *Sequence) Accept(raw []byte) error {
	validator, err := NewValidator()
	if err != nil {
		return err
	}
	event, err := validator.Event(raw)
	if err != nil {
		return err
	}
	kind := event["type"].(string)
	if s.terminal {
		return fmt.Errorf("event after the run terminal: %s", kind)
	}
	if !s.started && kind != "RUN_STARTED" {
		return fmt.Errorf("first event must be RUN_STARTED")
	}
	switch kind {
	case "RUN_STARTED":
		if s.started {
			return fmt.Errorf("run already started")
		}
		if event["threadId"] != s.threadID || event["runId"] != s.runID {
			return fmt.Errorf("run identity mismatch")
		}
		s.started = true
	case "RUN_FINISHED":
		if event["threadId"] != s.threadID || event["runId"] != s.runID {
			return fmt.Errorf("run identity mismatch")
		}
		for _, open := range s.messages {
			if open {
				return fmt.Errorf("text message still open")
			}
		}
		for _, phase := range s.tools {
			if phase == 1 {
				return fmt.Errorf("tool arguments still open")
			}
		}
		if len(s.steps) > 0 {
			return fmt.Errorf("step still open")
		}
		s.terminal = true
	case "RUN_ERROR":
		s.terminal = true
	case "TEXT_MESSAGE_CHUNK", "TOOL_CALL_CHUNK":
		return fmt.Errorf("Hub profile requires explicit start/content/end events")
	case "MESSAGES_SNAPSHOT":
		for _, open := range s.messages {
			if open {
				return fmt.Errorf("snapshot while a text message is open")
			}
		}
		ids := map[string]bool{}
		for _, raw := range event["messages"].([]any) {
			id := raw.(map[string]any)["id"].(string)
			if ids[id] {
				return fmt.Errorf("duplicate snapshot message id")
			}
			ids[id] = true
		}
		for id := range ids {
			s.messages[id] = false
		}
	case "TEXT_MESSAGE_START":
		id := event["messageId"].(string)
		if _, exists := s.messages[id]; exists {
			return fmt.Errorf("message id already used")
		}
		s.messages[id] = true
	case "TEXT_MESSAGE_CONTENT":
		if !s.messages[event["messageId"].(string)] {
			return fmt.Errorf("text content without an open message")
		}
	case "TEXT_MESSAGE_END":
		id := event["messageId"].(string)
		if !s.messages[id] {
			return fmt.Errorf("text end without an open message")
		}
		s.messages[id] = false
	case "TOOL_CALL_START":
		id := event["toolCallId"].(string)
		if _, exists := s.tools[id]; exists {
			return fmt.Errorf("tool call id already used")
		}
		s.tools[id] = 1
	case "TOOL_CALL_ARGS":
		if s.tools[event["toolCallId"].(string)] != 1 {
			return fmt.Errorf("tool arguments without an open call")
		}
	case "TOOL_CALL_END":
		id := event["toolCallId"].(string)
		if s.tools[id] != 1 {
			return fmt.Errorf("tool end without an open call")
		}
		s.tools[id] = 2
	case "TOOL_CALL_RESULT":
		id := event["toolCallId"].(string)
		if s.tools[id] != 2 {
			return fmt.Errorf("tool result before end or already answered")
		}
		messageID := event["messageId"].(string)
		if _, exists := s.messages[messageID]; exists {
			return fmt.Errorf("message id already used")
		}
		s.messages[messageID] = false
		s.tools[id] = 3
	case "STEP_STARTED":
		name := event["stepName"].(string)
		if s.steps[name] {
			return fmt.Errorf("step already open")
		}
		s.steps[name] = true
	case "STEP_FINISHED":
		name := event["stepName"].(string)
		if !s.steps[name] {
			return fmt.Errorf("step finished without start")
		}
		delete(s.steps, name)
	}
	return nil
}

// Complete reports receipt of the one terminal event. A truncated stream is
// incomplete even when its HTTP status was 200.
func (s *Sequence) Complete() bool { return s.terminal }
