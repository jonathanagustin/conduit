package agui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPinnedPrimaryBytes(t *testing.T) {
	var manifest struct {
		Version string `json:"protocolVersion"`
		Files   []struct {
			File string `json:"file"`
			SHA  string `json:"sha256"`
		} `json:"files"`
	}
	data, err := os.ReadFile("upstream/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != Version || len(manifest.Files) != 2 {
		t.Fatal("unexpected source manifest")
	}
	for _, file := range manifest.Files {
		data, err := os.ReadFile("upstream/" + file.File)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != file.SHA {
			t.Fatalf("source bytes changed: %s", file.File)
		}
	}
}

func TestPublicInputContract(t *testing.T) {
	validator, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"threadId":"thread-1","runId":"run-1","protocolVersion":"1.0","messages":[]}`,
		`{"threadId":"thread-1","runId":"run-2","messages":[],"resume":[{"interruptId":"question-1","status":"resolved","payload":"answer"}]}`,
	} {
		if _, err := validator.Input([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{
		`{"threadId":"thread-1","messages":[]}`,
		`{"threadId":"t","runId":"r","messages":[],"resume":[{"interruptId":"q","status":"answered","payload":"value"}]}`,
		`{"threadId":"thread-1","runId":"run-2","messages":[],"resume":[{"interruptId":"question-1","payload":"answer"}]}`,
		`{"threadId":"a","threadId":"b","runId":"run-1","messages":[]}`,
		`{"threadId":"a","runId":"r","messages":[],"state":{"x":1,"x":2}}`,
		`{"threadId":"a","runId":"r","messages":[]} {}`,
		strings.Repeat("[", 66) + strings.Repeat("]", 66),
	} {
		if _, err := validator.Input([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid envelope: %s", raw)
		}
	}
}

func TestPublicEventContract(t *testing.T) {
	validator, err := NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"type":"RUN_STARTED","threadId":"thread-1","runId":"run-1","protocolVersion":"1.0"}`,
		`{"type":"RUN_FINISHED","threadId":"thread-1","runId":"run-1","outcome":{"type":"interrupt","interrupts":[{"id":"q-1","reason":"needs_input","message":"Choose a namespace"}]}}`,
		`{"type":"RUN_ERROR","message":"The run failed","code":"MODEL_ERROR"}`,
		`{"type":"STATE_DELTA","delta":[{"op":"replace","path":"/status","value":"needs_input"}]}`,
	} {
		if _, err := validator.Event([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{
		`{"type":"RUN_FINISHED","threadId":"t","runId":"r","outcome":"interrupt"}`,
		`{"type":"RUN_FINISHED","threadId":"t","runId":"r","outcome":{"type":"interrupt","interrupts":[]}}`,
		`{"type":"TEXT_MESSAGE_CONTENT","delta":"missing-message-id"}`,
		`{"type":"RUN_ERROR"}`,
	} {
		if _, err := validator.Event([]byte(raw)); err == nil {
			t.Fatalf("accepted invalid event: %s", raw)
		}
	}
}
