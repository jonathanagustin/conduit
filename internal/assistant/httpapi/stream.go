package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/api/agui"
	"github.com/jonathanagustin/conduit/internal/assistant/application"
)

// streamLimit bounds one streamed turn. The model adapters have their own,
// shorter timeouts; this is the ceiling on the whole exchange.
const streamLimit = 10 * time.Minute

// keepAlive is how often an idle stream writes an SSE comment, so a proxy
// does not close it during one long model call.
var keepAlive = 15 * time.Second

// wantsEventStream reports whether the caller asked for the streamed turn.
// Anything else, including no Accept header, gets the JSON document.
func wantsEventStream(request *http.Request) bool {
	for _, part := range strings.Split(request.Header.Get("Accept"), ",") {
		if mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(part)); err == nil && mediaType == "text/event-stream" {
			return true
		}
	}
	return false
}

// stream writes AG-UI events for one turn, in the Hub's producer profile:
// explicit start/content/end events and exactly one terminal event. Every
// event passes agui.Sequence before it is written, so an event the contract
// would refuse is never sent. It reports what has happened, at the grain the
// model port has: there is no token stream to forward.
type stream struct {
	writer   http.ResponseWriter
	control  *http.ResponseController
	threadID string
	runID    string

	mu       sync.Mutex
	sequence *agui.Sequence
	started  bool
	step     string
}

func (out *stream) send(event map[string]any) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	out.mu.Lock()
	defer out.mu.Unlock()
	next, err := out.sequence.Next(raw)
	if err != nil {
		return err
	}
	if !out.started {
		header := out.writer.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-store")
		header.Set("X-Accel-Buffering", "no")
		header.Set("X-Content-Type-Options", "nosniff")
		out.writer.WriteHeader(http.StatusOK)
		out.started = true
	}
	if _, err := out.writer.Write(append(append([]byte("data: "), raw...), '\n', '\n')); err != nil {
		return err
	}
	if err := out.control.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	out.sequence = next
	return nil
}

func (out *stream) closeStep() error {
	if out.step == "" {
		return nil
	}
	name := out.step
	out.step = ""
	return out.send(map[string]any{"type": "STEP_FINISHED", "stepName": name})
}

func (out *stream) RoundStarted(_ context.Context, round int) error {
	if round == 1 {
		if err := out.send(map[string]any{"type": "RUN_STARTED", "threadId": out.threadID, "runId": out.runID, "protocolVersion": agui.Version}); err != nil {
			return err
		}
	}
	if err := out.closeStep(); err != nil {
		return err
	}
	out.step = fmt.Sprintf("round-%d", round)
	return out.send(map[string]any{"type": "STEP_STARTED", "stepName": out.step})
}

func (out *stream) ToolStarted(_ context.Context, call application.ToolCall) error {
	for _, event := range []map[string]any{
		{"type": "TOOL_CALL_START", "toolCallId": call.ID, "toolCallName": call.Name},
		{"type": "TOOL_CALL_ARGS", "toolCallId": call.ID, "delta": string(call.Arguments)},
		{"type": "TOOL_CALL_END", "toolCallId": call.ID},
	} {
		if err := out.send(event); err != nil {
			return err
		}
	}
	return nil
}

func (out *stream) ToolFinished(_ context.Context, call application.ToolCall, result application.Result) error {
	content, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return out.send(map[string]any{"type": "TOOL_CALL_RESULT", "toolCallId": call.ID, "messageId": call.ID + ":result", "content": string(content)})
}

// finish writes the answer and the terminal event. result is the same
// document the JSON turn returns.
func (out *stream) finish(turn application.Turn) error {
	if err := out.closeStep(); err != nil {
		return err
	}
	if turn.Text != "" {
		id := out.runID + ":answer"
		for _, event := range []map[string]any{
			{"type": "TEXT_MESSAGE_START", "messageId": id, "role": "assistant"},
			{"type": "TEXT_MESSAGE_CONTENT", "messageId": id, "delta": turn.Text},
			{"type": "TEXT_MESSAGE_END", "messageId": id},
		} {
			if err := out.send(event); err != nil {
				return err
			}
		}
	}
	return out.send(map[string]any{"type": "RUN_FINISHED", "threadId": out.threadID, "runId": out.runID, "result": turn})
}

func (out *stream) begun() bool {
	out.mu.Lock()
	defer out.mu.Unlock()
	return out.started
}

// streamTurn serves one turn as server-sent events. A failure before the
// first event is an ordinary JSON error; after it, the failure is RUN_ERROR.
// The turn runs on the request context, so a client that goes away cancels
// the model call in flight.
func streamTurn(tools Tools, caller application.Actor, turn application.TurnRequest, writer http.ResponseWriter, request *http.Request) {
	entropy := make([]byte, 8)
	if _, err := rand.Read(entropy); err != nil {
		writeError(writer, http.StatusInternalServerError, application.CodeTurnFailed, "run id could not be generated")
		return
	}
	runID := "arun_" + hex.EncodeToString(entropy)
	threadID := turn.ThreadID
	if threadID == "" {
		// A stateless turn has no thread; the protocol still requires the field.
		threadID = runID
	}
	ctx, cancel := context.WithTimeout(request.Context(), streamLimit)
	defer cancel()
	control := http.NewResponseController(writer)
	_ = control.SetWriteDeadline(time.Now().Add(streamLimit))
	out := &stream{writer: writer, control: control, threadID: threadID, runID: runID, sequence: agui.NewSequence(threadID, runID)}

	stopped := make(chan struct{})
	var beat sync.WaitGroup
	beat.Add(1)
	go func() {
		defer beat.Done()
		ticker := time.NewTicker(keepAlive)
		defer ticker.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				out.mu.Lock()
				if out.started {
					if _, err := writer.Write([]byte(": keep-alive\n\n")); err != nil {
						cancel()
					} else if err := control.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
						cancel()
					}
				}
				out.mu.Unlock()
			}
		}
	}()
	answered, err := tools.Converse(ctx, caller, turn, out)
	close(stopped)
	beat.Wait()

	if err == nil {
		if out.begun() {
			_ = out.finish(answered)
			return
		}
		writeJSON(writer, http.StatusOK, answered)
		return
	}
	if !out.begun() {
		writeTurnError(writer, answered, err)
		return
	}
	// Results that ran were already sent as TOOL_CALL_RESULT events.
	_ = out.send(map[string]any{"type": "RUN_ERROR", "code": application.ErrorCode(err), "message": err.Error()})
}
