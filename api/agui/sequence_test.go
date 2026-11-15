package agui

import "testing"

const started = `{"type":"RUN_STARTED","threadId":"t","runId":"r","protocolVersion":"1.0"}`
const finished = `{"type":"RUN_FINISHED","threadId":"t","runId":"r"}`

func TestOrderedTextAndToolExchange(t *testing.T) {
	sequence := NewSequence("t", "r")
	for _, event := range []string{started,
		`{"type":"STATE_SNAPSHOT","snapshot":{"status":"inspecting"}}`,
		`{"type":"STEP_STARTED","stepName":"inspect"}`,
		`{"type":"TOOL_CALL_START","toolCallId":"c","toolCallName":"inspect_repo"}`,
		`{"type":"TOOL_CALL_ARGS","toolCallId":"c","delta":"{}"}`,
		`{"type":"TOOL_CALL_END","toolCallId":"c"}`,
		`{"type":"TOOL_CALL_RESULT","toolCallId":"c","messageId":"tool-result","content":"inspected"}`,
		`{"type":"STEP_FINISHED","stepName":"inspect"}`,
		`{"type":"TEXT_MESSAGE_START","messageId":"m","role":"assistant"}`,
		`{"type":"TEXT_MESSAGE_CONTENT","messageId":"m","delta":"Plan ready"}`,
		`{"type":"TEXT_MESSAGE_END","messageId":"m"}`, finished,
	} {
		if err := sequence.Accept([]byte(event)); err != nil {
			t.Fatalf("%s: %v", event, err)
		}
	}
	if !sequence.Complete() {
		t.Fatal("terminal was not recorded")
	}
	if err := sequence.Accept([]byte(finished)); err == nil {
		t.Fatal("accepted a second terminal")
	}
}

func TestOutOfOrderEventsAreRejected(t *testing.T) {
	cases := []struct {
		name   string
		prefix []string
		event  string
	}{
		{"before start", nil, finished},
		{"identity", nil, `{"type":"RUN_STARTED","threadId":"other","runId":"r"}`},
		{"duplicate start", []string{started}, started},
		{"content before start", []string{started}, `{"type":"TEXT_MESSAGE_CONTENT","messageId":"m","delta":"bad"}`},
		{"end before start", []string{started}, `{"type":"TEXT_MESSAGE_END","messageId":"m"}`},
		{"args before call", []string{started}, `{"type":"TOOL_CALL_ARGS","toolCallId":"c","delta":"{}"}`},
		{"result before end", []string{started, `{"type":"TOOL_CALL_START","toolCallId":"c","toolCallName":"fill_field"}`}, `{"type":"TOOL_CALL_RESULT","toolCallId":"c","messageId":"result","content":"bad"}`},
		{"open text", []string{started, `{"type":"TEXT_MESSAGE_START","messageId":"m","role":"assistant"}`}, finished},
		{"open tool", []string{started, `{"type":"TOOL_CALL_START","toolCallId":"c","toolCallName":"fill_field"}`}, finished},
		{"open step", []string{started, `{"type":"STEP_STARTED","stepName":"inspect"}`}, finished},
		{"finish identity", []string{started}, `{"type":"RUN_FINISHED","threadId":"other","runId":"r"}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			sequence := NewSequence("t", "r")
			for _, event := range test.prefix {
				if err := sequence.Accept([]byte(event)); err != nil {
					t.Fatal(err)
				}
			}
			if err := sequence.Accept([]byte(test.event)); err == nil {
				t.Fatal("out-of-order event accepted")
			}
			if sequence.Complete() {
				t.Fatal("rejection falsely completed the stream")
			}
		})
	}
}

func TestTruncationAndFailure(t *testing.T) {
	sequence := NewSequence("t", "r")
	if err := sequence.Accept([]byte(started)); err != nil {
		t.Fatal(err)
	}
	if sequence.Complete() {
		t.Fatal("HTTP/open stream is not success")
	}
	if err := sequence.Accept([]byte(`{"type":"RUN_ERROR","message":"model failed"}`)); err != nil {
		t.Fatal(err)
	}
	if !sequence.Complete() {
		t.Fatal("failure did not terminate")
	}
	if err := sequence.Accept([]byte(finished)); err == nil {
		t.Fatal("failure followed by success accepted")
	}
}

func TestSnapshotIDsCannotBeReusedByNewText(t *testing.T) {
	sequence := NewSequence("t", "r")
	for _, event := range []string{started, `{"type":"MESSAGES_SNAPSHOT","messages":[{"id":"old","role":"assistant","content":"previous"}]}`} {
		if err := sequence.Accept([]byte(event)); err != nil {
			t.Fatal(err)
		}
	}
	if err := sequence.Accept([]byte(`{"type":"TEXT_MESSAGE_START","messageId":"old","role":"assistant"}`)); err == nil {
		t.Fatal("snapshot message id reused")
	}
	if err := sequence.Accept([]byte(`{"type":"MESSAGES_SNAPSHOT","messages":[{"id":"x","role":"user","content":"one"},{"id":"x","role":"user","content":"two"}]}`)); err == nil {
		t.Fatal("ambiguous snapshot accepted")
	}
}
