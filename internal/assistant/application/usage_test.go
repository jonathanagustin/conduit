package application

import (
	"context"
	"errors"
	"testing"

	"github.com/jonathanagustin/conduit/internal/assistant/adapters/threadmemory"
	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

type usageReportingModel struct {
	calls int
	usage []*domain.ReportedTokenUsage
	fail  bool
}

func (model *usageReportingModel) Complete(context.Context, string) (string, domain.Provenance, error) {
	index := model.calls
	model.calls++
	provenance := domain.Provenance{Adapter: "fixture", ProviderModel: "fixture-model", Usage: model.usage[index]}
	if index == 0 {
		return calls("looking", queryCall("Environment")), provenance, nil
	}
	if model.fail {
		return "", provenance, domain.ErrModelFailed
	}
	return `{"text":"done"}`, provenance, nil
}
func (*usageReportingModel) ReportedModel() string { return "fixture-model" }

func TestTurnReportsAndRemembersAllRoundsIncludingFailure(t *testing.T) {
	count := func(n int64) *int64 { return &n }
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "answer", true: "failure"}[fail], func(t *testing.T) {
			model := &usageReportingModel{usage: []*domain.ReportedTokenUsage{{PromptTokens: count(2), CompletionTokens: count(0)}, {PromptTokens: count(3), CompletionTokens: nil}}, fail: fail}
			service := Service{Model: model, Objects: environments(), Audit: &memoryAudit{}, Now: fixedClock(), Threads: threadmemory.New()}
			thread, err := service.CreateThread(t.Context(), operator(), "usage")
			if err != nil {
				t.Fatal(err)
			}
			turn, err := service.Converse(t.Context(), operator(), TurnRequest{Mode: domain.ModeExplore, Message: "look", ThreadID: thread.ID}, nil)
			if (err != nil) != fail || (fail && !errors.Is(err, domain.ErrModelFailed)) {
				t.Fatalf("turn failure=%v", err)
			}
			if turn.Rounds != 2 || turn.Usage == nil || turn.Usage.PromptTokens == nil || *turn.Usage.PromptTokens != 5 || turn.Usage.CompletionTokens != nil {
				t.Fatalf("wrong turn totals: rounds=%d usage=%+v", turn.Rounds, turn.Usage)
			}
			stored, err := service.GetThread(t.Context(), operator(), thread.ID)
			if err != nil {
				t.Fatal(err)
			}
			usage := stored.Messages[1].Usage
			if usage == nil || usage.PromptTokens == nil || *usage.PromptTokens != 5 || usage.CompletionTokens != nil {
				t.Fatalf("stored usage=%+v", usage)
			}
			*turn.Usage.PromptTokens = 900
			again, err := service.GetThread(t.Context(), operator(), thread.ID)
			if err != nil || *again.Messages[1].Usage.PromptTokens != 5 {
				t.Fatalf("turn mutation changed stored usage: %+v %v", again, err)
			}
		})
	}
}
