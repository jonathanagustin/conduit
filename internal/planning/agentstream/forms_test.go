package agentstream

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

func formRunInput(t *testing.T, session *domain.Session, events eventCollector, id string) RunInput {
	t.Helper()
	input := inputFor(session, id)
	interrupt := events.last()["outcome"].(map[string]any)["interrupts"].([]any)[0].(map[string]any)
	surfaceID := ""
	for _, event := range events {
		if event["type"] != "CUSTOM" {
			continue
		}
		message := event["value"].(map[string]any)
		if created, ok := message["createSurface"].(map[string]any); ok {
			surfaceID = created["surfaceId"].(string)
		}
	}
	if surfaceID == "" {
		t.Fatal("no A2UI surface published")
	}
	input.Resume = []ResumeEntry{{InterruptID: interrupt["id"].(string), Status: "resolved", Payload: json.RawMessage(`{"question:port":"9090"}`)}}
	raw, _ := json.Marshal(map[string]any{"a2uiAction": map[string]any{"version": "1.0", "action": map[string]any{"surfaceId": surfaceID, "sourceComponentId": "submit", "name": "submit_needs_input", "timestamp": "2026-11-15T08:00:00Z", "context": map[string]any{"question:port": "9090"}}}})
	input.ForwardedProps = map[string]json.RawMessage{"conduit": raw}
	return input
}
func formRunner(calls *int) plannerFunction {
	return func(_ context.Context, session *domain.Session, _ string) (*application.TurnResult, error) {
		*calls++
		if len(session.Answers) == 0 {
			return &application.TurnResult{ResolvedCommit: strings.Repeat("a", 40), NeedsInput: []domain.Question{{ID: "port", Name: "Port", Type: domain.QuestionString, Required: true}}}, nil
		}
		return &application.TurnResult{Plan: &domain.Plan{Text: "Build the reviewed source."}}, nil
	}
}
func TestA2UIActionsBindCurrentSurfaceAndExactResumePayload(t *testing.T) {
	calls := 0
	service, sessions, session, audit := makeRunService(t, formRunner(&calls))
	first := eventCollector{}
	if err := service.Run(t.Context(), inputFor(session, "initial"), first.write); err != nil {
		t.Fatal(err)
	}
	for index, mutate := range []func(map[string]any){
		func(action map[string]any) { action["surfaceId"] = "foreign" },
		func(action map[string]any) { action["sourceComponentId"] = "accept" },
		func(action map[string]any) { action["name"] = "accept" },
		func(action map[string]any) { action["context"] = map[string]any{"question:port": "8080"} },
		func(action map[string]any) {
			action["context"] = map[string]any{"question:port": "9090", "unknown": "x"}
		},
		func(action map[string]any) { action["timestamp"] = "invalid" },
	} {
		input := formRunInput(t, session, first, fmt.Sprintf("bad-%d", index))
		var options map[string]any
		if err := json.Unmarshal(input.ForwardedProps["conduit"], &options); err != nil {
			t.Fatal(err)
		}
		mutate(options["a2uiAction"].(map[string]any)["action"].(map[string]any))
		input.ForwardedProps["conduit"], _ = json.Marshal(options)
		output := eventCollector{}
		if err := service.Run(t.Context(), input, output.write); err != ErrInvalidResume || len(output) != 0 {
			t.Fatalf("invalid action admitted: %v %v", err, output)
		}
	}
	current, err := sessions.Get(t.Context(), session.ID)
	if err != nil || len(current.Answers) != 0 || calls != 1 {
		t.Fatalf("rejected action changed answers: %+v calls=%d err=%v", current, calls, err)
	}
	output := eventCollector{}
	if err := service.Run(t.Context(), formRunInput(t, session, first, "reviewed"), output.write); err != nil {
		t.Fatal(err)
	}
	output.validate(t, session.ID, "reviewed")
	current, err = sessions.Get(t.Context(), session.ID)
	if err != nil || current.Plan == nil || current.Binding != nil || current.Answers[0].Value != "9090" || calls != 2 {
		t.Fatalf("reviewed action: %+v calls=%d err=%v", current, calls, err)
	}
	found := false
	for _, entry := range audit.entries {
		if entry.Action == "agent.a2ui_action" {
			found = true
			if entry.RunID != "reviewed" || entry.ThreadID != session.ID || len(entry.PayloadSHA256) != 64 {
				t.Fatal(entry)
			}
		}
	}
	if !found {
		t.Fatal("A2UI action was not audited")
	}
}
func TestA2UIActionAuditFailureCannotApplyAnswers(t *testing.T) {
	calls := 0
	service, sessions, session, audit := makeRunService(t, formRunner(&calls))
	first := eventCollector{}
	if err := service.Run(t.Context(), inputFor(session, "initial"), first.write); err != nil {
		t.Fatal(err)
	}
	audit.failAction = "agent.a2ui_action"
	output := eventCollector{}
	if err := service.Run(t.Context(), formRunInput(t, session, first, "audit-failed"), output.write); err != ErrAuditUnavailable {
		t.Fatal(err)
	}
	current, err := sessions.Get(t.Context(), session.ID)
	if err != nil || len(current.Answers) != 0 || current.Binding != nil || calls != 1 {
		t.Fatalf("audit outage applied answers: %+v %v", current, err)
	}
	if output.last()["type"] != "RUN_ERROR" {
		t.Fatal(output.last())
	}
}
