package agentstream

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jonathanagustin/conduit/api/agui"
	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

type plannerFunction func(context.Context, *domain.Session, string) (*application.TurnResult, error)

func (f plannerFunction) Turn(ctx context.Context, s *domain.Session, message string) (*application.TurnResult, error) {
	return f(ctx, s, message)
}

type auditMemory struct {
	mu         sync.Mutex
	entries    []AuditRecord
	failAction string
}

func (a *auditMemory) Record(ctx context.Context, record AuditRecord) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if record.Action == a.failAction {
		return errors.New("audit unavailable")
	}
	a.entries = append(a.entries, record)
	return nil
}

type eventCollector []map[string]any

func (events *eventCollector) write(raw []byte) error {
	var event map[string]any
	if err := json.Unmarshal(raw, &event); err != nil {
		return err
	}
	*events = append(*events, event)
	return nil
}
func (events eventCollector) validate(t *testing.T, threadID, runID string) {
	t.Helper()
	sequence := agui.NewSequence(threadID, runID)
	for _, event := range events {
		raw, _ := json.Marshal(event)
		if err := sequence.Accept(raw); err != nil {
			t.Fatalf("invalid emitted stream: %v event=%v", err, event)
		}
	}
	if !sequence.Complete() {
		t.Fatal("stream has no terminal")
	}
}
func (events eventCollector) last() map[string]any { return events[len(events)-1] }

func makeRunService(t *testing.T, planner application.Planner) (*Service, *application.Service, *domain.Session, *auditMemory) {
	t.Helper()
	sessions := application.NewService(planner)
	session, err := sessions.Create(t.Context(), "https://github.com/org/app", "main")
	if err != nil {
		t.Fatal(err)
	}
	audit := &auditMemory{}
	service, err := NewService(sessions, audit)
	if err != nil {
		t.Fatal(err)
	}
	return service, sessions, session, audit
}
func inputFor(session *domain.Session, runID string) RunInput {
	return RunInput{ThreadID: session.ID, RunID: runID, ProtocolVersion: agui.Version, Messages: []map[string]any{{"id": "user-1", "role": "user", "content": "inspect"}}}
}

func TestRunInterruptResumesIntoExistingPlanningSession(t *testing.T) {
	calls := 0
	runner := plannerFunction(func(ctx context.Context, s *domain.Session, _ string) (*application.TurnResult, error) {
		threadID, runID := application.RunIdentifiers(ctx)
		if threadID != s.ID || runID == "" {
			t.Fatal("run correlation missing from planner context")
		}
		calls++
		if calls == 1 {
			return &application.TurnResult{Messages: []domain.Message{{Role: domain.RoleModel, Text: "Inspected the source."}}, NeedsInput: []domain.Question{{ID: "node_version", Name: "node_version", Type: domain.QuestionSelect, Required: true, Options: []string{"20", "22"}}}, ResolvedCommit: strings.Repeat("a", 40)}, nil
		}
		return &application.TurnResult{Plan: &domain.Plan{Text: "Build the reviewed source."}}, nil
	})
	service, sessions, session, _ := makeRunService(t, runner)
	first := eventCollector{}
	if err := service.Run(t.Context(), inputFor(session, "run-1"), first.write); err != nil {
		t.Fatal(err)
	}
	first.validate(t, session.ID, "run-1")
	outcome := first.last()["outcome"].(map[string]any)
	if outcome["type"] != "interrupt" {
		t.Fatalf("outcome=%v", outcome)
	}
	interrupt := outcome["interrupts"].([]any)[0].(map[string]any)
	input := inputFor(session, "run-2")
	input.Resume = []ResumeEntry{{InterruptID: interrupt["id"].(string), Status: "resolved", Payload: json.RawMessage(`{"question:node_version":"20"}`)}}
	second := eventCollector{}
	if err := service.Run(t.Context(), input, second.write); err != nil {
		t.Fatal(err)
	}
	second.validate(t, session.ID, "run-2")
	got, err := sessions.Get(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || got.Status != domain.StatusReady || got.Plan == nil || got.Binding != nil || got.Answers[0].Value != "20" {
		t.Fatalf("wrong resumed state: calls=%d session=%+v", calls, got)
	}
	if second.last()["outcome"].(map[string]any)["type"] != "success" {
		t.Fatal(second.last())
	}
}

func TestSnapshotRecoveryDoesNotInvokeTheModelOrDuplicateMessages(t *testing.T) {
	calls := 0
	service, _, session, _ := makeRunService(t, plannerFunction(func(context.Context, *domain.Session, string) (*application.TurnResult, error) {
		calls++
		return &application.TurnResult{Messages: []domain.Message{{Role: domain.RoleModel, Text: "Actual model reply."}}}, nil
	}))
	first := eventCollector{}
	if err := service.Run(t.Context(), inputFor(session, "run-1"), first.write); err != nil {
		t.Fatal(err)
	}
	recovery := inputFor(session, "recovery-1")
	recovery.Messages = append(recovery.Messages, map[string]any{"id": "must-not-run", "role": "user", "content": "new text must not execute in recovery mode"})
	recovery.ForwardedProps = map[string]json.RawMessage{"conduit": json.RawMessage(`{"mode":"snapshot"}`)}
	events := eventCollector{}
	if err := service.Run(t.Context(), recovery, events.write); err != nil {
		t.Fatal(err)
	}
	events.validate(t, session.ID, "recovery-1")
	if calls != 1 {
		t.Fatal("recovery repeated the model turn")
	}
	for _, event := range events {
		if event["type"] == "MESSAGES_SNAPSHOT" {
			messages := event["messages"].([]any)
			if len(messages) != 2 {
				t.Fatalf("duplicated/lost history: %v", messages)
			}
			seen := map[string]bool{}
			for _, raw := range messages {
				id := raw.(map[string]any)["id"].(string)
				if seen[id] {
					t.Fatal("duplicate message ID")
				}
				seen[id] = true
			}
		}
	}
	if err := service.Run(t.Context(), recovery, events.write); !errors.Is(err, ErrRunExists) {
		t.Fatalf("re-entered a closed exchange: %v", err)
	}
}

func TestAuditFailureStopsBeforeModelAndProducesOneErrorTerminal(t *testing.T) {
	calls := 0
	service, _, session, audit := makeRunService(t, plannerFunction(func(context.Context, *domain.Session, string) (*application.TurnResult, error) {
		calls++
		return nil, nil
	}))
	audit.failAction = "agent.event.STATE_SNAPSHOT"
	events := eventCollector{}
	if err := service.Run(t.Context(), inputFor(session, "run-1"), events.write); !errors.Is(err, ErrAuditUnavailable) {
		t.Fatal(err)
	}
	events.validate(t, session.ID, "run-1")
	if calls != 0 || events.last()["type"] != "RUN_ERROR" {
		t.Fatalf("audit veto ignored: calls=%d events=%v", calls, events)
	}
	for _, event := range events {
		if event["type"] == "STATE_SNAPSHOT" || event["type"] == "RUN_FINISHED" {
			t.Fatal("unaudited state/success was published")
		}
	}
}

func TestToolWriterFailurePreventsToolExecution(t *testing.T) {
	executed := false
	runner := plannerFunction(func(ctx context.Context, _ *domain.Session, _ string) (*application.TurnResult, error) {
		id, err := application.ToolStarted(ctx, "inspect_repo", json.RawMessage(`{}`))
		if err != nil {
			return nil, err
		}
		executed = true
		if err := application.ToolFinished(ctx, id, true); err != nil {
			return nil, err
		}
		return nil, nil
	})
	service, _, session, _ := makeRunService(t, runner)
	disconnected := errors.New("stream disconnected")
	err := service.Run(t.Context(), inputFor(session, "run-1"), func(raw []byte) error {
		var event map[string]any
		_ = json.Unmarshal(raw, &event)
		if event["type"] == "TOOL_CALL_ARGS" {
			return disconnected
		}
		return nil
	})
	if !errors.Is(err, disconnected) || executed {
		t.Fatalf("tool executed after stream admission failed: %v executed=%v", err, executed)
	}
}

func TestUnknownThreadFailsBeforeAnyStream(t *testing.T) {
	service, _, session, _ := makeRunService(t, nil)
	input := inputFor(session, "run-1")
	input.ThreadID = "another-owners-session"
	emitted := false
	if err := service.Run(t.Context(), input, func([]byte) error { emitted = true; return nil }); !errors.Is(err, domain.ErrNotFound) || emitted {
		t.Fatalf("thread isolation failed: %v emitted=%v", err, emitted)
	}
}

func TestRecoveryRetainsOrderedToolAndTextHistory(t *testing.T) {
	runner := plannerFunction(func(ctx context.Context, _ *domain.Session, _ string) (*application.TurnResult, error) {
		id, err := application.ToolStarted(ctx, "inspect_repo", json.RawMessage(`{}`))
		if err != nil {
			return nil, err
		}
		if err := application.ToolFinished(ctx, id, true); err != nil {
			return nil, err
		}
		return &application.TurnResult{Messages: []domain.Message{{Role: domain.RoleModel, Text: "Inspection complete."}}}, nil
	})
	service, _, session, _ := makeRunService(t, runner)
	first := eventCollector{}
	if err := service.Run(t.Context(), inputFor(session, "run-1"), first.write); err != nil {
		t.Fatal(err)
	}
	first.validate(t, session.ID, "run-1")
	input := inputFor(session, "recover")
	input.ForwardedProps = map[string]json.RawMessage{"conduit": json.RawMessage(`{"mode":"snapshot"}`)}
	events := eventCollector{}
	if err := service.Run(t.Context(), input, events.write); err != nil {
		t.Fatal(err)
	}
	events.validate(t, session.ID, "recover")
	for _, event := range events {
		if event["type"] != "MESSAGES_SNAPSHOT" {
			continue
		}
		messages := event["messages"].([]any)
		if len(messages) != 4 {
			t.Fatalf("recovery lost conversation/tool history: %v", messages)
		}
		for index, role := range []string{"user", "assistant", "tool", "assistant"} {
			if messages[index].(map[string]any)["role"] != role {
				t.Fatalf("history order=%v", messages)
			}
		}
		if messages[0].(map[string]any)["id"] != "user-1" {
			t.Fatal("user message identity changed")
		}
	}
}

func TestInvalidResumeCannotPartiallyChangeAnswers(t *testing.T) {
	calls := 0
	service, sessions, session, _ := makeRunService(t, plannerFunction(func(context.Context, *domain.Session, string) (*application.TurnResult, error) {
		calls++
		return &application.TurnResult{NeedsInput: []domain.Question{{ID: "a", Type: domain.QuestionString, Required: true}, {ID: "b", Type: domain.QuestionString, Required: true}}}, nil
	}))
	first := eventCollector{}
	if err := service.Run(t.Context(), inputFor(session, "run-1"), first.write); err != nil {
		t.Fatal(err)
	}
	interrupt := first.last()["outcome"].(map[string]any)["interrupts"].([]any)[0].(map[string]any)["id"].(string)
	for index, entry := range []ResumeEntry{
		{InterruptID: interrupt, Status: "resolved", Payload: json.RawMessage(`{"question:a":"one"}`)},
		{InterruptID: "stale", Status: "resolved", Payload: json.RawMessage(`{"question:a":"one","question:b":"two"}`)},
		{InterruptID: interrupt, Status: "resolved", Payload: json.RawMessage(`{"question:a":"one","question:b":"two","unknown":"forged"}`)},
	} {
		input := inputFor(session, "bad-"+string(rune('a'+index)))
		input.Resume = []ResumeEntry{entry}
		events := eventCollector{}
		if err := service.Run(t.Context(), input, events.write); !errors.Is(err, ErrInvalidResume) || len(events) > 0 {
			t.Fatalf("invalid resume started a run: %v events=%v", err, events)
		}
	}
	got, err := sessions.Get(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(got.Answers) != 0 {
		t.Fatalf("invalid resume changed state: calls=%d answers=%v", calls, got.Answers)
	}
}

func TestUncoveredInputInterruptDoesNotAdvanceTheModel(t *testing.T) {
	calls := 0
	service, _, session, _ := makeRunService(t, plannerFunction(func(context.Context, *domain.Session, string) (*application.TurnResult, error) {
		calls++
		return &application.TurnResult{NeedsInput: []domain.Question{{ID: "a", Type: domain.QuestionString, Required: true}}}, nil
	}))
	first := eventCollector{}
	if err := service.Run(t.Context(), inputFor(session, "run-1"), first.write); err != nil {
		t.Fatal(err)
	}
	input := inputFor(session, "run-2")
	input.Messages = []map[string]any{{"id": "user-2", "role": "user", "content": "advance without answering"}}
	events := eventCollector{}
	if err := service.Run(t.Context(), input, events.write); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || events.last()["outcome"].(map[string]any)["type"] != "interrupt" {
		t.Fatal("unanswered required input was bypassed")
	}
}

func TestFrontendDraftToolHandoffThenHumanResume(t *testing.T) {
	calls := 0
	runner := plannerFunction(func(ctx context.Context, _ *domain.Session, _ string) (*application.TurnResult, error) {
		calls++
		if calls == 1 {
			questions := []domain.Question{{ID: "node", Name: "node", Label: "Node version", Type: domain.QuestionSelect, Required: true, Options: []string{"20", "22"}}}
			if err := application.QuestionsObserved(ctx, questions); err != nil {
				return nil, err
			}
			if err := application.FrontendToolCalled(ctx, "fill_field", json.RawMessage(`{"field":"question:node","value":"20"}`)); err != nil {
				return nil, err
			}
			return &application.TurnResult{NeedsInput: questions, ResolvedCommit: strings.Repeat("a", 40)}, nil
		}
		return &application.TurnResult{Plan: &domain.Plan{Text: "Reviewed plan after human answers."}}, nil
	})
	service, sessions, session, audit := makeRunService(t, runner)
	firstInput := inputFor(session, "run-1")
	firstInput.Tools = offers(t, "fill_field")
	first := eventCollector{}
	if err := service.Run(t.Context(), firstInput, first.write); err != nil {
		t.Fatal(err)
	}
	first.validate(t, session.ID, "run-1")
	outcome := first.last()["outcome"].(map[string]any)
	pending := outcome["pendingToolCallIds"].([]any)
	if outcome["type"] != "success" || len(pending) != 1 {
		t.Fatalf("bad frontend handoff: %v", outcome)
	}
	callID := pending[0].(string)
	ackInput := inputFor(session, "run-2")
	ackInput.Tools = offers(t, "fill_field")
	ackInput.Messages = append(ackInput.Messages, map[string]any{"id": "client-result", "role": "tool", "toolCallId": callID, "content": `{"ok":true}`})
	second := eventCollector{}
	if err := service.Run(t.Context(), ackInput, second.write); err != nil {
		t.Fatal(err)
	}
	second.validate(t, session.ID, "run-2")
	if calls != 1 {
		t.Fatal("client prefill result advanced model before human answers")
	}
	got, _ := sessions.Get(t.Context(), session.ID)
	if len(got.Answers) > 0 || got.Binding != nil {
		t.Fatal("draft feedback became an answer/acceptance")
	}
	control := second.last()["outcome"].(map[string]any)
	if control["type"] != "interrupt" {
		t.Fatal("human input was not requested after prefill")
	}
	interruptID := control["interrupts"].([]any)[0].(map[string]any)["id"].(string)
	resumed := inputFor(session, "run-3")
	resumed.Tools = offers(t, "fill_field")
	resumed.Messages = ackInput.Messages
	resumed.Resume = []ResumeEntry{{InterruptID: interruptID, Status: "resolved", Payload: json.RawMessage(`{"question:node":"22"}`)}}
	final := eventCollector{}
	if err := service.Run(t.Context(), resumed, final.write); err != nil {
		t.Fatal(err)
	}
	got, _ = sessions.Get(t.Context(), session.ID)
	if calls != 2 || got.Answers[0].Value != "22" || got.Plan == nil || got.Binding != nil {
		t.Fatalf("human override was not authoritative: %+v calls=%d", got, calls)
	}
	recorded := false
	for _, event := range audit.entries {
		if event.Action == "agent.tool_result" && event.RunID == "run-2" {
			recorded = true
		}
	}
	if !recorded {
		t.Fatal("client result lost run/thread audit correlation")
	}
}

func TestModelCannotUseAnUnofferedFrontendTool(t *testing.T) {
	service, sessions, session, _ := makeRunService(t, plannerFunction(func(ctx context.Context, _ *domain.Session, _ string) (*application.TurnResult, error) {
		return nil, application.FrontendToolCalled(ctx, "request_confirmation", json.RawMessage(`{"message":"Approve?"}`))
	}))
	events := eventCollector{}
	if err := service.Run(t.Context(), inputFor(session, "run-1"), events.write); !errors.Is(err, ErrToolNotOffered) {
		t.Fatal(err)
	}
	events.validate(t, session.ID, "run-1")
	if events.last()["type"] != "RUN_ERROR" {
		t.Fatal(events.last())
	}
	for _, event := range events {
		if event["type"] == "TOOL_CALL_START" {
			t.Fatal("unoffered frontend call reached the client")
		}
	}
	got, _ := sessions.Get(t.Context(), session.ID)
	if got.Plan != nil || got.Binding != nil {
		t.Fatal("unoffered call changed planning state")
	}
}

func TestDeclinedClientConfirmationDoesNotContinuePlanning(t *testing.T) {
	calls := 0
	service, sessions, session, _ := makeRunService(t, plannerFunction(func(ctx context.Context, _ *domain.Session, _ string) (*application.TurnResult, error) {
		calls++
		if err := application.FrontendToolCalled(ctx, "request_confirmation", json.RawMessage(`{"message":"Continue planning?"}`)); err != nil {
			return nil, err
		}
		return &application.TurnResult{}, nil
	}))
	firstInput := inputFor(session, "run-1")
	firstInput.Tools = offers(t, "request_confirmation")
	first := eventCollector{}
	if err := service.Run(t.Context(), firstInput, first.write); err != nil {
		t.Fatal(err)
	}
	callID := first.last()["outcome"].(map[string]any)["pendingToolCallIds"].([]any)[0].(string)
	next := inputFor(session, "run-2")
	next.Tools = firstInput.Tools
	next.Messages = append(next.Messages, map[string]any{"id": "declined", "role": "tool", "toolCallId": callID, "content": `{"ok":true,"confirmed":false}`})
	events := eventCollector{}
	if err := service.Run(t.Context(), next, events.write); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || events.last()["outcome"].(map[string]any)["type"] != "cancelled" {
		t.Fatal("declined confirmation advanced the model")
	}
	got, _ := sessions.Get(t.Context(), session.ID)
	if got.Plan != nil || got.Binding != nil {
		t.Fatal("client confirmation granted authority")
	}
}
