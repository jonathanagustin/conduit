package agentstream

import (
	"context"
	"encoding/json"
	"maps"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/jonathanagustin/conduit/api/agui"
	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

// Sessions is the existing owner-scoped application port. It deliberately has
// no Accept or Compute method: form feedback cannot grant either operation.
type Sessions interface {
	Get(context.Context, string) (*domain.Session, error)
	Turn(context.Context, string, string) (*domain.Session, error)
	Answer(context.Context, string, []domain.Answer) (*domain.Session, error)
}

type thread struct {
	surfaceID     string
	surfaceFormID string
	active        chan struct{}
	seenUsers     map[string]string
	toolCalls     []map[string]any
	history       []map[string]any
	nativeCursor  int
	historyIDs    map[string]bool
	pending       map[string]pendingTool
	acknowledged  map[string]string
}

// Service belongs to one authenticated owner's planning-session store.
// It does not resume broken SSE exchanges; recovery is a new snapshot-only run.
type Service struct {
	sessions Sessions
	audit    Auditor
	mu       sync.Mutex
	runs     map[string]bool
	threads  map[string]*thread
}

// NewService requires both the session boundary and its audit persistence port.
func NewService(sessions Sessions, audit Auditor) (*Service, error) {
	if sessions == nil || audit == nil {
		return nil, ErrAuditUnavailable
	}
	return &Service{sessions: sessions, audit: audit, runs: map[string]bool{}, threads: map[string]*thread{}}, nil
}

// Run writes one exchange. The HTTP adapter authenticates and selects this
// owner-scoped service before decoding the request. Errors before RUN_STARTED
// are HTTP failures; errors afterward are also reported by a terminal event.
func (service *Service) Run(ctx context.Context, input RunInput, write func([]byte) error) (runErr error) {
	if write == nil {
		return ErrInvalidInput
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return ErrInvalidInput
	}
	input, err = DecodeRunInput(raw)
	if err != nil {
		return err
	}
	session, err := service.sessions.Get(ctx, input.ThreadID)
	if err != nil {
		return err
	}
	policy, err := NewToolPolicy(input.Tools, fieldsFor(session))
	if err != nil {
		return err
	}
	snapshot := input.snapshotOnly()
	if snapshot && len(input.Resume) > 0 {
		return ErrInvalidInput
	}
	form := currentForm(session)
	var answers []domain.Answer
	cancelled := false
	if len(input.Resume) > 0 {
		if form == nil {
			return ErrInvalidResume
		}
		answers, cancelled, err = form.answers(input.Resume)
		if err != nil {
			return err
		}
	}
	userID, message := input.lastUserText()
	if len(message) > 8192 || (userID != "" && !identifier.MatchString(userID)) {
		return ErrInvalidInput
	}
	service.mu.Lock()
	if service.runs[input.RunID] {
		service.mu.Unlock()
		return ErrRunExists
	}
	if len(service.runs) >= 1024 {
		service.mu.Unlock()
		return ErrRunCapacity
	}
	state := service.threads[input.ThreadID]
	if state == nil {
		if len(service.threads) >= 128 {
			service.mu.Unlock()
			return ErrRunCapacity
		}
		state = &thread{seenUsers: map[string]string{}, historyIDs: map[string]bool{}, pending: map[string]pendingTool{}, acknowledged: map[string]string{}}
		service.threads[input.ThreadID] = state
	}
	previous := state.active
	if previous != nil && !snapshot {
		service.mu.Unlock()
		return ErrRunBusy
	}
	if old, seen := state.seenUsers[userID]; userID != "" && seen {
		if old != digest([]byte(message)) {
			service.mu.Unlock()
			return ErrInvalidInput
		}
		message = ""
	}
	service.runs[input.RunID] = true
	var done chan struct{}
	if !snapshot {
		done = make(chan struct{})
		state.active = done
	}
	service.mu.Unlock()
	defer func() {
		if done != nil {
			service.mu.Lock()
			state.active = nil
			close(done)
			service.mu.Unlock()
		}
	}()
	if snapshot && previous != nil {
		select {
		case <-previous:
		case <-ctx.Done():
			return ctx.Err()
		}
		session, err = service.sessions.Get(ctx, input.ThreadID)
		if err != nil {
			return err
		}
	}
	if err := service.validateFormAction(input, state, form); err != nil {
		return err
	}
	if err := service.audit.Record(ctx, AuditRecord{ThreadID: input.ThreadID, RunID: input.RunID, Action: "agent.input", PayloadSHA256: digest(raw)}); err != nil {
		return ErrAuditUnavailable
	}
	service.mu.Lock()
	service.syncNativeLocked(state, session, -1, "")
	service.mu.Unlock()
	output := &publisher{threadID: input.ThreadID, runID: input.RunID, sequence: agui.NewSequence(input.ThreadID, input.RunID), audit: service.audit, write: write, trackActivity: !snapshot}
	if err := output.send(ctx, map[string]any{"type": "RUN_STARTED", "threadId": input.ThreadID, "runId": input.RunID, "protocolVersion": agui.Version}); err != nil {
		return err
	}
	defer func() {
		if runErr != nil {
			output.fail(ctx, runErr)
		}
	}()
	observer := &runObserver{service: service, state: state, input: input, policy: policy, output: output, calls: map[string]string{}, session: session}
	defer func() {
		if runErr != nil {
			observer.interrupted()
		}
	}()
	feedback, err := service.clientResults(ctx, state, input)
	if err != nil {
		return err
	}
	ctx = application.WithRunScope(ctx, application.RunScope{ThreadID: input.ThreadID, RunID: input.RunID, Tools: observer, Frontend: observer, Questions: observer, Grounding: input.grounding(fieldsFor(session))})
	if err := service.snapshot(ctx, output, state, session); err != nil {
		return err
	}
	if cancelled || feedback.Declined {
		return output.send(ctx, map[string]any{"type": "RUN_FINISHED", "threadId": input.ThreadID, "runId": input.RunID, "outcome": map[string]any{"type": "cancelled"}})
	}
	if !snapshot {
		// An empty opening message is still the intake. The console sends the
		// repository and ref, then a run with no text. Skipping that run left
		// the session at started, so Accept never appeared.
		opening := message == "" && len(answers) == 0 && session.Plan == nil &&
			session.Status == domain.StatusStarted && strings.TrimSpace(session.Intake.RepoURL) != ""
		if len(answers) > 0 {
			if action := inputAction(input); len(action) > 0 {
				if err := service.audit.Record(ctx, AuditRecord{ThreadID: input.ThreadID, RunID: input.RunID, Action: "agent.a2ui_action", PayloadSHA256: digest(action)}); err != nil {
					return ErrAuditUnavailable
				}
			}
			payload, _ := json.Marshal(input.Resume)
			if err := service.audit.Record(ctx, AuditRecord{ThreadID: input.ThreadID, RunID: input.RunID, Action: "agent.resume", PayloadSHA256: digest(payload)}); err != nil {
				return ErrAuditUnavailable
			}
			session, err = service.sessions.Answer(ctx, input.ThreadID, answers)
			if err != nil {
				return err
			}
			observer.policy, err = NewToolPolicy(input.Tools, fieldsFor(session))
			if err != nil {
				return err
			}
			observer.session = session
			ctx = application.WithRunScope(ctx, application.RunScope{ThreadID: input.ThreadID, RunID: input.RunID, Tools: observer, Frontend: observer, Questions: observer, Grounding: input.grounding(fieldsFor(session))})
		}
		if (message != "" || len(answers) > 0 || opening || (feedback.Changed && session.Plan == nil)) && (form == nil || len(input.Resume) > 0) {
			before := len(session.Messages)
			if message != "" {
				if userID == "" || strings.HasPrefix(userID, "conduit:") {
					return ErrInvalidInput
				}
				service.mu.Lock()
				if !state.historyIDs[userID] {
					service.appendHistoryLocked(state, map[string]any{"id": userID, "role": "user", "content": message})
				}
				service.mu.Unlock()
			}
			session, err = service.sessions.Turn(ctx, input.ThreadID, message)
			if err != nil {
				return err
			}
			service.mu.Lock()
			service.syncNativeLocked(state, session, before, userID)
			if userID != "" && message != "" {
				state.seenUsers[userID] = digest([]byte(message))
			}
			service.mu.Unlock()
			for index := before; index < len(session.Messages); index++ {
				item := session.Messages[index]
				if item.Role != domain.RoleModel {
					continue
				}
				id := messageID(session.ID, index)
				if err := output.send(ctx, map[string]any{"type": "TEXT_MESSAGE_START", "messageId": id, "role": "assistant"}); err != nil {
					return err
				}
				if item.Text != "" {
					if err := output.send(ctx, map[string]any{"type": "TEXT_MESSAGE_CONTENT", "messageId": id, "delta": item.Text}); err != nil {
						return err
					}
				}
				if err := output.send(ctx, map[string]any{"type": "TEXT_MESSAGE_END", "messageId": id}); err != nil {
					return err
				}
			}
		}
	}
	service.mu.Lock()
	for _, tool := range observer.frontend {
		state.pending[tool.ID] = tool
	}
	service.mu.Unlock()
	if err := service.snapshot(ctx, output, state, session); err != nil {
		return err
	}
	outcome := map[string]any{"type": "success"}
	if form := currentForm(session); form != nil {
		outcome = map[string]any{"type": "interrupt", "interrupts": []any{map[string]any{"id": form.id, "reason": "needs_input", "message": "Complete the required planning inputs.", "responseSchema": form.schema}}}
	}
	if len(observer.frontend) > 0 {
		ids := []string{}
		for _, tool := range observer.frontend {
			ids = append(ids, tool.ID)
		}
		outcome = map[string]any{"type": "success", "pendingToolCallIds": ids}
	}
	if err := output.send(ctx, map[string]any{"type": "RUN_FINISHED", "threadId": input.ThreadID, "runId": input.RunID, "outcome": outcome}); err != nil {
		return err
	}
	service.mu.Lock()
	for _, tool := range observer.frontend {
		pending := state.pending[tool.ID]
		pending.Eligible = true
		state.pending[tool.ID] = pending
	}
	service.mu.Unlock()
	return nil
}

func messageID(threadID string, index int) string {
	return "conduit:" + threadID + ":message:" + strconv.Itoa(index)
}

func (service *Service) snapshot(ctx context.Context, output *publisher, state *thread, session *domain.Session) error {
	view := domain.CloneSession(session)
	view.Events = []domain.Event{}
	service.mu.Lock()
	tools := []map[string]any{}
	for _, call := range state.toolCalls {
		tools = append(tools, maps.Clone(call))
	}
	pending := []pendingTool{}
	for _, tool := range state.pending {
		pending = append(pending, tool)
	}
	service.mu.Unlock()
	sort.Slice(pending, func(i, j int) bool { return pending[i].Order < pending[j].Order })
	if err := output.stateUpdate(ctx, map[string]any{"valueTier": "CONDUIT_NATIVE", "session": view, "toolCalls": tools, "pendingClientTools": pending}); err != nil {
		return err
	}
	if err := service.formSnapshot(ctx, output, state, session); err != nil {
		return err
	}
	service.mu.Lock()
	service.syncNativeLocked(state, session, -1, "")
	messages := []any{}
	for _, message := range state.history {
		messages = append(messages, maps.Clone(message))
	}
	service.mu.Unlock()
	return output.send(ctx, map[string]any{"type": "MESSAGES_SNAPSHOT", "messages": messages})
}

var _ application.ToolObserver = (*runObserver)(nil)

func (service *Service) appendHistoryLocked(state *thread, message map[string]any) {
	id := message["id"].(string)
	if state.historyIDs[id] {
		return
	}
	state.historyIDs[id] = true
	state.history = append(state.history, message)
}

func (service *Service) syncNativeLocked(state *thread, session *domain.Session, userIndex int, userID string) {
	for index := state.nativeCursor; index < len(session.Messages); index++ {
		item := session.Messages[index]
		role := "assistant"
		id := messageID(session.ID, index)
		if item.Role == domain.RoleOperator {
			role = "user"
			if index == userIndex && userID != "" {
				id = userID
			}
		}
		if item.Role == domain.RoleOperator {
			state.seenUsers[id] = digest([]byte(item.Text))
		}
		service.appendHistoryLocked(state, map[string]any{"id": id, "role": role, "content": item.Text})
	}
	state.nativeCursor = len(session.Messages)
}
