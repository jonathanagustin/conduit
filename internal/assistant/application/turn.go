package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

// TurnRequest is one operator message. An empty ThreadID is a stateless turn:
// nothing is read from or written to the conversation store.
type TurnRequest struct {
	Mode     string
	Message  string
	Chips    []domain.Chip
	ThreadID string
}

// Observer hears a turn as it happens. Every event is something that already
// occurred: a model call about to start, a tool call about to run, a tool call
// that returned. An error from the observer stops the turn.
type Observer interface {
	RoundStarted(ctx context.Context, round int) error
	ToolStarted(ctx context.Context, call ToolCall) error
	ToolFinished(ctx context.Context, call ToolCall, result Result) error
}

// Turn answers one stateless message.
func (service Service) Turn(ctx context.Context, actor Actor, mode, message string, chips []domain.Chip) (Turn, error) {
	return service.Converse(ctx, actor, TurnRequest{Mode: mode, Message: message, Chips: chips}, nil)
}

// Converse answers one message. It calls the model, runs the tools the reply
// asks for, and feeds their results back until the model answers in text or
// the round limit is reached. With a ThreadID the earlier turns are read into
// the prompt and this turn is appended, whether or not it finished.
//
// A failed turn still returns what ran: tool calls have audit entries and may
// have opened change requests, and the caller has to be able to show them.
func (service Service) Converse(ctx context.Context, actor Actor, request TurnRequest, observer Observer) (Turn, error) {
	if service.Model == nil {
		return Turn{Label: domain.LabelNoModel}, domain.ErrModelUnavailable
	}
	var history []domain.Message
	if request.ThreadID != "" {
		if len(request.Message) > domain.MaxMessageTextBytes {
			return Turn{Label: domain.LabelNoModel}, fmt.Errorf("%w: message is over %d bytes", domain.ErrThreadInvalid, domain.MaxMessageTextBytes)
		}
		thread, err := service.GetThread(ctx, actor, request.ThreadID)
		if err != nil {
			return Turn{Label: domain.LabelNoModel}, err
		}
		if thread.MessageCount+2 > domain.MaxMessagesPerThread {
			return Turn{Label: domain.LabelNoModel}, domain.ErrThreadFull
		}
		history = domain.Window(thread.Messages, domain.HistoryMaxMessages, domain.HistoryMaxBytes)
	}
	asked := service.stamp()
	turn, err := service.rounds(ctx, actor, request, history, observer)
	turn.ThreadID = request.ThreadID
	if request.ThreadID == "" {
		return turn, err
	}
	if stored := service.remember(ctx, actor, request, turn, err, asked); stored != nil && err == nil {
		return turn, stored
	}
	return turn, err
}

func (service Service) rounds(ctx context.Context, actor Actor, request TurnRequest, history []domain.Message, observer Observer) (Turn, error) {
	turn := Turn{Label: domain.LabelLiveModel}
	conversation := domain.Conversation{Mode: request.Mode, Message: request.Message, Chips: request.Chips, History: history}
	executed := 0
	for round := 1; ; round++ {
		if err := ctx.Err(); err != nil {
			return turn, err
		}
		if observer != nil {
			if err := observer.RoundStarted(ctx, round); err != nil {
				return turn, err
			}
		}
		conversation.Final = round == domain.MaxRounds
		prompt := domain.ConversationPrompt(conversation)
		raw, provenance, err := service.Model.Complete(ctx, prompt)
		turn.Usage = domain.IncludeReportedUsage(turn.Usage, provenance.Usage)
		turn.Rounds = round
		turn.Provenance = provenance
		turn.Model = served(provenance)
		if err != nil {
			return turn, err
		}
		reply, err := domain.ParseReply([]byte(raw))
		if err != nil {
			if _, auditErr := service.audit(ctx, actor.Username, "model_reply", []byte(prompt), []byte(raw), "DENY", err.Error(), "assistant/turn"); auditErr != nil {
				return turn, auditErr
			}
			return turn, err
		}
		if reply.Text != "" {
			turn.Text = reply.Text
		}
		if len(reply.ToolCalls) == 0 {
			return turn, nil
		}
		if conversation.Final {
			// Nothing would read these results, so the calls are not run.
			return turn, domain.ErrRoundCap
		}
		// A call is held once the model has read anything but the operator's
		// own message: earlier turns, or a tool result from a round before.
		held := round > 1 || len(history) > 0
		exchange := domain.Exchange{Reply: reply}
		for index, requested := range reply.ToolCalls {
			if executed >= domain.MaxToolCallsPerTurn {
				exchange.Skipped++
				continue
			}
			executed++
			call := ToolCall{
				ID: fmt.Sprintf("call_%d_%d", round, index+1), Round: round,
				Name: strings.TrimSpace(requested.Name), Arguments: executedArguments(requested, held),
			}
			if observer != nil {
				if err := observer.ToolStarted(ctx, call); err != nil {
					return turn, err
				}
			}
			result, err := service.invoke(ctx, actor, request.Mode, call.Name, call.Arguments, held)
			if err != nil {
				return turn, err
			}
			result.Label = domain.LabelLiveModel
			turn.ToolCalls = append(turn.ToolCalls, call)
			turn.Results = append(turn.Results, result)
			if observer != nil {
				if err := observer.ToolFinished(ctx, call, result); err != nil {
					return turn, err
				}
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				return turn, err
			}
			exchange.Records = append(exchange.Records, domain.ToolRecord{
				ID: call.ID, Round: round, Name: call.Name, Arguments: call.Arguments, Result: encoded,
			})
		}
		conversation.Rounds = append(conversation.Rounds, exchange)
	}
}

// executedArguments is what the server will run for a model's call: the
// model's arguments, minus a confirm flag on a held write. It is what the
// caller is shown, so the card never claims a confirmation nobody gave.
func executedArguments(requested domain.RawToolCall, held bool) json.RawMessage {
	arguments := requested.Arguments
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	if !held || strings.TrimSpace(requested.Name) != domain.ToolWrite {
		return arguments
	}
	call, err := domain.ParseToolCall(requested.Name, arguments)
	if err != nil || !call.Write.Confirm {
		return arguments
	}
	call.Write.Confirm = false
	stripped, err := json.Marshal(call.Write)
	if err != nil {
		return arguments
	}
	return stripped
}

func served(provenance domain.Provenance) *Served {
	if provenance.Adapter == "" {
		return nil
	}
	out := &Served{Kind: provenance.Adapter, Model: provenance.ProviderModel, ConfiguredModel: provenance.ConfiguredModel, ProviderModel: provenance.ProviderModel}
	if out.Model == "" {
		out.Model = provenance.ConfiguredModel
	}
	return out
}

// remember appends the turn to its thread. It runs on a context that outlives
// a cancelled request: the tools already ran, and a reload has to show that.
func (service Service) remember(ctx context.Context, actor Actor, request TurnRequest, turn Turn, failure error, asked time.Time) error {
	if service.Threads == nil {
		return domain.ErrThreadsUnavailable
	}
	answer := domain.Message{Role: domain.RoleAssistant, Text: turn.Text, Mode: request.Mode, CreatedAt: service.stamp(), Usage: turn.Usage.Clone()}
	if failure != nil {
		answer.ErrorCode = ErrorCode(failure)
	}
	for index, call := range turn.ToolCalls {
		record := domain.ToolRecord{ID: call.ID, Round: call.Round, Name: call.Name, Arguments: call.Arguments}
		if index < len(turn.Results) {
			if encoded, err := json.Marshal(turn.Results[index]); err == nil {
				record.Result = encoded
			}
		}
		answer.ToolCalls = append(answer.ToolCalls, record)
	}
	messages := []domain.Message{
		{Role: domain.RoleUser, Text: request.Message, Mode: request.Mode, CreatedAt: asked},
		answer.Bounded(),
	}
	storeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return service.Threads.Append(storeCtx, strings.TrimSpace(actor.Username), request.ThreadID, messages, domain.ThreadTitle(request.Message))
}

// Stable error codes. They are the contract a client branches on; the message
// beside one is for a person.
const (
	CodeModelNotConfigured = "MODEL_NOT_CONFIGURED"
	CodeModelTimeout       = "MODEL_TIMEOUT"
	CodeModelFailed        = "MODEL_FAILED"
	CodeReplyInvalid       = "MODEL_REPLY_INVALID"
	CodeRoundCap           = "ROUND_CAP"
	CodeAuditUnavailable   = "AUDIT_UNAVAILABLE"
	// CodeModelBusy and CodeModelBudgetExceeded are 429s: the call was
	// refused by a limit before it reached a model.
	CodeModelBusy           = "MODEL_BUSY"
	CodeModelBudgetExceeded = "MODEL_BUDGET_EXCEEDED"
	CodeCancelled           = "CANCELLED"
	CodeToolInvalid         = "TOOL_INVALID"
	CodeThreadNotFound      = "THREAD_NOT_FOUND"
	CodeThreadLimit         = "THREAD_LIMIT"
	CodeThreadFull          = "THREAD_FULL"
	CodeThreadInvalid       = "INVALID_REQUEST"
	CodeThreadsUnavailable  = "THREADS_NOT_CONFIGURED"
	CodeTurnFailed          = "TURN_FAILED"
)

// ErrorCode names a failure. Cancellation is checked first: an adapter
// reports a cancelled call as its own failure.
func ErrorCode(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return CodeCancelled
	case errors.Is(err, domain.ErrModelBusy):
		return CodeModelBusy
	case errors.Is(err, domain.ErrModelBudgetExceeded):
		return CodeModelBudgetExceeded
	case errors.Is(err, domain.ErrAuditUnavailable):
		return CodeAuditUnavailable
	case errors.Is(err, domain.ErrModelUnavailable):
		return CodeModelNotConfigured
	case errors.Is(err, domain.ErrModelTimeout), errors.Is(err, context.DeadlineExceeded):
		return CodeModelTimeout
	case errors.Is(err, domain.ErrModelFailed):
		return CodeModelFailed
	case errors.Is(err, domain.ErrRoundCap):
		return CodeRoundCap
	case errors.Is(err, domain.ErrAuditUnavailable):
		return CodeAuditUnavailable
	case errors.Is(err, domain.ErrReplyInvalid):
		return CodeReplyInvalid
	case errors.Is(err, domain.ErrToolInvalid):
		return CodeToolInvalid
	case errors.Is(err, domain.ErrThreadNotFound):
		return CodeThreadNotFound
	case errors.Is(err, domain.ErrThreadLimit):
		return CodeThreadLimit
	case errors.Is(err, domain.ErrThreadFull):
		return CodeThreadFull
	case errors.Is(err, domain.ErrThreadInvalid):
		return CodeThreadInvalid
	case errors.Is(err, domain.ErrThreadsUnavailable):
		return CodeThreadsUnavailable
	default:
		return CodeTurnFailed
	}
}

// stamp is the stored time: UTC at the microsecond a database keeps.
func (service Service) stamp() time.Time { return service.now().UTC().Truncate(time.Microsecond) }
